package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// NormalizedCacheUsage is a provider-neutral, availability-preserving view of
// one raw provider usage object. RawSchema and Derivation make every mapped or
// derived member auditable; nil always means unavailable, never zero.
type NormalizedCacheUsage struct {
	PromptTokens        *int
	OutputTokens        *int
	CacheReadTokens     *int
	CacheWriteTokens    *int
	CacheCreationTokens *int
	UncachedInputTokens *int
	RawSchema           string
	Derivation          string
	Diagnostic          string
}

// normalizeProviderUsage applies only documented complementary-field rules.
// It is intentionally independent from pricing/capability selection so raw
// payload fixtures can be verified before provider profiles are introduced.
func normalizeProviderUsage(raw json.RawMessage, family, transport string) NormalizedCacheUsage {
	family = strings.ToLower(strings.TrimSpace(family))
	transport = strings.ToLower(strings.TrimSpace(transport))
	result := NormalizedCacheUsage{}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		result.RawSchema = "unknown"
		result.Derivation = "unavailable"
		result.Diagnostic = "usage must be a JSON object: " + err.Error()
		return result
	}
	diagnostics := make([]string, 0, 2)
	readInt := func(container map[string]json.RawMessage, key, path string) *int {
		value, ok := container[key]
		if !ok || string(value) == "null" {
			return nil
		}
		var number json.Number
		if err := json.Unmarshal(value, &number); err != nil {
			diagnostics = append(diagnostics, path+" must be an integer")
			return nil
		}
		parsed, err := number.Int64()
		if err != nil || parsed < 0 || parsed > int64(^uint(0)>>1) {
			diagnostics = append(diagnostics, path+" must be a non-negative integer")
			return nil
		}
		converted := int(parsed)
		return &converted
	}

	if family == "minimax" && strings.Contains(transport, "anthropic") {
		result.RawSchema = "anthropic.usage"
		uncached := readInt(object, "input_tokens", "usage.input_tokens")
		output := readInt(object, "output_tokens", "usage.output_tokens")
		creation := readInt(object, "cache_creation_input_tokens", "usage.cache_creation_input_tokens")
		read := readInt(object, "cache_read_input_tokens", "usage.cache_read_input_tokens")
		result.UncachedInputTokens = uncached
		result.OutputTokens = output
		result.CacheReadTokens = read
		result.CacheCreationTokens = creation
		result.CacheWriteTokens = cloneInt(creation)
		if uncached != nil && creation != nil && read != nil {
			total := *uncached + *creation + *read
			result.PromptTokens = &total
			result.Derivation = "prompt=uncached+creation+read"
		} else {
			result.Derivation = "partial-anthropic"
		}
	} else {
		result.PromptTokens = readInt(object, "prompt_tokens", "usage.prompt_tokens")
		result.OutputTokens = readInt(object, "completion_tokens", "usage.completion_tokens")
		if family == "deepseek" {
			result.RawSchema = "deepseek.prompt_cache"
			result.CacheReadTokens = readInt(object, "prompt_cache_hit_tokens", "usage.prompt_cache_hit_tokens")
			result.UncachedInputTokens = readInt(object, "prompt_cache_miss_tokens", "usage.prompt_cache_miss_tokens")
			if result.CacheReadTokens != nil && result.UncachedInputTokens != nil {
				result.Derivation = "direct-complementary"
			} else if result.PromptTokens != nil || result.OutputTokens != nil {
				result.Derivation = "partial-deepseek"
			} else {
				result.Derivation = "unavailable"
			}
		} else {
			result.RawSchema = "openai.usage"
			var details map[string]json.RawMessage
			if encoded, ok := object["prompt_tokens_details"]; ok && string(encoded) != "null" {
				if err := json.Unmarshal(encoded, &details); err != nil {
					diagnostics = append(diagnostics, "usage.prompt_tokens_details must be an object")
				} else {
					result.CacheReadTokens = readInt(details, "cached_tokens", "usage.prompt_tokens_details.cached_tokens")
					result.RawSchema = "openai.prompt_tokens_details"
				}
			}
			if result.PromptTokens != nil && result.CacheReadTokens != nil {
				if *result.CacheReadTokens <= *result.PromptTokens {
					uncached := *result.PromptTokens - *result.CacheReadTokens
					result.UncachedInputTokens = &uncached
					result.Derivation = "uncached=prompt-cache_read"
				} else {
					diagnostics = append(diagnostics, "usage cache read exceeds prompt tokens")
					result.Derivation = "contradictory"
				}
			} else if result.PromptTokens != nil || result.OutputTokens != nil {
				result.Derivation = "direct-totals"
			} else {
				result.Derivation = "unavailable"
			}
		}
	}
	result.Diagnostic = strings.Join(diagnostics, "; ")
	return result
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// UsageResponse is the parsed GET /v1/usage payload (contract usage-api.md §2).
// Credits are in credits (1 credit = $0.01); window/spend figures are USD.
type UsageResponse struct {
	Plan struct {
		Name    string `json:"name"`
		Windows []struct {
			Name            string  `json:"name"`
			DurationSeconds int     `json:"duration_seconds"`
			BudgetUSD       float64 `json:"budget_usd"`
			CurrentSpentUSD float64 `json:"current_spent_usd"`
			ResetTime       string  `json:"reset_time"`
		} `json:"windows"`
	} `json:"plan"`
	Credits struct {
		ExtraTotal     float64 `json:"extra_total"`
		ExtraRemaining float64 `json:"extra_remaining"`
	} `json:"credits"`
	Spend struct {
		TodayUSD float64 `json:"today_usd"`
	} `json:"spend"`
}

