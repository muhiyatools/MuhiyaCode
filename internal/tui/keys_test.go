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

// TestShiftTabCyclesPermissionMode pins shift+tab as the permission-mode entry,
// with explicit confirmation before the unsafe direction.
func TestShiftTabCyclesPermissionMode(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.runtime.Settings.PermissionMode; got != "normal" {
		t.Fatalf("unexpected starting permission mode %q", got)
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := m.runtime.Settings.PermissionMode; got != "normal" || m.modal == nil {
		t.Fatalf("shift+tab must open confirmation without changing mode, got mode=%q modal=%v", got, m.modal)
	}
	callback := m.modal.onSelect
	m.closeModal(1)
	callback(1)
	if got := m.runtime.Settings.PermissionMode; got != "auto-accept" {
		t.Fatalf("explicit confirmation did not enable auto-accept, got %q", got)
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

// The agent-navigation tests (→/← ring cycling, alt+1..9 direct select, and
// Tab-no-longer-cycles) retired with the agent views they navigated. What
// survives is the property those tests protected in passing: arrows belong to
// the caret.

// TestArrowsBelongToTheCaret: with or without composer text, ←/→ are the
// caret's alone — no navigation mode may take them again.
func TestArrowsBelongToTheCaret(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("plain text, no slash")
	for _, code := range []rune{tea.KeyLeft, tea.KeyRight} {
		m.handleKey(tea.KeyPressMsg{Code: code})
	}
	if m.input.Value() != "plain text, no slash" {
		t.Fatalf("arrow handling mutated composer text: %q", m.input.Value())
	}
	// Empty composer: still a caret no-op, not a mode switch.
	m.input.SetValue("")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.input.Value() != "" {
		t.Fatalf("arrows on an empty composer produced text: %q", m.input.Value())
	}
}

// TestTabIsAutocompleteOnly (013 FR-027): Tab completes slash commands and does
// nothing else.
func TestTabIsAutocompleteOnly(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("plain text, no slash")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "plain text, no slash" {
		t.Fatalf("tab mutated the composer: %q", m.input.Value())
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

// TestAltDigitIsInert: the alt+1..9 agent-select shortcuts are gone, and the
// keys must now pass through harmlessly rather than panicking or typing digits.
func TestAltDigitIsInert(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, digit := range []rune{'1', '2', '9'} {
		m.handleKey(tea.KeyPressMsg{Code: digit, Mod: tea.ModAlt})
	}
	if m.input.Value() != "" {
		t.Fatalf("alt+digit typed into the composer: %q", m.input.Value())
	}
}
