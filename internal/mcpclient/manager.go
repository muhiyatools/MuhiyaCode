// Package mcpclient integrates external MCP tool servers behind the same
// contract.Tool boundary used by MuhiyaCode's local workspace tools.
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
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

type connection struct {
	server  state.MCPServer
	session *mcp.ClientSession
	tools   []*mcpTool
}

type mcpTool struct {
	manager     *Manager
	connection  *connection
	exposedName string
	remoteName  string
	definition  contract.ToolDefinition
}

type forwardingTool struct {
	manager    *Manager
	definition contract.ToolDefinition
}

type BoundaryChange struct {
	Tools []contract.Tool
	Scope string
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

// connectionForTest returns the live connection pointer for `serverName` so
// tests can prove that EnsureLive/replay paths do not merge or replace an
// existing connection. Not intended for production callers.
func (m *Manager) connectionForTest(serverName string) *connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, connection := range m.connections {
		if connection.server.Name == serverName {
			return connection
		}
	}
	return nil
}

// serverForTool extracts the MCP server name from a tool name. Names follow
// the `mcp__<server>__<tool>` convention; the prefix is mandatory whenever a
// tool name has been registered through the MCP machinery.
func serverForTool(toolName string) string {
	rest := strings.TrimPrefix(toolName, "mcp__")
	if rest == toolName {
		return ""
	}
	index := strings.Index(rest, "__")
	if index <= 0 {
		return ""
	}
	return rest[:index]
}

// PinnedTools loads the deterministic cached surface before the session's
// first request. Live handshakes do not mutate this surface for known servers.
func (m *Manager) PinnedTools() ([]contract.Tool, error) {
	definitions, known, err := m.cachedDefinitions()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.pinned = definitions
	m.applied = cloneDefinitionsByName(definitions)
	m.knownAtStart = known
	tools := m.forwardingToolsLocked()
	m.mu.Unlock()
	return tools, nil
}

// RequestSurfaceRefresh asks the next task boundary to adopt the effective
// cached surface after an explicit mutating /mcp action.
func (m *Manager) RequestSurfaceRefresh(scope string) {
	m.mu.Lock()
	m.pendingSurface = true
	m.pendingForce = true
	if strings.TrimSpace(scope) != "" {
		m.pendingScopes = append(m.pendingScopes, scope)
	}
	m.mu.Unlock()
}

func (m *Manager) TakeBoundaryChange() (BoundaryChange, bool, error) {
	m.mu.RLock()
	pending, force := m.pendingSurface, m.pendingForce
	m.mu.RUnlock()
	if !pending {
		return BoundaryChange{}, false, nil
	}
	var desired map[string]contract.ToolDefinition
	if force {
		loaded, _, err := m.cachedDefinitions()
		if err != nil {
			return BoundaryChange{}, false, err
		}
		desired = loaded
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.pendingSurface {
		return BoundaryChange{}, false, nil
	}
	if desired != nil {
		m.pinned = desired
	}
	changed := !definitionsEqual(m.applied, m.pinned)
	scope := strings.Join(uniqueStrings(m.pendingScopes), "; ")
	m.pendingSurface, m.pendingForce, m.pendingScopes = false, false, nil
	if !changed {
		return BoundaryChange{}, false, nil
	}
	m.applied = cloneDefinitionsByName(m.pinned)
	if scope == "" {
		scope = "MCP tool surface changed"
	}
	return BoundaryChange{Tools: m.forwardingToolsLocked(), Scope: scope}, true, nil
}

func (m *Manager) cachedDefinitions() (map[string]contract.ToolDefinition, map[string]bool, error) {
	return m.surfaceForCurrent(true)
}

// surfaceForCurrent rebuilds the cached surface for the active MCP config,
// optionally limiting the work to the "known" servers (those already on disk
// from past sessions). knownOnly=false prefills the `known` map with every
// enabled server — useful so M5 can decide whether the lazy path is safe.
func (m *Manager) surfaceForCurrent(knownOnly bool) (map[string]contract.ToolDefinition, map[string]bool, error) {
	config, err := state.LoadMCPConfig(m.paths)
	if err != nil {
		return nil, nil, err
	}
	secrets, err := state.LoadMCPSecrets(m.paths)
	if err != nil {
		return nil, nil, err
	}
	definitions := make(map[string]contract.ToolDefinition)
	known := make(map[string]bool)
	if m.surfaceStore == nil {
		return definitions, known, nil
	}
	for _, server := range config.Servers {
		if !server.Enabled {
			continue
		}
		fingerprint := state.MCPServerFingerprint(server, secrets.Env[server.Name])
		snapshot, ok := m.surfaceStore.Get(fingerprint)
		if knownOnly {
			known[fingerprint] = ok
		} else if !ok {
			continue
		}
		for _, definition := range snapshot.Tools {
			definitions[definition.Function.Name] = canonicalDefinition(definition)
		}
	}
	return definitions, known, nil
}

// AllConfiguredServersHaveSurface (M5) reports whether every enabled MCP
// server in the current config has a cached pinned surface. Session boot can
// safely skip the eager Refresh and rely on the lazy per-tool path when this
// returns true. Unknown auth state or store errors fall back to "no" to avoid
// suppressing the warm-up round.
func (m *Manager) AllConfiguredServersHaveSurface() bool {
	_, known, err := m.surfaceForCurrent(false)
	if err != nil || len(known) == 0 {
		return false
	}
	for _, ok := range known {
		if !ok {
			return false
		}
	}
	return true
}

func (m *Manager) forwardingToolsLocked() []contract.Tool {
	names := make([]string, 0, len(m.pinned))
	for name := range m.pinned {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]contract.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, &forwardingTool{manager: m, definition: canonicalDefinition(m.pinned[name])})
	}
	return tools
}

