// Package mcpclient integrates external MCP tool servers behind the same
// contract.Tool boundary used by MuhiyaCode's local workspace tools.
package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/processenv"
	"github.com/muhiya/muhiyacode/internal/state"
	workspacepolicy "github.com/muhiya/muhiyacode/internal/workspace"
)

// ErrAuthExpired (M6) is the sentinel returned by oauthAccessToken when the
// access+refresh tokens have nothing the manager can use. The manager routes
// this to auth_required state without string-sniffing.
var ErrAuthExpired = errors.New("mcp auth expired")

type Status struct {
	Name      string
	State     string
	Message   string
	ToolCount int
	UpdatedAt time.Time
}

type ConfirmFunc func(context.Context, string) (bool, error)

type Manager struct {
	paths      state.Paths
	httpClient *http.Client
	confirm    ConfirmFunc
	onTool     func(contract.Tool)
	sandbox    workspacepolicy.SandboxBackend
	workspace  string
	unsafeHost bool

	mu             sync.RWMutex
	refreshMu      sync.Mutex
	secretsMu      sync.Mutex
	refreshStop    context.CancelFunc
	closed         bool
	dedup          *refreshDedup
	connections    []*connection
	tools          map[string]*mcpTool
	statuses       map[string]Status
	usedNames      map[string]bool
	surfaceStore   *state.ToolSurfaceStore
	pinned         map[string]contract.ToolDefinition
	applied        map[string]contract.ToolDefinition
	knownAtStart   map[string]bool
	pendingSurface bool
	pendingForce   bool
	pendingScopes  []string
}

// SetProcessSandbox binds stdio MCP children to the same session sandbox as
// shell execution. unsafeHost is the explicit full-access escape hatch.
func (m *Manager) SetProcessSandbox(backend workspacepolicy.SandboxBackend, workspace string, unsafeHost bool) {
	m.mu.Lock()
	m.sandbox = backend
	m.workspace = workspace
	m.unsafeHost = unsafeHost
	m.mu.Unlock()
}

type connection struct {
	server  state.MCPServer
	session *mcp.ClientSession
	tools   []*mcpTool
}

func New(paths state.Paths, client *http.Client, confirm ConfirmFunc, onTool func(contract.Tool)) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	store, _ := state.NewToolSurfaceStore(paths)
	return &Manager{
		paths: paths, httpClient: client, confirm: confirm, onTool: onTool,
		tools: make(map[string]*mcpTool), statuses: make(map[string]Status), usedNames: make(map[string]bool),
		surfaceStore: store, pinned: make(map[string]contract.ToolDefinition), applied: make(map[string]contract.ToolDefinition), knownAtStart: make(map[string]bool),
	}
}

// lazyStartTimeout (M5) caps how long EnsureLive blocks on a single server's
// first-time connect. A wedged server cannot stall the agent turn past this
// horizon.
const lazyStartTimeout = 15 * time.Second

// EnsureLive (M5) lazily connects a single server named by `serverName`. It is
// called from the forwarding path when an mcp__ tool is dispatched but the
// manager has no live connection for that server yet. The call is bounded by
// lazyStartTimeout so a wedged server cannot stall the calling turn; on
// failure it surfaces an H8-style unavailable message and flips the status to
// `error` so the next call retries through a real Test.
func (m *Manager) EnsureLive(ctx context.Context, serverName string) error {
	if m.HasLiveServer(serverName) {
		return nil
	}
	m.setStatus(serverName, "connecting", "Connecting...", 0)
	connectCtx, cancel := context.WithTimeout(ctx, lazyStartTimeout)
	defer cancel()
	done := make(chan struct{})
	var refreshErr error
	go func() {
		defer close(done)
		m.refresh(connectCtx, serverName)
	}()
	select {
	case <-done:
	case <-connectCtx.Done():
		refreshErr = fmt.Errorf("MCP server %s did not start in %s", serverName, lazyStartTimeout)
	}
	if m.HasLiveServer(serverName) {
		return nil
	}
	if refreshErr == nil {
		// This text is read by the MODEL as a tool result, not by a Go caller
		// wrapping it: the sentences and the terminal period are the instruction.
		//lint:ignore ST1005 model-facing instruction, not a wrapped Go error
		refreshErr = fmt.Errorf("MCP tool %s is unavailable (server disconnected). Do not retry it this task; use another approach.", serverName)
	}
	m.setStatus(serverName, "error", refreshErr.Error(), 0)
	return refreshErr
}

