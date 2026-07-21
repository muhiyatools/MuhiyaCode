package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/app"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func (m *Model) handleKey(key tea.KeyPressMsg) tea.Cmd {
	if m.loading {
		return m.handleLoadingKey(key)
	}
	if m.modal != nil {
		return m.handleModalKey(key)
	}
	stroke := key.String()
	switch stroke {
	case "ctrl+c":
		// US4 T058 / contract §3: Ctrl+C copies an active selection first, then falls
		// through to the existing cancel/clear/quit behavior when there is none.
		if m.sel.active {
			cmd, ok := m.copySelection()
			m.clearSelection()
			if ok {
				m.notify("Copied selection to clipboard.")
				return cmd
			}
		}
		if m.busy {
			m.runtime.Engine.Cancel()
			m.status = "Stopping…"
			return nil
		}
		if m.input.Value() != "" {
			m.input.Reset()
			return nil
		}
		return tea.Quit
	case "ctrl+d":
		if m.input.Value() == "" && !m.busy {
			return tea.Quit
		}
	case "esc":
		if m.sel.active {
			// US4 T058: Esc first dismisses an active selection.
			m.clearSelection()
			return nil
		}
		if m.busy {
			m.runtime.Engine.Cancel()
			m.status = "Stopping…"
		} else {
			m.input.Reset()
		}
		return nil
	case "ctrl+p":
		m.input.SetValue("/")
		m.input.MoveToEnd()
		m.commandIndex = 0
		m.status = "Command search"
		return nil
	case "ctrl+s":
		if m.busy {
			m.notify("Sessions cannot be changed right now.")
		} else {
			m.input.SetValue("/resume")
			m.input.MoveToEnd()
			m.commandIndex = 0
		}
		return nil
	case "shift+tab":
		return m.cyclePermission()
	case "tab":
		// Tab is autocomplete only (013 FR-027). Its agent-switching branch moved to
		// the arrow keys; with no leading "/" it now does nothing, which is what a
		// user pressing Tab mid-sentence expects.
		if strings.HasPrefix(m.input.Value(), "/") {
			m.completeCommand()
		}
		return nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if strings.HasPrefix(value, "/") {
			value = m.resolveSlash(value)
		}
		m.input.Reset()
		m.commandIndex = 0
		if value == "" {
			return nil
		}
		if strings.HasPrefix(value, "/") {
			return m.runSlash(value)
		}
		return m.submit(value)
	case "up":
		if strings.HasPrefix(m.input.Value(), "/") {
			matches := m.commandMatches()
			m.commandIndex = max(0, min(len(matches)-1, m.commandIndex-1))
			return nil
		}
		if m.input.Value() != "" && len(m.history) > 0 {
			m.previousHistory()
			return nil
		}
		m.viewport.ScrollUp(3)
		return nil
	case "down":
		if strings.HasPrefix(m.input.Value(), "/") {
			matches := m.commandMatches()
			m.commandIndex = max(0, min(len(matches)-1, m.commandIndex+1))
			return nil
		}
		if m.input.Value() != "" && m.historyIndex >= 0 {
			m.nextHistory()
			return nil
		}
		m.viewport.ScrollDown(3)
		return nil
	case "pgup":
		m.viewport.PageUp()
		return nil
	case "pgdown":
		m.viewport.PageDown()
		return nil
	}
	// left/right agent cycling and alt+1..9 agent jumps were removed with the
	// subagent views they navigated. Both keys now belong entirely to the caret.
	updated, command := m.input.Update(key)
	m.input = updated
	if strings.HasPrefix(m.input.Value(), "/") {
		m.commandIndex = 0
	}
	return command
}

// resolveSlash turns the raw input into the command that should run. If the
// first token is already an exact command (so any following words are its
// arguments) the line is used verbatim; otherwise the highlighted palette
// match is substituted. This is what makes selecting "/mcp" from autocomplete
// run "/mcp" rather than the bare "/" that was typed.
func (m *Model) resolveSlash(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return value
	}
	first := strings.ToLower(fields[0])
	for _, command := range commands {
		if command.name == first {
			return value
		}
	}
	matches := m.commandMatches()
	if len(matches) == 0 {
		return value
	}
	index := max(0, min(m.commandIndex, len(matches)-1))
	return matches[index].name
}

