package tui

import (
	"strings"
	"time"
	"unicode/utf8"
)

func (m *Model) expireNotice() {
	if m.flash.text != "" && !m.flash.expires.IsZero() && time.Now().After(m.flash.expires) {
		m.flash = noticeState{}
	}
}

// appendStream accumulates visible assistant tokens into the active draft row.
// Model thinking never reaches here: the bridge does not wire ReasoningToken
// (raw thought text must never render), so a streamMsg carries answer text only.
func (m *Model) appendStream(stream streamMsg) {
	if stream.text == "" {
		return
	}
	if len(m.items) == 0 || m.items[len(m.items)-1].kind != "assistant_draft" {
		m.items = append(m.items, item{kind: "assistant_draft"})
		m.draft.Reset()
	}
	// US1 T019: accumulate in a Builder (O(1) amortized) rather than reallocating
	// the whole growing string per token (O(n²)). Builder.String() is O(1) and
	// returns an immutable snapshot the renderer/cache can hold safely.
	m.draft.WriteString(stream.text)
	m.items[len(m.items)-1].content = m.draft.String()
}

func (m *Model) resetStreamDraft() {
	for index := len(m.items) - 1; index >= 0; index-- {
		if m.items[index].kind == "assistant_draft" {
			m.items = append(m.items[:index], m.items[index+1:]...)
			break
		}
		if m.items[index].kind == "user" {
			break
		}
	}
	m.draft.Reset()
}

func (m *Model) finishAssistant(answer string) {
	for index := len(m.items) - 1; index >= 0; index-- {
		if m.items[index].kind == "assistant_draft" {
			if strings.TrimSpace(m.items[index].content) == "" {
				m.items[index].content = answer
			}
			m.items[index].kind = "assistant"
			return
		}
		if m.items[index].kind == "user" {
			break
		}
	}
	if strings.TrimSpace(answer) != "" {
		m.items = append(m.items, item{kind: "assistant", content: answer})
	}
}

// sweepRunningWork (004 US1, T013) is the task-end safety net: any tool still
// marked "running" when the task's stats arrive was cut short (user stop,
// error, or a crash mid-tool) and is reconciled to the terminal "cancelled"
// state so nothing keeps spinning in the transcript. Idempotent — items already
// ok/fail are left untouched.
func (m *Model) sweepRunningWork() {
	// US1 T019: the task ended, so no tool is active anymore.
	clear(m.activeTools)
	for _, entry := range m.items {
		if entry.tool != nil && entry.tool.state == "running" {
			entry.tool.state = "cancelled"
		}
	}
}

func finalizeTool(tool *toolView, output string) {
	tool.output, tool.summary = output, summarizeTool(output)
	tool.state = "ok"
	if isFailure(output) {
		tool.state = "fail"
	}
}

func (m *Model) finishTool(name, output string) {
	// FIFO pairing: a delegate fan-out is announced up front (engine
	// executeBatch), so several same-name rows can be "running" at once. A
	// result pairs with the OLDEST running row of that name — matching
	// dispatch order — never the newest, which would finalize a queued row
	// whose delegate had not even started. Single-instance tools (the common
	// case) are unaffected: the oldest running row IS the row.
	for index := 0; index < len(m.items); index++ {
		tool := m.items[index].tool
		if tool != nil && tool.name == name && tool.state == "running" {
			finalizeTool(tool, output)
			if m.activeTools[name] == tool {
				delete(m.activeTools, name)
			}
			return
		}
	}
	// Fallback: the O(1) registration covers a row evicted by transcript trim.
	if tool := m.activeTools[name]; tool != nil && tool.state == "running" {
		finalizeTool(tool, output)
		delete(m.activeTools, name)
	}
}

func (m *Model) appendToolOutput(name, chunk string) {
	tool := m.activeTools[name]
	if tool == nil || tool.state != "running" {
		// Fallback: linear scan handles any missed registration.
		for index := len(m.items) - 1; index >= 0; index-- {
			candidate := m.items[index].tool
			if candidate != nil && candidate.name == name && candidate.state == "running" {
				tool = candidate
				break
			}
		}
	}
	if tool == nil {
		return
	}
	tool.output += chunk
	if len(tool.output) > 8_000 {
		tail := tool.output[len(tool.output)-7_000:]
		// Advance to the next rune boundary so the tail never starts mid-rune (which
		// would render a replacement glyph for split CJK/emoji output).
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
		tool.output = "… live output truncated …\n" + tail
	}
	tool.summary = summarizeTool(tool.output)
}

// notify shows a transient informational message in the status zone; warn is
// the same but styled as a warning and held a little longer. Neither touches
// the transcript, which stays limited to user and assistant turns.
func (m *Model) notify(value string) {
	m.flash = noticeState{text: value, level: "info", expires: time.Now().Add(5 * time.Second)}
}

func (m *Model) warn(value string) {
	m.flash = noticeState{text: value, level: "warn", expires: time.Now().Add(10 * time.Second)}
}

func simplifyStatus(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "..."))
	if value == "" {
		return "Working"
	}
	return value
}