// UsageError classifies a /usage failure so the TUI can render the right
// friendly state (contract usage-api.md §2, FR-020).
type UsageError struct {
	Kind    string // "offline" | "unauthorized" | "unsupported" | "malformed" | "server"
	Message string
}

func (e *UsageError) Error() string { return e.Message }

// FetchUsage calls the gateway's self-service usage endpoint with the caller's
// own API key (Bearer). A 5s timeout bounds the call; every failure maps to a
// typed UsageError for a friendly message. Absence of the endpoint on an older
// gateway surfaces as "unsupported", part of the FR-020 friendly-failure family.
func FetchUsage(ctx context.Context, settings contract.Settings, apiKey string) (*UsageResponse, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, &UsageError{Kind: "unauthorized", Message: "sign in with an API key to view usage"}
	}
	base := usageBase(settings.Provider.BaseURL)
	if base == "" {
		return nil, &UsageError{Kind: "offline", Message: "no gateway base URL configured"}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/v1/usage", nil)
	if err != nil {
		return nil, &UsageError{Kind: "offline", Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Client-App", "MuhiyaCode")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, &UsageError{Kind: "offline", Message: "could not reach the gateway"}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, &UsageError{Kind: "unauthorized", Message: "the stored API key was rejected"}
	case resp.StatusCode == http.StatusNotFound:
		return nil, &UsageError{Kind: "unsupported", Message: "usage data requires an updated gateway"}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, &UsageError{Kind: "server", Message: fmt.Sprintf("gateway returned %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, &UsageError{Kind: "offline", Message: "could not read the usage response"}
	}
	var usage UsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, &UsageError{Kind: "malformed", Message: "the usage response was not understood"}
	}
	return &usage, nil
}

// BudgetExhausted reports whether the plan has no spend headroom left: at least
// one window is at or over its budget AND there are no extra credits to fall
// back on.
//
// This exists because the gateway answers 429 `rate_limit_error` for BOTH
// "you are going too fast" and "you are out of money". They need opposite
// responses — the first clears in a minute, the second never clears on its own —
// and a client that cannot tell them apart will retry a permanent condition
// until the user gives up. /v1/usage is the only surface that distinguishes
// them.
func (u *UsageResponse) BudgetExhausted() bool {
	if u == nil {
		return false
	}
	if u.Credits.ExtraRemaining > 0 {
		return false
	}
	for _, window := range u.Plan.Windows {
		if window.BudgetUSD > 0 && window.CurrentSpentUSD >= window.BudgetUSD {
			return true
		}
	}
	return false
}

// NextReset returns the reset_time of the exhausted window, so the user is told
// when work can resume rather than being left to guess. Empty when unknown.
func (u *UsageResponse) NextReset() string {
	if u == nil {
		return ""
	}
	for _, window := range u.Plan.Windows {
		if window.BudgetUSD > 0 && window.CurrentSpentUSD >= window.BudgetUSD {
			return window.ResetTime
		}
	}
	return ""
}

// usageBase strips a trailing /chat/completions and /v1 so /v1/usage resolves at
// the gateway root regardless of how BaseURL is written.
func usageBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, "/chat/completions")
	base = strings.TrimSuffix(base, "/v1")
	return strings.TrimRight(base, "/")
}
