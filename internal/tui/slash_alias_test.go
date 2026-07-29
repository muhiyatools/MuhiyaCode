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

// Shift+Tab remains the entry point, but unsafe mode requires confirmation.
func TestShiftTabRemainsTheModeSwitch(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, testSize())
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.runtime.Settings.PermissionMode != contract.PermissionNormal || m.modal == nil {
		t.Fatalf("shift+tab must require confirmation, got mode=%q modal=%v", m.runtime.Settings.PermissionMode, m.modal)
	}
	callback := m.modal.onSelect
	m.closeModal(1)
	callback(1)
	if m.runtime.Settings.PermissionMode != contract.PermissionAutoAccept {
		t.Fatalf("confirmed full access was not applied, got %q", m.runtime.Settings.PermissionMode)
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

func TestModelSlashCommandDirectAndModal(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, testSize())
	m.runtime.Settings.Provider.Models = []contract.Model{
		{ID: "main", Name: "Main Model", ContextLimit: 128000},
		{ID: "fast-model", Name: "Fast Model", ContextLimit: 200000},
	}

	// Test /model without args -> opens choice modal
	m.runSlash("/model")
	if m.modal == nil || !strings.Contains(m.modal.title, "Select Model") || len(m.modal.choices) != 2 {
		t.Fatalf("/model did not open model selection modal with 2 choices: %v", m.modal)
	}
	callback := m.modal.onSelect
	m.closeModal(-1)
	cmd := callback(1) // Pick index 1 ("fast-model")
	if cmd != nil {
		m = mustUpdate(t, m, cmd())
	}
	if m.runtime.Settings.Provider.ActiveModelID != "fast-model" {
		t.Fatalf("model choice selection failed: activeModelID=%q", m.runtime.Settings.Provider.ActiveModelID)
	}

	// Test /model <id> direct switch
	cmd = m.runSlash("/model main")
	if cmd != nil {
		m = mustUpdate(t, m, cmd())
	}
	if m.runtime.Settings.Provider.ActiveModelID != "main" {
		t.Fatalf("/model main direct switch failed: activeModelID=%q", m.runtime.Settings.Provider.ActiveModelID)
	}
}
