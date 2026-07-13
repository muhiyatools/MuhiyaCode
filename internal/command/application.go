package command

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/mcpclient"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

type ApplicationOptions struct {
	Context          context.Context
	Workspace        string
	SessionID        string
	NewSession       bool
	Title            string
	Callbacks        contract.Callbacks
	DisableMCP       bool
	MCPDeadline      time.Duration
	RawUsageObserver gateway.RawUsageObserver
}

// Application is the composition root for one CLI process. Packages below it
// remain independently testable; only this layer owns concrete lifetimes.
type Application struct {
	ctx        context.Context
	paths      state.Paths
	db         *state.DB
	sessions   *state.Sessions
	settings   *contract.Settings
	secrets    contract.Secrets
	provider   *gateway.OpenAICompatible
	probeStore *state.ProbeStore
	callbacks  contract.Callbacks
	disableMCP bool
	mcpWait    time.Duration

	session contract.Session // resolved by openApplicationCore, hydrated by Hydrate

	mu               sync.Mutex
	runtime          tui.Runtime
	recent           []contract.Event
	activeWorkspace  *workspace.Workspace
	activeCheckpoint *workspace.CheckpointStore
	activeMCP        *mcpclient.Manager
	activeRegistry   *orchestrator.Registry
}

type runtimeBundle struct {
	runtime    tui.Runtime
	recent     []contract.Event
	workspace  *workspace.Workspace
	checkpoint *workspace.CheckpointStore
	mcp        *mcpclient.Manager
	registry   *orchestrator.Registry
}

// openApplicationCore opens config, DB, probe store, provider, and resolves the
// session — the fast stage — without building the runtime. Hydrate() completes it.
func openApplicationCore(options ApplicationOptions) (*Application, error) {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	paths, err := state.EnsurePaths()
	if err != nil {
		return nil, err
	}
	settings, err := state.LoadSettings(paths)
	if err != nil {
		return nil, err
	}
	secrets, err := state.LoadSecrets(paths)
	if err != nil {
		return nil, err
	}
	db, err := state.Open(ctx, paths)
	if err != nil {
		return nil, err
	}
	store := &state.Sessions{DB: db, Secrets: secrets}
	probeStore, err := state.NewProbeStore(paths)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	app := &Application{
		ctx: ctx, paths: paths, db: db, sessions: store, settings: &settings,
		secrets: secrets, callbacks: options.Callbacks, disableMCP: options.DisableMCP,
		mcpWait: options.MCPDeadline, probeStore: probeStore,
	}
	if app.mcpWait <= 0 {
		app.mcpWait = 900 * time.Millisecond
	}
	app.provider = gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: secrets.ProviderAPIKey, RawUsageObserver: options.RawUsageObserver})
	if secrets.ProviderAPIKey != "" {
		active, ok := state.ActiveModel(settings)
		if !ok || active.ContextLimit <= 0 {
			discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			models, discoveryErr := app.provider.ListModels(discoveryCtx)
			cancel()
			if discoveryErr == nil && len(models) > 0 {
				addDiscoveredModels(app.settings, models)
				if saveErr := state.SaveSettings(*app.settings, paths); saveErr != nil {
					_ = db.Close()
					return nil, saveErr
				}
				app.provider.UpdateConfig(*app.settings, secrets.ProviderAPIKey)
			}
		}
	}

	var session contract.Session
	if options.SessionID != "" {
		var ok bool
		session, ok, err = db.Session(ctx, options.SessionID)
		if err == nil && !ok {
			err = fmt.Errorf("session not found: %s", options.SessionID)
		}
	} else {
		root := options.Workspace
		if root == "" {
			root, err = os.Getwd()
		}
		if err == nil {
			root, err = workspace.CanonicalPath(root)
		}
		if err == nil && options.NewSession {
			session, err = store.New(ctx, root, options.Title)
		} else if err == nil {
			session, err = store.GetOrCreate(ctx, root, options.Title)
		}
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	app.session = session
	return app, nil
}

// OpenApplication opens the app AND hydrates its runtime synchronously — the
// original, unchanged contract used by one-shot, resume, and tests. Interactive
// launch uses openApplicationCore + Hydrate to bring up the shell first (T021).
func OpenApplication(options ApplicationOptions) (*Application, error) {
	app, err := openApplicationCore(options)
	if err != nil {
		return nil, err
	}
	if err := app.Hydrate(app.ctx); err != nil {
		app.Close()
		return nil, err
	}
	return app, nil
}

// Hydrate builds and activates the session runtime (engine, workspace tools, MCP,
// registry). It is the expensive, network-touching stage; interactive launch runs
// it asynchronously after the composer is already on screen (T021).
func (a *Application) Hydrate(ctx context.Context) error {
	bundle, err := a.buildRuntime(ctx, a.session)
	if err != nil {
		return err
	}
	a.activate(bundle)
	return nil
}

func (a *Application) Runtime() tui.Runtime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runtime
}

func (a *Application) Recent() []contract.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]contract.Event(nil), a.recent...)
}

