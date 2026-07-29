package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestHistoryAppendOwnsNestedMessagePayloads(t *testing.T) {
	reasoning := "reason"
	message := contract.Message{
		Role:             contract.RoleAssistant,
		ReasoningContent: &reasoning,
		ReasoningDetails: json.RawMessage(`[{"text":"reason"}]`),
		ToolCalls:        []contract.ToolCall{contract.NewToolCall("call-1", "read_file", `{"path":"a.go"}`)},
	}
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(message)

	*message.ReasoningContent = "mutated"
	message.ReasoningDetails[0] = '!'
	message.ToolCalls[0].Arguments[0] = '!'
	message.ToolCalls[0].Function.Arguments = `{"path":"changed.go"}`

	stored := history.All()[0]
	if stored.ReasoningContent == nil || *stored.ReasoningContent != "reason" ||
		string(stored.ReasoningDetails) != `[{"text":"reason"}]` ||
		stored.ToolCalls[0].ArgumentsJSON() != `{"path":"a.go"}` {
		t.Fatalf("history retained caller-owned nested state: %+v", stored)
	}
}