// HasLiveServer reports whether `serverName` currently has an open MCP
// session. Used by M5's lazy-connect gate.
func (m *Manager) HasLiveServer(serverName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, connection := range m.connections {
		if connection.server.Name == serverName {
			return true
		}
	}
	return false
}

func (m *Manager) Refresh(ctx context.Context, deadline time.Duration) {
	m.refreshInternal(ctx, deadline, false, "")
}

// RefreshBlocking (M1) is the explicit user-initiated variant: it ignores any
// short implicit deadline and blocks until the underlying refresh completes
// (or the caller's context is cancelled). Authorize/Test rely on this so the
// modal they re-render after the call shows the new state, not "connecting".
func (m *Manager) RefreshBlocking(ctx context.Context) {
	m.refreshInternal(ctx, 0, true, "")
}

func (m *Manager) refreshInternal(ctx context.Context, deadline time.Duration, blocking bool, serverName string) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if !blocking && m.dedup != nil && time.Since(m.dedup.started) < debounceWindow && m.dedup.serverFP == serverName {
		done := m.dedup.done
		m.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	if m.refreshStop != nil {
		m.refreshStop()
	}
	refreshCtx, cancel := context.WithCancel(ctx)
	m.refreshStop = cancel
	done := make(chan struct{})
	m.dedup = &refreshDedup{started: time.Now(), done: done, serverFP: serverName}
	m.mu.Unlock()
	go func() {
		defer close(done)
		m.refresh(refreshCtx, serverName)
	}()
	if deadline <= 0 || blocking {
		// D3: bound the blocking wait on the caller's context too. With a plain
		// session context this only returns on shutdown (unchanged behaviour);
		// with a 30s-timeout context (Authorize/Test) it guarantees the wait
		// returns even if a transport ignores cancellation. The connect goroutine
		// is cancelled via refreshCtx (derived from ctx), so it does not leak.
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// refresh runs a connect round. If onlyName is empty, every enabled server is
// (re)connected. Otherwise only that one server is targeted — used by M5's
// lazy path so a single tool call never restarts every connection.
func (m *Manager) refresh(ctx context.Context, onlyName string) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	m.mu.Lock()
	previous := m.connections
	if onlyName == "" {
		m.connections = nil
		m.tools = make(map[string]*mcpTool)
		// M3: do NOT reset statuses wholesale — entries for configured servers
		// would briefly vanish and `mcpServerInfos` would render "not connected".
		// Set per-server "connecting" entries as each goroutine starts.
		m.usedNames = make(map[string]bool)
	} else {
		filtered := previous[:0]
		for _, connection := range previous {
			if connection.server.Name != onlyName {
				filtered = append(filtered, connection)
				continue
			}
			_ = connection.session.Close()
			for _, tool := range connection.tools {
				delete(m.tools, tool.exposedName)
			}
		}
		m.connections = filtered
	}
	m.mu.Unlock()

	config, err := state.LoadMCPConfig(m.paths)
	if err != nil {
		m.setStatus("config", "error", err.Error(), 0)
		return
	}
	secrets, err := state.LoadMCPSecrets(m.paths)
	if err != nil {
		m.setStatus("secrets", "error", err.Error(), 0)
		return
	}
	var wait sync.WaitGroup
	for _, server := range config.Servers {
		server := server
		if onlyName != "" && server.Name != onlyName {
			continue
		}
		if !server.Enabled {
			m.setStatus(server.Name, "disabled", "Server is disabled.", 0)
			continue
		}
		if server.Transport == "http" && server.OAuth != nil && server.OAuth.Enabled && !hasOAuthCredentials(secrets.OAuth[server.Name]) {
			m.setStatus(server.Name, "auth_required", "Run `muhiyacode mcp auth "+server.Name+"`.", 0)
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			m.setStatus(server.Name, "connecting", "Connecting...", 0)
			connection, err := m.connect(ctx, server, secrets)
			if err != nil {
				// M6: typed sentinel detection. ErrAuthExpired covers the
				// "token valid but no refresh" case. Otherwise fall back to
				// the legacy substring match for transports that report 401
				// in different formats.
				stateName := "error"
				if errors.Is(err, ErrAuthExpired) || strings.Contains(strings.ToLower(err.Error()), "401") || strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
					stateName = "auth_required"
				}
				m.setStatus(server.Name, stateName, err.Error(), 0)
				return
			}
			m.mu.Lock()
			m.connections = append(m.connections, connection)
			for _, tool := range connection.tools {
				m.tools[tool.exposedName] = tool
			}
			m.mu.Unlock()
			m.setStatus(server.Name, "connected", fmt.Sprintf("Connected with %d tool(s).", len(connection.tools)), len(connection.tools))
		}()
	}
	wait.Wait()
}