func (a *Application) Settings() *contract.Settings { return a.settings }
func (a *Application) Paths() state.Paths           { return a.paths }

func (a *Application) Close() error {
	a.mu.Lock()
	manager := a.activeMCP
	runtime := a.runtime
	a.activeMCP = nil
	a.mu.Unlock()
	var errs []error
	if runtime.Engine != nil {
		runtime.Engine.Cancel()
		waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := runtime.Engine.WaitIdle(waitCtx); err != nil {
			errs = append(errs, fmt.Errorf("wait for active task: %w", err))
		}
		cancel()
	}
	if manager != nil {
		if err := manager.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *Application) Actions() tui.Actions {
	return tui.Actions{
		SaveSettings: func(_ context.Context, settings *contract.Settings) error {
			if settings == nil {
				return errors.New("settings are nil")
			}
			if err := state.SaveSettings(*settings, a.paths); err != nil {
				return err
			}
			*a.settings = *settings
			a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
			return nil
		},
		SetModel: func(ctx context.Context, role, id string) error { return a.setModel(ctx, role, id) },
		SetPermission: func(_ context.Context, mode contract.PermissionMode) error {
			a.mu.Lock()
			active := a.activeWorkspace
			a.mu.Unlock()
			if active != nil {
				if err := active.SetPermissionMode(mode); err != nil {
					return err
				}
			}
			return state.SaveSettings(*a.settings, a.paths)
		},
		Rewind: func(ctx context.Context) (string, error) {
			a.mu.Lock()
			checkpoint := a.activeCheckpoint
			a.mu.Unlock()
			if checkpoint == nil {
				return "No checkpoint available.", nil
			}
			return checkpoint.RestoreLatest(ctx)
		},
		NewSession: func(ctx context.Context) (tui.Runtime, []contract.Event, error) {
			a.mu.Lock()
			root := a.runtime.Session.WorkspacePath
			a.mu.Unlock()
			session, err := a.sessions.New(ctx, root, "Workspace session")
			if err != nil {
				return tui.Runtime{}, nil, err
			}
			return a.switchSession(ctx, session)
		},
		ListSessions: func(ctx context.Context) ([]contract.Session, error) {
			a.mu.Lock()
			root := a.runtime.Session.WorkspacePath
			a.mu.Unlock()
			return a.db.ListSessions(ctx, root, 100)
		},
		Resume: func(ctx context.Context, id string) (tui.Runtime, []contract.Event, error) {
			session, ok, err := a.db.Session(ctx, id)
			if err != nil {
				return tui.Runtime{}, nil, err
			}
			if !ok {
				return tui.Runtime{}, nil, fmt.Errorf("session not found: %s", id)
			}
			return a.switchSession(ctx, session)
		},
		SetAPIKey:  func(ctx context.Context, key string) error { return a.setAPIKey(ctx, key) },
		Logout:     func(_ context.Context) error { return a.setAPIKey(context.Background(), "") },
		MCP:        a.mcpActions(),
		ListSkills: func(_ context.Context) ([]tui.Skill, error) { return a.listSkills() },
		LoadSkill:  func(_ context.Context, path string) (string, error) { return a.loadSkillBody(path) },
		FetchUsage: func(ctx context.Context) (*tui.UsageData, error) { return a.fetchUsage(ctx) },
		IsLoggedIn: func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return strings.TrimSpace(a.secrets.ProviderAPIKey) != ""
		},
		DiscoverModels: func(ctx context.Context) ([]contract.Model, error) {
			discoveryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			models, err := a.provider.ListModels(discoveryCtx)
			if err != nil {
				return nil, err
			}
			addDiscoveredModels(a.settings, models)
			if err := state.SaveSettings(*a.settings, a.paths); err != nil {
				return nil, err
			}
			a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
			return a.settings.Provider.Models, nil
		},
		// TranscriptPage (005 US1 T020) serves keyset pages of the durable transcript
		// for scroll-back. The session id is forced from the active session so a page
		// request can never read another session's transcript.
		TranscriptPage: func(ctx context.Context, req contract.TranscriptPageRequest) (contract.TranscriptPage, error) {
			req.SessionID = a.currentSessionID()
			return a.db.TranscriptPage(ctx, req)
		},
	}
}

func (a *Application) currentSessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runtime.Session.ID
}

