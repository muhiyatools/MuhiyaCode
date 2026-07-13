package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPasteClassification(t *testing.T) {
	if isLargePaste(strings.Repeat("a", 1023)) {
		t.Fatal("1023 code points should be small")
	}
	if !isLargePaste(strings.Repeat("a", 1024)) {
		t.Fatal("1024 code points should be large")
	}
	if isLargePaste("a\nb\nc\nd\ne") { // 5 lines
		t.Fatal("5 logical lines should be small")
	}
	if !isLargePaste("a\nb\nc\nd\ne\nf") { // 6 lines
		t.Fatal("6 logical lines should be large")
	}
	cases := map[string]int{
		"":           0,
		"noBreak":    1,
		"a\nb":       2,
		"a\r\nb":     2, // CRLF is one break
		"a\rb":       2, // lone CR
		"a\rb\nc":    3, // lone CR + LF
		"a\n\nb":     3,
		"trailing\n": 2,
	}
	for in, want := range cases {
		if got := logicalLineCount(in); got != want {
			t.Fatalf("logicalLineCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestLargePasteBecomesPlaceholderWithExactExpansion(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()

	raw := strings.Repeat("a line of pasted source code here\n", 12) // >6 lines → large
	updated, _ = m.Update(tea.PasteMsg{Content: raw})
	m = updated.(*Model)

	val := m.input.Value()
	if strings.Contains(val, "pasted source code") {
		t.Fatalf("raw paste leaked into the input box: %q", val)
	}
	placeholder := fmt.Sprintf("[#paste1 %d lines]", logicalLineCount(raw))
	if val != placeholder {
		t.Fatalf("placeholder = %q, want %q", val, placeholder)
	}
	if m.pastes[1] != raw {
		t.Fatal("raw paste not stashed for reconstruction")
	}

	// Fidelity oracle: placeholder alone reconstructs exactly the raw bytes.
	if got := m.expandPastes(val); got != raw {
		t.Fatalf("expansion lost fidelity:\n got %q\nwant %q", got, raw)
	}
	// Oracle with text before/after: T0 + P1 + T2.
	around := "before " + val + " after"
	if got := m.expandPastes(around); got != "before "+raw+" after" {
		t.Fatalf("expansion around text broke fidelity: %q", got)
	}

	// Release clears the stash (send/clear boundary).
	m.releasePastes()
	if len(m.pastes) != 0 || m.pasteSeq != 0 {
		t.Fatal("releasePastes did not clear the stash")
	}
	// After release, an intact placeholder is left literal (paste already sent).
	if got := m.expandPastes(val); got != val {
		t.Fatalf("stale placeholder should stay literal after release: %q", got)
	}
}

func TestPasteManagerInspectAndRemove(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	raw := strings.Repeat("a line of pasted code\n", 20) // large → stashed
	updated, _ = m.Update(tea.PasteMsg{Content: raw})
	m = updated.(*Model)

	if got := m.activePastes(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("activePastes = %v, want [1]", m.activePastes())
	}
	if m.renderPasteBar() == "" {
		t.Fatal("paste bar should render when a block is active")
	}
	// The paste bar has a click target after rendering.
	_ = m.View()
	hasBar := false
	for _, tg := range m.hits.targets {
		if tg.kind == targetPasteBar {
			hasBar = true
		}
	}
	if !hasBar {
		t.Fatal("paste bar has no click region")
	}
	// Fidelity holds while the block is present.
	if m.expandPastes(m.input.Value()) != raw {
		t.Fatal("paste block lost fidelity before removal")
	}
	// The manager opens as a modal.
	m.openPasteManager()
	if m.modal == nil || len(m.modal.choices) != 2 { // paste1 + Close
		t.Fatalf("paste manager modal shape wrong: %+v", m.modal)
	}
	m.closeModal(-1)
	// Removing the block drops the stash and the placeholder.
	m.removePaste(1)
	if len(m.pastes) != 0 {
		t.Fatal("removePaste left the stash")
	}
	if strings.Contains(m.input.Value(), "#paste") {
		t.Fatalf("removePaste left the placeholder: %q", m.input.Value())
	}
	if len(m.activePastes()) != 0 {
		t.Fatal("a removed block is still active")
	}
}

// TestPasteManagerHiddenWithoutBlocks proves /paste is hidden from the palette and
// the manager reports empty when there are no active blocks.
// TestLargeBinaryishPasteFidelity proves a >32 KiB paste with tabs, NUL, and
// unicode round-trips byte-for-byte through the stash and expansion.
func TestLargeBinaryishPasteFidelity(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	raw := strings.Repeat("tab\there\tNUL\x00unicode→你好\n", 2000) // well over 32 KiB
	if len(raw) < 32*1024 {
		t.Fatal("test paste is not large enough")
	}
	updated, _ = m.Update(tea.PasteMsg{Content: raw})
	m = updated.(*Model)
	if got := m.expandPastes(m.input.Value()); got != raw {
		t.Fatalf("large paste lost fidelity: got %d bytes, want %d", len(got), len(raw))
	}
}

func TestPasteManagerHiddenWithoutBlocks(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	if m.commandVisible("/paste") {
		t.Fatal("/paste should be hidden when there are no paste blocks")
	}
	m.openPasteManager()
	if m.modal != nil {
		t.Fatal("paste manager opened a modal with no blocks")
	}
}

func TestSmallPasteInsertsInline(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	updated, _ = m.Update(tea.PasteMsg{Content: "a short inline paste"})
	m = updated.(*Model)
	if len(m.pastes) != 0 {
		t.Fatal("a small paste must not be stashed")
	}
	if !strings.Contains(m.input.Value(), "a short inline paste") {
		t.Fatalf("small paste should insert inline: %q", m.input.Value())
	}
}
