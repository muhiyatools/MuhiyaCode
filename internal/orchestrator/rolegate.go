package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// Feature 012 phase-scoped role gate (contracts/role-gate.md). During
// full-depth implementation the MAIN model's implementation-file reads are
// denied with dispatch guidance: reads belong to the implementation subagents,
// the orchestrator works from reports. Gate-policy compliance: stated in
// advance by the implementing-phase prelude, one-step-fixable (dispatch or
// await), bounded (two denials then a recorded waiver), telemetered, and
// exempt wherever the harness itself directs main-model reads (degraded
// phases, post-failure diagnosis, light depth, validation).

// phaseReadGate returns (outcome, true) when the call is denied. It never
// records into the failed-call cache: the identical read becomes legal the
// moment the phase changes, and a cached denial would strand it.
func (e *Engine) phaseReadGate(ctx context.Context, sc dispatchScope, call contract.ToolCall, name, signature string) (toolOutcome, bool) {
	if !sc.trackStats || !e.contextLinkingEnabled() {
		return toolOutcome{}, false
	}
	isRead := name == "read_file"
	if name == "run_shell" {
		var args struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) != nil {
			return toolOutcome{}, false
		}
		// RG-2: shell-read equivalents share the gate; other shell commands
		// (builds, tests) pass — they are the orchestrator's job.
		isRead = terminalReadCommandRE.MatchString(args.Command)
	}
	if !isRead {
		return toolOutcome{}, false
	}
	l := e.Lifecycle()
	if !l.Orchestrated() || l.Depth != PipelineDepthFull || l.State != contract.LifecycleImplementing {
		return toolOutcome{}, false
	}
	if l.HasAnyDegradation() {
		// Degradation paths promise main-model reads in their advance-notice
		// texts (RG-1); the gate honoring that promise is what keeps clause (a).
		e.notePhaseReadExempt(ctx, "degraded")
		return toolOutcome{}, false
	}
	e.taskMu.Lock()
	if e.readGate.postFailure {
		e.readGate.exempt++
		e.taskMu.Unlock()
		return toolOutcome{}, false
	}
	if e.readGate.waived {
		e.taskMu.Unlock()
		return toolOutcome{}, false
	}
	if e.readGate.denied >= 2 {
		// RG-3 bound: the third attempt proceeds with a recorded waiver — a
		// gate can NEVER loop (gate-policy clause c).
		e.readGate.waived = true
		e.taskMu.Unlock()
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "phase-read-waived", name)
		return toolOutcome{}, false
	}
	e.readGate.denied++
	denied := e.readGate.denied
	e.taskMu.Unlock()
	output := fmt.Sprintf(instructions.GatePhaseReadBlockTmpl, denied)
	e.recordHarnessEvent(ctx, contract.HarnessGate, "phase-read-block", name)
	if sc.onEnd != nil {
		sc.onEnd(call, output)
	}
	_ = signature
	return toolOutcome{Call: call, Output: output, Failed: true}, true
}

// notePhaseReadExempt counts an exempt read; the harness event fires once per
// reason per task (evidence without friction spam).
func (e *Engine) notePhaseReadExempt(ctx context.Context, reason string) {
	e.taskMu.Lock()
	e.readGate.exempt++
	first := e.readGate.exempt == 1
	e.taskMu.Unlock()
	if first {
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "phase-read-exempt:"+reason, "")
	}
}

// markImplementFailureDiagnosis opens the gate for the phase's remainder after
// a failed implementation dispatch (RG-4): the orchestrator must never be
// blind after a failure (Constitution I).
func (e *Engine) markImplementFailureDiagnosis(ctx context.Context) {
	e.taskMu.Lock()
	already := e.readGate.postFailure
	e.readGate.postFailure = true
	e.taskMu.Unlock()
	if !already {
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "phase-read-exempt:post-failure", "")
	}
}

// resetLinkTaskState starts a task with a fresh link ledger and read-gate
// state; the task ordinal stamps record lineage so phase chains and
// follow-ups are distinguishable (contextlink.go decideLink). Caller holds
// taskMu (the task-start reset block).
func (e *Engine) resetLinkTaskState() {
	e.taskLinks = nil
	e.readGate = readGateState{}
	e.taskSeq++
}

// finalizeLinkStats stamps the feature-012 ledgers onto the task stats
// (FR-015): per-dispatch link outcomes and the read gate's counters.
func (e *Engine) finalizeLinkStats(stats *contract.TaskStats) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	stats.Links = append([]contract.LinkOutcome(nil), e.taskLinks...)
	stats.ReadGate = contract.ReadGateStats{Denied: e.readGate.denied, Exempt: e.readGate.exempt}
	if e.readGate.waived {
		stats.ReadGate.Waived = 1
	}
}
