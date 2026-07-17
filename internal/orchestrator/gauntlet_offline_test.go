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
// new invariants (greenfield research skip, first-try plan acceptance, clean
// provider-error surfacing) under the same forbidden-strings meta-check.
func TestGauntletOffline(t *testing.T) {
	// Scenario 1 — Greenfield pipeline: an empty workspace skips research entirely
	// (0 agents), the plan is accepted on the FIRST exit_plan_mode, and the run
	// halts at the approval pause (user decision).
	t.Run("greenfield-pipeline-first-try", func(t *testing.T) {
		var notices []string
		agentStarts := 0
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{
			Settings: &settings, Session: contract.Session{ID: "g-green", WorkspacePath: t.TempDir()},
			Provider: &scriptedProvider{responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("p", "update_plan", `{"steps":[{"title":"cmd/taskflow/main.go: create the CLI entry with add/list/done — Verify: go build ./... succeeds","status":"pending"}],"note":"Verification:\n- go build ./...\nRisks:\n- none"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"greenfield plan ready"}`)}},
			}},
			Registry: NewRegistry(),
			Callbacks: contract.Callbacks{
				Agent: func(e contract.AgentEvent) {
					if e.Kind == "start" {
						agentStarts++
					}
				},
				Notice: func(s string) { notices = append(notices, s) },
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		answer, stats, runErr := engine.Run(context.Background(), "Build a brand-new CLI called taskflow from scratch with add, list, and done subcommands. Research first, write a detailed plan, then implement.")
		assertRecoveryInvariant(t, engine, stats, runErr, answer, outcomeUserDecision)
		if agentStarts != 0 {
			t.Errorf("greenfield workspace spawned %d research agents, want 0", agentStarts)
		}
		scanForbidden(t, append(notices, answer)...)
	})

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

	// Scenario 3 — Subagent turn-budget: a subagent that can only loop returns a
	// guided partial, never the forbidden turn-limit string (INV-3), end to end.
	t.Run("subagent-turn-budget-guided-partial", func(t *testing.T) {
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{
			Settings: &settings, Session: contract.Session{ID: "g-sub", WorkspacePath: seededWorkspace(t)},
			Provider: alwaysToolCallProvider{}, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		})
		if err != nil {
			t.Fatal(err)
		}
		spec := engine.subagentSpecs()["explore"]
		spec.MaxTurns = 2
		result := engine.executeSubagent(context.Background(), "g-sub", subagentInput{Agent: "explore", Title: "loop", Task: "explore forever"}, spec)
		scanForbidden(t, result.Report)
		if !strings.Contains(result.Report, "partial progress") {
			t.Fatalf("expected a guided partial, got: %q", result.Report)
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
