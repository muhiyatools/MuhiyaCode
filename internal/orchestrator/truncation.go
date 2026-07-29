package orchestrator

import (
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// Output-cap truncation handling (TB03).
//
// When a model hits its output cap mid-tool-call, the stream still closes
// cleanly, so before this the half-written call was dispatched like any other:
// it failed argument validation, burned a turn, produced advice that told the
// model to "send a shorter version", and — worst — its giant broken payload was
// appended to history and re-sent as input on EVERY later request of the task.
// A few retries of a file write were enough to dominate a session's token bill.
//
// The handling here is: never dispatch it, answer it with the chunked-write
// protocol, count it toward the storm breaker so repeats escalate, and store a
// marker instead of the payload so it is billed once and never again.

// truncatedArgumentsMarker replaces a cut-off call's arguments in everything
// durable (history replay, transcript). It is valid JSON and tiny, so the
// assistant turn still parses and pairs correctly while costing nothing.
const truncatedArgumentsMarker = `{"truncated":true}`

// truncatedSet indexes the response's truncated call IDs for O(1) lookup.
func truncatedSet(response contract.ChatResponse) map[string]bool {
	if len(response.TruncatedCalls) == 0 {
		return nil
	}
	set := make(map[string]bool, len(response.TruncatedCalls))
	for _, id := range response.TruncatedCalls {
		set[id] = true
	}
	return set
}

// splitTruncatedCalls partitions a turn's calls into the ones safe to dispatch
// and the ones cut off at the cap. Order is preserved within each group.
func splitTruncatedCalls(calls []contract.ToolCall, truncated map[string]bool) (executable, cut []contract.ToolCall) {
	if len(truncated) == 0 {
		return calls, nil
	}
	for _, call := range calls {
		if truncated[call.ID] {
			cut = append(cut, call)
			continue
		}
		executable = append(executable, call)
	}
	return executable, cut
}

// sanitizeTruncatedCalls rewrites cut-off calls' arguments to the compact marker
// so the broken payload never enters history (and so never rides every later
// request). Calls that arrived whole are returned untouched.
func sanitizeTruncatedCalls(calls []contract.ToolCall, truncated map[string]bool) []contract.ToolCall {
	if len(truncated) == 0 {
		return calls
	}
	sanitized := make([]contract.ToolCall, 0, len(calls))
	for _, call := range calls {
		if truncated[call.ID] {
			sanitized = append(sanitized, contract.NewToolCall(call.ID, call.ToolName(), truncatedArgumentsMarker))
			continue
		}
		sanitized = append(sanitized, call)
	}
	return sanitized
}

// truncationRecoveryBody is the tool result a cut-off call receives. File writes
// get the chunked-write protocol; everything else gets the compact-retry form.
func truncationRecoveryBody(toolName string) string {
	switch toolName {
	case "write_file", "multi_edit", "apply_patch", "edit_file":
		return instructions.GateTruncatedWriteBody
	default:
		return instructions.GateTruncatedCallBody
	}
}

// truncatedOutcomes answers each cut-off call without dispatching it and records
// a storm-breaker class hit so three repeats escalate through the scope's own
// loop guard (the pre-dispatch rejection path records nothing, which is why a
// truncation loop could previously run to the turn ceiling unchecked).
func (e *Engine) truncatedOutcomes(sc dispatchScope, cut []contract.ToolCall) []toolOutcome {
	outcomes := make([]toolOutcome, 0, len(cut))
	for _, call := range cut {
		output := truncationRecoveryBody(call.ToolName())
		// Class-keyed, not signature-keyed: two truncations of the same write cut
		// at different byte offsets are different strings, so only a class key
		// ("this tool keeps getting cut off") can accumulate.
		e.recordFailedCall(sc, call.ToolName()+" json-truncated", output)
		if sc.onStart != nil {
			sc.onStart(call)
		}
		if sc.onEnd != nil {
			sc.onEnd(call, output)
		}
		outcomes = append(outcomes, toolOutcome{Call: call, Status: contract.ToolOutcomeRejected, Output: output, ErrorText: output, MutationCertainty: contract.MutationNotStarted})
	}
	return outcomes
}
