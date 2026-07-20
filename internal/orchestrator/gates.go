package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// gatedExecute is the SINGLE shared tool-dispatch gate used by both the main
// loop and every subagent run (contracts/dispatch-gate.md). Scope-specific
// behaviour is carried by sc, so subagents no longer bypass validation, the
// failed-call cache, the repeat limiter, or the storm breaker. (B6/T024/T025.)
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
		return toolOutcome{Call: call, Output: output, Failed: true, Err: err}
	}
	// H2: verbatim failed-call short-circuit.
	signature := callSignature(call)
	if prior, had := failedCallLastError(sc.counters, signature); had {
		output := fmt.Sprintf("This exact call already failed: %s. Do not repeat verbatim — change your approach (e.g. re-read the file first).", prior)
		e.recordHarnessEvent(ctx, contract.HarnessGate, "repeat-failed-call:"+name, prior)
		end(output)
		e.recordFailedCall(sc, signature, output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	// Read-only subagents may receive run_shell so they can execute observable
	// test/vet/status checks. Keep the capability guided at the shared gate:
	// read-only commands may be chained, but state-changing shell commands
	// never reach the registry.
	if sc.readOnly && name == "run_shell" {
		var args struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) != nil || !IsReadOnlyShell(args.Command) {
			output := instructions.GateSubagentShellBlockedBody
			e.recordHarnessEvent(ctx, contract.HarnessGate, "shell-readonly-block", args.Command)
			end(output)
			e.recordFailedCall(sc, signature, output)
			return toolOutcome{Call: call, Output: output, Failed: true}
		}
	}
	// Feature 012 R-D2: a review-after-implement continuation carries the
	// implementer's full tool array on the wire (cache identity) but is
	// review-only in effect — mutations are refused harness-side. Only bites
	// read-only scopes whose definitions include mutating tools (continuation
	// runs); fresh read-only kinds never offer these tools, so H1 catches them
	// first.
	if sc.readOnly && isMutation(name) && name != "run_shell" {
		output := instructions.GateContinuationReviewMutationBody
		e.recordHarnessEvent(ctx, contract.HarnessGate, "continuation-review-mutation-block", name)
		end(output)
		e.recordFailedCall(sc, signature, output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	// The plan/execute split: the main model instructs, the execution agent
	// executes. Placed after the capability gates above (they are about what a
	// scope MAY do) and before the repeat limiter, so a refused mutation is not
	// counted as a repeat.
	if outcome, blocked := e.executionRoleGate(ctx, sc, call); blocked {
		return outcome
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
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	// Duplicate-read guard (main scope only; the inspection ledger is main-task state).
	if sc.dedupe {
		if entry, duplicate := e.inspection.Duplicate(call, e.history.IsToolResultIntact); duplicate {
			if sc.trackStats {
				e.taskMu.Lock()
				e.taskDuplicates++
				e.taskMu.Unlock()
			}
			output := fmt.Sprintf(instructions.GateDuplicateReadTmpl, entry.CallID)
			end(output)
			return toolOutcome{Call: call, Output: output, Failed: false}
		}
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
		// The workspace checklist feed lives here, at the ONE point both the
		// main loop and every subagent pass through, so a subagent checking off
		// its work refreshes the panel exactly like a main-loop edit does.
		if e.touchesChecklist(call) {
			e.refreshChecklist()
		}
		// Record changed files from EITHER scope. Under the plan/execute split
		// the executor makes every workspace change, so a main-loop-only tally
		// would report zero files changed and silently disable the review gate.
		e.noteChangedFiles(call)
	}
	return toolOutcome{Call: call, Output: output, Failed: failed, Err: dispatchErr}
}
