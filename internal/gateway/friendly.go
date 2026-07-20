package gateway

import (
	"context"
	"errors"
	"net"
	"strings"
)

// FriendlyRequestError renders a provider/transport failure as actionable
// guidance instead of a raw HTTP status or Go network error (feature 014 —
// the live incident was a bare `Post ".../chat/completions": context
// canceled` reaching the user through a subagent report). One mapping serves
// every surface: the TUI's task-stop line and the orchestrator's subagent
// reports/notices. Anything unrecognized passes through unchanged — no
// information is hidden, only the common fixable failures get a plain-language
// next step.
func FriendlyRequestError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "stopped."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the request timed out — the gateway or provider was slow; try again."
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.Status == 401 || httpErr.Status == 403:
			return "the gateway rejected your API key — run /login to set a valid one."
		case httpErr.Status == 402:
			return "out of credits — top up your MuhiyaCode balance to continue."
		case httpErr.Status == 429:
			if strings.Contains(strings.ToLower(httpErr.Body), "budget limit") {
				return "your plan's budget window is used up — it resets automatically when the window rolls over; to continue sooner, raise the window budget in the gateway admin panel."
			}
			return "rate limited — the request was retried automatically; try again in a moment."
		case httpErr.Status >= 500:
			return "the gateway had a server error (retried automatically) — try again shortly."
		}
	}
	// Cancellation wrapped beyond errors.Is reach (some transports stringify
	// it) still means "the user or harness stopped this" — never a network
	// diagnosis.
	if strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return "stopped."
	}
	var netErr net.Error
	if errors.As(err, &netErr) || looksLikeConnectionError(err) {
		return "can't reach the gateway — check your connection, then run `muhiyacode doctor`."
	}
	return err.Error()
}

// Recoverable reports whether repeating the identical request could plausibly
// succeed. It is the gate on the harness's one-shot turn retry, so it must be
// conservative in BOTH directions: retrying a bad API key or an exhausted
// budget just wastes the user's time on a failure that will repeat, while
// refusing to retry a dropped connection throws away a whole task's completed
// work over a blip.
//
// Cancellation is never recoverable — the user asked to stop.
func Recoverable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		// 5xx and 408 are transient by definition. 401/402/403/429 are states,
		// not blips: they need a key, a top-up, or a wait, and the friendly text
		// already tells the user which.
		return httpErr.Status >= 500 || httpErr.Status == 408
	}
	// Timeouts and transport failures: the request never landed, or the answer
	// never arrived. Both are worth exactly one more try.
	return errors.Is(err, context.DeadlineExceeded) || looksLikeConnectionError(err) || isNetErr(err)
}

func isNetErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

// IsRateLimited reports a 429 from the gateway. The caller still has to work out
// WHICH limit (throughput or budget) — the gateway uses one status and one error
// type for both, which is why explainRateLimit has to consult /v1/usage.
func IsRateLimited(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.Status == 429
}

// looksLikeConnectionError catches connection failures that aren't typed as
// net.Error by the time they surface (DNS, refused, reset).
func looksLikeConnectionError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"no such host", "connection refused", "connection reset", "dial tcp", "network is unreachable", "i/o timeout", "tls handshake", "unexpected eof"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