// refreshDedup tracks the last refresh start time so M7 can short-circuit
// near-simultaneous callers instead of cancelling the in-flight round.
type refreshDedup struct {
	started  time.Time
	done     chan struct{}
	serverFP string
}

var debounceWindow = time.Second

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
		cmd := exec.Command(server.Command, server.Args...)
		cmd.Dir = server.CWD
		cmd.Env = os.Environ()
		for key, value := range server.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		for key, value := range secrets.Env[server.Name] {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}
	} else {
		httpClient := m.httpClient
		token, tokenErr := m.oauthAccessToken(ctx, server.Name, secrets.OAuth[server.Name])
		if tokenErr != nil {
			return nil, tokenErr
		}
		if token != "" {
			clone := *m.httpClient
			clone.Transport = bearerTransport{base: transportOf(m.httpClient), token: token}
			httpClient = &clone
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
		tool := &mcpTool{manager: m, connection: connection, exposedName: name, remoteName: remote.Name, definition: canonicalDefinition(contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: "[MCP:" + server.Name + "] " + firstNonempty(remote.Description, remote.Name), Parameters: normalizeSchema(remote.InputSchema)}})}
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

func (t *mcpTool) Definition() contract.ToolDefinition { return t.definition }

func (t *forwardingTool) Definition() contract.ToolDefinition { return t.definition }

func (t *forwardingTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	name := t.definition.Function.Name
	t.manager.mu.RLock()
	live := t.manager.tools[name]
	t.manager.mu.RUnlock()
	if live != nil {
		return live.Execute(ctx, arguments)
	}
	// M5 lazy-connect: the model surfaced this tool from the cached pinned
	// surface but the live session is not open yet. Connect just this
	// server on demand so an unused MCP server still costs nothing at boot.
	server := serverForTool(name)
	if server == "" {
		return "", fmt.Errorf("MCP tool %s is unavailable (server disconnected). Do not retry it this task; use another approach.", name)
	}
	if err := t.manager.EnsureLive(ctx, server); err != nil {
		return "", err
	}
	t.manager.mu.RLock()
	live = t.manager.tools[name]
	t.manager.mu.RUnlock()
	if live == nil {
		return "", fmt.Errorf("MCP tool %s is unavailable (server disconnected). Do not retry it this task; use another approach.", name)
	}
	return live.Execute(ctx, arguments)
}

