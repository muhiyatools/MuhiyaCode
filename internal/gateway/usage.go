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

// UsageResponse is the parsed GET /v1/usage payload (contract usage-api.md §2).
// Credits are in credits (1 credit = $0.01); window/spend figures are USD.
type UsageResponse struct {
	User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
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
