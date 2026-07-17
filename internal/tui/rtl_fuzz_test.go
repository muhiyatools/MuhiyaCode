package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRTLRobustness (006 T031, FR-019): the display pass, width, composer render,
// copy recovery, wrapping, and markdown never panic on Arabic + harakat + emoji +
// Arabic-Indic digits + long/defective/mixed input, across every mode and width.
func TestRTLRobustness(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.input.Focus()
	for _, mode := range []string{"auto", "visual", "native", "off"} {
		m.runtime.Settings.RTL.Mode = mode
		for _, s := range rtlCorpus {
			for _, align := range []string{"auto", "right", "left"} {
				dl := renderForDisplay(s, mode, align)
				_ = recoverLogical(dl.Visual)
			}
			_ = displayWidth(s)
			_ = reverseGraphemes(s)
			m.input.SetValue(s)
			for _, w := range []int{1, 3, 10, 40, 200} {
				_ = m.composerView(w)
				_ = truncateToWidth(s, w)
				_ = wrapPlain(s, w)
			}
			_ = RenderMarkdown(s, 20, mode, "auto", m.palette)
		}
	}
}

// FuzzRenderForDisplay (006 T031) fuzzes the display pass + copy recovery.
func FuzzRenderForDisplay(f *testing.F) {
	for _, s := range rtlCorpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		dl := renderForDisplay(s, "visual", "auto")
		got := recoverLogical(dl.Visual)
		// The presentation forms MY shaping introduces all decompose via NFKC, so
		// recovery of CLEAN logical text never leaks them. If the input itself carried
		// a presentation form (not valid logical text — and some forms have no
		// decomposition), recovery can't invent a base letter, so exempt that case.
		if !hasPresentationForms(s) && hasPresentationForms(got) {
			t.Fatalf("presentation forms leaked into recovered logical of %q -> %q", s, got)
		}
		_ = displayWidth(dl.Visual)
		_ = truncateToWidth(dl.Visual, 7)
	})
}

// TestRTLLiveSettingChange (006 T034): switching RTL mode/align changes rendering
// immediately (rendering reads the live settings each frame — no restart).
func TestRTLLiveSettingChange(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.input.Focus()
	m.input.SetValue(corpusPlain)
	m.runtime.Settings.RTL.Mode = "off"
	off := m.composerView(40)
	m.runtime.Settings.RTL.Mode = "visual"
	visual := m.composerView(40)
	if off == visual {
		t.Fatal("changing RTL mode did not change rendering live")
	}
}
