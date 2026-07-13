package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCommandVisibilityTracksSignIn (003 T031/FR-019): /login shows only when
// signed out; /usage and /logout only when signed in.
func TestCommandVisibilityTracksSignIn(t *testing.T) {
	loggedIn := false
	opts := Options{Runtime: testRuntime(t), Version: "test"}
	opts.Actions.IsLoggedIn = func() bool { return loggedIn }
	m := NewModel(opts)
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("/")

	names := func() string {
		var b strings.Builder
		for _, c := range m.commandMatches() {
			b.WriteString(c.name + " ")
		}
		return b.String()
	}

	// Signed out: /login visible; /usage and /logout hidden.
	out := names()
	if !strings.Contains(out, "/login") {
		t.Fatalf("signed out: /login should be visible: %s", out)
	}
	if strings.Contains(out, "/usage") || strings.Contains(out, "/logout") {
		t.Fatalf("signed out: /usage and /logout should be hidden: %s", out)
	}

	// Signed in: the reverse.
	loggedIn = true
	out = names()
	if strings.Contains(out, "/login") {
		t.Fatalf("signed in: /login should be hidden: %s", out)
	}
	if !strings.Contains(out, "/usage") || !strings.Contains(out, "/logout") {
		t.Fatalf("signed in: /usage and /logout should be visible: %s", out)
	}
}
