package mcpclient

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFormatResultPreservesMCPErrorContent(t *testing.T) {
	result := &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: "remote validation failed"},
		},
	}

	output := formatResult(result)
	if !strings.Contains(output, "MCP tool reported an error") {
		t.Fatalf("error result must identify the MCP failure: %q", output)
	}
	if !strings.Contains(output, "remote validation failed") {
		t.Fatalf("error result must preserve the server's diagnostic: %q", output)
	}
}
