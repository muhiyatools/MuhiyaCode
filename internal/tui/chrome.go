package tui

// chromeCache memoizes the four chrome panes (header, activity, to-do panel, mode
// line) for one frame so they render at most once per Update+View cycle instead of
// the 2–3× layout() previously incurred (Experience Overhaul A5, D7). It is
// invalidated at the top of every Update; the first accessor call in the frame
// renders all four, and the rest reuse the strings — which also guarantees that
// layout()'s height budget and View()'s output are computed from byte-identical
// chrome.
type chromeCache struct {
	valid                             bool
	header, activity, todos, modeLine string
	renders                           int // test hook: counts full chrome render passes
}

// ensureChrome renders the four chrome panes once per frame and caches them.
func (m *Model) ensureChrome() {
	if m.chrome.valid {
		return
	}
	m.chrome.header = m.renderHeader()
	m.chrome.activity = m.renderActivity()
	m.chrome.todos = m.renderTodos()
	m.chrome.modeLine = m.renderModeLine()
	m.chrome.valid = true
	m.chrome.renders++
}

func (m *Model) chromeHeader() string   { m.ensureChrome(); return m.chrome.header }
func (m *Model) chromeActivity() string { m.ensureChrome(); return m.chrome.activity }
func (m *Model) chromeTodos() string    { m.ensureChrome(); return m.chrome.todos }
func (m *Model) chromeModeLine() string { m.ensureChrome(); return m.chrome.modeLine }