func (m *Manager) connect(parent context.Context, server state.MCPServer, secrets state.MCPSecrets) (*connection, error) {
	timeout := time.Duration(server.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "MuhiyaCode", Version: "1.0.0"}, nil)
	var transport mcp.Transport
	if server.Transport == "stdio" {
		cmd, commandErr := m.stdioCommand(server)
		if commandErr != nil {
			return nil, commandErr
		}
		cmd.Env = processenv.Sanitized(os.Environ(), server.Env, secrets.Env[server.Name])
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}
	} else {
		token, tokenErr := m.oauthAccessToken(ctx, server.Name, secrets.OAuth[server.Name])
		if tokenErr != nil {
			return nil, tokenErr
		}
		httpClient, scopeErr := destinationScopedClient(m.httpClient, server.URL, token)
		if scopeErr != nil {
			return nil, scopeErr
		}
		transport = &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: httpClient, MaxRetries: 1, DisableStandaloneSSE: true}
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP %s: %w", server.Name, err)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("list MCP tools for %s: %w", server.Name, err)
	}
	connection := &connection{server: server, session: session}
	sort.Slice(listed.Tools, func(i, j int) bool { return listed.Tools[i].Name < listed.Tools[j].Name })
	used := make(map[string]bool)
	for _, remote := range listed.Tools {
		name := deterministicToolName("mcp__"+safeName(server.Name)+"__"+safeName(remote.Name), used)
		// readOnlyHint is advisory. The SDK decodes it as a plain bool, so an
		// absent annotation and an explicit false are the same value — which is
		// the behavior we want: only an explicit true opens the tool to the
		// planner (contract.ReadOnlyDeclaring explains why trusting it is safe).
		readOnly := remote.Annotations != nil && remote.Annotations.ReadOnlyHint
		tool := &mcpTool{manager: m, connection: connection, exposedName: name, remoteName: remote.Name, readOnly: readOnly, definition: canonicalDefinition(contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: "[MCP:" + server.Name + "] " + firstNonempty(remote.Description, remote.Name), Parameters: normalizeSchema(remote.InputSchema)}})}
		connection.tools = append(connection.tools, tool)
	}
	fingerprint := state.MCPServerFingerprint(server, secrets.Env[server.Name])
	definitions := make([]contract.ToolDefinition, 0, len(connection.tools))
	for _, tool := range connection.tools {
		definitions = append(definitions, tool.definition)
	}
	if m.surfaceStore != nil {
		if err := m.surfaceStore.Put(state.ToolSurfaceSnapshot{Fingerprint: fingerprint, Server: server.Name, Tools: definitions, CapturedAt: time.Now().UTC()}); err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("persist MCP tool surface for %s: %w", server.Name, err)
		}
	}
	m.mu.Lock()
	if !m.knownAtStart[fingerprint] {
		for _, definition := range definitions {
			m.pinned[definition.Function.Name] = canonicalDefinition(definition)
		}
		m.knownAtStart[fingerprint] = true
		m.pendingSurface = true
		m.pendingScopes = append(m.pendingScopes, "first handshake added MCP server "+server.Name)
	}
	m.mu.Unlock()
	return connection, nil
}

