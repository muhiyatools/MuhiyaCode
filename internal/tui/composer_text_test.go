package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// composerModel returns a focused 80x24 model for composer compatibility checks.
func composerModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	return m
}

// typeInto sends each rune of s as a key press through the real Update path.
func typeInto(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(*Model)
	}
	return m
}

// TestComposerTypingAccumulates (US5 T007) — typing through the Composer builds the
// value exactly, preserving the textarea's editing semantics.
func TestComposerTypingAccumulates(t *testing.T) {
	m := composerModel(t)
	m = typeInto(t, m, "hello world")
	if m.input.Value() != "hello world" {
		t.Fatalf("typed value = %q, want %q", m.input.Value(), "hello world")
	}
}

// TestComposerSetValueResetFocus (US5 T007) — SetValue/Value round-trip, Reset
// clears, and Focus reports focused, all promoted from the embedded editor.
func TestComposerSetValueResetFocus(t *testing.T) {
	m := composerModel(t)
	m.input.SetValue("some draft text")
	if m.input.Value() != "some draft text" {
		t.Fatalf("SetValue/Value mismatch: %q", m.input.Value())
	}
	if !m.input.Focused() {
		t.Fatal("composer should be focused")
	}
	m.input.Reset()
	if m.input.Value() != "" {
		t.Fatalf("Reset left content: %q", m.input.Value())
	}
}

// TestComposerGraphemeNavigation (US5 T007) — the caret navigates by grapheme:
// after typing CJK + ASCII, MoveToBegin + inserting lands before the first glyph.
func TestComposerGraphemeNavigation(t *testing.T) {
	m := composerModel(t)
	m.input.SetValue("你好world")
	m.input.MoveToBegin()
	m = typeInto(t, m, "Z")
	if got := m.input.Value(); got != "Z你好world" {
		t.Fatalf("grapheme-nav insert wrong: %q", got)
	}
	m.input.MoveToEnd()
	m = typeInto(t, m, "!")
	if got := m.input.Value(); got != "Z你好world!" {
		t.Fatalf("MoveToEnd insert wrong: %q", got)
	}
}

// TestComposerHistoryRecall (US5 T007) — Up recalls the previous submitted entry
// through the composer (history is populated on submit).
func TestComposerHistoryRecall(t *testing.T) {
	m := composerModel(t)
	m.history = []string{"first request", "second request"}
	// The recall path requires a non-empty draft (matches existing behavior).
	m.input.SetValue("x")
	updated := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	_ = updated
	if m.input.Value() != "second request" {
		t.Fatalf("history recall = %q, want %q", m.input.Value(), "second request")
	}
}
