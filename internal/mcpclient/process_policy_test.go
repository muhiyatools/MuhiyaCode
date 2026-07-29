package mcpclient

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/muhiya/muhiyacode/internal/state"
	workspacepolicy "github.com/muhiya/muhiyacode/internal/workspace"
)

type mcpSandboxFixture struct {
	capability workspacepolicy.SandboxCapability
}

func (fixture mcpSandboxFixture) Capability() workspacepolicy.SandboxCapability {
	return fixture.capability
}

func (fixture mcpSandboxFixture) Wrap(request workspacepolicy.SandboxRequest) ([]string, error) {
	return append([]string{"sandbox-wrapper", "--"}, request.Arguments...), nil
}

func TestStdioMCPUsesConfiguredSandbox(t *testing.T) {
	manager := New(state.Paths{}, nil, nil, nil)
	manager.SetProcessSandbox(mcpSandboxFixture{capability: workspacepolicy.SandboxCapability{
		Backend:       "fixture",
		Enforced:      true,
		NetworkDenied: true,
	}}, t.TempDir(), false)
	command, err := manager.stdioCommand(state.MCPServer{
		Name: "fixture", Command: "server", Args: []string{"--stdio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Path != "sandbox-wrapper" || len(command.Args) != 4 {
		t.Fatalf("stdio command was not wrapped: path=%q args=%v", command.Path, command.Args)
	}
}

func TestStdioMCPFailsClosedWhenSandboxUnavailable(t *testing.T) {
	manager := New(state.Paths{}, nil, nil, nil)
	manager.SetProcessSandbox(mcpSandboxFixture{capability: workspacepolicy.SandboxCapability{
		Backend: "fixture",
		Reason:  "missing",
	}}, t.TempDir(), false)
	_, err := manager.stdioCommand(state.MCPServer{Name: "fixture", Command: "server"})
	if !errors.Is(err, workspacepolicy.ErrSandboxUnavailable) {
		t.Fatalf("expected sandbox unavailable, got %v", err)
	}
}

func TestMCPDestinationPolicyPinsProtocolHostAndPort(t *testing.T) {
	client, err := destinationScopedClient(&http.Client{}, "https://api.example.test:8443/mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(destinationTransport)
	for _, allowed := range []string{
		"https://api.example.test:8443/other",
		"https://API.EXAMPLE.TEST:8443/mcp",
	} {
		target, _ := url.Parse(allowed)
		if err := transport.authorize(target); err != nil {
			t.Fatalf("expected %s to be allowed: %v", allowed, err)
		}
	}
	for _, denied := range []string{
		"http://api.example.test:8443/mcp",
		"https://evil.example.test:8443/mcp",
		"https://api.example.test:9443/mcp",
	} {
		target, _ := url.Parse(denied)
		if err := transport.authorize(target); err == nil {
			t.Fatalf("expected %s to be denied", denied)
		}
	}
}
