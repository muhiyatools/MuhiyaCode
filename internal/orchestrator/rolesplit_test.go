package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// The plan/execute split (F0). The main model instructs; the execution agent
// executes. These tests pin both halves of the boundary — what is refused and,
// just as importantly, what must still get through, because a carve-out that
// closes silently makes the agent unable to plan at all.

func roleSplitEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "rolesplit", WorkspacePath: dir},
		Provider: &scriptedProvider{}, Registry: NewRegistry(service.Tools()...),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, dir
}

// roleSplitScope builds a scope in either role. trackStats marks the MAIN loop
// (the same discriminator production uses); a subagent scope leaves it false.
func roleSplitScope(engine *Engine, mainLoop bool) dispatchScope {
	allowed := map[string]bool{}
	for _, name := range engine.registry.Names() {
		allowed[name] = true
	}
	return dispatchScope{
		counters:   newCallCounters(),
		trackStats: mainLoop,
		dispatch: func(ctx context.Context, call contract.ToolCall) (string, error) {
			return engine.registry.Execute(ctx, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), allowed)
		},
	}
}

func TestRoleGateBlocksMainLoopWorkspaceMutations(t *testing.T) {
	engine, _ := roleSplitEngine(t)
	definitions := engine.registry.Definitions(map[string]bool{"write_file": true, "edit_file": true, "run_shell": true})
	scope := roleSplitScope(engine, true)
	for _, row := range []struct{ name, args string }{
		{"write_file", `{"path":"main.go","content":"package main\n"}`},
		{"edit_file", `{"path":"main.go","oldString":"a","newString":"b"}`},
		{"run_shell", `{"command":"Remove-Item main.go"}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c", row.name, row.args), definitions, Profile(contract.EffortLow), scope)
			if !outcome.Failed {
				t.Fatalf("main-loop %s was allowed to mutate the workspace", row.name)
			}
			if !strings.Contains(outcome.Output, "run_subagent") {
				t.Fatalf("refusal does not name the affordable next action: %q", outcome.Output)
			}
		})
	}
}

func TestRoleGateAllowsTheExecutionAgent(t *testing.T) {
	engine, _ := roleSplitEngine(t)
	definitions := engine.registry.Definitions(map[string]bool{"write_file": true})
	scope := roleSplitScope(engine, false) // subagent scope
	outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c", "write_file", `{"path":"main.go","content":"package main\n"}`), definitions, Profile(contract.EffortLow), scope)
	if outcome.Failed {
		t.Fatalf("the execution agent was blocked from executing: %s", outcome.Output)
	}
}

// The carve-outs. Each of these closing silently would leave the main model
// unable to do its own job.
func TestRoleGateCarveOuts(t *testing.T) {
	engine, _ := roleSplitEngine(t)
	definitions := engine.registry.Definitions(map[string]bool{"write_file": true, "run_shell": true, "read_file": true})
	scope := roleSplitScope(engine, true)
	t.Run("tasks.md is the planner's own artifact", func(t *testing.T) {
		outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c", "write_file", `{"path":"tasks.md","content":"- [ ] Add validation\n"}`), definitions, Profile(contract.EffortLow), scope)
		if outcome.Failed {
			t.Fatalf("the planner was blocked from writing its own checklist: %s", outcome.Output)
		}
	})
	t.Run("read-only shell verifies the agent's work", func(t *testing.T) {
		outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c", "run_shell", `{"command":"go test ./internal/orchestrator"}`), definitions, Profile(contract.EffortLow), roleSplitScope(engine, true))
		if outcome.Failed && strings.Contains(outcome.Output, "run_subagent") {
			t.Fatalf("a verification command was treated as execution: %s", outcome.Output)
		}
	})
	t.Run("reads are always open", func(t *testing.T) {
		if workspaceMutation(contract.NewToolCall("c", "read_file", `{"path":"main.go"}`)) {
			t.Fatal("read_file classified as a workspace mutation")
		}
	})
	t.Run("memory writes are planning, not execution", func(t *testing.T) {
		for _, name := range []string{"save_memory", "edit_memory"} {
			if workspaceMutation(contract.NewToolCall("c", name, `{}`)) {
				t.Fatalf("%s classified as a workspace mutation", name)
			}
		}
	})
}

// An unparseable shell command must fail closed — treated as execution rather
// than waved through.
func TestRoleGateUnparseableShellFailsClosed(t *testing.T) {
	if !workspaceMutation(contract.NewToolCall("c", "run_shell", `{not json`)) {
		t.Fatal("unparseable run_shell arguments were treated as read-only")
	}
}

// The deadlock guard: under the split, a class with a zero agent budget could
// neither mutate directly nor delegate. Only chat may be zero.
func TestEveryMutatingClassCanAffordAnExecutionAgent(t *testing.T) {
	for class, agents := range classAgents {
		if class == ClassChat {
			continue
		}
		if agents < 1 {
			t.Errorf("class %q affords %d subagent runs — under the plan/execute split it could never apply a change", class, agents)
		}
	}
	budget := BudgetFor(Assessment{Class: ClassTiny}, Profile(contract.EffortLow))
	if budget.MaxAgentRuns < 1 {
		t.Fatalf("a tiny task at low effort affords %d runs; it must be able to delegate its one change", budget.MaxAgentRuns)
	}
}
