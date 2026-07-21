package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// forbiddenStrings are the exact live-failure phrasings that must NEVER surface
// again (Stability Overhaul T061). Each maps to a real incident this overhaul
// fixed. The gauntlet scans every scenario's surfaced text against this list.
var forbiddenStrings = []string{
	"reached its bounded turn limit", // INV-3 (T035)
	"panic:",                         // uncontained crash (Phase 5)
	"GetFileAttributesEx",            // raw OS error (D9)
	"commit unknown, built unknown",  // unstamped build leaking (D2/T002)
	"Plan updated:",                  // old plan-vs-todo wording (Experience Overhaul A4)
	"Plan mode on —",                 // removed /plan toggle notice (Ultimate Polish P1)
	"Run /plan again",                // removed /plan toggle notice (P1)
	"Proceed with Plan",              // removed plan-approval affordance (Phase 1 planning-pipeline removal)
	"Plan mode",                      // removed plan mode entirely (Phase 1 planning-pipeline removal)
	"agents=0",                       // removed subagent budgets (FS-1) — this exact brief starved the "Go" turn
	"budget exhausted",               // removed subagent budgets (FS-1)
	"run(s) remaining",               // removed subagent budgets (FS-1)
	"no subagent budget",             // removed subagent budgets (FS-1)
}

// alwaysToolCallProvider never produces final text — every turn is another tool
// call, so a subagent driven by it can only ever exhaust its turn budget.
type alwaysToolCallProvider struct{}

func (alwaysToolCallProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	return contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("t", "read_file", `{"path":"nope.go"}`)}}, nil
}
func (alwaysToolCallProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }

func scanForbidden(t *testing.T, texts ...string) {
	t.Helper()
	joined := strings.Join(texts, "\n")
	for _, bad := range forbiddenStrings {
		if strings.Contains(joined, bad) {
			t.Errorf("surfaced forbidden string %q:\n%s", bad, joined)
		}
	}
}

// TestGauntletOffline is the deterministic end-to-end acceptance battery (T060):
// each scenario drives a full Engine.Run against a real temp workspace and
// asserts BOTH the recovery outcome and that no forbidden string surfaced. It
// complements the fault catalog (faultinjection_test.go), adding the overhaul's
// new invariants (clean provider-error surfacing, bounded subagent budgets,
// memory round-trip) under the same forbidden-strings meta-check.
func TestGauntletOffline(t *testing.T) {
	// Scenario 2 — Tool failure recovers: a failing tool ends in recorded
	// degradation, never a crash, never a forbidden string.
	t.Run("tool-failure-recovers", func(t *testing.T) {
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{
			Settings: &settings, Session: contract.Session{ID: "g-tool", WorkspacePath: seededWorkspace(t)},
			Provider: &scriptedProvider{responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "do_thing", `{}`)}},
				{Content: "The tool failed; reporting the blocker and stopping."},
			}},
			Registry: NewRegistry(&failingTool{name: "do_thing"}),
		})
		if err != nil {
			t.Fatal(err)
		}
		answer, stats, runErr := engine.Run(context.Background(), "Use do_thing to fix the widget.")
		if runErr != nil {
			t.Fatalf("a tool failure must not crash the run: %v", runErr)
		}
		if stats.HarnessEvents == 0 {
			t.Error("a tool failure should have registered harness friction")
		}
		scanForbidden(t, answer)
	})

	// Scenario 3 — Turn-budget liveness: a session that can only loop still ends
	// with a usable answer and never emits a forbidden turn-limit string (INV-3).
	// It exercised a subagent's own ladder while subagents existed; the surviving
	// bound is the main loop's hard turn ceiling.
	t.Run("turn-budget-guided-answer", func(t *testing.T) {
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{
			Settings: &settings, Session: contract.Session{ID: "g-loop", WorkspacePath: seededWorkspace(t)},
			Provider: alwaysToolCallProvider{}, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		})
		if err != nil {
			t.Fatal(err)
		}
		answer, stats, err := engine.Run(context.Background(), "read the seeded file over and over")
		if err != nil {
			t.Fatalf("a looping session must still return: %v", err)
		}
		scanForbidden(t, answer)
		if stats.Turns == 0 {
			t.Fatal("expected the loop to burn turns before the ceiling stopped it")
		}
	})

	// Scenario 4 — Memory topic round-trip: a save under a topic files into the
	// topic file and leaves an index pointer; a recall returns it into history;
	// nothing forbidden surfaces (Experience Overhaul B6 T110).
	t.Run("memory-topic-roundtrip", func(t *testing.T) {
		store := t.TempDir()
		var notices []string
		settings := engineSettings() // auto-accept: no approval prompt
		engine, err := NewEngine(EngineConfig{
			Settings: &settings, Session: contract.Session{ID: "g-mem", WorkspacePath: t.TempDir()},
			MemoryDir: store,
			Provider: &scriptedProvider{responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "save_memory", `{"title":"Build system","content":"cmake then make; tests need dockerd running","topic":"build-system"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("r", "recall_memory", `{"topic":"build-system"}`)}},
				{Content: "Recalled the build-system notes and proceeding."},
			}},
			Registry:  NewRegistry(&recordingTool{name: "read_file"}),
			Callbacks: contract.Callbacks{Notice: func(s string) { notices = append(notices, s) }},
		})
		if err != nil {
			t.Fatal(err)
		}
		answer, _, runErr := engine.Run(context.Background(), "remember a durable note in memory")
		if runErr != nil {
			t.Fatalf("a memory round-trip must not crash: %v", runErr)
		}
		if topicFile, readErr := os.ReadFile(filepath.Join(store, "build-system.md")); readErr != nil || !strings.Contains(string(topicFile), "cmake then make") {
			t.Fatalf("topic file missing the saved entry: %v / %q", readErr, topicFile)
		}
		if index, _ := os.ReadFile(filepath.Join(store, projectMemoryFile)); !strings.Contains(string(index), "- [build-system]") {
			t.Fatalf("index missing the topic pointer:\n%s", index)
		}
		recalled := false
		for _, m := range engine.history.All() {
			if m.Role == contract.RoleTool && strings.Contains(m.Content, "cmake then make") {
				recalled = true
			}
		}
		if !recalled {
			t.Fatal("recall_memory did not return the saved content into history")
		}
		scanForbidden(t, append(notices, answer)...)
	})
}
