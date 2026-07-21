package tui

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestChromeRendersOncePerView pins the A5 memoization (T051): a single View()
// renders the header/activity/to-do/mode-line chrome exactly once, even though it
// consumes all four panes. If the per-frame cache stopped deduping, this would
// exceed 1.
func TestChromeRendersOncePerView(t *testing.T) {
	m := todoModel(t)
	before := m.chrome.renders
	_ = m.View()
	if got := m.chrome.renders - before; got != 1 {
		t.Fatalf("one View() rendered chrome %d times, want exactly 1", got)
	}
}

// TestChromeReflectsBusyMutation proves the memo never serves stale chrome: a busy
// status set directly (no intervening Update) still appears because View()
// invalidates the cache at its start (A5 freshness).
func TestChromeReflectsBusyMutation(t *testing.T) {
	m := todoModel(t)
	_ = m.View() // populate the cache while idle
	m.busy = true
	m.status = "Reticulating…"
	if !strings.Contains(m.View().Content, "Reticulating…") {
		t.Fatal("View() served stale chrome after a direct state mutation")
	}
}

// TestChromeReflectsResize proves the mode line is re-rendered at the new width
// each frame (never served stale across a resize).
func TestChromeReflectsResize(t *testing.T) {
	// The pane is multi-line (chips + cycle hint), so measure its widest line.
	widestLine := func(pane string) int {
		widest := 0
		for _, line := range strings.Split(pane, "\n") {
			widest = max(widest, ansi.StringWidth(line))
		}
		return widest
	}
	m := todoModel(t)
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	wide := widestLine(m.chromeModeLine())
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 64, Height: 30})
	narrow := widestLine(m.chromeModeLine())
	if narrow > 64 || narrow >= wide {
		t.Fatalf("mode line did not shrink on resize: wide=%d narrow=%d", wide, narrow)
	}
}

// TestNoDirectChromeRenderCalls guards the D7 fix at the source level: layout.go
// and view.go must reach the header/activity/to-do/mode-line panes ONLY through the
// memoized chrome* accessors, never the underlying render* funcs — a direct call
// would render that pane an extra time per frame.
func TestNoDirectChromeRenderCalls(t *testing.T) {
	direct := regexp.MustCompile(`m\.render(Header|Activity|ModeLine|Todos)\(`)
	for _, f := range []string{"layout.go", "view.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if loc := direct.FindIndex(src); loc != nil {
			t.Errorf("%s calls a chrome render func directly near byte %d — use the chrome* accessor", f, loc[0])
		}
	}
}

// BenchmarkTypingFrame measures one keystroke's Update+View cost against a
// 200-item transcript (A5 T052, informational — not gated). The transcript is
// cached (typing is not a dirty message), so this is dominated by chrome + frame
// assembly. Run: go test -bench=TypingFrame -run=^$ ./internal/tui
func BenchmarkTypingFrame(b *testing.B) {
	m := NewModel(Options{Runtime: testRuntime(b), Version: "test", Recent: eventFixture(200)})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(*Model)
	m.refreshViewport(true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, _ := m.Update(tea.KeyPressMsg{Code: 'x'})
		m = u.(*Model)
		_ = m.View()
	}
}