func (t *mcpTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	if t.manager.confirm != nil {
		approved, err := t.manager.confirm(ctx, "Allow MCP tool "+t.exposedName+"?")
		if err != nil || !approved {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("permission denied")
		}
	}
	var args map[string]any
	if len(arguments) > 0 && json.Unmarshal(arguments, &args) != nil {
		return "", fmt.Errorf("invalid MCP tool arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.connection.server.TimeoutMS)*time.Millisecond)
	defer cancel()
	result, err := t.connection.session.CallTool(ctx, &mcp.CallToolParams{Name: t.remoteName, Arguments: args})
	if err != nil {
		// M8: transport-layer failures (closed connection / EOF / broken
		// pipe) flip to error state and drop the live connection so the
		// next call retries once through the M5 lazy path. Other errors
		// leave the connection alone.
		if isTransportClosed(err) {
			t.manager.markServerError(t.connection.server.Name, t.connection.server.Name+": "+err.Error())
		}
		return "", err
	}
	return formatResult(result), nil
}

// isTransportClosed (M8) sniffs the few common transport-level failure shapes;
// conservatively returns true only when the error text matches a closed /
// EOF / broken-pipe shape, not on every API error.
func isTransportClosed(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "broken pipe") || strings.Contains(s, "eof") || strings.Contains(s, "connection closed") || strings.Contains(s, "connection reset")
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

func formatResult(result *mcp.CallToolResult) string {
	var parts []string
	if result.IsError {
		parts = append(parts, "MCP tool reported an error.")
	}
	if result.StructuredContent != nil {
		payload, _ := json.MarshalIndent(result.StructuredContent, "", "  ")
		parts = append(parts, "structuredContent:\n"+string(payload))
	}
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		} else if payload, err := json.MarshalIndent(content, "", "  "); err == nil {
			parts = append(parts, string(payload))
		}
	}
	if len(parts) == 0 {
		return "MCP tool completed with no content."
	}
	return strings.Join(parts, "\n\n")
}

func (m *Manager) setStatus(name, stateName, message string, count int) {
	m.mu.Lock()
	m.statuses[name] = Status{Name: name, State: stateName, Message: message, ToolCount: count, UpdatedAt: time.Now()}
	m.mu.Unlock()
}

func (m *Manager) uniqueName(base string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(base) > 64 {
		base = base[:64]
	}
	if !m.usedNames[base] {
		m.usedNames[base] = true
		return base
	}
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("_%d", index)
		name := base[:min(len(base), 64-len(suffix))] + suffix
		if !m.usedNames[name] {
			m.usedNames[name] = true
			return name
		}
	}
}

func normalizeSchema(value any) map[string]any {
	if schema, ok := value.(map[string]any); ok {
		if _, exists := schema["type"]; !exists {
			schema["type"] = "object"
		}
		if _, exists := schema["properties"]; !exists {
			schema["properties"] = map[string]any{}
		}
		return schema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}
}

func canonicalDefinition(definition contract.ToolDefinition) contract.ToolDefinition {
	payload, _ := json.Marshal(definition)
	var result contract.ToolDefinition
	_ = json.Unmarshal(payload, &result)
	if result.Type == "" {
		result.Type = "function"
	}
	result.Function.Parameters = normalizeSchema(result.Function.Parameters)
	return result
}

func deterministicToolName(base string, used map[string]bool) string {
	if len(base) > 64 {
		base = base[:64]
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("_%d", index)
		name := base[:min(len(base), 64-len(suffix))] + suffix
		if !used[name] {
			used[name] = true
			return name
		}
	}
}

func cloneDefinitionsByName(values map[string]contract.ToolDefinition) map[string]contract.ToolDefinition {
	result := make(map[string]contract.ToolDefinition, len(values))
	for name, definition := range values {
		result[name] = canonicalDefinition(definition)
	}
	return result
}

func definitionsEqual(left, right map[string]contract.ToolDefinition) bool {
	if len(left) != len(right) {
		return false
	}
	for name, definition := range left {
		other, ok := right[name]
		if !ok {
			return false
		}
		a, _ := json.Marshal(definition)
		b, _ := json.Marshal(other)
		if string(a) != string(b) {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func transportOf(client *http.Client) http.RoundTripper {
	if client.Transport != nil {
		return client.Transport
	}
	return http.DefaultTransport
}

func hasOAuthCredentials(secret map[string]any) bool {
	if secret == nil {
		return false
	}
	tokens, _ := secret["tokens"].(map[string]any)
	if tokens == nil {
		tokens = secret
	}
	for _, key := range []string{"access_token", "accessToken", "refresh_token", "refreshToken"} {
		if value, ok := tokens[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func safeName(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
	}
	value = strings.Trim(builder.String(), "_")
	if value == "" {
		return "tool"
	}
	return value
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "MCP tool"
}
