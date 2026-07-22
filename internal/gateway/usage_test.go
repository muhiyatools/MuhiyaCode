package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNormalizeProviderCacheUsageFixtures(t *testing.T) {
	ptr := func(value int) *int { return &value }
	tests := []struct {
		name, family, transport, raw string
		want                         NormalizedCacheUsage
	}{
		{
			name: "minimax openai", family: "minimax", transport: "openai-compatible",
			raw:  `{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":700}}`,
			want: NormalizedCacheUsage{PromptTokens: ptr(1000), OutputTokens: ptr(20), CacheReadTokens: ptr(700), UncachedInputTokens: ptr(300), RawSchema: "openai.prompt_tokens_details", Derivation: "uncached=prompt-cache_read"},
		},
		{
			name: "minimax anthropic", family: "minimax", transport: "anthropic-compatible",
			raw:  `{"input_tokens":30,"output_tokens":12,"cache_creation_input_tokens":100,"cache_read_input_tokens":200}`,
			want: NormalizedCacheUsage{PromptTokens: ptr(330), OutputTokens: ptr(12), CacheReadTokens: ptr(200), CacheWriteTokens: ptr(100), CacheCreationTokens: ptr(100), UncachedInputTokens: ptr(30), RawSchema: "anthropic.usage", Derivation: "prompt=uncached+creation+read"},
		},
		{
			name: "deepseek", family: "deepseek", transport: "openai-compatible",
			raw:  `{"prompt_tokens":120,"completion_tokens":11,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":20}`,
			want: NormalizedCacheUsage{PromptTokens: ptr(120), OutputTokens: ptr(11), CacheReadTokens: ptr(100), UncachedInputTokens: ptr(20), RawSchema: "deepseek.prompt_cache", Derivation: "direct-complementary"},
		},
		{
			name: "generic totals only", family: "generic", transport: "openai-compatible",
			raw:  `{"prompt_tokens":9,"completion_tokens":2}`,
			want: NormalizedCacheUsage{PromptTokens: ptr(9), OutputTokens: ptr(2), RawSchema: "openai.usage", Derivation: "direct-totals"},
		},
		{name: "missing", family: "generic", transport: "openai-compatible", raw: `{}`, want: NormalizedCacheUsage{RawSchema: "openai.usage", Derivation: "unavailable"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeProviderUsage(json.RawMessage(test.raw), test.family, test.transport)
			assertNormalizedCacheUsage(t, got, test.want)
		})
	}
}

func TestNormalizeProviderCacheUsageMalformedIsDiagnostic(t *testing.T) {
	got := normalizeProviderUsage(json.RawMessage(`{"prompt_tokens":"bad","prompt_tokens_details":{"cached_tokens":{}}}`), "minimax", "openai-compatible")
	if got.PromptTokens != nil || got.CacheReadTokens != nil || got.UncachedInputTokens != nil {
		t.Fatalf("malformed values became measurements: %+v", got)
	}
	if !strings.Contains(got.Diagnostic, "prompt_tokens") || !strings.Contains(got.Diagnostic, "cached_tokens") {
		t.Fatalf("missing diagnostic: %+v", got)
	}
}

func assertNormalizedCacheUsage(t *testing.T, got, want NormalizedCacheUsage) {
	t.Helper()
	intValue := func(value *int) any {
		if value == nil {
			return nil
		}
		return *value
	}
	if intValue(got.PromptTokens) != intValue(want.PromptTokens) ||
		intValue(got.OutputTokens) != intValue(want.OutputTokens) ||
		intValue(got.CacheReadTokens) != intValue(want.CacheReadTokens) ||
		intValue(got.CacheWriteTokens) != intValue(want.CacheWriteTokens) ||
		intValue(got.CacheCreationTokens) != intValue(want.CacheCreationTokens) ||
		intValue(got.UncachedInputTokens) != intValue(want.UncachedInputTokens) ||
		got.RawSchema != want.RawSchema || got.Derivation != want.Derivation {
		t.Fatalf("normalized usage\n got: %+v\nwant: %+v", got, want)
	}
}
