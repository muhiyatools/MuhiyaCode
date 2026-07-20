package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// The plan/execute role split (owner directive 2026-07-20, NATIVE_AGENT_PLAN
// §7 F0). The MAIN model analyzes, plans, and instructs; the EXECUTION model
// does the work inside a subagent session whose cache stays warm all session.
// The main model must not perform execution or anything unrelated to planning.
//
// Two layers enforce this. The instruction layer (the operating contract and
// the run_subagent description) tells the model to compose a handoff and
// delegate. This gate is the second layer: it makes the boundary real, so a
// model that forgets gets a specific, actionable refusal instead of silently
// doing the executor's job in the expensive stream.
//
// Deliberate carve-outs, each with a reason:
//   - tasks.md — the checklist IS the execution strategy, and writing it is
//     planning work.
//   - save_memory / edit_memory — recording decisions is planning, not
//     execution; the memory store is not the workspace.
//   - propose_changes — an approval flow, not a direct mutation.
//   - subagent scopes — the executor must obviously be able to execute.

// workspaceMutation reports whether a call changes workspace files. run_shell
// counts only when the command is not read-only: builds, tests, and status
// probes are how the main model verifies a report, and blocking those would
// leave it unable to check the executor's work.
func workspaceMutation(call contract.ToolCall) bool {
	name := call.ToolName()
	switch name {
	case "edit_file", "multi_edit", "write_file", "apply_patch":
		return true
	case "run_shell":
		var args struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) != nil {
			return true // unparseable shell is treated as mutating (fail closed)
		}
		return !IsReadOnlyShell(args.Command)
	}
	// MCP tools can do anything, including writing files, so they are execution.
	return strings.HasPrefix(name, "mcp__")
}

// executionRoleGate blocks workspace mutations attempted from the MAIN loop.
// It returns (outcome, true) when the call is refused. Subagent scopes pass
// through untouched — sc.trackStats is true only for the main loop.
//
// It deliberately does NOT record into the failed-call cache: the identical
// call is legitimate the moment the executor makes it, and a cached denial
// would poison the executor's own attempt.
//
// Gate-policy compliance: stated in advance (the operating contract's rule 2),
// one-step fixable (call run_subagent), and ESCALATING rather than repeating —
// a gate that returns the same sentence forever is indistinguishable from a
// loop, which the fault-injection invariant treats as a liveness failure.
func (e *Engine) executionRoleGate(ctx context.Context, sc dispatchScope, call contract.ToolCall) (toolOutcome, bool) {
	if !sc.trackStats || !workspaceMutation(call) {
		return toolOutcome{}, false
	}
	// The checklist is the main model's own artifact.
	if e.touchesChecklist(call) {
		return toolOutcome{}, false
	}
	e.taskMu.Lock()
	e.taskRoleBlocks++
	blocks := e.taskRoleBlocks
	e.taskMu.Unlock()
	output := instructions.GateRoleExecutionBody
	if blocks >= 3 {
		output = fmt.Sprintf(instructions.GateRoleExecutionEscalatedTmpl, blocks)
	}
	e.recordHarnessEvent(ctx, contract.HarnessGate, "role-execution-block", call.ToolName())
	if sc.onEnd != nil {
		sc.onEnd(call, output)
	}
	return toolOutcome{Call: call, Output: output, Failed: true}, true
}

// hardTurnCeiling is the task's absolute liveness backstop, scaled by effort so
// a max-effort task — which legitimately climbs the runway ladder further — is
// not stopped by a bound calibrated for medium. It is the ONLY remaining
// unconditional turn bound in the main loop: everything below it extends while
// real work lands.
func (e *Engine) hardTurnCeiling() int {
	return int(math.Ceil(hardTurnCeiling * Profile(e.effort()).AgentTurnScale))
}

// noteChangedFiles records a successful mutation's targets on the task ledger.
// It runs in the SHARED gate, so files changed by the execution agent count
// toward the same task tally the main loop's own writes would — the review
// gate and the end-of-task stats must see the work wherever it happened.
func (e *Engine) noteChangedFiles(call contract.ToolCall) {
	paths := contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON()))
	if len(paths) == 0 {
		return
	}
	e.taskMu.Lock()
	if e.taskFilesChanged == nil {
		e.taskFilesChanged = map[string]bool{}
	}
	for _, path := range paths {
		e.taskFilesChanged[filepathSlash(path)] = true
	}
	e.taskMu.Unlock()
}

// mergeChangedFiles folds the shared ledger into the turn loop's own set.
func (e *Engine) mergeChangedFiles(files map[string]bool) {
	e.taskMu.Lock()
	for path := range e.taskFilesChanged {
		files[path] = true
	}
	e.taskMu.Unlock()
}
