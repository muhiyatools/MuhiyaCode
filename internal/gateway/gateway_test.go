package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestSSEAccumulator(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hi ","reasoning_content":"think","tool_calls":[{"index":0,"id":"c1","function":{"name":"read_","arguments":"{\"pa"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":"there","tool_calls":[{"index":0,"function":{"name":"file","arguments":"th\":\"a.go\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":8}}}`,
		`data: [DONE]`,
	}, "\n")
	result, err := ParseOpenAIStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Hi there" || result.Reasoning != "think" || len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName() != "read_file" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Usage.PromptTokens != 12 || result.Usage.CachedTokens != 8 {
		t.Fatalf("usage not parsed: %+v", result.Usage)
	}
}

func TestProviderVirtualModelAndRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("busy"))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "virtual-pro" {
			t.Errorf("wire model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	settings := testSettings(server.URL)
	provider := NewOpenAICompatible(Config{Settings: settings, APIKey: "sk-test-value", MaxRetries: 2, IdleTimeout: time.Second})
	response, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "done" || calls.Load() != 2 {
		t.Fatalf("response=%+v calls=%d", response, calls.Load())
	}
}

func TestProviderErrorsOnEmptyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": comment only, no data lines\n\n"))
	}))
	defer server.Close()
	settings := testSettings(server.URL)
	provider := NewOpenAICompatible(Config{Settings: settings, APIKey: "sk-test-value", MaxRetries: 1, IdleTimeout: time.Second})
	_, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected an error for a stream with no recognizable data, got a fabricated success")
	}
}

func TestRescueToolCalls(t *testing.T) {
	content := `<|DSML|tool_calls><|DSML|invoke name="edit_file"><|DSML|parameter name="path">a.go</|DSML|parameter><|DSML|parameter name="old_string">true</|DSML|parameter><|DSML|parameter name="new_string">false</|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`
	calls, clean := RescueToolCalls(content, []string{"edit_file"})
	if len(calls) != 1 || clean != "" {
		t.Fatalf("calls=%+v clean=%q", calls, clean)
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(calls[0].ArgumentsJSON()), &args)
	if args["oldString"] != "true" || args["newString"] != "false" {
		t.Fatalf("string arguments coerced: %#v", args)
	}
}

func testSettings(base string) contract.Settings {
	var settings contract.Settings
	settings.Version = 1
	settings.Provider.Type = "openai-compatible"
	settings.Provider.BaseURL = base
	settings.Provider.ActiveModelID = "virtual-pro"
	settings.Provider.Models = []contract.Model{{ID: "virtual-pro", Name: "Display Name", ContextLimit: 128000}}
	return settings
}
