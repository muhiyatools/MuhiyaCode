package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// TestRawUsageObserverToleratesFrameCount (A2 / T006) verifies that a benchmark
// RawUsageObserver no longer fails an otherwise-successful request when the
// provider emits zero or several usage frames. Zero frames ⇒ success, observer
// not called; multiple frames ⇒ success, observer called once with the LAST
// frame. A hard error here would abort the very benchmark runs that validate the
// cache metrics.
func TestRawUsageObserverToleratesFrameCount(t *testing.T) {
	sse := func(w http.ResponseWriter, payload string) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(payload))
	}

	t.Run("zero frames succeeds without recording", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sse(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\ndata: [DONE]\n\n")
		}))
		defer server.Close()
		var observed int
		provider := NewOpenAICompatible(Config{
			Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second,
			RawUsageObserver: func(RawUsagePayload) error { observed++; return nil },
		})
		resp, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
		if err != nil {
			t.Fatalf("zero usage frames must not fail the request: %v", err)
		}
		if resp.Content != "done" {
			t.Fatalf("unexpected content %q", resp.Content)
		}
		if observed != 0 {
			t.Fatalf("observer should not be called with no usage frame, called %d", observed)
		}
	})

	t.Run("multiple frames succeeds recording the last", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sse(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}],\"usage\":{\"prompt_tokens\":10,\"prompt_cache_hit_tokens\":4}}\n\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}],\"usage\":{\"prompt_tokens\":20,\"prompt_cache_hit_tokens\":16}}\n\n"+
				"data: [DONE]\n\n")
		}))
		defer server.Close()
		var recorded []RawUsagePayload
		provider := NewOpenAICompatible(Config{
			Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second,
			RawUsageObserver: func(p RawUsagePayload) error { recorded = append(recorded, p); return nil },
		})
		if _, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}}); err != nil {
			t.Fatalf("multiple usage frames must not fail the request: %v", err)
		}
		if len(recorded) != 1 {
			t.Fatalf("observer should be called exactly once for the last frame, called %d", len(recorded))
		}
		if !bytes.Contains(recorded[0].Payload, []byte("\"prompt_tokens\":20")) {
			t.Fatalf("observer should have recorded the LAST usage frame, got %s", recorded[0].Payload)
		}
	})
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

// C1: X-Muhiya-Session must be present on every chat call, stable across two
// consecutive calls for the same Request (cache pin identity), and derived
// from input.SessionID without ever leaking into the JSON body.
func TestSessionHeaderStableAcrossSameStream(t *testing.T) {
	var observed []string
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = append(observed, r.Header.Get("X-Muhiya-Session"))
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	settings := testSettings(server.URL)
	provider := NewOpenAICompatible(Config{Settings: settings, APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second})
	request := contract.ChatRequest{SessionID: "session-A:main", Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}}
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0] == "" || observed[0] != observed[1] {
		t.Fatalf("X-Muhiya-Session not stable across calls: %#v", observed)
	}
	if observed[0] != "session-A:main" {
		t.Fatalf("X-Muhiya-Session must derive from SessionID, got %q", observed[0])
	}
	for index, body := range bodies {
		if strings.Contains(string(body), "SessionID") || bytes.Contains(body, []byte("session-A:")) {
			t.Fatalf("body %d leaked SessionID: %s", index, body)
		}
	}
}

func TestSessionHeaderDifferentiatesStreams(t *testing.T) {
	var observed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = append(observed, r.Header.Get("X-Muhiya-Session"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	settings := testSettings(server.URL)
	provider := NewOpenAICompatible(Config{Settings: settings, APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second})
	for _, id := range []string{"session-X:main", "session-X:sub", "session-X:aux"} {
		if _, err := provider.Chat(context.Background(), contract.ChatRequest{SessionID: id, Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(observed) != 3 || observed[0] == observed[1] || observed[1] == observed[2] {
		t.Fatalf("streams share a routing pin: %#v", observed)
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
