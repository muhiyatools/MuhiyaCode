package command

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	Context     context.Context
	Workspace   string
	SessionID   string
	NewSession  bool
	Title       string
	Callbacks   contract.Callbacks
	DisableMCP  bool
	MCPDeadline time.Duration
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
	callbacks  contract.Callbacks
	disableMCP bool
	mcpWait    time.Duration

	mu               sync.Mutex
	runtime          tui.Runtime
	recent           []contract.Event
	activeWorkspace  *workspace.Workspace
	activeCheckpoint *workspace.CheckpointStore
	activeMCP        *mcpclient.Manager
	activeRegistry   *orchestrator.Registry
	webFingerprint   string
	webSupported     bool
}

type runtimeBundle struct {
	runtime    tui.Runtime
	recent     []contract.Event
	workspace  *workspace.Workspace
	checkpoint *workspace.CheckpointStore
	mcp        *mcpclient.Manager
	registry   *orchestrator.Registry
}

func OpenApplication(options ApplicationOptions) (*Application, error) {
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
	app := &Application{
		ctx: ctx, paths: paths, db: db, sessions: store, settings: &settings,
		secrets: secrets, callbacks: options.Callbacks, disableMCP: options.DisableMCP,
		mcpWait: options.MCPDeadline,
	}
	if app.mcpWait <= 0 {
		app.mcpWait = 900 * time.Millisecond
	}
	app.provider = gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: secrets.ProviderAPIKey})
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
	bundle, err := app.buildRuntime(ctx, session)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	app.activate(bundle)
	return app, nil
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
	}
}

func (a *Application) mcpActions() tui.MCPActions {
	return tui.MCPActions{
		List: func(_ context.Context) ([]tui.MCPServerInfo, error) { return a.mcpServerInfos() },
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
			a.refreshMCP(ctx)
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
			a.refreshMCP(ctx)
			return nil
		},
		SetEnabled: func(ctx context.Context, name string, enabled bool) error {
			if err := a.setMCPEnabled(name, enabled); err != nil {
				return err
			}
			a.refreshMCP(ctx)
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
			a.refreshMCP(ctx)
			return nil
		},
		Test: func(ctx context.Context, _ string) ([]tui.MCPServerInfo, error) {
			a.refreshMCP(ctx)
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
	history := orchestrator.NewHistory(historySnapshot, func(value orchestrator.HistorySnapshot) error {
		return a.sessions.WriteJSON(session.ID, "history.json", value)
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
	registry := orchestrator.NewRegistry(service.Tools()...)
	if a.webSearchAvailable(ctx) {
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
		}, registry.Add)
		manager.Refresh(a.ctx, a.mcpWait)
		a.emitMCPProblems(manager)
	}

	active, _ := state.ActiveModel(*a.settings)
	subagent, _ := state.SubagentModel(*a.settings)
	profile := gateway.ResolveModelProfile(active.ID + " " + active.Name)
	shell, _ := workspace.ChooseShell(a.settings.Shell.Preferred)
	planText, _ := a.sessions.ReadPlan(session.ID)
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
			WritePlan: func(_ context.Context, content string) error { return a.sessions.WritePlan(session.ID, content) },
		},
		Prompt: orchestrator.PromptContext{
			Workspace: session.WorkspacePath, Shell: shell, Date: time.Now().Format("2006-01-02"),
			Model: active.Name, ModelAddendum: profile.PromptAddendum, SubagentModel: subagent.Name,
		},
		InitialPlan: parsePlan(planText),
		Rescue:      gateway.RescueToolCalls,
		Redact:      func(value string) string { return state.Redact(value, a.secrets) },
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

func (a *Application) webSearchAvailable(ctx context.Context) bool {
	fingerprint := a.settings.Provider.BaseURL + "\x00" + a.secrets.ProviderAPIKey
	a.mu.Lock()
	if fingerprint == a.webFingerprint {
		supported := a.webSupported
		a.mu.Unlock()
		return supported
	}
	a.mu.Unlock()
	if strings.TrimSpace(a.settings.Provider.BaseURL) == "" || strings.TrimSpace(a.secrets.ProviderAPIKey) == "" {
		a.mu.Lock()
		a.webFingerprint, a.webSupported = fingerprint, false
		a.mu.Unlock()
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	supported := (gateway.WebSearch{BaseURL: a.settings.Provider.BaseURL, APIKey: a.secrets.ProviderAPIKey}).Supported(probeCtx)
	a.mu.Lock()
	a.webFingerprint, a.webSupported = fingerprint, supported
	a.mu.Unlock()
	return supported
}

func (a *Application) setAPIKey(ctx context.Context, key string) error {
	a.secrets.ProviderAPIKey = strings.TrimSpace(key)
	a.sessions.Secrets = a.secrets
	if err := state.SaveSecrets(a.secrets, a.paths); err != nil {
		return err
	}
	a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
	a.mu.Lock()
	a.webFingerprint = ""
	a.mu.Unlock()
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
		instructions, loadErr := workspace.LoadSkillInstructions(skill, 32*1024)
		if loadErr != nil {
			continue
		}
		result = append(result, tui.Skill{Name: skill.Name, Description: skill.Description, Instructions: instructions})
	}
	return result, nil
}

func (a *Application) refreshMCP(ctx context.Context) {
	a.mu.Lock()
	manager := a.activeMCP
	registry := a.activeRegistry
	a.mu.Unlock()
	if manager == nil {
		return
	}
	if registry != nil {
		registry.RemovePrefix("mcp__")
	}
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
