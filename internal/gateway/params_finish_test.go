package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// B-2: an optional param is emitted only when the model's profile supports it (or
// the profile is empty = permissive), so the request builder honors model.go's
// "never emits an unsupported param" contract.
func TestReasoningEffortGatedBySupportedParams(t *testing.T) {
	if !profileSupportsParam(ModelProfile{SupportedParams: deepSeekSupportedParams}, "reasoning_effort") {
		t.Error("DeepSeek's profile lists reasoning_effort — it must be emitted")
	}
	if profileSupportsParam(ModelProfile{SupportedParams: []string{"model", "messages", "reasoning_split"}}, "reasoning_effort") {
		t.Error("a profile without reasoning_effort must not advertise it")
	}
	if !profileSupportsParam(ModelProfile{}, "reasoning_effort") {
		t.Error("an empty (unprofiled) SupportedParams must be permissive")
	}
}

// B-3: finish_reason is threaded through to the caller so a "length" truncation is
// distinguishable from a natural stop.
func TestFinishReasonThreadedThrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test",
		IdleTimeout: 2 * time.Second, RequestLifetime: 5 * time.Second,
	})
	resp, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "length" {
		t.Fatalf("FinishReason = %q, want %q (truncation must be surfaced)", resp.FinishReason, "length")
	}
}
