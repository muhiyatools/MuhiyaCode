package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestMarshalDeterminismForIdenticalLogicalRequest(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider := testDeterministicProvider(server.URL)
	request := deterministicRequest()
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || string(bodies[0]) != string(bodies[1]) {
		t.Fatalf("identical logical requests marshaled differently\nfirst: %s\nsecond:%s", bodies[0], bodies[1])
	}
}

func TestRetryReplayBodyIsByteIdentical(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.Header().Set("Retry-After", "0.001")
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider := testDeterministicProvider(server.URL)
	if _, err := provider.Chat(context.Background(), deterministicRequest()); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || string(bodies[0]) != string(bodies[1]) {
		t.Fatalf("retry body drifted\nfirst: %s\nretry:%s", bodies[0], bodies[1])
	}
}

func TestDeepSeekReasoningReplayUsesMinimalRequiredKey(t *testing.T) {
	messages := []contract.Message{
		{Role: contract.RoleAssistant, Content: "visible", ToolCalls: []contract.ToolCall{contract.NewToolCall("1", "tool", `{}`)}},
		{Role: contract.RoleAssistant, Content: "answer"},
	}
	replayed := replayMessages(messages, ResolveModelProfile("deepseek"), contract.ReasoningHigh)
	if replayed[0].ReasoningContent == nil || *replayed[0].ReasoningContent != "" {
		t.Fatalf("tool-call reasoning key = %#v", replayed[0].ReasoningContent)
	}
	if replayed[1].ReasoningContent != nil {
		t.Fatal("reasoning key leaked onto ordinary assistant message")
	}
}

func TestDeepSeekSettledReplayIsIndependentOfLiveEffort(t *testing.T) {
	messages := []contract.Message{{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("1", "tool", `{}`)}}}
	low, err := json.Marshal(replayMessages(messages, ResolveModelProfile("deepseek"), contract.ReasoningLow))
	if err != nil {
		t.Fatal(err)
	}
	high, err := json.Marshal(replayMessages(messages, ResolveModelProfile("deepseek"), contract.ReasoningMax))
	if err != nil {
		t.Fatal(err)
	}
	if string(low) != string(high) {
		t.Fatalf("effort rewrote settled bytes\nlow: %s\nhigh: %s", low, high)
	}
}

func TestMiniMaxSettledReplayPrefixIsStableAcrossTurns(t *testing.T) {
	details := json.RawMessage(`[{"type":"text","text":"settled"}]`)
	settled := []contract.Message{
		{Role: contract.RoleSystem, Content: "system"},
		{Role: contract.RoleUser, Content: "inspect"},
		{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("call-1", "read_file", `{}`)}, ReasoningDetails: details},
		{Role: contract.RoleTool, ToolCallID: "call-1", Content: "result"},
	}
	profile := ResolveModelProfile("MiniMax-M3")
	first := replayMessages(settled, profile, contract.ReasoningLow)
	second := replayMessages(append(append([]contract.Message(nil), settled...), contract.Message{Role: contract.RoleUser, Content: "continue"}), profile, contract.ReasoningMax)
	firstBytes, _ := json.Marshal(first)
	secondPrefixBytes, _ := json.Marshal(second[:len(first)])
	if string(firstBytes) != string(secondPrefixBytes) {
		t.Fatalf("MiniMax settled prefix changed across turns\nfirst: %s\nnext:  %s", firstBytes, secondPrefixBytes)
	}
	if !strings.Contains(string(firstBytes), `"reasoning_details"`) {
		t.Fatalf("MiniMax stable prefix must preserve reasoning_details: %s", firstBytes)
	}
}

// TestWireMessageShapesFrozen (T002 / contracts/request-assembly.md §3) freezes
// the wire shape of each message role. Any change to these shapes is a one-time
// cache break, so it must be a deliberate, reviewed change — not an accident.
func TestWireMessageShapesFrozen(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ = io.ReadAll(request.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider := testDeterministicProvider(server.URL)
	req := contract.ChatRequest{
		ModelID: "deepseek-test", Reasoning: contract.ReasoningLow,
		Messages: []contract.Message{
			{Role: contract.RoleUser, Content: "hi"},
			{Role: contract.RoleAssistant, Content: "answer"},
			{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "tool", `{"a":1}`)}},
			{Role: contract.RoleTool, ToolCallID: "c1", Content: "result"},
		},
	}
	if _, err := provider.Chat(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("wire body not valid JSON: %v\n%s", err, body)
	}
	if len(parsed.Messages) != 4 {
		t.Fatalf("expected 4 wire messages, got %d:\n%s", len(parsed.Messages), body)
	}
	user := parsed.Messages[0]
	if user["role"] != "user" || user["content"] != "hi" {
		t.Fatalf("user message shape changed: %v", user)
	}
	asstText := parsed.Messages[1]
	if asstText["role"] != "assistant" || asstText["content"] != "answer" {
		t.Fatalf("assistant-text shape changed: %v", asstText)
	}
	asstCall := parsed.Messages[2]
	if asstCall["role"] != "assistant" || asstCall["tool_calls"] == nil {
		t.Fatalf("assistant-tool-call shape changed: %v", asstCall)
	}
	tool := parsed.Messages[3]
	if tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != "result" {
		t.Fatalf("tool-result shape changed: %v", tool)
	}
}

func testDeterministicProvider(baseURL string) *OpenAICompatible {
	var settings contract.Settings
	settings.Provider.BaseURL = baseURL
	settings.Provider.ActiveModelID = "deepseek-test"
	settings.Provider.Models = []contract.Model{{ID: "deepseek-test", Name: "DeepSeek Test", ContextLimit: 128000}}
	return NewOpenAICompatible(Config{Settings: settings, APIKey: "test", MaxRetries: 1})
}

func deterministicRequest() contract.ChatRequest {
	return contract.ChatRequest{
		ModelID: "deepseek-test", Reasoning: contract.ReasoningHigh,
		Messages: []contract.Message{{Role: contract.RoleSystem, Content: "system"}, {Role: contract.RoleUser, Content: "hello"}},
		Tools:    []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{Name: "tool", Parameters: map[string]any{"properties": map[string]any{"b": map[string]any{"type": "string"}, "a": map[string]any{"type": "integer"}}, "type": "object"}}}},
	}
}
