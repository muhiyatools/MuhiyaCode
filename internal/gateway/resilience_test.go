package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The wire-resilience guards (G2). A coding session is a long sequence of
// requests over minutes; the question is not whether a connection drops but
// what happens when one does.

// A stream that dies after headers used to be terminal, and the turn loop's
// only response to a Chat error is to abort — so one dropped connection killed
// a whole task. It is now retried exactly once, against a prefix the provider
// has just cached.
func TestDiedStreamIsRetriedOnce(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		if n == 1 {
			// Headers sent, one frame, then the connection dies mid-stream.
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"))
			flusher.Flush()
			server, _, _ := w.(http.Hijacker).Hijack()
			_ = server.Close()
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"complete\"}}]}\n\ndata: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	var retries int
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1,
		IdleTimeout: 2 * time.Second, RequestLifetime: 5 * time.Second,
		StreamRetryObserver: func(error) { retries++ },
	})
	response, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("a died stream must be retried, not surfaced as a dead task: %v", err)
	}
	if response.Content != "complete" {
		t.Fatalf("content = %q, want the retry's full answer (the partial must be discarded)", response.Content)
	}
	if retries != 1 {
		t.Fatalf("stream retries = %d, want exactly 1 (telemetered, not silent)", retries)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("upstream attempts = %d, want 2", got)
	}
}

// The retry is bounded at ONE. A genuinely dead upstream must surface promptly
// rather than being hidden behind an escalating retry ladder.
func TestDiedStreamRetriesOnlyOnce(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		flusher.Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	defer server.Close()

	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 3,
		IdleTimeout: 2 * time.Second, RequestLifetime: 5 * time.Second,
	})
	if _, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}}); err == nil {
		t.Fatal("a permanently dying stream must eventually surface an error")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("upstream attempts = %d, want 2 (the original plus one retry)", got)
	}
}

// An explicitly non-retryable status must be sent ONCE. It used to be retried
// three times: HTTPError.Retryable correctly said no, and then the transport
// fallback promoted it back to yes because isNetworkError only excludes
// cancellation. A bad key hammered the gateway; a malformed request burned the
// user's clock reproducing the same 400.
func TestNonRetryableStatusIsSentOnce(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1,
		IdleTimeout: time.Second, RequestLifetime: 5 * time.Second,
	})
	if _, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}}); err == nil {
		t.Fatal("a 400 must surface")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 — a 4xx is not a transport blip", got)
	}
}

// The other half: statuses that ARE transient must still climb the retry ladder.
// The fix above must not have turned every status error into a single shot.
func TestRetryableStatusStillRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{
		Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 2,
		IdleTimeout: time.Second, RequestLifetime: 5 * time.Second,
	})
	response, err := provider.Chat(context.Background(), contract.ChatRequest{Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("a 503 must still be retried: %v", err)
	}
	if response.Content != "ok" || attempts.Load() != 2 {
		t.Fatalf("content=%q attempts=%d, want a retried success", response.Content, attempts.Load())
	}
}

// Recoverable is the single classifier behind the harness's one-shot turn
// retry. Retrying a key or budget failure wastes the user's time on something
// that will repeat identically; refusing to retry a dropped connection throws
// away a task's completed work.
func TestRecoverableClassification(t *testing.T) {
	for _, row := range []struct {
		name string
		err  error
		want bool
	}{
		{"server error", &HTTPError{Status: 500}, true},
		{"bad gateway", &HTTPError{Status: 502}, true},
		{"request timeout", &HTTPError{Status: 408}, true},
		{"bad key is a state, not a blip", &HTTPError{Status: 401}, false},
		{"out of credits is a state", &HTTPError{Status: 402}, false},
		{"rate limited needs a wait, not a retry", &HTTPError{Status: 429}, false},
		{"bad request repeats identically", &HTTPError{Status: 400}, false},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"unexpected eof", errors.New("unexpected EOF"), true},
		{"user cancelled", context.Canceled, false},
		{"stringified cancel", errors.New(`Post "x": context canceled`), false},
		{"nil", nil, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := Recoverable(row.err); got != row.want {
				t.Fatalf("Recoverable(%v) = %v, want %v", row.err, got, row.want)
			}
		})
	}
}

// The gateway answers 429 rate_limit_error for BOTH throughput throttling and
// budget exhaustion. They need opposite responses, and /v1/usage is the only
// thing that tells them apart.
func TestBudgetExhaustionIsDistinguishableFromThrottling(t *testing.T) {
	usage := func(spent, budget, credits float64) *UsageResponse {
		var u UsageResponse
		u.Plan.Windows = append(u.Plan.Windows, struct {
			Name            string  `json:"name"`
			DurationSeconds int     `json:"duration_seconds"`
			BudgetUSD       float64 `json:"budget_usd"`
			CurrentSpentUSD float64 `json:"current_spent_usd"`
			ResetTime       string  `json:"reset_time"`
		}{Name: "5h", BudgetUSD: budget, CurrentSpentUSD: spent, ResetTime: "2026-07-20T18:00:00Z"})
		u.Credits.ExtraRemaining = credits
		return &u
	}
	if usage(0.10, 0.50, 0).BudgetExhausted() {
		t.Fatal("a window with headroom must read as throttling, not exhaustion")
	}
	if !usage(0.50, 0.50, 0).BudgetExhausted() {
		t.Fatal("a window at its budget with no credits is exhausted")
	}
	if !usage(0.90, 0.50, 0).BudgetExhausted() {
		t.Fatal("a window over its budget is exhausted")
	}
	// Credits are the fallback the gateway itself checks before refusing.
	if usage(0.90, 0.50, 5).BudgetExhausted() {
		t.Fatal("remaining credits mean work can continue — not exhausted")
	}
	if got := usage(0.90, 0.50, 0).NextReset(); got != "2026-07-20T18:00:00Z" {
		t.Fatalf("NextReset = %q — the user must be told when work can resume", got)
	}
	var nilUsage *UsageResponse
	if nilUsage.BudgetExhausted() || nilUsage.NextReset() != "" {
		t.Fatal("a nil usage response must degrade quietly, not panic or claim exhaustion")
	}
	// And the 429 itself must be recognizable at all.
	if !IsRateLimited(&HTTPError{Status: 429}) || IsRateLimited(&HTTPError{Status: 500}) {
		t.Fatal("IsRateLimited misclassifies")
	}
}

// FetchUsage must survive a gateway that answers with something unexpected —
// this runs on an error path, so it can never make a bad situation worse.
func TestFetchUsageDegradesQuietly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()
	if _, err := FetchUsage(context.Background(), testSettings(server.URL), "sk-test"); err == nil {
		t.Fatal("malformed usage must be an error, not a silently zero-valued response")
	} else {
		var usageErr *UsageError
		if !errors.As(err, &usageErr) || usageErr.Kind != "malformed" {
			t.Fatalf("want a typed malformed UsageError, got %v", err)
		}
	}
	// A well-formed response still parses the fields the caller depends on.
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan":    map[string]any{"name": "free", "windows": []any{map[string]any{"name": "5h", "budget_usd": 0.5, "current_spent_usd": 0.5, "reset_time": "soon"}}},
			"credits": map[string]any{"extra_remaining": 0.0},
		})
	}))
	defer good.Close()
	parsed, err := FetchUsage(context.Background(), testSettings(good.URL), "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.BudgetExhausted() || parsed.NextReset() != "soon" {
		t.Fatalf("parsed usage lost the fields the 429 explanation depends on: %+v", parsed)
	}
}
