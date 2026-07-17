package tui

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"golang.org/x/text/unicode/norm"
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
		if m.viewAgent != "" {
			m.viewAgent = ""
			m.status = "Main session"
		} else if m.busy {
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
	case "ctrl+t":
		// Toggle the live to-do checklist panel (Experience Overhaul A3). Pure UI
		// state — no engine call, no notice.
		m.todoVisible = !m.todoVisible
		return nil
	case "tab":
		if strings.HasPrefix(m.input.Value(), "/") {
			m.completeCommand()
		} else {
			m.cycleAgent()
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
	if strings.HasPrefix(stroke, "alt+") && len(stroke) == 5 && stroke[4] >= '1' && stroke[4] <= '9' {
		index := int(stroke[4] - '0')
		for _, agent := range m.agents {
			if agent.index == index {
				m.viewAgent = agent.id
				return nil
			}
		}
	}
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
	prompt = m.expandPastes(prompt)
	m.releasePastes()
	// 006 (T030, FR-013): the model receives normalized logical Unicode. NFC is
	// idempotent and meaning-preserving; it never introduces presentation forms or
	// reordering, and it rides the user message (dynamic) so the cached prefix is
	// untouched. Digit systems are preserved as written.
	prompt = norm.NFC.String(prompt)
	// T1: a fresh prompt clears the persistent usage footer so prior
	// duration / effort / totals no longer mislead.
	m.lastStats = nil
	if m.busy {
		if m.runtime.Engine.QueueUserMessage(prompt) {
			m.items = append(m.items, item{kind: "user", content: display, title: "steering"})
			m.status = "Message queued"
		}
		return nil
	}
	m.items = append(m.items, item{kind: "user", content: display})
	m.history = append(m.history, prompt)
	m.historyIndex = -1
	if len(m.selectedSkills) > 0 {
		var names []string
		for _, skill := range m.selectedSkills {
			names = append(names, skill.Name)
		}
		sort.Strings(names)
		var sections []string
		for _, name := range names {
			skill := m.selectedSkills[name]
			body := strings.TrimSpace(skill.Instructions)
			// 003 (T036): load the body on demand for selected skills only.
			if body == "" && skill.Path != "" && m.actions.LoadSkill != nil {
				if loaded, err := m.actions.LoadSkill(m.ctx, skill.Path); err == nil {
					body = strings.TrimSpace(loaded)
				}
			}
			if body == "" {
				body = skill.Description
			}
			sections = append(sections, "<skill name=\""+skill.Name+"\">\n"+body+"\n</skill>")
		}
		prompt = "Follow the selected skill instructions when relevant to this turn.\n\n" + strings.Join(sections, "\n\n") + "\n\nUser prompt:\n" + prompt
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