func (a *Application) setModel(ctx context.Context, role, id string) error {
	var selected contract.Model
	found := false
	for _, model := range a.settings.Provider.Models {
		if model.ID == id {
			selected, found = model, true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown model %q", id)
	}
	a.mu.Lock()
	engine := a.runtime.Engine
	a.mu.Unlock()
	if engine == nil {
		return errors.New("runtime engine is unavailable")
	}
	addendum := ""
	if role != "subagent" {
		role = "main"
		addendum = gateway.ResolveModelProfile(selected.ID + " " + selected.Name).PromptAddendum
	}
	if err := engine.SwitchModel(ctx, role, selected.ID, selected.Name, addendum); err != nil {
		return err
	}
	if err := state.SaveSettings(*a.settings, a.paths); err != nil {
		return err
	}
	a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
	return nil
}

func (a *Application) mcpActions() tui.MCPActions {
	return tui.MCPActions{
		List: func(_ context.Context) ([]tui.MCPServerInfo, error) { return a.mcpServerInfos() },
		// Refresh (D2/T033) kicks a background manager refresh when the modal
		// opens so lazy-connect servers begin connecting; the modal's M2 tick then
		// observes them settle. Short-budget (non-blocking) like the startup path.
		Refresh: func(ctx context.Context) { a.refreshMCP(ctx, "mcp modal open") },
		Add: func(ctx context.Context, spec tui.MCPAddSpec) error {
			name := state.SanitizeMCPName(spec.Name)
			if name == "" {
				return errors.New("MCP server name is invalid")
			}
			var server state.MCPServer
			switch spec.Transport {
			case "stdio":
				if strings.TrimSpace(spec.Command) == "" {
					return errors.New("stdio server needs a command")
				}
				server = state.MCPServer{Name: name, Enabled: true, TimeoutMS: 30_000, Transport: "stdio", Command: spec.Command, Args: spec.Args}
			case "http":
				server = state.MCPServer{Name: name, Enabled: true, TimeoutMS: 30_000, Transport: "http", URL: spec.URL, OAuth: &state.MCPOAuth{Enabled: spec.OAuth, RedirectPort: 35698}}
			default:
				return errors.New("transport must be stdio or http")
			}
			if err := state.UpsertMCPServer(server, a.paths); err != nil {
				return err
			}
			a.refreshMCP(ctx, "mcp add "+name)
			return nil
		},
		Remove: func(ctx context.Context, name string) error {
			removed, err := state.RemoveMCPServer(name, a.paths)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("MCP server not found: %s", name)
			}
			a.refreshMCP(ctx, "mcp remove "+name)
			return nil
		},
		SetEnabled: func(ctx context.Context, name string, enabled bool) error {
			if err := a.setMCPEnabled(name, enabled); err != nil {
				return err
			}
			a.refreshMCP(ctx, fmt.Sprintf("mcp %s %s", map[bool]string{true: "enable", false: "disable"}[enabled], name))
			return nil
		},
		Authorize: func(ctx context.Context, name string) error {
			a.mu.Lock()
			manager := a.activeMCP
			a.mu.Unlock()
			if manager == nil {
				return errors.New("MCP is disabled for this session")
			}
			if err := manager.Authorize(ctx, name, mcpclient.AuthorizeOptions{OnURL: func(url string) {
				if a.callbacks.Status != nil {
					a.callbacks.Status("Open the browser to authorize MCP: " + url)
				}
			}}); err != nil {
				return err
			}
			// M1: blocking refresh so the modal snapshot the test code
			// reads right after shows `connected`, not `connecting`.
			// D3/T034: bound the blocking wait to 30s so a wedged server cannot
			// stall the Authorize action indefinitely.
			waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			manager.RefreshBlocking(waitCtx)
			cancel()
			a.emitMCPProblems(manager)
			return nil
		},
		Test: func(ctx context.Context, _ string) ([]tui.MCPServerInfo, error) {
			a.mu.Lock()
			manager := a.activeMCP
			a.mu.Unlock()
			if manager != nil {
				// M1: blocking refresh so the modal reflects the final
				// state immediately rather than capturing mid-connect.
				// D3/T034: bound the wait to 30s so a wedged server cannot stall
				// the Test action indefinitely.
				waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				manager.RefreshBlocking(waitCtx)
				cancel()
				a.emitMCPProblems(manager)
			}
			return a.mcpServerInfos()
		},
	}
}

