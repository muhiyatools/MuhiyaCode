package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) taskToolAdmission() (bool, string) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	budget := e.taskBudget
	if budget.Class == "" {
		return true, ""
	}
	if e.taskFinalizing {
		return false, "task is complete or at its hard budget; give the final answer without another tool call"
	}
	if e.taskToolAttempts >= budget.ToolCalls {
		e.taskOverBudget++
		return false, fmt.Sprintf("hard tool limit reached (%d); give the final factual status", budget.ToolCalls)
	}
	e.taskToolAttempts++
	return true, ""
}

func (e *Engine) taskCheckAdmission(call contract.ToolCall) (bool, string) {
	if !isCheckCall(call) {
		return true, ""
	}
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskBudget.Class == "" {
		return true, ""
	}
	if e.taskCheckAttempts >= e.taskBudget.MaxChecks+e.taskCheckRetries {
		e.taskOverBudget++
		return false, fmt.Sprintf("verification limit reached (%d planned checks; a retry is allowed only after an actual failure); do not create another check or harness", e.taskBudget.MaxChecks)
	}
	e.taskCheckAttempts++
	return true, ""
}

func (e *Engine) grantFailedCheckRetry() {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskCheckRetries == 0 {
		e.taskCheckRetries = 1
	}
}

func (e *Engine) taskScopeAllows(call contract.ToolCall) (bool, string) {
	extension, changed := e.singleArtifactState()
	if extension == "" || !e.callMayMutate(call) {
		return true, ""
	}
	if call.ToolName() == "run_shell" {
		return false, "single-file task: mutating shell commands and scratch harnesses are out of scope; write the requested file directly"
	}
	paths := contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON()))
	if len(paths) == 0 {
		return false, "single-file task: this mutation has no target matching the requested artifact"
	}
	allowedPath, reason := singleArtifactTarget(paths, extension)
	if reason != "" {
		return false, reason
	}
	for existing := range changed {
		if existing != allowedPath {
			return false, fmt.Sprintf("single-file task: %s is already the deliverable; a second artifact is out of scope", existing)
		}
	}
	return true, ""
}

func (e *Engine) singleArtifactState() (string, map[string]bool) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	changed := make(map[string]bool, len(e.taskFilesChanged))
	for path := range e.taskFilesChanged {
		changed[path] = true
	}
	return e.taskBudget.SingleArtifactExtension, changed
}

func singleArtifactTarget(paths []string, extension string) (string, string) {
	var allowedPath string
	for _, path := range paths {
		normalized := filepathSlash(path)
		if !strings.EqualFold(filepath.Ext(normalized), extension) {
			return "", fmt.Sprintf("single-file task: only one %s artifact is allowed; %s is out of scope", extension, normalized)
		}
		if allowedPath != "" && allowedPath != normalized {
			return "", "single-file task: one mutation cannot target multiple files"
		}
		allowedPath = normalized
	}
	return allowedPath, ""
}

func (e *Engine) beginTaskFinalization() bool {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskFinalizing {
		return false
	}
	e.taskFinalizing = true
	return true
}

func (e *Engine) taskTokenLimitReached() bool {
	// Task token limits disabled per user budget policy configuration - tasks are never terminated by hard task token limits
	return false
}

func (e *Engine) taskRequestPolicy() (contract.ReasoningTier, int) {
	e.taskMu.Lock()
	budget := e.taskBudget
	e.taskMu.Unlock()
	if budget.Class == "" {
		return ReasoningForEffort(e.effort()), e.outputBudget(e.settings.Provider.ActiveModelID)
	}
	return budget.Reasoning, e.outputBudget(e.settings.Provider.ActiveModelID)
}

func shouldFinalizeAfterEvidence(budget Budget, toolCalls, checksRun, changedFiles int) bool {
	if toolCalls >= budget.ToolCalls {
		return true
	}
	if changedFiles == 0 || checksRun == 0 {
		return false
	}
	return checksRun >= budget.MaxChecks && (budget.Class == ClassTiny || budget.SingleArtifactExtension != "")
}
