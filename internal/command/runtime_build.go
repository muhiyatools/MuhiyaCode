package command

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	BenchmarkMode    bool
	Overrides        *BenchmarkOverrides
}

type BenchmarkOverrides struct {
	Model        string
	BaseURL      string
	APIKey       string
	Effort       string
	AdvisorMode  string
	ContextLimit int
	Seed         *int
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
	guard            *workspace.Guard
	benchmarkMode    bool
	seed             *int
}

type runtimeBundle struct {
	runtime    tui.Runtime
	recent     []contract.Event
	workspace  *workspace.Workspace
	checkpoint *workspace.CheckpointStore
	mcp        *mcpclient.Manager
	registry   *orchestrator.Registry
	guard      *workspace.Guard
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
	if options.BenchmarkMode && options.Overrides != nil {
		ov := options.Overrides
		if ov.APIKey != "" {
			secrets.ProviderAPIKey = ov.APIKey
		}
		if ov.BaseURL != "" {
			settings.Provider.BaseURL = ov.BaseURL
		}
		if ov.Model != "" {
			settings.Provider.ActiveModelID = ov.Model
		}
		if ov.Effort != "" {
			settings.Effort = contract.EffortLevel(ov.Effort)
		}
		if ov.AdvisorMode != "" {
			settings.Provider.Advisor = ov.AdvisorMode
		}
		if ov.ContextLimit > 0 {
			for i, m := range settings.Provider.Models {
				if m.ID == settings.Provider.ActiveModelID {
					settings.Provider.Models[i].ContextLimit = ov.ContextLimit
				}
			}
		}
		if ov.Model != "" {
			found := false
			for _, m := range settings.Provider.Models {
				if m.ID == ov.Model {
					found = true
					break
				}
			}
			if !found {
				_ = db.Close()
				return nil, fmt.Errorf("configured benchmark model %q is missing from the local catalog", ov.Model)
			}
		}
	}
	var appSeed *int
	if options.BenchmarkMode && options.Overrides != nil && options.Overrides.Seed != nil {
		appSeed = options.Overrides.Seed
	}
	app := &Application{
		ctx: ctx, paths: paths, db: db, sessions: store, settings: &settings,
		secrets: secrets, callbacks: options.Callbacks, disableMCP: options.DisableMCP,
		mcpWait: options.MCPDeadline, probeStore: probeStore, benchmarkMode: options.BenchmarkMode, seed: appSeed,
	}
	if app.mcpWait <= 0 {
		app.mcpWait = 900 * time.Millisecond
	}
	app.provider = gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: secrets.ProviderAPIKey, RawUsageObserver: options.RawUsageObserver})
	if secrets.ProviderAPIKey != "" && !options.BenchmarkMode {
		active, ok := state.ActiveModel(settings)
		// Discover when there is nothing usable yet, OR when the catalog has gone
		// stale. The staleness path matters now that the interactive refresh is
		// gone: without it the catalog would freeze at its first-run snapshot and
		// a model added to the gateway later would never be selectable.
		if !ok || active.ContextLimit <= 0 || catalogStale(settings.Provider.ModelsRefreshedAt) {
			discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			models, discoveryErr := app.provider.ListModels(discoveryCtx)
			cancel()
			if discoveryErr == nil && len(models) > 0 {
				addDiscoveredModels(app.settings, models)
				app.settings.Provider.ModelsRefreshedAt = time.Now().UTC().Format(time.RFC3339)
				if saveErr := state.SaveSettings(*app.settings, paths); saveErr != nil {
					_ = db.Close()
					return nil, saveErr
				}
				app.provider.UpdateConfig(*app.settings, secrets.ProviderAPIKey)
			}
			// A failed refresh is silent: a stale catalog still works, and an
			// offline start must never block the session.
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

func (a *Application) currentSessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runtime.Session.ID
}

func (a *Application) PreTrustWorkspace(ctx context.Context) error {
	a.mu.Lock()
	guard := a.guard
	a.mu.Unlock()
	if guard == nil {
		return nil
	}
	return guard.PreTrust(ctx)
}

// The interactive model switcher lived here until v1.1.0 removed /model: users
// no longer manage models mid-session, and freezing the pairing for the whole
// session is what keeps both prefix caches warm. Engine.SwitchModel remains the
// entry point for the CLI config path and the session advisor.

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
	a.activeWorkspace, a.activeCheckpoint, a.activeMCP, a.activeRegistry, a.guard = bundle.workspace, bundle.checkpoint, bundle.mcp, bundle.registry, bundle.guard
	a.mu.Unlock()
	if previous != nil && previous != bundle.mcp {
		_ = previous.Close()
	}
}