// mcpServerInfos merges the persisted MCP configuration with the manager's live
// connection status for the management modal.
func (a *Application) mcpServerInfos() ([]tui.MCPServerInfo, error) {
	config, err := state.LoadMCPConfig(a.paths)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	manager := a.activeMCP
	a.mu.Unlock()
	statuses := map[string]mcpclient.Status{}
	if manager != nil {
		for _, status := range manager.Statuses() {
			statuses[status.Name] = status
		}
	}
	infos := make([]tui.MCPServerInfo, 0, len(config.Servers))
	for _, server := range config.Servers {
		info := tui.MCPServerInfo{Name: server.Name, Transport: server.Transport, Enabled: server.Enabled}
		if server.Transport == "stdio" {
			info.Target = strings.TrimSpace(server.Command + " " + strings.Join(server.Args, " "))
		} else {
			info.Target = server.URL
			info.OAuth = server.OAuth != nil && server.OAuth.Enabled
		}
		switch {
		case !server.Enabled:
			info.State = "disabled"
		default:
			info.State = "not connected"
		}
		if status, ok := statuses[server.Name]; ok {
			info.State, info.Status, info.ToolCount = status.State, status.Message, status.ToolCount
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (a *Application) setMCPEnabled(name string, enabled bool) error {
	config, err := state.LoadMCPConfig(a.paths)
	if err != nil {
		return err
	}
	for i := range config.Servers {
		if config.Servers[i].Name == name {
			config.Servers[i].Enabled = enabled
			return state.SaveMCPConfig(config, a.paths)
		}
	}
	return fmt.Errorf("MCP server not found: %s", name)
}

func (a *Application) switchSession(ctx context.Context, session contract.Session) (tui.Runtime, []contract.Event, error) {
	bundle, err := a.buildRuntime(ctx, session)
	if err != nil {
		return tui.Runtime{}, nil, err
	}
	a.activate(bundle)
	return bundle.runtime, bundle.recent, nil
}

func (a *Application) activate(bundle runtimeBundle) {
	a.mu.Lock()
	previous := a.activeMCP
	a.runtime, a.recent = bundle.runtime, append([]contract.Event(nil), bundle.recent...)
	a.activeWorkspace, a.activeCheckpoint, a.activeMCP, a.activeRegistry = bundle.workspace, bundle.checkpoint, bundle.mcp, bundle.registry
	a.mu.Unlock()
	if previous != nil && previous != bundle.mcp {
		_ = previous.Close()
	}
}

func (a *Application) buildRuntime(ctx context.Context, session contract.Session) (runtimeBundle, error) {
	if _, err := os.Stat(session.WorkspacePath); err != nil {
		return runtimeBundle{}, fmt.Errorf("open session workspace %s: %w", session.WorkspacePath, err)
	}
	var historySnapshot orchestrator.HistorySnapshot
	if err := a.sessions.ReadJSON(session.ID, "history.json", orchestrator.HistorySnapshot{Version: 1}, &historySnapshot); err != nil {
		return runtimeBundle{}, err
	}
	var inspectionSnapshot orchestrator.InspectionSnapshot
	if err := a.sessions.ReadJSON(session.ID, "inspection.json", orchestrator.InspectionSnapshot{Version: 3, Signatures: map[string]orchestrator.InspectionEntry{}, Coverage: map[string]orchestrator.InspectionCoverage{}}, &inspectionSnapshot); err != nil {
		return runtimeBundle{}, err
	}
	var knowledgeSnapshot orchestrator.KnowledgeSnapshot
	if err := a.sessions.ReadJSON(session.ID, "knowledge.json", orchestrator.KnowledgeSnapshot{Version: 1, Files: map[string]string{}}, &knowledgeSnapshot); err != nil {
		return runtimeBundle{}, err
	}
	usageRecords, err := a.sessions.UsageRecords(session.ID)
	if err != nil {
		return runtimeBundle{}, fmt.Errorf("load session usage: %w", err)
	}
	invalidationEvents, err := a.sessions.InvalidationEvents(session.ID)
	if err != nil {
		return runtimeBundle{}, fmt.Errorf("load session invalidations: %w", err)
	}
	history := orchestrator.NewHistory(historySnapshot, func(value orchestrator.HistorySnapshot) error {
		return a.sessions.WriteJSON(session.ID, "history.json", value)
	})
	// T041: archive originals of reclaimed tool results to the session's
	// pruned.jsonl before reclamation shortens them, so they stay recoverable.
	history.SetPruneArchive(func(records []contract.PrunedRecord) error {
		for _, record := range records {
			if err := a.sessions.AppendPruned(session.ID, record); err != nil {
				return err
			}
		}
		return nil
	})
	inspection := orchestrator.NewInspection(inspectionSnapshot, func(value orchestrator.InspectionSnapshot) error {
		return a.sessions.WriteJSON(session.ID, "inspection.json", value)
	}, session.WorkspacePath)
	knowledge := orchestrator.NewKnowledge(knowledgeSnapshot, func(value orchestrator.KnowledgeSnapshot) error {
		return a.sessions.WriteJSON(session.ID, "knowledge.json", value)
	})
	sessionDir, err := state.SessionDir(a.paths, session.ID)
	if err != nil {
		return runtimeBundle{}, err
	}
	checkpoint := &workspace.CheckpointStore{SessionID: session.ID, SessionDir: sessionDir, Workspace: session.WorkspacePath, Metadata: a.db}
	approver := workspace.ApproverFunc(func(ctx context.Context, request workspace.ApprovalRequest) (bool, error) {
		if a.callbacks.Confirm == nil {
			return false, nil
		}
		return a.callbacks.Confirm(ctx, approvalMessage(request))
	})
	service, err := workspace.New(session.WorkspacePath, workspace.Options{
		PermissionMode: a.settings.PermissionMode,
		Trust:          a.db,
		Approver:       approver,
		KnownFile:      inspection.Known,
		PreferredShell: a.settings.Shell.Preferred,
		ShellTimeout:   time.Duration(a.settings.Shell.TimeoutMS) * time.Millisecond,
		OutputLimit:    a.settings.Shell.OutputLimit,
		Checkpoints:    checkpoint,
		Status:         a.callbacks.Status,
		ShellOutput: func(chunk string) {
			if a.callbacks.ToolOutput != nil {
				a.callbacks.ToolOutput("run_shell", chunk)
			}
		},
	})
	if err != nil {
		return runtimeBundle{}, err
	}
	// T022 (US1): do not block interactive launch on a network probe. Reuse the
	// persisted probe snapshot for the exact provider config (fingerprint); only
	// when there is no snapshot for this config — a first run, or a deliberate
	// config change that already minted a new fingerprint — do a one-time
	// synchronous probe and persist it. The tool surface is therefore fixed once at
	// session start (a deliberate boundary), which keeps the prefix byte-stable
	// within the session and removes the mid-config probe-change invalidation.
	webSupported := false
	if strings.TrimSpace(a.settings.Provider.BaseURL) != "" && strings.TrimSpace(a.secrets.ProviderAPIKey) != "" {
		fingerprint := state.ProbeFingerprint(a.settings.Provider.BaseURL, a.secrets.ProviderAPIKey)
		if snap, ok := a.probeStore.Get(fingerprint); ok {
			webSupported = snap.WebSearch == state.ProbeSupported
		} else {
			supported, _, _, probeErr := a.webSearchAvailable(ctx)
			if probeErr != nil {
				return runtimeBundle{}, probeErr
			}
			webSupported = supported
		}
	}
	registry := orchestrator.NewRegistry(service.Tools()...)
	if webSupported {
		registry.Add(gateway.WebSearchTool{Searcher: gateway.WebSearch{BaseURL: a.settings.Provider.BaseURL, APIKey: a.secrets.ProviderAPIKey, Client: &http.Client{Timeout: 30 * time.Second}}})
	}

	var manager *mcpclient.Manager
	if !a.disableMCP {
		manager = mcpclient.New(a.paths, &http.Client{Timeout: 2 * time.Minute}, func(ctx context.Context, message string) (bool, error) {
			if a.settings.PermissionMode == contract.PermissionAutoAccept {
				return true, nil
			}
			if a.callbacks.Confirm == nil {
				return false, nil
			}
			return a.callbacks.Confirm(ctx, message)
		}, nil)
		pinned, err := manager.PinnedTools()
		if err != nil {
			return runtimeBundle{}, err
		}
		for _, tool := range pinned {
			registry.Add(tool)
		}
		// M5: when every configured MCP server already has a pinned
		// surface on disk, skip the eager round and trust the lazy path.
		// First-session users still get the warm-up connect so we can
		// learn the schemas.
		if !manager.AllConfiguredServersHaveSurface() {
			manager.Refresh(a.ctx, a.mcpWait)
			a.emitMCPProblems(manager)
		}
	}

	active, _ := state.ActiveModel(*a.settings)
	subagent, _ := state.SubagentModel(*a.settings)
	profile := gateway.ResolveModelProfile(active.ID + " " + active.Name)
	shell, _ := workspace.ChooseShell(a.settings.Shell.Preferred)
	planText, _ := a.sessions.ReadPlan(session.ID)
	// G4: restore an active goal from the per-session goal.json sidecar.
	// Only an active goal resurrects; completed/blocked goals are dropped so a
	// finished objective never re-activates. The engine surfaces a one-shot
	// notice (RestoredGoalNotice) for the TUI to display.
	var initialGoal *contract.GoalSnapshot
	if snapshot, ok, _ := a.sessions.ReadGoal(session.ID); ok && snapshot.Status == string(orchestrator.GoalActive) && strings.TrimSpace(snapshot.Text) != "" {
		copySnapshot := snapshot
		initialGoal = &copySnapshot
	}
	// P2: restore plan-mode and pending-plan flags from the plan_state.json
	// sidecar so a mid-plan restart or a saved-but-not-yet-executed plan
	// survives. Plan content was already loaded above (planText → InitialPlan);
	// this carries only the two flags. The engine surfaces a one-shot notice
	// (RestoredPlanNotice) for the TUI to display.
	var initialPlanState *contract.PlanStateSnapshot
	// 004 US2: thread the sidecar in whenever it carries any state — including a
	// terminal or interrupted phase where both booleans are false — so the engine
	// can apply the load-time truthfulness corrections and surface the correct
	// resume notice (an interrupted plan resumes partial; a finished one is silent).
	if state, ok, _ := a.sessions.ReadPlanState(session.ID); ok && (state.PlanMode || state.PendingPlan || state.Phase != contract.PlanPhaseNone) {
		copyState := state
		initialPlanState = &copyState
	}
	// 005 US3: compose (new session) or restore (resume) the project-context boot
	// snapshot BEFORE NewEngine, which sends no provider request — so the boot
	// block rides the first user submit in one send. On resume the persisted
	// RenderedBootContext is reused verbatim; it is never recompiled from live
	// workspace state, or the cached prefix (SystemHash) would diverge mid-session.
	workspaceKey, _ := workspace.WorkspaceKey(session.WorkspacePath)
	secretValues := append([]string{a.secrets.ProviderAPIKey}, mcpSecretValues(a.paths)...)
	skills := workspaceSkillListings(session.WorkspacePath)
	// 006: create the MUHIYA.md template when the workspace has none, so the user
	// has a clear file to edit. Non-fatal on a read-only workspace.
	_ = workspace.EnsureProjectInstructionsTemplate(session.WorkspacePath)
	var projectContext contract.ProjectContextSnapshot
	restored, ok, readErr := a.sessions.ReadProjectContext(session.ID, workspaceKey)
	if ok {
		projectContext = restored
	} else {
		instructions := workspace.LoadProjectInstructions(session.WorkspacePath, secretValues...)
		memory := workspace.LoadProjectMemory(session.WorkspacePath, secretValues...)
		block := orchestrator.RenderProjectContextBlock(instructions.ContentHash, instructions.CanonicalContent, memory.ContentHash, memory.CanonicalContent)
		skillsSnapshot := make([]string, 0, len(skills))
		for _, skill := range skills {
			skillsSnapshot = append(skillsSnapshot, skill.Name+"\t"+skill.Path+"\t"+skill.Description)
		}
		projectContext = contract.ProjectContextSnapshot{
			Version: contract.ProjectContextVersion, WorkspaceKey: workspaceKey, RenderedBootContext: block,
			InstructionsHash: instructions.ContentHash, InstructionsState: string(instructions.State),
			MemoryHash: memory.ContentHash, MemoryState: string(memory.State), SkillsSnapshot: skillsSnapshot,
			AppliedInstructionHash: instructions.ContentHash, AppliedMemoryHash: memory.ContentHash,
		}
		// Persist ONLY when the sidecar is genuinely absent/corrupt/foreign
		// (readErr == nil; the corrupt/foreign cases already backed up the file).
		// A transient read failure (e.g. an AV scan briefly holding the handle on
		// Windows) must NOT overwrite a possibly-valid sidecar: run from this
		// in-memory bootstrap this session and leave the on-disk bytes intact so a
		// later session can still restore the exact cached prefix.
		if readErr == nil {
			_ = a.sessions.WriteProjectContext(session.ID, projectContext)
		}
	}
	initialProjectContext := projectContext
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{
		Settings:   a.settings,
		Secrets:    a.secrets,
		Session:    session,
		Provider:   a.provider,
		Registry:   registry,
		History:    history,
		Inspection: inspection,
		Knowledge:  knowledge,
		Callbacks:  a.callbacks,
		Persistence: orchestrator.Persistence{
			AddEvent: func(ctx context.Context, role, kind, content string) error {
				return a.db.AddEvent(ctx, session.ID, role, kind, content)
			},
			AppendTranscript: func(_ context.Context, value map[string]any) error {
				return a.sessions.AppendTranscript(session.ID, value)
			},
			AppendUsage: func(_ context.Context, record contract.UsageRecord) error {
				return a.sessions.AppendUsage(session.ID, record)
			},
			AppendInvalidation: func(_ context.Context, event contract.InvalidationEvent) error {
				return a.sessions.AppendInvalidation(session.ID, event)
			},
			WritePlan: func(_ context.Context, content string) error { return a.sessions.WritePlan(session.ID, content) },
			WriteGoal: func(_ context.Context, snapshot contract.GoalSnapshot) error {
				return a.sessions.WriteGoal(session.ID, snapshot)
			},
			ClearGoal: func(_ context.Context) error { return a.sessions.ClearGoal(session.ID) },
			WritePlanState: func(_ context.Context, snapshot contract.PlanStateSnapshot) error {
				return a.sessions.WritePlanState(session.ID, snapshot)
			},
			ClearPlanState: func(_ context.Context) error { return a.sessions.ClearPlanState(session.ID) },
			WriteProjectContext: func(_ context.Context, snapshot contract.ProjectContextSnapshot) error {
				return a.sessions.WriteProjectContext(session.ID, snapshot)
			},
		},
		Prompt: orchestrator.PromptContext{
			Workspace: session.WorkspacePath, Shell: shell,
			Model: active.Name, ModelAddendum: profile.PromptAddendum, SubagentModel: subagent.Name,
			Skills:              skills,
			ProjectMemory:       true,
			ProjectContextBlock: projectContext.RenderedBootContext,
		},
		InitialProjectContext: &initialProjectContext,
		ProjectContextProbe: func(pctx context.Context) (contract.ProjectContextProbe, error) {
			// 006: re-read both project files at the submit boundary. A change to
			// either (the agent's own file-tool edit, or a manual user edit) surfaces
			// once as an update block; unchanged files inject nothing.
			instructions := workspace.LoadProjectInstructions(session.WorkspacePath, secretValues...)
			memory := workspace.LoadProjectMemory(session.WorkspacePath, secretValues...)
			return contract.ProjectContextProbe{
				InstructionsHash:    instructions.ContentHash,
				InstructionsContent: instructions.CanonicalContent,
				MemoryHash:          memory.ContentHash,
				MemoryContent:       memory.CanonicalContent,
			}, nil
		},
		InitialPlan:          parsePlan(planText),
		InitialGoal:          initialGoal,
		InitialPlanState:     initialPlanState,
		InitialUsageRecords:  usageRecords,
		InitialInvalidations: invalidationEvents,
		BoundaryTools: func() (orchestrator.BoundaryToolChange, bool, error) {
			if manager == nil {
				return orchestrator.BoundaryToolChange{}, false, nil
			}
			change, changed, err := manager.TakeBoundaryChange()
			return orchestrator.BoundaryToolChange{Tools: change.Tools, Scope: change.Scope}, changed, err
		},
		Rescue: gateway.RescueToolCalls,
		Redact: func(value string) string { return state.Redact(value, a.secrets, secretValues[1:]...) },
	})
	if err != nil {
		if manager != nil {
			_ = manager.Close()
		}
		return runtimeBundle{}, err
	}
	recent, err := a.db.Events(ctx, session.ID, 300)
	if err != nil {
		if manager != nil {
			_ = manager.Close()
		}
		return runtimeBundle{}, err
	}
	return runtimeBundle{runtime: tui.Runtime{Engine: engine, Session: session, Settings: a.settings}, recent: recent, workspace: service, checkpoint: checkpoint, mcp: manager, registry: registry}, nil
}

func (a *Application) webSearchAvailable(ctx context.Context) (bool, bool, string, error) {
	fingerprint := state.ProbeFingerprint(a.settings.Provider.BaseURL, a.secrets.ProviderAPIKey)
	previous, exists := a.probeStore.Get(fingerprint)
	if strings.TrimSpace(a.settings.Provider.BaseURL) == "" || strings.TrimSpace(a.secrets.ProviderAPIKey) == "" {
		return false, false, "", nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	result, definitive, _ := (gateway.WebSearch{BaseURL: a.settings.Provider.BaseURL, APIKey: a.secrets.ProviderAPIKey}).Probe(probeCtx)
	if !definitive {
		if exists {
			return previous.WebSearch == state.ProbeSupported, false, "", nil
		}
		if err := a.probeStore.Put(state.ProbeSnapshot{Fingerprint: fingerprint, WebSearch: state.ProbeUnsupported, CheckedAt: time.Now().UTC()}); err != nil {
			return false, false, "", err
		}
		return false, false, "", nil
	}
	current := state.ProbeUnsupported
	if result == gateway.WebSearchProbeSupported {
		current = state.ProbeSupported
	}
	if err := a.probeStore.Put(state.ProbeSnapshot{Fingerprint: fingerprint, WebSearch: current, CheckedAt: time.Now().UTC()}); err != nil {
		return false, false, "", err
	}
	changed := exists && previous.WebSearch != current
	return current == state.ProbeSupported, changed, fmt.Sprintf("web_search probe changed from %s to %s", previous.WebSearch, current), nil
}

func (a *Application) setAPIKey(ctx context.Context, key string) error {
	// M3: the render loop reads a.secrets through IsLoggedIn under a.mu, so the
	// write must take the same lock (the I/O below stays outside it to avoid
	// blocking the UI during disk/network work).
	a.mu.Lock()
	a.secrets.ProviderAPIKey = strings.TrimSpace(key)
	a.sessions.Secrets = a.secrets
	secretsSnapshot := a.secrets
	a.mu.Unlock()
	if err := state.SaveSecrets(secretsSnapshot, a.paths); err != nil {
		return err
	}
	a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
	if a.secrets.ProviderAPIKey == "" || len(a.settings.Provider.Models) > 0 {
		return nil
	}
	models, err := a.provider.ListModels(ctx)
	if err != nil {
		if a.callbacks.Status != nil {
			a.callbacks.Status("API key saved; add a model with /model or `muhiyacode config set model <id>`.")
		}
		return nil
	}
	addDiscoveredModels(a.settings, models)
	if err := state.SaveSettings(*a.settings, a.paths); err != nil {
		return err
	}
	a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
	return nil
}

// workspaceSkillListings discovers workspace-resident skills once and renders
// them as deterministic prompt listings (003 T034/T035): workspace-relative
// forward-slash paths, single-line descriptions truncated to 200 chars. Same
// config + files ⇒ identical slice ⇒ byte-identical prompt section.
func workspaceSkillListings(root string) []orchestrator.SkillListing {
	discovered, err := workspace.DiscoverWorkspaceSkills(root, 40)
	if err != nil {
		return nil
	}
	listings := make([]orchestrator.SkillListing, 0, len(discovered))
	for _, skill := range discovered {
		rel := skill.Path
		if r, relErr := filepath.Rel(root, skill.Path); relErr == nil {
			rel = r
		}
		rel = filepath.ToSlash(rel)
		listings = append(listings, orchestrator.SkillListing{Name: skill.Name, Path: rel, Description: singleLineDescription(skill.Description)})
	}
	return listings
}

func singleLineDescription(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:199]) + "…"
	}
	return value
}

