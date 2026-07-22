package orchestrator

import (
	"math"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Task-scoped bookkeeping shared by the dispatch gate and the turn loop.
//
// These four helpers used to live in subagent.go and rolesplit.go — files that
// existed for the plan/execute split. The split is gone (the agent is one
// unified session again), but every one of these serves the MAIN loop and
// outlived it, so they moved here rather than dying with their old homes.

// isMutation reports whether a tool changes durable state. save_memory and
// edit_memory mutate the memory store (feature 008 US5, Memory Parity N2), so
// they take the same plan-mode read-only block as the file edits they parallel
// (memory-tool.md MT-9 — no gate weaker than edit_file's). recall_memory is
// read-only and stays open.
func isMutation(name string) bool {
	return name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "run_shell" || name == "save_memory" || name == "edit_memory" || strings.HasPrefix(name, "mcp__")
}

// hardTurnCeiling is the task's absolute liveness backstop, scaled by effort so
// a max-effort task — which legitimately climbs the runway ladder further — is
// not stopped by a bound calibrated for medium. It is the ONLY unconditional
// turn bound in the loop: everything below it extends while real work lands.
func hardTurnCeilingFor(profile EffortProfile) int {
	return int(math.Ceil(hardTurnCeiling * profile.AgentTurnScale))
}

// noteChangedFiles records a successful mutation's targets on the task ledger.
// It runs in the shared gate, so every write the session makes counts toward
// the tally the review gate and the end-of-task stats read.
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
