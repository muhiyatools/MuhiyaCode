// Package mcpclient integrates external MCP tool servers behind the same
// contract.Tool boundary used by MuhiyaCode's local workspace tools.
package mcpclient

import (
	"context"
	"encoding/json"
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

	mu          sync.RWMutex
	refreshMu   sync.Mutex
	secretsMu   sync.Mutex
	refreshStop context.CancelFunc
	closed      bool
	connections []*connection
	tools       map[string]*mcpTool
	statuses    map[string]Status
	usedNames   map[string]bool
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

func New(paths state.Paths, client *http.Client, confirm ConfirmFunc, onTool func(contract.Tool)) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Manager{paths: paths, httpClient: client, confirm: confirm, onTool: onTool, tools: make(map[string]*mcpTool), statuses: make(map[string]Status), usedNames: make(map[string]bool)}
}

func (m *Manager) Refresh(ctx context.Context, deadline time.Duration) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.refreshStop != nil {
		m.refreshStop()
	}
	refreshCtx, cancel := context.WithCancel(ctx)
	m.refreshStop = cancel
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.refresh(refreshCtx)
	}()
	if deadline <= 0 {
		<-done
		return
	}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func (m *Manager) refresh(ctx context.Context) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	m.mu.Lock()
	previous := m.connections
	m.connections = nil
	m.tools = make(map[string]*mcpTool)
	m.statuses = make(map[string]Status)
	m.usedNames = make(map[string]bool)
	m.mu.Unlock()
	for _, connection := range previous {
		_ = connection.session.Close()
	}

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
				stateName := "error"
				if strings.Contains(strings.ToLower(err.Error()), "401") || strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
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
			for _, tool := range connection.tools {
				if m.onTool != nil {
					m.onTool(tool)
				}
			}
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
	for _, remote := range listed.Tools {
		name := m.uniqueName("mcp__" + safeName(server.Name) + "__" + safeName(remote.Name))
		tool := &mcpTool{manager: m, connection: connection, exposedName: name, remoteName: remote.Name, definition: contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: "[MCP:" + server.Name + "] " + firstNonempty(remote.Description, remote.Name), Parameters: normalizeSchema(remote.InputSchema)}}}
		connection.tools = append(connection.tools, tool)
	}
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
		return "", err
	}
	return formatResult(result), nil
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
