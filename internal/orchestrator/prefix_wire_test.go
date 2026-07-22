package orchestrator

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// The Prefix-bytes WIRE golden: the exact cached region a request carries —
// the rendered system prompt plus the serialized tool definitions. The
// instructions package pins its own texts; this pins what the ENGINE actually
// composes from them, which is the thing a provider's prefix cache matches on.
//
// It is the epoch artifact: any change here is a one-time cache invalidation
// for every session, so a stale golden must fail loudly rather than drift.
//
// Recreated after the unified-session change — the previous copy lived in a
// wiring test that also cross-checked subagent tool allowlists, which no longer
// exist. The golden's purpose (byte-level cache accounting) outlived them.
var updatePrefixGolden = flag.Bool("update-prefix-golden", false, "regenerate the Prefix-bytes wire golden")

func TestWiring_PrefixBytesGolden(t *testing.T) {
	ctx := stablePromptContext()
	settings := engineSettings()
	// A REAL workspace registry: the wire prefix is dominated by the workspace
	// tools' schemas, so a golden built on an empty registry would pin a fiction.
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "prefix-golden", WorkspacePath: dir},
		Provider: &scriptedProvider{}, Registry: NewRegistry(service.Tools()...),
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := SystemPrompt(ctx)
	tools, err := json.MarshalIndent(engine.sessionDefinitions(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("=== SYSTEM PROMPT (%d chars) ===\n%s\n\n=== TOOL DEFINITIONS JSON (%d bytes) ===\n%s\n", len(prompt), prompt, len(tools), tools)

	path := filepath.Join("testdata", "prefix_bytes_wire.golden")
	if *updatePrefixGolden {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-prefix-golden to create it)", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(got, "\r\n", "\n") {
		t.Errorf("Prefix wire golden %s is stale — this is the recorded cache-epoch artifact: confirm the diff is an intended prefix change, then rerun with -update-prefix-golden", path)
	}
}

func TestWiring_BalancedPrefixBytesGolden(t *testing.T) {
	ctx := stablePromptContext()
	ctx.Lean = true
	settings := engineSettings()
	settings.TokenEconomyMode = "balanced"
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{Settings: &settings,
		Session:  contract.Session{ID: "balanced-prefix-golden", WorkspacePath: dir},
		Provider: &scriptedProvider{}, Registry: NewRegistry(service.Tools()...)})
	if err != nil {
		t.Fatal(err)
	}
	prompt := SystemPrompt(ctx)
	tools, err := json.MarshalIndent(engine.sessionDefinitions(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("=== SYSTEM PROMPT (%d chars) ===\n%s\n\n=== TOOL DEFINITIONS JSON (%d bytes) ===\n%s\n", len(prompt), prompt, len(tools), tools)
	path := filepath.Join("testdata", "prefix_bytes_wire_balanced.golden")
	if *updatePrefixGolden {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-prefix-golden to create it)", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(got, "\r\n", "\n") {
		t.Errorf("balanced Prefix wire golden %s is stale; confirm the deliberate prompt/tool epoch then update once", path)
	}
}
