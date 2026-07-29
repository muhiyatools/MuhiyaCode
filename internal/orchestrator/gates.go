package orchestrator

import (
	"context"
	"fmt"
	"strings"

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
		return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: err.Error(), MutationCertainty: contract.MutationNotStarted, Err: err}
	}
	if allowed, reason := e.taskScopeAllows(call); !allowed {
		output := "task scope denied mutation: " + reason
		e.recordHarnessEvent(ctx, contract.HarnessGate, "task-scope:"+name, reason)
		end(output)
		return toolOutcome{
			Call: call, Status: contract.ToolOutcomeRejected, Output: output,
			ErrorText: output, MutationCertainty: contract.MutationNotStarted,
		}
	}
	if e.callMayMutate(call) && e.recoveryBlockReason != "" {
		output := "recovery required before mutation: " + e.recoveryBlockReason +
			". Inspect with `muhiyacode doctor`, reconcile with `muhiyacode doctor repair-session`, then restart this session."
		end(output)
		return toolOutcome{
			Call: call, Status: contract.ToolOutcomeRejected, Output: output,
			ErrorText: output, MutationCertainty: contract.MutationNotStarted,
		}
	}
	// Recovered text masquerading as a tool call did not pass through the
	// provider's structured tool-call channel. A recovered mutation therefore
	// requires explicit human approval even in auto-accept mode; without an
	// interactive confirmation callback it fails closed.
	if strings.HasPrefix(call.ID, "rescued_") && e.callMayMutate(call) {
		if e.callbacks.Confirm == nil {
			output := "permission denied: recovered mutating tool calls require interactive confirmation"
			end(output)
			return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted}
		}
		approved, err := e.callbacks.Confirm(ctx, "The model emitted an unstructured tool call recovered from text. Allow recovered mutation "+name+"?")
		if err != nil {
			output := fmt.Sprintf("permission denied: recovered mutation confirmation failed: %v", err)
			end(output)
			return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: err.Error(), MutationCertainty: contract.MutationNotStarted, Err: err}
		}
		if !approved {
			output := "permission denied: recovered mutating tool call was not approved"
			end(output)
			return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted}
		}
	}
	if e.callMayMutate(call) {
		if allowed, reason := e.planScopeAllows(call); !allowed {
			output := "plan scope denied mutation: " + reason
			end(output)
			return toolOutcome{
				Call: call, Status: contract.ToolOutcomeRejected, Output: output,
				ErrorText: output, MutationCertainty: contract.MutationNotStarted,
			}
		}
	}
	if allowed, reason := e.taskToolAdmission(); !allowed {
		output := "task governor: " + reason
		e.recordHarnessEvent(ctx, contract.HarnessGate, "task-budget:"+name, reason)
		end(output)
		return toolOutcome{
			Call: call, Status: contract.ToolOutcomeRejected, Output: output,
			ErrorText: output, MutationCertainty: contract.MutationNotStarted,
		}
	}
	// H2: verbatim failed-call short-circuit.
	signature := callSignature(call)
	if prior, had := failedCallLastError(sc.counters, signature); had {
		output := fmt.Sprintf("This exact call already failed: %s. Do not repeat verbatim — change your approach (e.g. re-read the file first).", prior)
		e.recordHarnessEvent(ctx, contract.HarnessGate, "repeat-failed-call:"+name, prior)
		end(output)
		e.recordFailedCall(sc, signature, output)
		return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted}
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
		return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted}
	}
	// Duplicate-read guard: the task-scoped inspection ledger holds every read
	// this session has made, with mtime-gated freshness for files and
	// mutation-invalidated signatures for searches.
	//
	// F8: increment the discipline counter ONLY when the served entry is still
	// intact in the conversation. The Duplicate() check has already confirmed
	// the entry is reachable AND each underlying segment is still intact at the
	// moment of serving, so a successful dedupe is a real token saving worth
	// crediting. The previous code counted simply on the duplicate flag, which
	// double-credited any dedupe served across a brief invalidation-then-restore
	// pattern.
	if entry, duplicate := e.inspection.Duplicate(call, e.history.IsToolResultIntact); duplicate {
		e.taskMu.Lock()
		e.taskDuplicates++
		e.taskMu.Unlock()
		output := fmt.Sprintf(instructions.GateDuplicateReadTmpl, entry.CallID)
		end(output)
		return toolOutcome{Call: call, Status: contract.ToolOutcomeSucceeded, Output: output, MutationCertainty: contract.MutationNotApplicable}
	}
	// H4: cancel-safe pairing before dispatch.
	if ctx.Err() != nil {
		output := "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Status: contract.ToolOutcomeCancelled, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted}
	}
	if cached, ok := e.cachedVerification(ctx, call); ok {
		end(cached.Output)
		return toolOutcome{
			Call: call, Status: cached.Status, Output: cached.Output,
			MutationCertainty: contract.MutationNotApplicable,
		}
	}
	if allowed, reason := e.taskCheckAdmission(call); !allowed {
		output := "task governor: " + reason
		e.recordHarnessEvent(ctx, contract.HarnessGate, "task-checks:"+name, reason)
		end(output)
		return toolOutcome{
			Call: call, Status: contract.ToolOutcomeRejected, Output: output,
			ErrorText: output, MutationCertainty: contract.MutationNotStarted,
		}
	}
	mayMutate := e.callMayMutate(call)
	leaseOwner := ""
	if mayMutate {
		leaseOwner = e.executionTaskID() + ":" + call.ID
		if err := e.fileLeases.acquire(
			leaseOwner,
			e.session.WorkspacePath,
			contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON())),
		); err != nil {
			output := "tool failed: " + err.Error()
			end(output)
			return toolOutcome{
				Call: call, Status: contract.ToolOutcomeRejected, Output: output,
				ErrorText: err.Error(), MutationCertainty: contract.MutationNotStarted, Err: err,
			}
		}
		defer e.fileLeases.release(leaseOwner)
	}
	toolStarted, err := e.persistToolStarted(ctx, call, mayMutate)
	if err != nil {
		output := "tool failed: " + err.Error()
		end(output)
		return toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: err.Error(), MutationCertainty: contract.MutationNotStarted, Err: err}
	}
	var mutationIntent contract.MutationIntent
	if mayMutate {
		mutationIntent, err = e.persistMutationIntent(ctx, call)
		if err != nil {
			output := "tool failed: " + err.Error()
			outcome := toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: err.Error(), MutationCertainty: contract.MutationNotStarted, Err: err}
			outcome = e.finalizeDispatchJournal(ctx, toolStarted, outcome, nil)
			output = outcome.Output
			end(output)
			return outcome
		}
	}
	dispatchResult := sc.dispatch(ctx, call)
	output, dispatchErr := dispatchResult.Output, dispatchResult.Err
	if dispatchErr != nil {
		if output == "" {
			output = fmt.Sprintf("Tool %s failed: %v", name, dispatchErr)
		} else {
			output = fmt.Sprintf("Tool %s failed: %v\n%s", name, dispatchErr, output)
		}
	}
	status := dispatchResult.Status
	certainty := contract.MutationNotApplicable
	errorText := ""
	if mayMutate && dispatchResult.State == contract.ToolExecutionNotStarted {
		certainty = contract.MutationNotStarted
	} else if mayMutate && status == contract.ToolOutcomeSucceeded {
		certainty = contract.MutationCommitted
	}
	if status != contract.ToolOutcomeSucceeded {
		errorText = output
		if dispatchErr != nil {
			errorText = dispatchErr.Error()
		}
	}
	indeterminate := mayMutate && dispatchResult.State == contract.ToolExecutionIndeterminate
	if indeterminate {
		status = contract.ToolOutcomeIndeterminate
		certainty = contract.MutationIndeterminate
		output += "\nmutation outcome: indeterminate; inspect the workspace or external system before retrying"
	}
	outcome := toolOutcome{Call: call, Status: status, Output: output, ErrorText: errorText, MutationCertainty: certainty, Err: dispatchErr}
	outcome.Output = CapToolOutput(outcome.Output, effort.ToolOutputCap)
	var intent *contract.MutationIntent
	if mayMutate {
		intent = &mutationIntent
	}
	outcome = e.finalizeDispatchJournal(ctx, toolStarted, outcome, intent)
	if outcome.Succeeded() {
		e.rememberVerification(ctx, call, contract.ToolResult{Status: outcome.Status, Output: outcome.Output})
	}
	output, dispatchErr = outcome.Output, outcome.Err
	if sc.postDispatch != nil {
		sc.postDispatch(call, contract.ToolResult{Status: outcome.Status, Output: output, Err: dispatchErr})
	}
	end(output)
	if outcome.IsFailure() {
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
		e.recordTaskEvidence(call)
	}
	return outcome
}
