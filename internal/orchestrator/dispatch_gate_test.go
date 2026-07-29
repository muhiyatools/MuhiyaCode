package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func gateTestEngine(t *testing.T, tools ...contract.Tool) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "gate", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry(tools...), Prompt: PromptContext{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// subScopeFor builds a subagent-style dispatch scope: its OWN counters, no
// duplicate guard / discipline stats, dispatch restricted to `allowed` via the
// registry (never the synthetic-tool switch). Mirrors executeSubagent's scope.
func subScopeFor(engine *Engine, allowed map[string]bool) dispatchScope {
	return dispatchScope{
		counters: newCallCounters(),
		dispatch: func(c context.Context, call contract.ToolCall) contract.ToolResult {
			return engine.registry.Execute(c, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), allowed)
		},
	}
}

// TestGatedExecuteSubagentShortCircuitsVerbatimFailure (T026 / dispatch-gate
// §4.2): a subagent scope no longer bypasses the failed-call cache — an
// identical failing call short-circuits on the 2nd attempt.
func TestGatedExecuteSubagentShortCircuitsVerbatimFailure(t *testing.T) {
	engine := gateTestEngine(t, &failingTool{name: "read_file"})
	allowed := map[string]bool{"read_file": true}
	defs := engine.registry.Definitions(allowed)
	sc := subScopeFor(engine, allowed)
	call := contract.NewToolCall("r", "read_file", `{"path":"x"}`)

	o1 := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), sc)
	if !o1.IsFailure() {
		t.Fatalf("first call should fail, got %+v", o1)
	}
	o2 := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), sc)
	if !o2.IsFailure() || !strings.Contains(o2.Output, "already failed") {
		t.Fatalf("verbatim failed repeat should short-circuit in the subagent scope, got %q", o2.Output)
	}
}

// TestGatedExecuteSubagentMalformedArgsActionableError (T026 / dispatch-gate
// §4.7): a subagent scope now runs H1 validation — malformed args get the
// actionable error instead of a raw dispatch failure.
func TestGatedExecuteSubagentMalformedArgsActionableError(t *testing.T) {
	engine := gateTestEngine(t, &recordingTool{name: "read_file"})
	allowed := map[string]bool{"read_file": true}
	defs := engine.registry.Definitions(allowed)
	sc := subScopeFor(engine, allowed)
	call := contract.NewToolCall("r", "read_file", `{"path":`) // truncated JSON

	o := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), sc)
	// TB04: the actionable recovery now names the output limit; a read_file (not a
	// write) gets the compact-retry form rather than the chunked-write protocol.
	if !o.IsFailure() || !strings.Contains(o.Output, "cut off at the output limit") {
		t.Fatalf("malformed args should get the actionable validation error, got %q", o.Output)
	}
}

// TestGatedExecuteConcurrentSubagentScopesRaceClean (T026 / dispatch-gate §4.8):
// parallel subagents each have their own counters, so concurrent gate use is
// race-clean. Meaningful under `CGO_ENABLED=1 go test -race`; must also pass
// (no panic/deadlock) without it.
func TestGatedExecuteConcurrentSubagentScopesRaceClean(t *testing.T) {
	engine := gateTestEngine(t, &failingTool{name: "read_file"})
	allowed := map[string]bool{"read_file": true}
	defs := engine.registry.Definitions(allowed)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := subScopeFor(engine, allowed) // independent counters per subagent
			for j := 0; j < 20; j++ {
				engine.gatedExecute(context.Background(), contract.NewToolCall("r", "read_file", `{"path":"x"}`), defs, Profile(contract.EffortMedium), sc)
			}
		}()
	}
	wg.Wait()
}

func TestGatedExecutePreservesCompletedOutcomeWhenCancellationRacesReturn(t *testing.T) {
	engine := gateTestEngine(t, &recordingTool{name: "write_file"})
	defs := engine.registry.Definitions(nil)
	ctx, cancel := context.WithCancel(context.Background())
	postDispatchCalled := false
	sc := dispatchScope{
		counters: newCallCounters(),
		dispatch: func(context.Context, contract.ToolCall) contract.ToolResult {
			cancel()
			return contract.AdaptToolResult("wrote file", nil)
		},
		postDispatch: func(contract.ToolCall, contract.ToolResult) {
			postDispatchCalled = true
		},
	}

	outcome := engine.gatedExecute(ctx, contract.NewToolCall("w", "write_file", `{"path":"x","content":"y"}`), defs, Profile(contract.EffortMedium), sc)
	if outcome.IsFailure() || outcome.Output != "wrote file" {
		t.Fatalf("a completed dispatch must not be rewritten as pre-execution cancellation: %+v", outcome)
	}
	if !postDispatchCalled {
		t.Fatal("completed dispatch must still run bookkeeping when cancellation races its return")
	}
}

func TestGatedExecuteMarksInterruptedMutationIndeterminate(t *testing.T) {
	engine := gateTestEngine(t, &recordingTool{name: "write_file"})
	defs := engine.registry.Definitions(nil)
	ctx, cancel := context.WithCancel(context.Background())
	sc := dispatchScope{
		counters: newCallCounters(),
		dispatch: func(context.Context, contract.ToolCall) contract.ToolResult {
			cancel()
			return contract.AdaptToolResult("side effect may have committed", context.Canceled)
		},
	}

	outcome := engine.gatedExecute(ctx, contract.NewToolCall("w", "write_file", `{"path":"x","content":"y"}`), defs, Profile(contract.EffortMedium), sc)
	if !outcome.IsFailure() || !outcome.IsIndeterminate() {
		t.Fatalf("interrupted mutation must be indeterminate, got %+v", outcome)
	}
	if !strings.Contains(outcome.Output, "side effect may have committed") || !strings.Contains(outcome.Output, "inspect") {
		t.Fatalf("indeterminate outcome must preserve diagnostics and recovery guidance: %q", outcome.Output)
	}
}

func TestGatedExecuteRecoveredMutationFailsClosedWithoutConfirmation(t *testing.T) {
	tool := &recordingTool{name: "write_file"}
	engine := gateTestEngine(t, tool)
	defs := engine.registry.Definitions(nil)
	call := contract.NewToolCall("rescued_1_write_file", "write_file", `{"path":"x","content":"y"}`)

	outcome := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), subScopeFor(engine, nil))

	if !outcome.IsFailure() || !outcome.WasRejected() || !strings.Contains(outcome.Output, "require interactive confirmation") {
		t.Fatalf("recovered mutation must fail closed without a confirmation channel: %+v", outcome)
	}
	if tool.calls != 0 {
		t.Fatalf("recovered mutation dispatched without approval; calls=%d", tool.calls)
	}
}

func TestGatedExecuteRecoveredReadDoesNotPrompt(t *testing.T) {
	tool := &recordingTool{name: "read_file"}
	engine := gateTestEngine(t, tool)
	engine.callbacks.Confirm = func(context.Context, string) (bool, error) {
		t.Fatal("read-only recovered call should not require mutation confirmation")
		return false, nil
	}
	defs := engine.registry.Definitions(nil)
	call := contract.NewToolCall("rescued_2_read_file", "read_file", `{"path":"x"}`)

	outcome := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), subScopeFor(engine, nil))

	if outcome.IsFailure() || tool.calls != 1 {
		t.Fatalf("read-only recovered call should execute normally: outcome=%+v calls=%d", outcome, tool.calls)
	}
}
