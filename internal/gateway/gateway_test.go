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
	result, err := decodeSSEStream(stream, ModelProfile{})
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
	var attempts []string
	var requestIDs []string
	var cacheEpochs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts = append(attempts, r.Header.Get("X-Muhiya-Attempt"))
		requestIDs = append(requestIDs, r.Header.Get("X-Muhiya-Request-ID"))
		cacheEpochs = append(cacheEpochs, r.Header.Get("X-Muhiya-Cache-Epoch"))
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
	response, err := provider.Chat(context.Background(), contract.ChatRequest{
		RequestID: "logical-turn-1", CacheEpoch: 7,
		Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "done" || calls.Load() != 2 {
		t.Fatalf("response=%+v calls=%d", response, calls.Load())
	}
	if len(attempts) != 2 || attempts[0] != "1" || attempts[1] != "2" {
		t.Fatalf("attempt headers = %#v", attempts)
	}
	if len(requestIDs) != 2 || requestIDs[0] != "logical-turn-1" || requestIDs[1] != requestIDs[0] {
		t.Fatalf("logical request headers = %#v", requestIDs)
	}
	if len(cacheEpochs) != 2 || cacheEpochs[0] != "7" || cacheEpochs[1] != "7" {
		t.Fatalf("cache epoch headers = %#v", cacheEpochs)
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

// OpenRouter sticky routing receives the same stable identity in its documented
// header/body forms. The legacy Muhiya header remains identical during rollout.
func TestSessionHeaderStableAcrossSameStream(t *testing.T) {
	var observed []string
	var openRouterHeaders []string
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = append(observed, r.Header.Get("X-Muhiya-Session"))
		openRouterHeaders = append(openRouterHeaders, r.Header.Get("X-Session-Id"))
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	settings := testSettings(server.URL + "/openrouter.ai")
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
	if len(openRouterHeaders) != 2 || openRouterHeaders[0] != observed[0] || openRouterHeaders[1] != observed[1] {
		t.Fatalf("OpenRouter session header diverged: %#v vs %#v", openRouterHeaders, observed)
	}
	for index, body := range bodies {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["session_id"] != "session-A:main" {
			t.Fatalf("body %d session_id = %v", index, payload["session_id"])
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

func TestSessionIDBodyScope(t *testing.T) {
	if !supportsSessionIDBody("https://openrouter.ai/api/v1") || !supportsSessionIDBody("https://api.muhiya.com/v1") {
		t.Fatal("known routing gateways must receive session_id")
	}
	if supportsSessionIDBody("https://api.deepseek.com/v1") || supportsSessionIDBody("http://localhost:8080/v1") {
		t.Fatal("generic OpenAI-compatible endpoints must not receive non-standard session_id")
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

func TestNormalizeModelsVirtualName(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":           "minimax/minimax-m3",
				"virtual_name": "minimax-m3",
				"display_name": "MiniMax M3",
				"coding_tier":  float64(5),
				"info": map[string]any{
					"input_cost_per_million":  "0.30",
					"output_cost_per_million": float64(1.20),
					"health":                  "healthy",
				},
			},
			map[string]any{
				"id":    "google/gemini-2.5-flash-lite",
				"alias": "gemini-2.5-flash-lite",
			},
		},
	}
	models := normalizeModels(payload)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "minimax-m3" || models[0].Name != "MiniMax M3" {
		t.Errorf("model[0] = %+v, want ID minimax-m3, Name MiniMax M3", models[0])
	}
	if models[0].CodingTier != 5 || models[0].InputCostPerMillion != 0.30 ||
		models[0].OutputCostPerMillion != 1.20 || models[0].Health != "healthy" {
		t.Errorf("model routing metadata was not normalized: %+v", models[0])
	}
	if models[1].ID != "gemini-2.5-flash-lite" {
		t.Errorf("model[1] = %+v, want ID gemini-2.5-flash-lite", models[1])
	}
}

func TestNormalizeMuhiyaCatalogV2PreservesContract(t *testing.T) {
	payload := map[string]any{
		"schema_version": float64(2),
		"models": []any{map[string]any{
			"record_id": "model:abc", "model_id": "minimax-m3",
			"target_model": "minimax/minimax-m3", "display_name": "MiniMax M3",
			"tags": []any{"muhiyacode"}, "provider_family": "minimax-openrouter",
			"adapter_version": "1", "compatibility_epoch": float64(3),
			"context_window": float64(1_000_000), "max_output_tokens": float64(16_000),
			"supported_parameters": []any{"messages", "tools"},
			"cache_contract":       map[string]any{"supports_prompt_cache": true},
			"pricing": map[string]any{
				"rule_set_id":                      "pricing:m3",
				"input_nano_usd_per_million":       float64(300_000_000),
				"output_nano_usd_per_million":      float64(1_200_000_000),
				"cache_read_nano_usd_per_million":  float64(60_000_000),
				"cache_write_nano_usd_per_million": float64(300_000_000),
				"tiers":                            []any{},
			},
			"capabilities": map[string]any{"thinking": true},
			"health":       "healthy",
		}},
	}
	models := normalizeModels(payload)
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	model := models[0]
	if model.RecordID != "model:abc" || model.TargetModel != "minimax/minimax-m3" ||
		model.CompatibilityEpoch != 3 || model.InputNanoPerMillion != 300_000_000 ||
		!model.Capabilities.Thinking {
		t.Fatalf("catalog contract lost fields: %+v", model)
	}
}
