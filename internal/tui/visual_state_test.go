package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestNoColorProfileStaysLegible (US6 T072/T075) — under NO_COLOR the frame still
// renders and the key cues remain identifiable without any color (hierarchy comes
// from bold/reverse/glyphs, not hue).
func TestNoColorProfileStaysLegible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "an assistant reply"})
	m.warn("a problem occurred")
	m.refreshViewport(true)
	text := ansi.Strip(m.View().Content)
	if !strings.Contains(text, "a problem occurred") {
		t.Fatal("NO_COLOR: error cue not legible")
	}
	// No 24-bit/256 color SGR should be emitted when NO_COLOR is set.
	if strings.Contains(m.View().Content, "\x1b[38;2;") || strings.Contains(m.View().Content, "\x1b[38;5;") {
		t.Fatal("NO_COLOR: color escape sequences were emitted")
	}
}

// TestAsciiProfileRenders (US6 T072) — with ASCII glyphs the frame renders without
// any of the unicode box/marker glyphs leaking through.
func TestAsciiProfileRenders(t *testing.T) {
	t.Setenv("MUHIYA_ASCII", "1")
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "tool", tool: &toolView{name: "grep", target: "x", state: "ok", output: "1 match"}})
	m.refreshViewport(true)
	text := ansi.Strip(m.View().Content)
	if text == "" {
		t.Fatal("ASCII profile produced an empty frame")
	}
	for _, glyph := range []string{"◆", "●", "─", "▏"} {
		if strings.Contains(text, glyph) {
			t.Fatalf("ASCII profile leaked a unicode glyph %q", glyph)
		}
	}
}

// TestFloorSizesRender (US6 T072) — the frame renders coherently right at and just
// below the render floor without panicking or producing an empty screen.
func TestFloorSizesRender(t *testing.T) {
	for _, sz := range [][2]int{{59, 19}, {60, 20}, {61, 21}} {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = updated.(*Model)
		m.items = append(m.items, item{kind: "assistant", content: "content"})
		m.refreshViewport(true)
		if m.View().Content == "" {
			t.Fatalf("empty frame at %dx%d", sz[0], sz[1])
		}
	}
}

// TestRTLContentRenders (US6 T072) — mixed RTL/LTR transcript content renders
// without panicking.
func TestRTLContentRenders(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "mixed العربية and english text here"})
	m.refreshViewport(true)
	if m.View().Content == "" {
		t.Fatal("RTL content produced an empty frame")
	}
}
