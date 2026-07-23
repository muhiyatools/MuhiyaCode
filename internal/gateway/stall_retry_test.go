package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// B-1: a stream that goes silent mid-response must be RETRIED, not surfaced as a
// user "stopped." The first attempt sends one frame then stalls past the idle
// window; the retry completes. Before the fix the stall arrived as raw
// context.Canceled, which isNetworkError excluded, so the stream retry never
// fired and the task died.
func TestStalledStreamRetriesAndSucceeds(t *testing.T) {
	const idle = 150 * time.Millisecond
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		if attempts.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"))
			flusher.Flush()
			time.Sleep(idle * 5) // go silent well past the idle window
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 2,
		IdleTimeout: idle, RequestLifetime: 5 * time.Second,
	})
	resp, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("a mid-stream stall must be retried, not surfaced as an error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want the retried answer %q (the stalled partial is discarded)", resp.Content, "ok")
	}
	if got := attempts.Load(); got < 2 {
		t.Fatalf("attempts = %d, want >= 2 — the stall must trigger a retry", got)
	}
}

// The classification a StalledError must satisfy for the retry and recovery
// paths to engage, and for the user to never see "stopped."
func TestStalledErrorClassification(t *testing.T) {
	stall := &StalledError{Phase: "mid-stream"}
	if !isNetworkError(stall) {
		t.Error("isNetworkError(StalledError) must be true so the Chat stream retry fires")
	}
	if !Recoverable(stall) {
		t.Error("Recoverable(StalledError) must be true so the turn-loop recovery fires")
	}
	if got := FriendlyRequestError(stall); strings.Contains(strings.ToLower(got), "stopped") {
		t.Errorf("FriendlyRequestError(StalledError) = %q, must not read as a user stop", got)
	}
}
