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
		dispatch: func(c context.Context, call contract.ToolCall) (string, error) {
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
	if !o1.Failed {
		t.Fatalf("first call should fail, got %+v", o1)
	}
	o2 := engine.gatedExecute(context.Background(), call, defs, Profile(contract.EffortMedium), sc)
	if !o2.Failed || !strings.Contains(o2.Output, "already failed") {
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
	if !o.Failed || !strings.Contains(o.Output, "cut off mid-generation") {
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
