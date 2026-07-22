package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestMiniMaxReplayPreservesReasoningDetailsIncludingToolTurns(t *testing.T) {
	details := json.RawMessage(`[{"type":"thinking","text":"inspect first","signature":"sig-123"}]`)
	reasoning := "fallback reasoning"
	messages := []contract.Message{
		{Role: contract.RoleAssistant, Content: "", ToolCalls: []contract.ToolCall{contract.NewToolCall("call-1", "read_file", `{"path":"a.go"}`)}, ReasoningContent: &reasoning, ReasoningDetails: details},
		{Role: contract.RoleTool, ToolCallID: "call-1", Content: "result"},
	}
	replayed := replayMessages(messages, ResolveModelProfile("MiniMax-M3"), contract.ReasoningHigh)
	if string(replayed[0].ReasoningDetails) != string(details) || replayed[0].ReasoningContent != nil {
		t.Fatalf("MiniMax replay changed reasoning: %+v", replayed[0])
	}
	if len(replayed[1].ReasoningDetails) != 0 || replayed[1].ReasoningContent != nil {
		t.Fatalf("reasoning leaked to tool result: %+v", replayed[1])
	}
}

func TestMiniMaxReplayPreservesVisibleContentThinkingSignatureAndToolUse(t *testing.T) {
	details := json.RawMessage(`[{"type":"thinking","text":"bounded thought","signature":"signed"}]`)
	messages := []contract.Message{
		{Role: contract.RoleAssistant, Content: "I will inspect.", ReasoningDetails: details, ToolCalls: []contract.ToolCall{contract.NewToolCall("r1", "read_file", `{"path":"a.go"}`)}},
		{Role: contract.RoleTool, ToolCallID: "r1", Content: "package a"},
	}
	replayed := replayMessages(messages, ResolveModelProfile("minimax-m3"), contract.ReasoningLow)
	if replayed[0].Content != messages[0].Content || string(replayed[0].ReasoningDetails) != string(details) || len(replayed[0].ToolCalls) != 1 || replayed[1].ToolCallID != "r1" {
		t.Fatalf("MiniMax active tool chain changed: %+v", replayed)
	}
}

func TestMiniMaxReplaySynthesizesDetailsFromSettledReasoning(t *testing.T) {
	reasoning := "interleaved thought"
	replayed := replayMessages([]contract.Message{{Role: contract.RoleAssistant, ReasoningContent: &reasoning}}, ResolveModelProfile("M2.7"), contract.ReasoningLow)
	if string(replayed[0].ReasoningDetails) != `[{"text":"interleaved thought","type":"text"}]` {
		t.Fatalf("synthesized reasoning_details = %s", replayed[0].ReasoningDetails)
	}
}

func TestDeepSeekReplayRemainsByteIdentical(t *testing.T) {
	reasoning := "must be stripped"
	messages := []contract.Message{
		{Role: contract.RoleAssistant, Content: "visible", ToolCalls: []contract.ToolCall{contract.NewToolCall("call-1", "read_file", `{}`)}, ReasoningContent: &reasoning, ReasoningDetails: json.RawMessage(`[{"type":"text","text":"also stripped"}]`)},
		{Role: contract.RoleTool, ToolCallID: "call-1", Content: "result"},
	}
	got, err := json.Marshal(replayMessages(messages, ResolveModelProfile("deepseek-reasoner"), contract.ReasoningMax))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"assistant","content":"visible","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{}"}}],"reasoning_content":""},{"role":"tool","content":"result","tool_call_id":"call-1"}]`
	if string(got) != want {
		t.Fatalf("DeepSeek replay bytes changed\ngot:  %s\nwant: %s", got, want)
	}
}

func TestUnknownFamilyKeepsLegacyStripPolicy(t *testing.T) {
	reasoning := "strip"
	replayed := replayMessages([]contract.Message{{Role: contract.RoleAssistant, ReasoningContent: &reasoning, ReasoningDetails: json.RawMessage(`[{}]`)}}, ResolveModelProfile("unknown"), contract.ReasoningHigh)
	if replayed[0].ReasoningContent != nil || len(replayed[0].ReasoningDetails) != 0 {
		t.Fatalf("unknown-family replay policy changed: %+v", replayed[0])
	}
}

func TestReplayRepairsMissingToolResultsWithoutChangingValidHistory(t *testing.T) {
	valid := []contract.Message{
		{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{}`), contract.NewToolCall("b", "read_file", `{}`)}},
		{Role: contract.RoleTool, ToolCallID: "a", Content: "A"},
		{Role: contract.RoleTool, ToolCallID: "b", Content: "B"},
		{Role: contract.RoleUser, Content: "continue"},
	}
	profile := ResolveModelProfile("deepseek-reasoner")
	validReplay := replayMessages(valid, profile, contract.ReasoningHigh)
	if len(validReplay) != len(valid) || validReplay[1].Content != "A" || validReplay[2].Content != "B" {
		t.Fatalf("valid history changed: %+v", validReplay)
	}

	broken := append([]contract.Message(nil), valid[:2]...)
	broken = append(broken, contract.Message{Role: contract.RoleUser, Content: "continue"})
	repaired := replayMessages(broken, profile, contract.ReasoningHigh)
	if len(repaired) != 4 || repaired[2].Role != contract.RoleTool || repaired[2].ToolCallID != "b" || !strings.Contains(repaired[2].Content, "repaired") || repaired[3].Role != contract.RoleUser {
		t.Fatalf("missing result was not repaired in place: %+v", repaired)
	}
}