// listSkills powers the manual /skills modal. It is lazy (003 T036): it does NOT
// load any instruction bodies here, so opening the modal never triggers a burst
// of file reads. The Path travels with each skill; the body is loaded on demand
// for the selected skills only, at submit time (LoadSkill).
func (a *Application) listSkills() ([]tui.Skill, error) {
	a.mu.Lock()
	root := a.runtime.Session.WorkspacePath
	a.mu.Unlock()
	discovered, err := workspace.DiscoverSkills(root, nil, 40)
	if err != nil {
		return nil, err
	}
	result := make([]tui.Skill, 0, len(discovered))
	for _, skill := range discovered {
		result = append(result, tui.Skill{Name: skill.Name, Description: skill.Description, Path: skill.Path})
	}
	return result, nil
}

// loadSkillBody loads one skill's instructions on demand (32 KiB cap), used at
// submit for user-selected skills only.
func (a *Application) loadSkillBody(path string) (string, error) {
	return workspace.LoadSkillInstructions(workspace.Skill{Path: path}, 32*1024)
}

// fetchUsage retrieves account usage from the gateway with the stored key and
// maps it into the TUI's UsageData (003 US5). Errors carry a friendly message.
func (a *Application) fetchUsage(ctx context.Context) (*tui.UsageData, error) {
	a.mu.Lock()
	settings := *a.settings
	key := a.secrets.ProviderAPIKey
	a.mu.Unlock()
	resp, err := gateway.FetchUsage(ctx, settings, key)
	if err != nil {
		return nil, err
	}
	data := &tui.UsageData{
		PlanName:       resp.Plan.Name,
		ExtraTotal:     resp.Credits.ExtraTotal,
		ExtraRemaining: resp.Credits.ExtraRemaining,
		SpendTodayUSD:  resp.Spend.TodayUSD,
	}
	for _, w := range resp.Plan.Windows {
		data.Windows = append(data.Windows, tui.UsageWindow{
			Name: w.Name, BudgetUSD: w.BudgetUSD, CurrentSpentUSD: w.CurrentSpentUSD,
			ResetTime: w.ResetTime, DurationSeconds: w.DurationSeconds,
		})
	}
	return data, nil
}

