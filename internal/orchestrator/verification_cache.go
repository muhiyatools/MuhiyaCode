package orchestrator

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type verificationCacheEntry struct {
	Fingerprint string
	Result      contract.ToolResult
}

func (e *Engine) cachedVerification(ctx context.Context, call contract.ToolCall) (contract.ToolResult, bool) {
	if !isCheckCall(call) || e.workspaceFingerprint == nil {
		return contract.ToolResult{}, false
	}
	command := verificationCommand(call)
	if command == "" {
		return contract.ToolResult{}, false
	}
	fingerprint, err := e.workspaceFingerprint(ctx)
	if err != nil || fingerprint == "" {
		return contract.ToolResult{}, false
	}
	e.verificationMu.Lock()
	entry, exists := e.verificationCache[command]
	e.verificationMu.Unlock()
	if !exists || entry.Fingerprint != fingerprint || entry.Result.Status != contract.ToolOutcomeSucceeded {
		return contract.ToolResult{}, false
	}
	result := entry.Result
	result.Output = "[verification cache hit: workspace fingerprint unchanged]\n" + result.Output
	return result, true
}

func (e *Engine) rememberVerification(ctx context.Context, call contract.ToolCall, result contract.ToolResult) {
	if !isCheckCall(call) || result.Status != contract.ToolOutcomeSucceeded || e.workspaceFingerprint == nil {
		return
	}
	command := verificationCommand(call)
	if command == "" {
		return
	}
	fingerprint, err := e.workspaceFingerprint(ctx)
	if err != nil || fingerprint == "" {
		return
	}
	result.Err = nil
	e.verificationMu.Lock()
	e.verificationCache[command] = verificationCacheEntry{Fingerprint: fingerprint, Result: result}
	e.verificationMu.Unlock()
}

func verificationCommand(call contract.ToolCall) string {
	var input struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(call.ArgumentsJSON()), &input) != nil {
		return ""
	}
	return strings.TrimSpace(input.Command)
}