func (m *Manager) stdioCommand(server state.MCPServer) (*exec.Cmd, error) {
	m.mu.RLock()
	backend, workspaceRoot, unsafeHost := m.sandbox, m.workspace, m.unsafeHost
	m.mu.RUnlock()
	cwd := server.CWD
	if cwd == "" {
		cwd = workspaceRoot
	}
	arguments := append([]string{server.Command}, server.Args...)
	if backend != nil && !unsafeHost {
		capability := backend.Capability()
		if !capability.Enforced {
			return nil, fmt.Errorf("%w for MCP %s: %s", workspacepolicy.ErrSandboxUnavailable, server.Name, capability.Reason)
		}
		wrapped, err := backend.Wrap(workspacepolicy.SandboxRequest{Arguments: arguments, CWD: cwd})
		if err != nil {
			return nil, fmt.Errorf("sandbox MCP %s: %w", server.Name, err)
		}
		arguments = wrapped
	}
	if len(arguments) == 0 {
		return nil, fmt.Errorf("MCP %s has an empty command", server.Name)
	}
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = cwd
	return command, nil
}

type destinationTransport struct {
	allowed *url.URL
	base    http.RoundTripper
}

func destinationScopedClient(client *http.Client, endpoint, token string) (*http.Client, error) {
	allowed, err := url.Parse(endpoint)
	if err != nil || allowed.Scheme == "" || allowed.Host == "" {
		return nil, fmt.Errorf("invalid MCP endpoint %q", endpoint)
	}
	base := transportOf(client)
	if token != "" {
		base = bearerTransport{base: base, token: token}
	}
	scope := destinationTransport{allowed: allowed, base: base}
	clone := *client
	clone.Transport = scope
	clone.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		return scope.authorize(request.URL)
	}
	return &clone, nil
}

func (transport destinationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := transport.authorize(request.URL); err != nil {
		return nil, err
	}
	return transport.base.RoundTrip(request)
}

func (transport destinationTransport) authorize(target *url.URL) error {
	if !strings.EqualFold(target.Scheme, transport.allowed.Scheme) ||
		!strings.EqualFold(target.Hostname(), transport.allowed.Hostname()) ||
		effectivePort(target) != effectivePort(transport.allowed) {
		return fmt.Errorf("MCP network policy denied destination %s", target.Redacted())
	}
	return nil
}

func effectivePort(target *url.URL) string {
	if target.Port() != "" {
		return target.Port()
	}
	if strings.EqualFold(target.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(target.Scheme, "http") {
		return "80"
	}
	return ""
}

func (m *Manager) Tools() []contract.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]contract.Tool, 0, len(names))
	for _, name := range names {
		result = append(result, m.tools[name])
	}
	return result
}

func (m *Manager) Statuses() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Status, 0, len(m.statuses))
	for _, status := range m.statuses {
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (m *Manager) Errors() []string {
	var result []string
	for _, status := range m.Statuses() {
		if status.State == "error" || status.State == "auth_required" {
			result = append(result, status.Name+": "+status.Message)
		}
	}
	return result
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	if m.refreshStop != nil {
		m.refreshStop()
	}
	m.mu.Unlock()
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	connections := m.connections
	m.connections = nil
	m.tools = make(map[string]*mcpTool)
	m.usedNames = make(map[string]bool)
	m.mu.Unlock()
	var first error
	for _, connection := range connections {
		if err := connection.session.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// markServerError (M8) drops a server's live connection and marks its status
// as error so the lazy M5 path retries once on the next tool call. Other
// servers are not touched.
func (m *Manager) markServerError(name string, msg string) {
	m.mu.Lock()
	for index, connection := range m.connections {
		if connection.server.Name == name {
			m.connections = append(m.connections[:index], m.connections[index+1:]...)
			_ = connection.session.Close()
			// T031/REV D1: also drop this server's tool entries from m.tools,
			// mirroring refresh(). Without this the forwarding tool keeps finding
			// the stale live tool (pointing at the now-closed session) and calls
			// the dead session on every retry — the M5 lazy reconnect (EnsureLive,
			// which only runs when the live lookup is nil) is never reached, so the
			// server can never recover without a manual Test.
			for _, tool := range connection.tools {
				delete(m.tools, tool.exposedName)
			}
			break
		}
	}
	m.mu.Unlock()
	m.setStatus(name, "error", msg, 0)
}

func (m *Manager) setStatus(name, stateName, message string, count int) {
	m.mu.Lock()
	m.statuses[name] = Status{Name: name, State: stateName, Message: message, ToolCount: count, UpdatedAt: time.Now()}
	m.mu.Unlock()
}
