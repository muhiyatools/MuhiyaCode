package state

import (
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestMCPFingerprintSortsEnvironmentAndToolStorePersists(t *testing.T) {
	serverA := MCPServer{Transport: "stdio", Name: "demo", Command: "server", Args: []string{"--x"}, Env: map[string]string{"B": "2", "A": "1"}}
	serverB := serverA
	serverB.Env = map[string]string{"A": "1", "B": "2"}
	if MCPServerFingerprint(serverA, map[string]string{"TOKEN": "x"}, nil) != MCPServerFingerprint(serverB, map[string]string{"TOKEN": "x"}, nil) {
		t.Fatal("environment map order changed fingerprint")
	}
	paths := testPaths(t)
	store, err := NewToolSurfaceStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := MCPServerFingerprint(serverA, nil, nil)
	snapshot := ToolSurfaceSnapshot{Fingerprint: fingerprint, Server: "demo", CapturedAt: time.Unix(1, 0), Tools: []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{Name: "mcp__demo__one", Parameters: map[string]any{"type": "object"}}}}}
	if err := store.Put(snapshot); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewToolSurfaceStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(fingerprint)
	if !ok || len(got.Tools) != 1 || got.Tools[0].Function.Name != "mcp__demo__one" {
		t.Fatalf("snapshot=%+v ok=%v", got, ok)
	}
}

func TestMCPFingerprintSeparatesLogicalServerAndOAuthScope(t *testing.T) {
	base := MCPServer{Transport: "http", Name: "one", URL: "https://example.test/mcp", OAuth: &MCPOAuth{Enabled: true, Scope: "read"}}
	renamed := base
	renamed.Name = "two"
	if MCPServerFingerprint(base, nil, nil) == MCPServerFingerprint(renamed, nil, nil) {
		t.Fatal("logical servers with different exposed-name namespaces shared a surface fingerprint")
	}
	rescope := base
	rescope.OAuth = &MCPOAuth{Enabled: true, Scope: "admin"}
	if MCPServerFingerprint(base, nil, nil) == MCPServerFingerprint(rescope, nil, nil) {
		t.Fatal("OAuth scope changes must invalidate account-scoped tool surfaces")
	}
	accountA := map[string]any{"tokens": map[string]any{"refresh_token": "account-a"}}
	accountB := map[string]any{"tokens": map[string]any{"refresh_token": "account-b"}}
	if MCPServerFingerprint(base, nil, accountA) == MCPServerFingerprint(base, nil, accountB) {
		t.Fatal("different OAuth accounts must not share a cached tool surface")
	}
}

func TestProbeStoreLastGoodIsCallerControlled(t *testing.T) {
	paths := testPaths(t)
	store, err := NewProbeStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := ProbeFingerprint("https://example/v1", "secret")
	if err := store.Put(ProbeSnapshot{Fingerprint: fingerprint, WebSearch: ProbeSupported, CheckedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewProbeStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(fingerprint)
	if !ok || got.WebSearch != ProbeSupported {
		t.Fatalf("snapshot=%+v ok=%v", got, ok)
	}
}
