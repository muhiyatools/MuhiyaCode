package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Feature 010 US5 (WI-7): these pin the keybindings that TestEverySlashCommandFlowIsCrashFree
// and the other handleKey-adjacent tests (tui_test.go, selection_test.go, composer_text_test.go)
// never exercised — ctrl+d, ctrl+p, ctrl+s, shift+tab, tab (both branches), pgdown, and
// alt+1..9 — so every stroke handleKey's switch recognizes has a real assertion behind it,
// per the R5 "UNVERIFIED per-keybinding cell" gap the wiring inventory closes.

// TestCtrlDQuitsOnlyWhenIdleAndEmpty pins ctrl+d's guarded quit: it only fires when the
// composer is empty and no task is running, matching keys.go's condition exactly (unlike
// the always-fires ctrl+c/esc paths).
func TestCtrlDQuitsOnlyWhenIdleAndEmpty(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// Busy: ctrl+d must not quit even with an empty composer.
	m.busy = true
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("ctrl+d quit while a task was running")
		}
	}
	m.busy = false

	// Non-empty composer: ctrl+d must not quit.
	m.input.SetValue("draft")
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("ctrl+d quit with a non-empty composer")
		}
	}

	// Idle and empty: ctrl+d quits.
	m.input.Reset()
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+d on an idle, empty composer produced no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("ctrl+d on an idle, empty composer did not quit")
	}
}

// TestCtrlPOpensCommandSearch pins the ctrl+p shortcut into the command palette.
func TestCtrlPOpensCommandSearch(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("leftover")
	m.commandIndex = 3

	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+p returned a non-nil command: %v", cmd)
	}
	if m.input.Value() != "/" {
		t.Fatalf("ctrl+p did not reset the composer to \"/\": %q", m.input.Value())
	}
	if m.commandIndex != 0 {
		t.Fatalf("ctrl+p left commandIndex=%d, want 0", m.commandIndex)
	}
	if m.status != "Command search" {
		t.Fatalf("ctrl+p status=%q, want \"Command search\"", m.status)
	}
}

// TestCtrlSOpensResumeWhenIdleAndBlocksWhenBusy pins ctrl+s's two branches: the
// /resume shortcut when idle, and the busy-guard notice otherwise (the same guard
// /resume's own runSlash case enforces).
func TestCtrlSOpensResumeWhenIdleAndBlocksWhenBusy(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	if cmd := m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+s (idle) returned a non-nil command: %v", cmd)
	}
	if m.input.Value() != "/resume" {
		t.Fatalf("ctrl+s (idle) did not fill the composer with /resume: %q", m.input.Value())
	}

	m.busy = true
	m.input.Reset()
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+s (busy) returned a non-nil command: %v", cmd)
	}
	if m.input.Value() != "" {
		t.Fatalf("ctrl+s (busy) touched the composer: %q", m.input.Value())
	}
	if !strings.Contains(m.flash.text, "Sessions cannot be changed right now") {
		t.Fatalf("ctrl+s (busy) did not surface the busy-guard notice: %q", m.flash.text)
	}
}

// TestShiftTabCyclesPermissionMode pins shift+tab as the permission-mode cycle
// (the same transition /permissions and /mode drive).
func TestShiftTabCyclesPermissionMode(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.runtime.Settings.PermissionMode; got != "normal" {
		t.Fatalf("unexpected starting permission mode %q", got)
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := m.runtime.Settings.PermissionMode; got != "auto-accept" {
		t.Fatalf("shift+tab did not cycle normal -> auto-accept, got %q", got)
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := m.runtime.Settings.PermissionMode; got != "normal" {
		t.Fatalf("shift+tab did not cycle auto-accept -> normal, got %q", got)
	}
}

// TestTabCompletesCommandWhenPaletteOpen pins plain tab's first branch: completing
// the highlighted palette match when the composer holds a slash command.
func TestTabCompletesCommandWhenPaletteOpen(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/comp") // uniquely matches "/compact"

	if cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}); cmd != nil {
		t.Fatalf("tab (palette open) returned a non-nil command: %v", cmd)
	}
	if m.input.Value() != "/compact" {
		t.Fatalf("tab did not complete to /compact: %q", m.input.Value())
	}
}

// TestTabCyclesAgentWhenPaletteClosed pins plain tab's second branch: cycling the
// viewed subagent when the composer is not a slash command (mirrors alt+N's target).
func TestTabCyclesAgentWhenPaletteClosed(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("plain text, no slash")
	m.agents = append(m.agents,
		&agentView{id: "a1", index: 1, status: "running"},
		&agentView{id: "a2", index: 2, status: "running"},
	)

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.viewAgent != "a1" {
		t.Fatalf("first tab did not select the first agent: viewAgent=%q", m.viewAgent)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.viewAgent != "a2" {
		t.Fatalf("second tab did not advance to the second agent: viewAgent=%q", m.viewAgent)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.viewAgent != "" {
		t.Fatalf("third tab did not wrap back to the main session: viewAgent=%q", m.viewAgent)
	}
	if m.input.Value() != "plain text, no slash" {
		t.Fatalf("agent-cycling tab touched the composer: %q", m.input.Value())
	}
}

// TestPageDownScrollsViewportDown pins pgdown as PageUp's inverse (TestScrollUpFromBottomSticks
// in tui_test.go already pins pgup).
func TestPageDownScrollsViewportDown(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line content number filler text here")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	m.refreshViewport(true)

	// Scroll away from the bottom so PageDown has somewhere to go.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	before := m.viewport.YOffset()
	if before == 0 {
		t.Fatal("setup did not scroll away from the top; PageDown has nothing to prove")
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.viewport.YOffset() <= before {
		t.Fatalf("pgdown did not scroll further down: before=%d after=%d", before, m.viewport.YOffset())
	}
}

// TestAltDigitSelectsAgentByIndex pins the alt+1..9 direct-select shortcuts
// (keys.go's alt+<digit> branch), including that an out-of-range digit is a no-op
// rather than a panic.
func TestAltDigitSelectsAgentByIndex(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.agents = append(m.agents,
		&agentView{id: "first", index: 1, status: "running"},
		&agentView{id: "second", index: 2, status: "running"},
	)

	m.handleKey(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if m.viewAgent != "second" {
		t.Fatalf("alt+2 did not select the second agent: viewAgent=%q", m.viewAgent)
	}
	m.handleKey(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if m.viewAgent != "first" {
		t.Fatalf("alt+1 did not select the first agent: viewAgent=%q", m.viewAgent)
	}
	// alt+9: no agent at that index — must not panic and must leave viewAgent alone.
	m.handleKey(tea.KeyPressMsg{Code: '9', Mod: tea.ModAlt})
	if m.viewAgent != "first" {
		t.Fatalf("alt+9 (no such agent) changed viewAgent to %q", m.viewAgent)
	}
}
