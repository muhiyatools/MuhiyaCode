package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func testSize() tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: 80, Height: 24} }

// Feature 010 US5 (WI-7): runSlash recognizes aliases the command palette never
// lists (/effort, /mode, /sessions, /session) — commandMatches() only ever
// surfaces the canonical /reasoning, /permissions, /resume rows, so
// TestEverySlashCommandFlowIsCrashFree (tui_test.go) never drives these
// switch cases. These tests pin that each alias reaches the exact same
// outcome as its canonical name, closing the R5 "UNVERIFIED per-command cell"
// gap for the alias rows.

// TestEffortAliasMatchesCanonicalCommand pins /effort as a byte-identical
// alias of /reasoning (slash.go: `case "/reasoning", "/effort":`).
func TestEffortAliasMatchesCanonicalCommand(t *testing.T) {
	canonical := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	canonical = mustUpdate(t, canonical, testSize())
	canonical.runSlash("/reasoning high")

	alias := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	alias = mustUpdate(t, alias, testSize())
	alias.runSlash("/effort high")

	if canonical.runtime.Settings.Effort != contract.EffortHigh {
		t.Fatalf("/reasoning high did not set effort=high, got %q", canonical.runtime.Settings.Effort)
	}
	if alias.runtime.Settings.Effort != canonical.runtime.Settings.Effort {
		t.Fatalf("/effort high (%q) diverged from /reasoning high (%q)", alias.runtime.Settings.Effort, canonical.runtime.Settings.Effort)
	}
	if alias.flash.text != canonical.flash.text {
		t.Fatalf("/effort notice %q diverged from /reasoning notice %q", alias.flash.text, canonical.flash.text)
	}
}

// TestRemovedCommandsAreUnknown (013 FR-014/FR-018): /permissions, its /mode
// alias, and /errors are gone. Typing one must produce the ordinary
// unknown-command notice — not a silent no-op, and not a half-working leftover.
func TestRemovedCommandsAreUnknown(t *testing.T) {
	for _, command := range []string{"/permissions", "/mode auto-accept", "/errors"} {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		m = mustUpdate(t, m, testSize())
		before := m.runtime.Settings.PermissionMode
		if cmd := m.runSlash(command); cmd != nil {
			t.Fatalf("%s returned a command; it should be unknown", command)
		}
		if !strings.HasPrefix(m.flash.text, "Unknown command:") {
			t.Fatalf("%s notice = %q, want the unknown-command notice", command, m.flash.text)
		}
		if m.runtime.Settings.PermissionMode != before {
			t.Fatalf("%s still changed the permission mode: %q", command, m.runtime.Settings.PermissionMode)
		}
		if m.modal != nil {
			t.Fatalf("%s still opened a modal", command)
		}
	}
}

// Shift+Tab remains the one way to change the mode, so it must keep working
// with the command gone.
func TestShiftTabRemainsTheModeSwitch(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, testSize())
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.runtime.Settings.PermissionMode != contract.PermissionAutoAccept {
		t.Fatalf("shift+tab did not cycle to auto-accept, got %q", m.runtime.Settings.PermissionMode)
	}
}

// TestSessionAliasesMatchCanonicalCommand pins /sessions and /session as
// aliases of /resume (slash.go: `case "/resume", "/sessions", "/session":`).
// None of the three configures Actions.ListSessions here, so all must take
// the identical "cannot be changed" guard branch — proving the switch really
// treats the three names as one case, not three independently-behaving ones.
func TestSessionAliasesMatchCanonicalCommand(t *testing.T) {
	const wantNotice = "Sessions cannot be changed right now."
	for _, command := range []string{"/resume", "/sessions", "/session"} {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		m = mustUpdate(t, m, testSize())
		if cmd := m.runSlash(command); cmd != nil {
			t.Fatalf("%s returned a non-nil command with no ListSessions action", command)
		}
		if m.flash.text != wantNotice {
			t.Fatalf("%s notice = %q, want %q", command, m.flash.text, wantNotice)
		}
	}
}
