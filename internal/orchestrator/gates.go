package orchestrator

import (
	"context"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// gatedExecute is the single tool-dispatch gate every call passes through
// (contracts/dispatch-gate.md): argument validation, the verbatim failed-call
// short-circuit, the repeat limiter, the duplicate-read guard, cancel-safe
// pairing, and the storm breaker, in that order.
func (e *Engine) gatedExecute(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile, sc dispatchScope) toolOutcome {
	name := call.ToolName()
	if sc.onStart != nil {
		sc.onStart(call)
	}
	end := func(output string) {
		if sc.onEnd != nil {
			sc.onEnd(call, output)
		}
	}
	// H1: pre-dispatch argument validation against the definitions offered this turn.
	if err := validateCallArgs(call, definitions); err != nil {
		output := err.Error() + " Re-emit the call with well-formed arguments."
		e.recordHarnessEvent(ctx, contract.HarnessGate, "arg-validation:"+name, err.Error())
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true, GateRejected: true, Err: err}
	}
	// H2: verbatim failed-call short-circuit.
	signature := callSignature(call)
	if prior, had := failedCallLastError(sc.counters, signature); had {
		output := fmt.Sprintf("This exact call already failed: %s. Do not repeat verbatim — change your approach (e.g. re-read the file first).", prior)
		e.recordHarnessEvent(ctx, contract.HarnessGate, "repeat-failed-call:"+name, prior)
		end(output)
		e.recordFailedCall(sc, signature, output)
		return toolOutcome{Call: call, Output: output, Failed: true, GateRejected: true}
	}
	// Repeat limiter.
	sc.counters.mu.Lock()
	sc.counters.callCounts[signature]++
	repeats := sc.counters.callCounts[signature]
	sc.counters.mu.Unlock()
	if repeats > 3 {
		output := instructions.GateRepeatLimiterBody
		e.recordHarnessEvent(ctx, contract.HarnessGate, "repeat-limiter:"+name, callSignature(call))
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true, GateRejected: true}
	}
	// Duplicate-read guard: the task-scoped inspection ledger holds every read
	// this session has made, with mtime-gated freshness for files and
	// mutation-invalidated signatures for searches.
	if entry, duplicate := e.inspection.Duplicate(call, e.history.IsToolResultIntact); duplicate {
		e.taskMu.Lock()
		e.taskDuplicates++
		e.taskMu.Unlock()
		output := fmt.Sprintf(instructions.GateDuplicateReadTmpl, entry.CallID)
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: false}
	}
	// H4: cancel-safe pairing before dispatch.
	if ctx.Err() != nil {
		output := "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	output, dispatchErr := sc.dispatch(ctx, call)
	if dispatchErr != nil {
		output = fmt.Sprintf("Tool %s failed: %v", name, dispatchErr)
	}
	// Re-check after dispatch in case cancellation landed during execution.
	if ctx.Err() != nil {
		output = "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	output = CapToolOutput(output, effort.ToolOutputCap)
	failed := IsToolFailure(output, dispatchErr)
	if sc.postDispatch != nil {
		sc.postDispatch(call, output, failed, dispatchErr)
	}
	end(output)
	if failed {
		e.recordHarnessEvent(ctx, contract.HarnessTool, "tool-failure:"+name, output)
		e.recordFailedCall(sc, signature, output)
	} else {
		sc.counters.mu.Lock()
		delete(sc.counters.failedCalls, signature)
		sc.counters.mu.Unlock()
		// The workspace checklist feed lives here, at the one point every tool call
		// passes through, so ticking an item refreshes the panel immediately.
		// TA01: the write that just landed also NAMES the active checklist, so a
		// tasks.md created beside the work (games/snake/tasks.md) drives the panel
		// instead of being written, accepted, and then never read.
		if e.touchesChecklist(call) {
			e.adoptChecklistPath(call)
			e.refreshChecklist()
		}
		// Tally every successful mutation for the review gate and end-of-task stats.
		e.noteChangedFiles(call)
	}
	return toolOutcome{Call: call, Output: output, Failed: failed, Err: dispatchErr}
}
