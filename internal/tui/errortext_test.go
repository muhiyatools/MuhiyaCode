package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/gateway"
)

func TestFriendlyTaskError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // substring the friendly text must contain
	}{
		{"auth-401", &gateway.HTTPError{Status: 401}, "/login"},
		{"forbidden-403", &gateway.HTTPError{Status: 403}, "/login"},
		{"credits-402", &gateway.HTTPError{Status: 402}, "out of credits"},
		{"rate-429", &gateway.HTTPError{Status: 429}, "rate limited"},
		{"server-503", &gateway.HTTPError{Status: 503}, "server error"},
		{"network", errors.New("dial tcp 1.2.3.4:443: connection refused"), "check your connection"},
		{"dns", errors.New("lookup api.muhiya.com: no such host"), "check your connection"},
		{"wrapped-http", fmt.Errorf("chat: %w", &gateway.HTTPError{Status: 401}), "/login"},
		{"passthrough", errors.New("something specific and unmapped"), "something specific and unmapped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := friendlyTaskError(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("friendlyTaskError(%v) = %q, want it to contain %q", tc.err, got, tc.want)
			}
			// No raw HTTP status codes should leak into the mapped messages.
			if tc.name != "passthrough" && strings.Contains(got, "HTTP") {
				t.Fatalf("mapped message leaked a raw HTTP error: %q", got)
			}
		})
	}
}
