package tui

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/muhiya/muhiyacode/internal/gateway"
)

// friendlyTaskError renders a task-ending error as actionable guidance instead of
// a raw HTTP status or Go network error (Stability Overhaul T051, defect D10).
// Anything it does not recognize passes through unchanged, so no information is
// hidden — only the common, fixable failures get a plain-language next step.
func friendlyTaskError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "stopped."
	}
	var httpErr *gateway.HTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.Status == 401 || httpErr.Status == 403:
			return "the gateway rejected your API key — run /login to set a valid one."
		case httpErr.Status == 402:
			return "out of credits — top up your MuhiyaCode balance to continue."
		case httpErr.Status == 429:
			return "rate limited — the request was retried automatically; try again in a moment."
		case httpErr.Status >= 500:
			return "the gateway had a server error (retried automatically) — try again shortly."
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) || looksLikeConnectionError(err) {
		return "can't reach the gateway — check your connection, then run `muhiyacode doctor`."
	}
	return err.Error()
}

// looksLikeConnectionError catches connection failures that aren't typed as
// net.Error by the time they surface (DNS, refused, reset).
func looksLikeConnectionError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"no such host", "connection refused", "connection reset", "dial tcp", "network is unreachable", "i/o timeout", "tls handshake"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
