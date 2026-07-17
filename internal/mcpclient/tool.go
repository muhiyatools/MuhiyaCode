package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muhiya/muhiyacode/internal/contract"
)

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
