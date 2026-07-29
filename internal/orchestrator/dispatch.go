package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

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

const maxParallelReads = 4

// executeBatch preserves provider order in its result while running independent
// local reads concurrently. Any mutation, interactive tool, or external tool
// forces the entire batch through the deterministic serial path.
func (e *Engine) executeBatch(ctx context.Context, calls []contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) []toolOutcome {
	result := make([]toolOutcome, len(calls))
	if !parallelReadBatch(calls) {
		for index, call := range calls {
			started := time.Now()
			result[index] = e.executeCall(ctx, call, definitions, effort)
			result[index].DurationMS = time.Since(started).Milliseconds()
		}
		return result
	}
	limit := make(chan struct{}, maxParallelReads)
	var wait sync.WaitGroup
	for _, call := range calls {
		if e.callbacks.ToolStart != nil {
			e.callbacks.ToolStart(call.ToolName(), json.RawMessage(call.ArgumentsJSON()))
		}
	}
	for i, call := range calls {
		index, call := i, call
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				result[index] = toolOutcome{
					Call: call, Status: contract.ToolOutcomeCancelled,
					Output: "[cancelled before parallel read]", MutationCertainty: contract.MutationNotStarted,
				}
				return
			}
			started := time.Now()
			scope := e.mainScope(definitions)
			scope.onStart = nil
			scope.onEnd = nil
			result[index] = e.gatedExecute(ctx, call, definitions, effort, scope)
			result[index].DurationMS = time.Since(started).Milliseconds()
		}()
	}
	wait.Wait()
	for index, call := range calls {
		e.endTool(call.ToolName(), result[index].Output)
	}
	return result
}

func parallelReadBatch(calls []contract.ToolCall) bool {
	if len(calls) < 2 {
		return false
	}
	for _, call := range calls {
		if !readonlyTools[call.ToolName()] {
			return false
		}
	}
	return true
}

// callCounters holds the task's dispatch-gate state: identical-call repeats,
// the verbatim failed-call cache, the storm-breaker class counts, and the
// plan-mode violation count.
type callCounters struct {
	mu                sync.Mutex
	callCounts        map[string]int
	failedCalls       map[string]string
	failedClassCounts map[string]int
}

func newCallCounters() *callCounters {
	return &callCounters{
		callCounts:        map[string]int{},
		failedCalls:       map[string]string{},
		failedClassCounts: map[string]int{},
	}
}

// dispatchScope carries the task-local callbacks and executor used by the
// shared dispatch gate.
type dispatchScope struct {
	counters     *callCounters
	onStart      func(contract.ToolCall)
	onEnd        func(contract.ToolCall, string)
	escalate     func(string)
	dispatch     func(context.Context, contract.ToolCall) contract.ToolResult
	postDispatch func(contract.ToolCall, contract.ToolResult)
}

func (e *Engine) executeCall(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) toolOutcome {
	return e.gatedExecute(ctx, call, definitions, effort, e.mainScope(definitions))
}

// mainScope is the session's dispatch scope: per-task counters, ToolStart/
// ToolEnd callbacks, loop-guard notices on the history tail, dispatch through
// the synthetic-tool switch, and read-coverage / supersede bookkeeping.
func (e *Engine) mainScope(definitions []contract.ToolDefinition) dispatchScope {
	return dispatchScope{
		counters: e.taskCounters,
		onStart: func(call contract.ToolCall) {
			if e.callbacks.ToolStart != nil {
				e.callbacks.ToolStart(call.ToolName(), json.RawMessage(call.ArgumentsJSON()))
			}
		},
		onEnd:    func(call contract.ToolCall, output string) { e.endTool(call.ToolName(), output) },
		escalate: func(notice string) { e.history.Append(contract.Message{Role: contract.RoleUser, Content: notice}) },
		dispatch: func(c context.Context, call contract.ToolCall) contract.ToolResult {
			return e.executeOne(c, call, definitions)
		},
		postDispatch: func(call contract.ToolCall, result contract.ToolResult) {
			name := call.ToolName()
			if result.Status == contract.ToolOutcomeSucceeded && readonlyTools[name] {
				e.inspection.Record(call, result.Output)
			}
			if isMutation(name) {
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
		if !outcome.IsFailure() {
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
