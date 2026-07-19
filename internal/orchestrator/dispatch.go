package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// subagentKind reads the "agent" field out of a run_subagent call's raw
// arguments and returns the kind. H3: malformed JSON must fail closed —
// parallel-batch decisions (engine.go:executeBatch) treat non-"general"
// kinds as parallelizable, and a malformed/unknown general-subagent call
// would otherwise be RUN IN PARALLEL while still having full mutation
// permission. Treat any decode failure or empty value as "general".
func subagentKind(call contract.ToolCall) string {
	var input struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(call.ArgumentsJSON()), &input); err != nil {
		return "general"
	}
	kind := strings.TrimSpace(input.Agent)
	if kind == "" {
		return "general"
	}
	return kind
}

// failedCallLastError returns the cached error for an identical prior
// verbatim call, or ("", false) when no such failure has been recorded.
func failedCallLastError(c *callCounters, signature string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.failedCalls[signature]
	return v, ok
}

// normalizeError reduces an error to its first line and strips paths/numbers
// so the storm breaker keys on the failure class, not cosmetic variation.
// (H2.)
func normalizeError(msg string) string {
	msg = strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0]
	// Strip absolute-looking paths and any hex-looking numbers so cosmetic
	// tweaks (different `line 32` mentions, different mtimes) collapse.
	tokens := strings.Fields(msg)
	for index, token := range tokens {
		if strings.HasSuffix(token, ":") {
			tokens[index] = token
			continue
		}
		if _, err := fmt.Sscanf(token, "%d", new(int)); err == nil {
			tokens[index] = "<num>"
			continue
		}
		if strings.HasPrefix(token, "/") || strings.Contains(token, "\\") {
			tokens[index] = "<path>"
		}
	}
	return strings.Join(tokens, " ")
}

// failedClassKey is the H2 storm-breaker key. Pure function so it's safe
// from any goroutine.
func failedClassKey(name, message string) string {
	return name + "|" + normalizeError(message)
}

func (e *Engine) executeBatch(ctx context.Context, calls []contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) []toolOutcome {
	// Only read-only subagent kinds (explore/plan/review) may run
	// concurrently. "general" subagents get the full mutating tool registry,
	// so running more than one at once risks unordered, interleaved edits to
	// the same workspace with no locking anywhere in the tool registry.
	allSubagents := len(calls) > 1
	allAgents := allSubagents && effort.ParallelAgents
	for _, call := range calls {
		allSubagents = allSubagents && call.ToolName() == "run_subagent"
		allAgents = allAgents && call.ToolName() == "run_subagent" && subagentKind(call) != "general"
	}
	// Announce every delegate in the batch BEFORE any of them executes. The
	// per-call ToolStart inside gatedExecute fires only when that call is
	// dispatched, so a serial delegate batch (ParallelAgents off, or any
	// "general" delegate forcing the safe serial path) revealed delegate N+1's
	// "Delegate … running" transcript row only after delegate N fully
	// completed — the announced fan-out looked frozen for minutes. Announcing
	// up front renders every row at batch launch; execution order, the shared
	// dispatch gate, and all taskMu-guarded counters are untouched (announced
	// suppresses only the duplicate onStart). Restricted to all-run_subagent
	// batches: delegates never stream ToolOutput, so the TUI's name-keyed
	// live-output routing cannot mis-route, and ordinary tool batches keep
	// their strict start/end nesting.
	announced := allSubagents && e.callbacks.ToolStart != nil
	if announced {
		for _, call := range calls {
			e.callbacks.ToolStart(call.ToolName(), json.RawMessage(call.ArgumentsJSON()))
		}
	}
	execute := func(c context.Context, call contract.ToolCall) toolOutcome {
		if !announced {
			return e.executeCall(c, call, definitions, effort)
		}
		scope := e.mainScope(definitions)
		scope.onStart = nil // already announced at batch launch
		return e.gatedExecute(c, call, definitions, effort, scope)
	}
	result := make([]toolOutcome, len(calls))
	if allAgents {
		var wait sync.WaitGroup
		for i, call := range calls {
			wait.Add(1)
			go func(index int, value contract.ToolCall) {
				defer wait.Done()
				result[index] = execute(ctx, value)
			}(i, call)
		}
		wait.Wait()
		return result
	}
	for i, call := range calls {
		result[i] = execute(ctx, call)
	}
	return result
}

