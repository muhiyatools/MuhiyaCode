package tui

import "strings"

// commandWindow is how many palette rows fit without pushing the input off the
// screen. It shrinks on short terminals so the palette can never overflow.
func (m *Model) commandWindow() int {
	reserved := 3 + 1 + (m.input.Height() + 2) + 1 + 3 // header, status, input, mode line, min viewport
	return max(1, min(6, m.height-reserved-1))         // -1 leaves room for the count hint
}

func (m *Model) refreshViewport(forceBottom bool) {
	// transcriptRenders counts full transcript re-renders (US1/US2 perf guard):
	// tests assert this does NOT advance on a keystroke or an idle tick, so the
	// O(all items) render cost stays off the typing hot path.
	m.transcriptRenders++
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) renderTranscript() string {
	items := m.items
	if m.viewAgent != "" {
		if agent := m.agentByID[m.viewAgent]; agent != nil {
			items = append([]item{{kind: "system", content: "Agent task: " + agent.task}}, agent.items...)
		}
	}
	// US6: an empty transcript shows a first-run cue instead of a blank void — it
	// also teaches the "/" palette and the mouse affordances so they are discoverable.
	if len(items) == 0 {
		m.transcriptChips = m.transcriptChips[:0]
		hint := m.firstRunHint()
		m.transcriptContent = hint
		return hint
	}
	// Reserve the left gutter and a small right margin so text never touches
	// either edge of the terminal. T048: use the single shared contentWidth so
	// transcript, thinking, and footer never drift apart.
	width := m.contentWidth()
	rtl := m.runtime.Settings.RTL.Mode
	m.transcriptChips = m.transcriptChips[:0]
	blocks := make([]string, 0, len(items))
	// row tracks the content-row where the NEXT appended block begins. Blocks are
	// joined with "\n\n", so each contributes its own rows plus one separator blank.
	// Empty blocks are skipped entirely — they would otherwise inject stray blank
	// rows and desync the chip row math. A tool/agent chip's clickable header is its
	// block's first row (row). Chips register in the agent detail view too (009
	// polish): a subagent's tool rows were previously inert — their captured
	// output existed but could never be expanded.
	row := 0
	appendBlock := func(block string, tool *toolView, agentID string) {
		if block == "" {
			return
		}
		if tool != nil || agentID != "" {
			m.transcriptChips = append(m.transcriptChips, chipSpan{row: row, tool: tool, agentID: agentID})
		}
		blocks = append(blocks, block)
		row += lineCount(block) + 1
	}
	for i := range items {
		entry := &items[i]
		switch entry.kind {
		case "user", "assistant", "assistant_draft", "system":
			// US1: reuse the memoized block when nothing this render depends on has
			// changed (width, content length, title). Immutable items hit this every
			// frame, so streaming re-renders only the growing draft, not the whole
			// history. cachedWidth==0 (zero value) means "no cache yet".
			if entry.cachedWidth == width && entry.cachedLen == len(entry.content) && entry.cachedTitle == entry.title {
				appendBlock(entry.cachedBlock, nil, "")
				continue
			}
			block := m.renderTextBlock(entry, width, rtl)
			entry.cachedBlock, entry.cachedWidth, entry.cachedLen, entry.cachedTitle = block, width, len(entry.content), entry.title
			appendBlock(block, nil, "")
		case "summary":
			if entry.stats != nil {
				appendBlock(taskSummaryLine(*entry.stats, m.palette), nil, "")
			}
		case "tool":
			appendBlock(m.renderTool(entry.tool, width), entry.tool, "")
		case "agent":
			if agent := m.agentByID[entry.agentID]; agent != nil {
				appendBlock(m.renderAgentChip(agent, width), nil, entry.agentID)
			}
		}
	}
	content := indentLines(strings.Join(blocks, "\n\n"), transcriptGutter)
	// US4 T058: keep the exact styled content so a text selection can extract its
	// ANSI-free plain text on copy without re-deriving the transcript.
	m.transcriptContent = content
	return content
}

// firstRunHint is the US6 empty-session cue: a calm welcome that teaches the
// command palette and the mouse affordances so a new user is never faced with a
// blank screen. It disappears the moment the first turn is added.
func (m *Model) firstRunHint() string {
	lines := []string{
		m.palette.brand.Render(m.glyphs.brand + " Welcome to MuhiyaCode"),
		"",
		m.palette.muted.Render("Type a request below, or press ") + m.palette.brandSoft.Render("/") + m.palette.muted.Render(" for commands."),
		m.palette.faint.Render("Project instructions (MUHIYA.md) and saved memory load automatically."),
		m.palette.faint.Render("Drag to select · Ctrl+C copies · click a tool row to expand it."),
	}
	return indentLines(strings.Join(lines, "\n"), transcriptGutter)
}

// renderTextBlock renders one cacheable text item (user/assistant/system). It is
// a pure function of the entry, width, and RTL mode (palette is startup-constant),
// which is what makes the per-item cache in renderTranscript correct.
func (m *Model) renderTextBlock(entry *item, width int, rtl string) string {
	switch entry.kind {
	case "user":
		// 003 (T027/FR-015): user messages are identified by the surface background
		// band alone — no "> " prefix. An optional title (e.g. "steering") is a
		// small faint prefix, not a marker. 006: wrap the LOGICAL text first, then
		// shape each display line (wrap-then-shape) so Arabic joins correctly.
		prefix := ""
		if entry.title != "" {
			prefix = m.palette.faint.Render(entry.title + " ")
		}
		wrapped := wrapPlain(entry.content, max(10, width-4))
		for i, line := range wrapped {
			visual := renderForDisplay(line, rtl, "left").Visual
			wrapped[i] = m.palette.surface.Padding(0, 1).Render(visual)
		}
		return prefix + strings.Join(wrapped, "\n  ")
	case "assistant", "assistant_draft":
		return RenderMarkdown(entry.content, width, rtl, m.runtime.Settings.RTL.Align, m.palette)
	case "system":
		return m.palette.faint.Render("○ ") + RenderMarkdown(entry.content, width-2, rtl, m.runtime.Settings.RTL.Align, m.palette)
	}
	return ""
}

// indentLines prefixes every non-empty line with the gutter so the whole
// transcript keeps a consistent left margin.
func indentLines(value, gutter string) string {
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = gutter + line
		}
	}
	return strings.Join(lines, "\n")
}

// rtl shapes s for display in the current RTL mode (visual only, no alignment),
// for inline chrome strings — tool targets, modal text/choices, plan steps — where
// Arabic can appear. It is a byte-identical no-op for pure-LTR content (006).
func (m *Model) rtl(s string) string {
	return renderForDisplay(s, m.runtime.Settings.RTL.Mode, "left").Visual
}