func (a *Application) refreshMCP(ctx context.Context, scope string) {
	a.mu.Lock()
	manager := a.activeMCP
	a.mu.Unlock()
	if manager == nil {
		return
	}
	manager.RequestSurfaceRefresh(scope)
	manager.Refresh(ctx, a.mcpWait)
	a.emitMCPProblems(manager)
}

// emitMCPProblems surfaces only servers that failed or need authorization, so
// healthy servers stay silent and the notice line is not spammed on startup.
func (a *Application) emitMCPProblems(manager *mcpclient.Manager) {
	if a.callbacks.MCPStatus == nil || manager == nil {
		return
	}
	for _, status := range manager.Statuses() {
		if status.State == "error" || status.State == "auth_required" {
			a.callbacks.MCPStatus(fmt.Sprintf("%s: %s", status.Name, status.Message))
		}
	}
}

func formatMCPList(paths state.Paths) (string, error) {
	config, err := state.LoadMCPConfig(paths)
	if err != nil {
		return "", err
	}
	if len(config.Servers) == 0 {
		return "No MCP servers registered.", nil
	}
	lines := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		target := server.URL
		auth := ""
		if server.Transport == "stdio" {
			target = strings.TrimSpace(server.Command + " " + strings.Join(server.Args, " "))
		} else if server.OAuth != nil && server.OAuth.Enabled {
			auth = " oauth"
		}
		status := "off"
		if server.Enabled {
			status = "on "
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s%s  %s", status, server.Name, server.Transport, auth, target))
	}
	return strings.Join(lines, "\n"), nil
}

