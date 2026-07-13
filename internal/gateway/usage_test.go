package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func settingsWithBase(base string) contract.Settings {
	var s contract.Settings
	s.Provider.BaseURL = base
	return s
}

// TestFetchUsageHappyPath (003 T029) parses a well-formed response.
func TestFetchUsageHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" || r.Header.Get("Authorization") != "Bearer sk-virt-x" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"plan":{"name":"Pro","windows":[{"name":"Monthly","duration_seconds":2678400,"budget_usd":100,"current_spent_usd":41.87,"reset_time":"2026-08-01T00:00:00Z"}]},"credits":{"extra_total":500,"extra_remaining":349.53},"spend":{"today_usd":3.42}}`))
	}))
	defer srv.Close()
	got, err := FetchUsage(context.Background(), settingsWithBase(srv.URL), "sk-virt-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Plan.Name != "Pro" || len(got.Plan.Windows) != 1 || got.Credits.ExtraRemaining != 349.53 || got.Spend.TodayUSD != 3.42 {
		t.Fatalf("unexpected usage: %+v", got)
	}
}

// TestFetchUsageFailureModes (003 T029/FR-020) maps each failure to a typed kind.
func TestFetchUsageFailureModes(t *testing.T) {
	t.Run("no key ⇒ unauthorized", func(t *testing.T) {
		_, err := FetchUsage(context.Background(), settingsWithBase("http://x"), "")
		if ue, ok := err.(*UsageError); !ok || ue.Kind != "unauthorized" {
			t.Fatalf("err = %v, want unauthorized", err)
		}
	})
	t.Run("401 ⇒ unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
		defer srv.Close()
		_, err := FetchUsage(context.Background(), settingsWithBase(srv.URL), "sk-virt-x")
		if ue, ok := err.(*UsageError); !ok || ue.Kind != "unauthorized" {
			t.Fatalf("err = %v, want unauthorized", err)
		}
	})
	t.Run("404 ⇒ unsupported", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }))
		defer srv.Close()
		_, err := FetchUsage(context.Background(), settingsWithBase(srv.URL), "sk-virt-x")
		if ue, ok := err.(*UsageError); !ok || ue.Kind != "unsupported" {
			t.Fatalf("err = %v, want unsupported", err)
		}
	})
	t.Run("malformed body ⇒ malformed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("not json")) }))
		defer srv.Close()
		_, err := FetchUsage(context.Background(), settingsWithBase(srv.URL), "sk-virt-x")
		if ue, ok := err.(*UsageError); !ok || ue.Kind != "malformed" {
			t.Fatalf("err = %v, want malformed", err)
		}
	})
}

// TestUsageBaseNormalization (003 T029) confirms /v1/usage resolves at the root
// regardless of how BaseURL is written.
func TestUsageBaseNormalization(t *testing.T) {
	for _, base := range []string{"https://api.muhiya.com", "https://api.muhiya.com/v1", "https://api.muhiya.com/v1/chat/completions", "https://api.muhiya.com/"} {
		if got := usageBase(base); got != "https://api.muhiya.com" {
			t.Fatalf("usageBase(%q) = %q, want https://api.muhiya.com", base, got)
		}
	}
}
