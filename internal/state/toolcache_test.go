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
	if MCPServerFingerprint(serverA, map[string]string{"TOKEN": "x"}) != MCPServerFingerprint(serverB, map[string]string{"TOKEN": "x"}) {
		t.Fatal("environment map order changed fingerprint")
	}
	paths := testPaths(t)
	store, err := NewToolSurfaceStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := MCPServerFingerprint(serverA, nil)
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
	got, ok := store.Get(fingerprint)
	if !ok || got.WebSearch != ProbeSupported {
		t.Fatalf("snapshot=%+v ok=%v", got, ok)
	}
}
