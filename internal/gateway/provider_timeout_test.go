package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestKeepAliveResetsIdleTimeout proves the rolling idle timeout is refreshed by
// streaming keep-alive traffic (feature 007 R1). The upstream drips ": keep-alive"
// SSE comment lines for well past the idle window before finally emitting the real
// data frame; because each line resets the timer, the request must SUCCEED even
// though the total silent-of-data span exceeds IdleTimeout.
func TestKeepAliveResetsIdleTimeout(t *testing.T) {
	const idle = 200 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server response writer is not a flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		// Six keep-alive comments spaced under the idle window; their combined span
		// (~360ms) exceeds IdleTimeout, so a non-resetting timer would have fired.
		for i := 0; i < 6; i++ {
			time.Sleep(idle * 3 / 10)
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1,
		IdleTimeout: idle, RequestLifetime: 5 * time.Second,
	})
	resp, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("keep-alive traffic must reset the idle timer and keep the request alive: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected content %q", resp.Content)
	}
}

// TestSilentStreamHitsIdleTimeout is the negative counterpart: an upstream that
// sends headers and then goes fully silent past IdleTimeout must have its request
// FAIL, since nothing resets the rolling timer.
func TestSilentStreamHitsIdleTimeout(t *testing.T) {
	const idle = 150 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server response writer is not a flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		time.Sleep(idle * 5) // silent well past the idle window
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1,
		IdleTimeout: idle, RequestLifetime: 5 * time.Second,
	})
	_, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("a stream that stalls past IdleTimeout must fail, not hang or fabricate success")
	}
}
