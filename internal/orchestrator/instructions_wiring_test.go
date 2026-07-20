package orchestrator

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/instructions"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// updatePrefixGolden regenerates TestWiring_PrefixBytesGolden's golden file:
// `go test ./internal/orchestrator/... -run TestWiring_PrefixBytesGolden
// -update-prefix-golden`. Named distinctly from internal/instructions'
// `-update` flag (separate test binaries; no collision) so it is obvious
// from the flag name alone which golden a regeneration touches.
var updatePrefixGolden = flag.Bool("update-prefix-golden", false, "regenerate the Prefix-bytes wire golden")

// Feature 010 US3 (IS-6): internal/instructions/audit_test.go mirrors the real
// capability-reference allowlists locally, because that package imports only
// contract and cannot reach orchestrator's unexported subagentSpecs. This file
// is the other half: it builds the REAL
// registry the production binary uses (workspace.New(...).Tools(), exactly
// as internal/command/application.go does) and cross-checks every
// instructions.Text against the LIVE data, so a drift between the mirror and
// the real implementation fails here even if the mirror itself still looks
// internally consistent.

// realSubagentSpecs builds an Engine wired to the production workspace tool
// set (no provider, no session — subagentSpecs only reads e.registry) and
// returns its real subagentSpecs() map, the same one runSubagentInput uses.
func realSubagentSpecs(t *testing.T) map[string]subagentSpec {
	t.Helper()
	ws, err := workspace.New(t.TempDir(), workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: workspace.NewMemoryTrustStore()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	registry := NewRegistry(ws.Tools()...)
	e := &Engine{registry: registry}
	return e.subagentSpecs()
}

// TestWiring_CapabilityReferenceMatchesRealSubagentSpecs cross-checks EVERY
// instructions.Text with a subagent AllowlistCtx against the REAL
// subagentSpecs() Allowed map for that kind (IS-6, "wired from real
// allowlists"). This is what actually verifies fix (a): if the general
// subagent's real Allowed map ever grows to include a synthetic tool (a
// future regression reintroducing the pre-010 bug), any instructions.Text
// that then names that tool would still pass instructions/audit_test.go's
// static mirror — this test is what would catch the drift.
func TestWiring_CapabilityReferenceMatchesRealSubagentSpecs(t *testing.T) {
	specs := realSubagentSpecs(t)
	ctxToKind := map[string]string{
		"subagent.general": "general",
		"subagent.explore": "explore",
		"subagent.review":  "review",
	}
	for _, tx := range instructions.All() {
		kind, ok := ctxToKind[tx.AllowlistCtx]
		if !ok {
			continue
		}
		spec, ok := specs[kind]
		if !ok {
			t.Fatalf("subagentSpecs() has no entry for kind %q referenced by text %q", kind, tx.ID)
		}
		for _, tool := range tx.MentionsTools {
			if !spec.Allowed[tool] {
				t.Errorf("text %q names tool %q, not in the REAL subagentSpecs()[%q].Allowed (IS-6 live check)", tx.ID, tool, kind)
			}
		}
	}
}

// TestWiring_GeneralSubagentRealAllowedNeverIncludesSyntheticTools is the
// direct, live pin for fix (a): the actual Allowed map built by
// subagentSpecs() for "general" must never contain any of the four
// main-loop-only synthetic tools, because they are dispatched exclusively
// through engine.go's executeOne switch (mainScope's dispatch), never
// through registry.Execute (the only dispatch path executeSubagent's
// subScope uses). If this ever regresses, the general subagent's advertised
// schema (built from the SAME Allowed map via registry.Definitions) would
// once again promise a tool its executor drops to "unknown tool".
func TestWiring_GeneralSubagentRealAllowedNeverIncludesSyntheticTools(t *testing.T) {
	specs := realSubagentSpecs(t)
	general, ok := specs["general"]
	if !ok {
		t.Fatal("subagentSpecs() has no \"general\" entry")
	}
	for _, synthetic := range []string{"run_subagent", "ask_user", "propose_changes", "save_memory", "edit_memory"} {
		if general.Allowed[synthetic] {
			t.Errorf("general subagent's real Allowed map now includes synthetic tool %q — its advertised schema and capabilityStatement would over-promise (fix a regression)", synthetic)
		}
	}
}

// TestWiring_ReadOnlyShellAllowlistTextMatchesRealValidator spot-checks a
// handful of commands the canonical allowlist claims are read-only against
// the REAL IsReadOnlyShell classifier (fix b): the stated rule must not lie
// about what the gate actually allows.
func TestWiring_ReadOnlyShellAllowlistTextMatchesRealValidator(t *testing.T) {
	allowed := []string{"git status", "git diff", "go test ./...", "npx tsc --noEmit", "ls -la", "cat foo.go", `cd internal && git status`}
	blocked := []string{"rm -rf /", "git commit -am x", "echo hi > out.txt", "sed -i s/a/b/ f"}
	for _, cmd := range allowed {
		if !IsReadOnlyShell(cmd) {
			t.Errorf("command %q should be read-only per the real classifier but is not — the canonical allowlist text would then overclaim", cmd)
		}
	}
	for _, cmd := range blocked {
		if IsReadOnlyShell(cmd) {
			t.Errorf("command %q is NOT read-only but the real classifier allowed it — the canonical allowlist text's write-command exclusions would then be wrong", cmd)
		}
	}
	// Sanity: the canonical text names the two representative write commands
	// this test blocks, so a future edit to either side is visible in a diff.
	if !strings.Contains(instructions.ReadOnlyShellAllowlistBody, "git add/commit/push") {
		t.Error("canonical read-only-shell allowlist text no longer names git add/commit/push as blocked")
	}
}

// TestWiring_PrefixBytesGolden is the SEPARATE Prefix-bytes golden IS-9
// requires: the exact wire bytes of the cached prefix (the system prompt
// plus the serialized tool JSON) under fixed, session-invariant inputs. It
// lives here rather than in internal/instructions because composing the
// real wire shape needs orchestrator's SystemPrompt/sessionDefinitions and
// the workspace tool registry, none of which the foundation-layer
// instructions package may import (internal/instructions/dump_test.go's
// renderPackagePrefixDump documents this split). A diff here IS the
// reviewable epoch artifact FR-018 requires; the ONE sanctioned diff this
// feature spends is fix (d) (the write_file sentence) and fix (c) (the
// run_subagent report-format field list) — both landed together with the
// re-baselined promptBaselineChars in prompt_budget_test.go.
func TestWiring_PrefixBytesGolden(t *testing.T) {
	ws, err := workspace.New(t.TempDir(), workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: workspace.NewMemoryTrustStore()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	e := &Engine{registry: NewRegistry(ws.Tools()...)}
	ctx := PromptContext{
		Workspace: "/workspace", OS: "linux", Shell: "bash", Model: "deepseek-v4-flash",
		HasWeb: false, HasSubagents: true, SubagentModel: "deepseek-v4-flash",
		ModelAddendum: gateway.ResolveModelProfile("deepseek-v4-flash").PromptAddendum,
	}
	systemPrompt := SystemPrompt(ctx)
	toolsJSON, err := json.MarshalIndent(e.sessionDefinitions(), "", "  ")
	if err != nil {
		t.Fatalf("marshal sessionDefinitions: %v", err)
	}
	got := "=== SYSTEM PROMPT (" + strconv.Itoa(len(systemPrompt)) + " chars) ===\n" + systemPrompt +
		"\n\n=== TOOL DEFINITIONS JSON (" + strconv.Itoa(len(toolsJSON)) + " bytes) ===\n" + string(toolsJSON) + "\n"

	path := filepath.Join("testdata", "prefix_bytes_wire.golden")
	if *updatePrefixGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-prefix-golden to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("Prefix wire golden %s is stale — this is the recorded epoch artifact (FR-018): confirm the diff is one of the five sanctioned incoherence fixes, then rerun with -update-prefix-golden", path)
	}
}