// callCounters holds the per-scope dispatch-gate state: identical-call repeats,
// the verbatim failed-call cache, the storm-breaker class counts, and the
// plan-mode violation count. One instance per task (main loop) and one per
// subagent RUN, guarded by its own mutex so parallel subagents never share or
// race gate state. (B6.)
type callCounters struct {
	mu                sync.Mutex
	callCounts        map[string]int
	failedCalls       map[string]string
	failedClassCounts map[string]int
	planViolations    int
}

func newCallCounters() *callCounters {
	return &callCounters{
		callCounts:        map[string]int{},
		failedCalls:       map[string]string{},
		failedClassCounts: map[string]int{},
	}
}

// dispatchScope carries the scope-specific wiring for gatedExecute so ONE gate
// serves both the main loop and subagent runs without divergence. (B6.)
type dispatchScope struct {
	counters     *callCounters
	dedupe       bool // consult the inspection ledger (main loop only)
	trackStats   bool // increment engine-level discipline stats (main loop only)
	readOnly     bool // allow shell checks only when IsReadOnlyShell approves them
	onStart      func(contract.ToolCall)
	onEnd        func(contract.ToolCall, string)
	escalate     func(string)
	dispatch     func(context.Context, contract.ToolCall) (string, error)
	postDispatch func(contract.ToolCall, string, bool, error)
}

func (e *Engine) executeCall(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) toolOutcome {
	return e.gatedExecute(ctx, call, definitions, effort, e.mainScope(definitions))
}

// mainScope is the dispatch scope for the primary agent loop: per-task counters,
// the duplicate-read guard + discipline stats, ToolStart/ToolEnd callbacks,
// loop-guard notices on the main history tail, dispatch through the synthetic-
// tool switch, and read-coverage / supersede bookkeeping. (B6.)
func (e *Engine) mainScope(definitions []contract.ToolDefinition) dispatchScope {
	return dispatchScope{
		counters:   e.taskCounters,
		dedupe:     true,
		trackStats: true,
		onStart: func(call contract.ToolCall) {
			if e.callbacks.ToolStart != nil {
				e.callbacks.ToolStart(call.ToolName(), json.RawMessage(call.ArgumentsJSON()))
			}
		},
		onEnd:    func(call contract.ToolCall, output string) { e.endTool(call.ToolName(), output) },
		escalate: func(notice string) { e.history.Append(contract.Message{Role: contract.RoleUser, Content: notice}) },
		dispatch: func(c context.Context, call contract.ToolCall) (string, error) {
			return e.executeOne(c, call, definitions)
		},
		postDispatch: func(call contract.ToolCall, output string, failed bool, dispatchErr error) {
			name := call.ToolName()
			if !failed && readonlyTools[name] {
				e.inspection.Record(call, output)
			}
			if isMutation(name) && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
				e.history.MarkSuperseded(e.inspection.InvalidateFor(call))
			}
		},
	}
}

// recordFailedCall caches the last error for a verbatim signature and bumps
// the storm-breaker counter keyed on (toolName, normalizedError). At 3 hits
// on the same class within a task, appends a loop-guard governor notice.
func (e *Engine) recordFailedCall(sc dispatchScope, signature, output string) {
	class := failedClassKey(callName(signature), output)
	sc.counters.mu.Lock()
	sc.counters.failedCalls[signature] = output
	sc.counters.failedClassCounts[class]++
	hits := sc.counters.failedClassCounts[class]
	sc.counters.mu.Unlock()
	if hits == 3 && sc.escalate != nil {
		sc.escalate(fmt.Sprintf("[loop guard] The same failure keeps recurring with %s. Stop retrying variations; take a fundamentally different action or finish.", callName(signature)))
	}
}

// allFailed reports whether a non-empty batch of outcomes all failed. (B7.)
func allFailed(outcomes []toolOutcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, outcome := range outcomes {
		if !outcome.Failed {
			return false
		}
	}
	return true
}

// callName extracts the leading tool-name from a signature.
func callName(signature string) string {
	for index := 0; index < len(signature); index++ {
		if signature[index] == ' ' {
			return signature[:index]
		}
	}
	return signature
}

func (e *Engine) endTool(name, output string) {
	if e.callbacks.ToolEnd != nil {
		e.callbacks.ToolEnd(name, output)
	}
}