// migrateWorkspaceMemory copies a legacy workspace-root MEMORY.md into the store
// index the first time the store index does not yet exist (Experience Overhaul B1,
// INV-5). It is copy-only: the legacy workspace file is never modified, renamed, or
// deleted, and once the store index exists the legacy file is ignored forever.
// Every failure is silent and non-fatal. Returns true when a copy happened.
func migrateWorkspaceMemory(workspaceRoot, memoryDir string) bool {
	storeIndex := filepath.Join(memoryDir, workspace.ProjectMemoryFile)
	if existing, err := os.ReadFile(storeIndex); err == nil {
		// A store with real content is never re-migrated. A pristine comment-only
		// template (Memory Parity N1 creates one for every project) carries no
		// information, so a legacy workspace file may still replace it.
		if workspace.HasMeaningfulDocContent(string(existing)) {
			return false
		}
	} else if !os.IsNotExist(err) {
		return false // unreadable store — do not risk clobbering it
	}
	legacy := filepath.Join(workspaceRoot, workspace.ProjectMemoryFile)
	data, err := os.ReadFile(legacy)
	if err != nil {
		return false
	}
	body := strings.TrimPrefix(strings.TrimRight(string(data), "\n"), "# Project Memory")
	body = strings.TrimLeft(body, "\n")
	if strings.TrimSpace(body) == "" {
		return false // nothing meaningful to migrate
	}
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		return false
	}
	header := "# Project Memory\n<!-- migrated from " + legacy + " on first load; the workspace file is no longer read and can be deleted -->\n\n"
	if err := os.WriteFile(storeIndex, []byte(header+body+"\n"), 0o644); err != nil {
		return false
	}
	return true
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
	// engineRef is bound after NewEngine below; the shell-output closure captures
	// it (a var, so the later assignment is visible) to route run_shell streaming
	// through the engine's scope — a subagent's shell to its own card, the main
	// loop's to the transcript (D2).
	var engineRef *orchestrator.Engine
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
			if engineRef != nil {
				engineRef.RouteShellOutput(chunk)
				return
			}
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
	if !a.benchmarkMode && strings.TrimSpace(a.settings.Provider.BaseURL) != "" && strings.TrimSpace(a.secrets.ProviderAPIKey) != "" {
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
	profile := gateway.ResolveModelProfile(active.ID + " " + active.Name)
	shell, _ := workspace.ChooseShell(a.settings.Shell.Preferred)
	// 005 US3: compose (new session) or restore (resume) the project-context boot
	// snapshot BEFORE NewEngine, which sends no provider request — so the boot
	// block rides the first user submit in one send. On resume the persisted
	// RenderedBootContext is reused verbatim; it is never recompiled from live
	// workspace state, or the cached prefix (SystemHash) would diverge mid-session.
	workspaceKey, _ := workspace.WorkspaceKey(session.WorkspacePath)
	// Experience Overhaul B1: project memory lives in the per-project store, not the
	// repo. One-time copy of a legacy workspace MEMORY.md into the store (copy-only —
	// the original is never touched), and the user-level ~/.muhiya/MUHIYA.md that
	// applies across every project.
	memoryDir := state.ProjectMemoryDir(a.paths, workspaceKey)
	_ = migrateWorkspaceMemory(session.WorkspacePath, memoryDir)
	// Memory Parity N1: every project gets its memory store + MEMORY.md template at
	// session start (after migration, which must see an absent store to copy into).
	// The template is comment-only, so it injects nothing until real entries exist.
	_ = workspace.EnsureMemoryIndexTemplate(memoryDir)
	userInstructionsPath := filepath.Join(a.paths.Home, workspace.ProjectInstructionsFile)
	secretValues := append([]string{a.secrets.ProviderAPIKey}, mcpSecretValues(a.paths)...)
	skills := sessionSkillListings(session.WorkspacePath)
	// 006: create the MUHIYA.md template when the workspace has none, so the user
	// has a clear file to edit. Non-fatal on a read-only workspace.
	_ = workspace.EnsureProjectInstructionsTemplate(session.WorkspacePath)
	var projectContext contract.ProjectContextSnapshot
	restored, ok, readErr := a.sessions.ReadProjectContext(session.ID, workspaceKey)
	if ok {
		projectContext = restored
	} else {
		userInstr := workspace.LoadUserInstructions(userInstructionsPath, secretValues...)
		instructions := workspace.LoadProjectInstructions(session.WorkspacePath, secretValues...)
		memory := workspace.LoadMemoryIndex(memoryDir, secretValues...)
		block := orchestrator.RenderProjectContextBlock(userInstr.ContentHash, userInstr.CanonicalContent, instructions.ContentHash, instructions.CanonicalContent, memory.ContentHash, memory.CanonicalContent)
		skillsSnapshot := make([]string, 0, len(skills))
		for _, skill := range skills {
			skillsSnapshot = append(skillsSnapshot, skill.Name+"\t"+skill.Path+"\t"+skill.Description)
		}
		projectContext = contract.ProjectContextSnapshot{
			Version: contract.ProjectContextVersion, WorkspaceKey: workspaceKey, RenderedBootContext: block,
			InstructionsHash: instructions.ContentHash, InstructionsState: string(instructions.State),
			MemoryHash: memory.ContentHash, MemoryState: string(memory.State),
			UserInstructionsHash: userInstr.ContentHash, UserInstructionsState: string(userInstr.State),
			SkillsSnapshot:         skillsSnapshot,
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
	// C3: restore the prior session's prefix shape (if any) so the first request of a
	// resumed session can attribute a system/tools/model change instead of a silent
	// cold start. A missing/corrupt sidecar is a fresh baseline (nil).
	var priorPrefixShape *contract.PrefixShapeSnapshot
	if shape, ok, _ := a.sessions.ReadPrefixShape(session.ID); ok {
		priorPrefixShape = &shape
	}
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{
		Settings:   a.settings,
		Secrets:    a.secrets,
		Session:    session,
		MemoryDir:  memoryDir,
		Provider:   a.provider,
		Registry:   registry,
		History:    history,
		Inspection: inspection,
		Knowledge:  knowledge,
		Callbacks:  a.callbacks,
		Persistence: orchestrator.Persistence{
			AddEvent: func(ctx context.Context, role, kind, content, target string) error {
				return a.db.AddEvent(ctx, session.ID, role, kind, content, target)
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
			WriteProjectContext: func(_ context.Context, snapshot contract.ProjectContextSnapshot) error {
				return a.sessions.WriteProjectContext(session.ID, snapshot)
			},
			WritePrefixShape: func(_ context.Context, snapshot contract.PrefixShapeSnapshot) error {
				return a.sessions.WritePrefixShape(session.ID, snapshot)
			},
		},
		Prompt: orchestrator.PromptContext{
			Workspace: session.WorkspacePath, Shell: shell,
			Model: active.Name, ModelAddendum: profile.PromptAddendum,
			Skills:              skills,
			ProjectMemory:       true,
			ProjectContextBlock: projectContext.RenderedBootContext,
		},
		// 013 US1: the same listing the prompt advertises backs read_skill's
		// name resolution, so the catalog can never disagree with the prompt.
		SkillCatalog:          skills,
		LoadSkill:             a.loadSkillBody,
		InitialProjectContext: &initialProjectContext,
		InitialPrefixShape:    priorPrefixShape,
		ProjectContextProbe: func(pctx context.Context) (contract.ProjectContextProbe, error) {
			// 006 + B1: re-read the project instructions and the store memory index at
			// the submit boundary. A change to either (the agent's own save_memory /
			// file-tool edit, or a manual user edit) surfaces once as an update block;
			// unchanged files inject nothing.
			instructions := workspace.LoadProjectInstructions(session.WorkspacePath, secretValues...)
			memory := workspace.LoadMemoryIndex(memoryDir, secretValues...)
			return contract.ProjectContextProbe{
				InstructionsHash:    instructions.ContentHash,
				InstructionsContent: instructions.CanonicalContent,
				MemoryHash:          memory.ContentHash,
				MemoryContent:       memory.CanonicalContent,
			}, nil
		},
		InitialUsageRecords:  usageRecords,
		InitialInvalidations: invalidationEvents,
		Seed:                 a.seed,
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
	// Bind the shell-output router now that the engine exists; until this line the
	// closure above falls back to the direct sink (no task can run before it anyway).
	engineRef = engine
	// Legacy note: sessions created before the unified-session change may still
	// hold agents/<runID>.json sidecars. Nothing reads them now; they are inert
	// and are removed with the session directory.
	recent, err := a.db.Events(ctx, session.ID, 300)
	if err != nil {
		if manager != nil {
			_ = manager.Close()
		}
		return runtimeBundle{}, err
	}
	return runtimeBundle{runtime: tui.Runtime{Engine: engine, Session: session, Settings: a.settings}, recent: recent, workspace: service, checkpoint: checkpoint, mcp: manager, registry: registry, guard: service.Guard()}, nil
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
			a.callbacks.Status("API key saved; run `muhiyacode config discover` to load the model catalog.")
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

// catalogRefreshTTL bounds how long a discovered model catalog is trusted.
// One day keeps a newly added gateway model reachable by the next session
// without paying a network round trip at every start.
const catalogRefreshTTL = 24 * time.Hour

// catalogStale reports whether the recorded discovery time is missing or older
// than the TTL. An unparseable timestamp is treated as stale so a corrupted
// value self-heals on the next start.
func catalogStale(refreshedAt string) bool {
	if strings.TrimSpace(refreshedAt) == "" {
		return true
	}
	at, err := time.Parse(time.RFC3339, refreshedAt)
	if err != nil {
		return true
	}
	return time.Since(at) > catalogRefreshTTL
}
