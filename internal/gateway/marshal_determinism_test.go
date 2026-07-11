package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
