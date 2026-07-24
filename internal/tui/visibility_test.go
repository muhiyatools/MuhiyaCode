package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// legacyGrayRGB are the exact truecolor triples of the pre-fix low-contrast
// gray tokens (muted/faint/border on both themes). A rendered frame must never
// emit them again — that was the "invisible on some terminal themes" defect.
var legacyGrayRGB = []string{
	"38;2;138;155;146", // muted dark  #8A9B92
	"38;2;90;107;97",   // muted light #5A6B61
	"38;2;97;112;104",  // faint dark  #617068
	"38;2;138;150;143", // faint light #8A968F
	"38;2;49;66;57",    // border dark #314239
	"38;2;185;201;191", // border light #B9C9BF
}

// TestRenderedFrameHasNoLegacyGray renders a full representative frame — header,
// user turn, assistant markdown, a tool entry with a diff, and the task summary
// — on both themes and asserts none of the legacy gray colors appear in the
// actual output. This is the durable regression guard for the visibility fix.
func TestRenderedFrameHasNoLegacyGray(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, theme := range []string{"dark", "light"} {
		rt := testRuntime(t)
		rt.Settings.Theme = theme
		m := NewModel(Options{Runtime: rt, Version: "test"})
		m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		diff := "\n--- diff ---\n--- a/x\n+++ b/x\n@@ -1 +1,2 @@\n-old\n+new1\n+new2"
		sessionRate := 0.9
		m.items = append(m.items,
			item{kind: "user", content: "polish the interface"},
			item{kind: "assistant", content: "## Done\nColors are legible now."},
			item{kind: "tool", tool: &toolView{name: "edit_file", target: "render.go", state: "ok", output: "edited" + diff}},
			item{kind: "summary", stats: &contract.TaskStats{Effort: contract.EffortLow, DurationMS: 1200, Usage: contract.Usage{TotalTokens: 5000, CacheReadTokens: ptrInt(4500), CacheMissTokens: ptrInt(500)}, SessionHitRate: &sessionRate}},
		)
		m.refreshViewport(true)
		frame := m.View().Content
		for _, gray := range legacyGrayRGB {
			if strings.Contains(frame, gray) {
				t.Errorf("theme %s: legacy gray %q leaked into the rendered frame", theme, gray)
			}
		}
	}
}

// TestPaletteNeverCombinesFaintWithColor: the ANSI Faint attribute renders
// near-invisible on many terminal themes, so in color mode hierarchy must come
// from the hex values alone. Under NO_COLOR, Faint IS the hierarchy channel
// and must return.
func TestPaletteNeverCombinesFaintWithColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, theme := range []string{"dark", "light"} {
		p := newPalette(theme)
		for name, style := range map[string]lipgloss.Style{"muted": p.muted, "faint": p.faint, "meta": p.meta} {
			if style.GetFaint() {
				t.Errorf("%s/%s carries the Faint attribute in color mode", theme, name)
			}
		}
	}
	t.Setenv("NO_COLOR", "1")
	p := newPalette("dark")
	for name, style := range map[string]lipgloss.Style{"muted": p.muted, "faint": p.faint, "meta": p.meta} {
		if !style.GetFaint() {
			t.Errorf("%s must keep Faint under NO_COLOR (only hierarchy channel)", name)
		}
	}
}

// TestHeaderIsAFixedIdentityRow: the top bar starts with one blank spacer row so
// it never sits flush against the terminal's upper edge, and it is exactly three
// rows — spacer, identity line, rule. The header used to grow a second line for
// the focused-agent view; with one session there is nothing to focus, so the
// height is fixed and the layout accounting can rely on it.
func TestHeaderIsAFixedIdentityRow(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	header := m.renderHeader()
	if header != "" {
		t.Fatalf("topbar should be empty (0 rows), got:\n%q", header)
	}
	modeLine := m.chromeModeLine()
	if !strings.Contains(modeLine, "vtest") || !strings.Contains(modeLine, "context") {
		t.Fatalf("mode line should carry the version and context meter:\n%q", modeLine)
	}
}

// TestDiffCountsUseAddRemoveColors: the collapsed "+N −N" outcome must carry
// the diff.add color on additions and diff.remove on deletions so the two are
// distinguishable at a glance (and not rendered in the faint metadata tone).
func TestDiffCountsUseAddRemoveColors(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	diff := "\n--- diff ---\n--- a/x\n+++ b/x\n@@ -1 +1,2 @@\n-old\n+new1\n+new2"
	tool := &toolView{name: "edit_file", target: "x.go", state: "ok", output: "edited" + diff}
	collapsed := m.renderTool(tool, 90)
	if !strings.Contains(stripANSI(collapsed), "+2 −1") {
		t.Fatalf("collapsed outcome lost the counts: %q", stripANSI(collapsed))
	}
	if !strings.Contains(collapsed, m.palette.add.Render("+2")) {
		t.Fatalf("additions are not styled with diff.add:\n%q", collapsed)
	}
	if !strings.Contains(collapsed, m.palette.remove.Render("−1")) {
		t.Fatalf("deletions are not styled with diff.remove:\n%q", collapsed)
	}
	// Failures keep the danger tone, not the diff colors.
	failed := &toolView{name: "edit_file", state: "fail", output: "edit failed: no match"}
	rendered := m.renderTool(failed, 90)
	if !strings.Contains(rendered, m.palette.danger.Render(oneLine("edit failed: no match", 30))) {
		t.Fatalf("failed outcome lost the danger style:\n%q", rendered)
	}
}