func (m *Model) previousHistory() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIndex < 0 {
		m.historyIndex = len(m.history) - 1
	} else {
		m.historyIndex = max(0, m.historyIndex-1)
	}
	m.input.SetValue(m.history[m.historyIndex])
	m.input.MoveToEnd()
}

func (m *Model) nextHistory() {
	if m.historyIndex < 0 {
		return
	}
	m.historyIndex++
	if m.historyIndex >= len(m.history) {
		m.historyIndex = -1
		m.input.Reset()
		return
	}
	m.input.SetValue(m.history[m.historyIndex])
	m.input.MoveToEnd()
}

func (m *Model) submit(prompt string) tea.Cmd {
	// US2 T033: submitting resumes auto-follow so the user's new message and the
	// response that follows scroll into view even if they had scrolled up to read.
	m.followOutput = true
	m.clearSelection() // US4 T058: a new turn invalidates any transcript selection

	// US5: the transcript keeps the compact placeholder; the engine receives the
	// exact reconstructed paste content. Assembly happens once, here, at the send
	// boundary, then the stash is released.
	display := prompt
	// Paste expansion and NFC normalization live in the core (app.AssemblePrompt)
	// so every frontend sends identical bytes; skills are folded in below, after
	// the steering branch, because a queued steering message carries none.
	prompt = app.AssemblePrompt(m.ctx, prompt, nil, m.expandPastes, nil)
	m.releasePastes()
	// T1: a fresh prompt clears the persistent usage footer so prior
	// duration / effort / totals no longer mislead.
	m.lastStats = nil
	if m.busy {
		if m.runtime.Engine.QueueUserMessage(prompt) {
			m.items = append(m.items, item{kind: "user", content: display, title: "steering"})
			m.status = "Message queued"
			return nil
		}
		// D1: m.busy is true but the engine has no task to steer — the run is still
		// starting, or the previous one just finished and its completion has not yet
		// reached the UI. The enter handler already cleared the composer, so dropping
		// here would silently lose what the user typed (the reported steering-loss
		// bug). Restore the text instead: one more Enter delivers it — as steering
		// once the run is live, or as a fresh task once busy clears. Falling through
		// to start a task now would race the starting run into "a task is already
		// running".
		m.input.SetValue(display)
		m.input.MoveToEnd()
		m.notify("One moment — press Enter to send.")
		return nil
	}
	m.items = append(m.items, item{kind: "user", content: display})
	m.history = append(m.history, prompt)
	m.historyIndex = -1
	if len(m.selectedSkills) > 0 {
		queued := make([]Skill, 0, len(m.selectedSkills))
		for _, skill := range m.selectedSkills {
			queued = append(queued, skill)
		}
		// Already expanded and normalized above, so pass no expander: the core
		// orders the skills, loads bodies on demand, and wraps them.
		prompt = app.AssemblePrompt(m.ctx, prompt, queued, nil, m.actions.LoadSkill)
		m.selectedSkills = make(map[string]Skill)
	}
	// A fresh task starts its live counters from zero — without this reset the
	// activity line shows the PREVIOUS task's tokens/cache tag until the new
	// task's first usage event arrives.
	m.usage = contract.Usage{}
	m.busy, m.status, m.started = true, "Working…", time.Now()
	engine := m.runtime.Engine
	ctx := m.ctx
	return func() (msg tea.Msg) {
		// T052/D11: a panic in the engine task goroutine is contained here — it
		// becomes a task error so the terminal survives, never a raw crash.
		defer func() {
			if r := recover(); r != nil {
				msg = recoverTaskPanic(r)
			}
		}()
		answer, _, err := engine.Run(ctx, prompt)
		return resultMsg{answer: answer, err: err}
	}
}