func approvalMessage(request workspace.ApprovalRequest) string {
	if request.TrustWorkspace {
		return "Trust this workspace for mutations?\n" + request.Workspace
	}
	if request.Command != "" {
		return "Allow shell command in " + request.Workspace + "?\n\n" + request.Command
	}
	label := strings.Title(string(request.Action))
	if request.OutsideWorkspace {
		label += " outside the workspace"
	}
	return fmt.Sprintf("%s?\n%s", label, request.Path)
}

var planLine = regexp.MustCompile(`^- \[([ xX])\] (.+) \((pending|in_progress|completed)\)$`)

func parsePlan(content string) contract.Plan {
	var plan contract.Plan
	var notes []string
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		match := planLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 4 {
			plan.Steps = append(plan.Steps, contract.PlanStep{Title: match[2], Status: contract.PlanStatus(match[3])})
		} else if strings.TrimSpace(line) != "" && line != "No task has been planned yet." {
			notes = append(notes, line)
		}
	}
	if len(plan.Steps) > 0 {
		plan.Note = strings.Join(notes, "\n")
		plan.UpdatedAt = time.Now().UTC()
	}
	return plan
}

func addDiscoveredModels(settings *contract.Settings, models []contract.Model) {
	sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	for _, model := range models {
		if model.ContextLimit <= 0 {
			model.ContextLimit = gateway.ResolveModelProfile(model.ID + " " + model.Name).DefaultContextWindow
		}
		state.UpsertModel(settings, model, false)
	}
	state.AutoAssignModels(settings)
}
