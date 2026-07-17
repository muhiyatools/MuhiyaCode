package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

func (e *Engine) recordPlanViolationAndBlock(ctx context.Context, sc dispatchScope, call contract.ToolCall, kind, soft string) toolOutcome {
	sc.counters.mu.Lock()
	sc.counters.planViolations++
	n := sc.counters.planViolations
	sc.counters.mu.Unlock()
	out := soft
	if n >= 3 {
		out = fmt.Sprintf(instructions.GatePlanModeEscalatedTmpl, n, kind)
	} else {
		out = soft + instructions.GatePlanModeSoftSuffix
	}
	e.recordHarnessEvent(ctx, contract.HarnessGate, "plan-mode-"+kind+"-block", call.ToolName())
	if sc.onEnd != nil {
		sc.onEnd(call, out)
	}
	return toolOutcome{Call: call, Output: out, Failed: true}
}

func (e *Engine) recordPipelineViolationAndBlock(ctx context.Context, sc dispatchScope, call contract.ToolCall, state contract.LifecycleState) toolOutcome {
	sc.counters.mu.Lock()
	sc.counters.planViolations++
	n := sc.counters.planViolations
	sc.counters.mu.Unlock()
	phase := pipelineLabel(state)
	out := fmt.Sprintf(instructions.GatePipelineMutationTmpl, phase)
	if n >= 3 {
		out = fmt.Sprintf(instructions.GatePipelineEscalatedTmpl, n, phase)
	}
	e.recordHarnessEvent(ctx, contract.HarnessGate, "pipeline-mutation-block", call.ToolName()+" in "+phase)
	if sc.onEnd != nil {
		sc.onEnd(call, out)
	}
	return toolOutcome{Call: call, Output: out, Failed: true}
}

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
	// 009 PL-6 / 010 UL-3: the ONE mutation gate. The 009 audit proved the two
	// former gates guarded the SAME state set — the read-only window
	// (research/planning/awaiting-approval) — so they collapse onto one predicate,
	// BlocksMutation(). An orchestrated read-only phase emits the pipeline block
	// (main loop only, where the implementation gate is enforced); a manual
	// plan-mode read-only state emits the plan-mode block. Both preserve their
	// escalation counter and every pinned byte. A read-only shell probe passes in
	// a manual plan-mode state (as before) but not in an orchestrated phase, where
	// every mutation — run_shell included — is blocked until implementation.
	if isMutation(name) {
		l := e.Lifecycle()
		if l.State.BlocksMutation() {
			if sc.trackStats && l.Orchestrated() {
				return e.recordPipelineViolationAndBlock(ctx, sc, call, l.State)
			}
			if name == "run_shell" {
				var args struct {
					Command string `json:"command"`
				}
				if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) == nil && IsReadOnlyShell(args.Command) {
					// Allow read-only shell probes through.
				} else {
					return e.recordPlanViolationAndBlock(ctx, sc, call, "shell", instructions.GatePlanModeShellBlocked)
				}
			} else if strings.HasPrefix(name, "mcp__") {
				// 004 US3 (T033): name the allowed alternative instead of a bare block.
				return e.recordPlanViolationAndBlock(ctx, sc, call, "mcp", instructions.GatePlanModeMCPBlocked)
			} else {
				return e.recordPlanViolationAndBlock(ctx, sc, call, "mutation", instructions.GatePlanModeMutationBody)
			}
		}
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
	if dispatchErr != nil && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		output = fmt.Sprintf("Tool %s failed: %v", name, dispatchErr)
	}
	// Re-check after dispatch in case cancellation landed during execution.
	if ctx.Err() != nil && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		output = "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	output = CapToolOutput(output, effort.ToolOutputCap)
	failed := errors.Is(dispatchErr, contract.ErrPlanModeExited) || IsToolFailure(output, dispatchErr)
	if sc.postDispatch != nil {
		sc.postDispatch(call, output, failed, dispatchErr)
	}
	end(output)
	if failed && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		e.recordHarnessEvent(ctx, contract.HarnessTool, "tool-failure:"+name, output)
		e.recordFailedCall(sc, signature, output)
	} else if !failed {
		sc.counters.mu.Lock()
		delete(sc.counters.failedCalls, signature)
		sc.counters.mu.Unlock()
	}
	return toolOutcome{Call: call, Output: output, Failed: failed, Err: dispatchErr}
}
