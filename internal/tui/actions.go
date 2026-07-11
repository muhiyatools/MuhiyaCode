package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

type runtimeActionValue struct {
	runtime Runtime
	events  []contract.Event
}

func (m *Model) runSlash(value string) tea.Cmd {
	args := strings.Fields(value)
	command := strings.ToLower(args[0])
	if m.busy {
		allowed := command == "/reasoning" || command == "/effort" || command == "/goal" || command == "/context" || command == "/stop" || command == "/exit" || command == "/quit"
		if !allowed {
			m.notify(command + " is unavailable while a task is running. Send plain text to steer the active task.")
			return nil
		}
	}
	switch command {
	case "/exit", "/quit":
		if m.busy {
			m.runtime.Engine.Cancel()
		}
		return tea.Quit
	case "/stop":
		if m.busy {
			m.runtime.Engine.Cancel()
			m.status = "Stopping…"
		} else {
			m.notify("No task is running.")
		}
	case "/context":
		report := m.runtime.Engine.ContextReport()
		message := fmt.Sprintf(
			"History in context: %s / %s tokens (%.1f%%)\n\nSession billed: %s\nCached: %s",
			formatTokens(report.HistoryTokens), formatTokens(report.ContextLimit), report.Percent,
			formatTokens(report.Usage.TotalTokens), formatTokens(report.Usage.CachedTokens))
		m.openInfo("Context usage", message)
	case "/compact":
		if m.busy {
			m.notify("Stop the running task before compacting.")
			return nil
		}
		return actionCommand("compact", func() (any, error) { return m.runtime.Engine.Compact(m.ctx) })
	case "/rewind":
		if m.actions.Rewind == nil {
			m.notify("Rewind is unavailable in this interface.")
			return nil
		}
		return actionCommand("rewind", func() (any, error) { return m.actions.Rewind(m.ctx) })
	case "/reasoning", "/effort":
		if len(args) > 1 {
			return m.setEffort(args[1])
		}
		choices := []contract.QuestionChoice{}
		for _, level := range []contract.EffortLevel{contract.EffortLow, contract.EffortMedium, contract.EffortHigh, contract.EffortMax} {
			choices = append(choices, contract.QuestionChoice{Label: string(level), Description: reasoningSummary(level), Recommended: level == m.runtime.Settings.Effort})
		}
		m.openChoice("Reasoning effort", "How hard the model thinks. The gateway maps this to each model's thinking level (DeepSeek: low/medium→high, high/max→max).", choices, func(index int) tea.Cmd { return m.setEffort(choices[index].Label) })
	case "/goal":
		return m.handleGoalCommand(strings.TrimSpace(strings.TrimPrefix(value, args[0])))
	case "/plan":
		return m.handlePlanCommand(strings.TrimSpace(strings.TrimPrefix(value, args[0])))
	case "/permissions", "/mode":
		if len(args) > 1 {
			return m.setPermission(contract.PermissionMode(args[1]))
		}
		choices := []contract.QuestionChoice{{Label: "normal", Description: "Confirm mutations and shell commands", Recommended: m.runtime.Settings.PermissionMode == contract.PermissionNormal}, {Label: "auto-accept", Description: "Automatically allow safe workspace actions", Recommended: m.runtime.Settings.PermissionMode == contract.PermissionAutoAccept}}
		m.openChoice("Permission mode", "Destructive commands and credential paths remain blocked in every mode.", choices, func(index int) tea.Cmd { return m.setPermission(contract.PermissionMode(choices[index].Label)) })
	case "/model":
		return m.handleModelCommand(args[1:])
	case "/login":
		if m.actions.SetAPIKey == nil {
			m.notify("Use `muhiyacode config set apiKey <key>` in this build.")
			return nil
		}
		m.openText("Muhiya API key", "Paste the API key. It is stored separately with user-only permissions.", true, func(value string) tea.Cmd {
			return actionCommand("login", func() (any, error) { return nil, m.actions.SetAPIKey(m.ctx, strings.TrimSpace(value)) })
		})
	case "/logout":
		if m.actions.Logout == nil {
			m.notify("Logout is unavailable.")
			return nil
		}
		return actionCommand("logout", func() (any, error) { return nil, m.actions.Logout(m.ctx) })
	case "/diff":
		return m.submit("Inspect the current git diff and summarize the meaningful changes, risks, and verification status. Do not modify files.")
	case "/new":
		if m.actions.NewSession == nil || m.busy {
			m.notify("A new session cannot be opened right now.")
			return nil
		}
		return actionCommand("new", func() (any, error) {
			runtime, events, err := m.actions.NewSession(m.ctx)
			return runtimeActionValue{runtime: runtime, events: events}, err
		})
	case "/resume", "/sessions", "/session":
		if m.actions.ListSessions == nil || m.busy {
			m.notify("Sessions cannot be changed right now.")
			return nil
		}
		if len(args) > 1 {
			return m.resumeCommand(args[1])
		}
		return actionCommand("sessions", func() (any, error) { return m.actions.ListSessions(m.ctx) })
	case "/skills":
		if m.actions.ListSkills == nil {
			m.notify("No skill catalog is configured.")
			return nil
		}
		return actionCommand("skills", func() (any, error) { return m.actions.ListSkills(m.ctx) })
	case "/mcp":
		if m.actions.MCP.List == nil {
			m.notify("Use the `muhiyacode mcp` CLI commands to manage servers.")
			return nil
		}
		return m.openMCP()
	default:
		m.notify("Unknown command: " + command)
	}
	return nil
}

func reasoningSummary(level contract.EffortLevel) string {
	switch level {
	case contract.EffortLow:
		return "Lightest thinking, fastest — the default. DeepSeek maps this to high."
	case contract.EffortMedium:
		return "Balanced thinking. DeepSeek maps this to high."
	case contract.EffortHigh:
		return "Deep thinking for tricky work. DeepSeek maps this to max."
	case contract.EffortMax:
		return "Maximum thinking for the hardest problems. DeepSeek maps this to max."
	default:
		return ""
	}
}

func (m *Model) handleGoalCommand(rest string) tea.Cmd {
	switch strings.ToLower(rest) {
	case "", "status":
		if goal, ok := m.runtime.Engine.GoalSnapshot(); ok {
			m.openInfo("Active goal", fmt.Sprintf("%s\n\nStatus: %s", goal.Text, goal.Status))
		} else {
			m.notify("No active goal. Use /goal <objective> to set one.")
		}
	case "clear", "off", "stop", "none", "done":
		m.runtime.Engine.ClearGoal()
		m.notify("Goal cleared.")
	default:
		m.runtime.Engine.SetGoal(rest)
		m.notify("Goal set — the agent will keep working toward it until it is met.")
	}
	return nil
}

func (m *Model) handlePlanCommand(rest string) tea.Cmd {
	if rest == "" {
		on := !m.runtime.Engine.PlanMode()
		m.runtime.Engine.SetPlanMode(on)
		if on {
			m.notify("Plan mode on — the agent researches and proposes a plan without editing. Run /plan again to resume editing.")
		} else {
			m.notify("Plan mode off — editing is allowed again.")
		}
		return nil
	}
	if m.busy {
		m.notify("Finish the running task before starting a plan.")
		return nil
	}
	m.runtime.Engine.SetPlanMode(true)
	return m.submit(rest)
}

func (m *Model) setEffort(value string) tea.Cmd {
	level, ok := orchestrator.NormalizeEffort(strings.ToLower(value))
	if !ok {
		m.notify("Effort must be low, medium, high, or max.")
		return nil
	}
	m.runtime.Engine.SetEffort(level)
	m.notify("Reasoning effort: " + string(level))
	if m.actions.SaveSettings == nil {
		return nil
	}
	return actionCommand("settings", func() (any, error) { return nil, m.actions.SaveSettings(m.ctx, m.runtime.Settings) })
}

func (m *Model) setPermission(mode contract.PermissionMode) tea.Cmd {
	if mode != contract.PermissionNormal && mode != contract.PermissionAutoAccept {
		m.notify("Permission mode must be normal or auto-accept.")
		return nil
	}
	m.runtime.Settings.PermissionMode = mode
	if m.actions.SetPermission != nil {
		return actionCommand("permission", func() (any, error) { return mode, m.actions.SetPermission(m.ctx, mode) })
	}
	if m.actions.SaveSettings != nil {
		return actionCommand("permission", func() (any, error) { return mode, m.actions.SaveSettings(m.ctx, m.runtime.Settings) })
	}
	return nil
}

func (m *Model) cyclePermission() tea.Cmd {
	mode := contract.PermissionAutoAccept
	if m.runtime.Settings.PermissionMode == contract.PermissionAutoAccept {
		mode = contract.PermissionNormal
	}
	return m.setPermission(mode)
}

func (m *Model) handleModelCommand(args []string) tea.Cmd {
	if len(args) >= 2 {
		role, id := strings.ToLower(args[0]), args[1]
		return m.chooseModel(role, id)
	}
	if len(args) == 1 {
		return m.chooseModel("main", args[0])
	}
	main, sub := m.runtime.Settings.Provider.ActiveModelID, m.runtime.Settings.Provider.SubagentModelID
	choices := []contract.QuestionChoice{
		{Label: "Main model · " + main, Description: "Planning, edits, and final answers"},
		{Label: "Subagent model · " + sub, Description: "Delegated exploration and isolated work"},
	}
	if m.actions.DiscoverModels != nil {
		choices = append(choices, contract.QuestionChoice{Label: "Refresh from gateway", Description: "Fetch the latest model catalog and metadata"})
	}
	m.openChoice("Models", "Choose which role to configure.", choices, func(index int) tea.Cmd {
		switch index {
		case 0:
			m.openModelList("main")
		case 1:
			m.openModelList("subagent")
		default:
			return actionCommand("models-refresh", func() (any, error) { return m.actions.DiscoverModels(m.ctx) })
		}
		return nil
	})
	return nil
}

func (m *Model) openModelList(role string) {
	models := m.runtime.Settings.Provider.Models
	if len(models) == 0 {
		m.notify("No models configured. Use `muhiyacode config set model <id>`.")
		return
	}
	choices := make([]contract.QuestionChoice, len(models))
	current := m.runtime.Settings.Provider.ActiveModelID
	if role == "subagent" {
		current = m.runtime.Settings.Provider.SubagentModelID
	}
	for i, model := range models {
		detail := fmt.Sprintf("%s · %s context", model.ID, formatTokens(model.ContextLimit))
		if model.MaxOutput > 0 {
			detail += " · " + formatTokens(model.MaxOutput) + " out"
		}
		if model.Provider != "" {
			detail += " · " + model.Provider
		}
		choices[i] = contract.QuestionChoice{Label: model.Name, Description: detail, Recommended: model.ID == current}
	}
	m.openChoice(strings.Title(role)+" model", "Select a configured virtual model id.", choices, func(index int) tea.Cmd { return m.chooseModel(role, models[index].ID) })
}

func (m *Model) chooseModel(role, id string) tea.Cmd {
	found := false
	for _, model := range m.runtime.Settings.Provider.Models {
		if model.ID == id || model.Name == id {
			id, found = model.ID, true
			break
		}
	}
	if !found {
		m.notify("Unknown model: " + id)
		return nil
	}
	if role == "subagent" {
		m.runtime.Settings.Provider.SubagentModelID = id
	} else {
		m.runtime.Settings.Provider.ActiveModelID = id
	}
	if m.actions.SaveSettings == nil {
		return nil
	}
	return actionCommand("settings", func() (any, error) { return nil, m.actions.SaveSettings(m.ctx, m.runtime.Settings) })
}

func (m *Model) resumeCommand(id string) tea.Cmd {
	if m.actions.Resume == nil {
		return nil
	}
	return actionCommand("resume", func() (any, error) {
		runtime, events, err := m.actions.Resume(m.ctx, id)
		return runtimeActionValue{runtime: runtime, events: events}, err
	})
}

func actionCommand(kind string, run func() (any, error)) tea.Cmd {
	return func() tea.Msg {
		value, err := run()
		return actionMsg{kind: kind, value: value, err: err}
	}
}

func (m *Model) handleAction(action actionMsg) tea.Cmd {
	if action.err != nil {
		m.warn(action.kind + " failed: " + action.err.Error())
		return nil
	}
	switch action.kind {
	case "compact", "rewind":
		if text, ok := action.value.(string); ok {
			m.notify(text)
		}
	case "mcp-list":
		return m.showMCPList(action.value)
	case "mcp-action":
		return m.afterMCPAction(action.value)
	case "models-refresh":
		models, _ := action.value.([]contract.Model)
		m.notify(fmt.Sprintf("Model catalog refreshed: %d model(s) available.", len(models)))
	case "login":
		m.notify("API key saved.")
	case "logout":
		m.notify("Signed out. The stored API key was cleared.")
	case "permission":
		m.notify("Permission mode: " + string(m.runtime.Settings.PermissionMode))
	case "settings":
		m.status = "Settings saved"
	case "new", "resume":
		payload, ok := action.value.(runtimeActionValue)
		if ok && payload.runtime.Engine != nil {
			m.runtime = payload.runtime
			m.items, m.agents, m.agentByID, m.viewAgent = nil, nil, make(map[string]*agentView), ""
			m.loadEvents(payload.events)
			m.plan, m.usage = payload.runtime.Engine.CurrentPlan(), payload.runtime.Engine.Usage()
			// Show the restored session's real context usage immediately
			// instead of a blank "context —" until the next model turn.
			report := payload.runtime.Engine.ContextReport()
			m.context = contract.ContextInfo{HistoryTokens: report.HistoryTokens, ContextLimit: report.ContextLimit, Percent: report.Percent}
			m.notify("Session opened: " + payload.runtime.Session.ID)
		}
	case "sessions":
		sessions, _ := action.value.([]contract.Session)
		if len(sessions) == 0 {
			m.notify("No sessions found for this workspace.")
			break
		}
		choices := make([]contract.QuestionChoice, len(sessions))
		for i, session := range sessions {
			choices[i] = contract.QuestionChoice{Label: session.Title, Description: session.ID + " · " + session.UpdatedAt.Local().Format("Jan 2 15:04"), Recommended: session.ID == m.runtime.Session.ID}
		}
		m.openChoice("Resume session", "Choose a workspace session.", choices, func(index int) tea.Cmd { return m.resumeCommand(sessions[index].ID) })
	case "skills":
		skills, _ := action.value.([]Skill)
		if len(skills) == 0 {
			m.notify("No installed skills found.")
			break
		}
		choices := make([]contract.QuestionChoice, len(skills))
		checked := make(map[int]bool)
		for i, skill := range skills {
			choices[i] = contract.QuestionChoice{Label: skill.Name, Description: skill.Description}
			_, checked[i] = m.selectedSkills[skill.Name]
		}
		m.openMulti("Skills for next prompt", "Space toggles. Enter confirms.", choices, checked, func(selected map[int]bool) tea.Cmd {
			m.selectedSkills = make(map[string]Skill)
			for index, on := range selected {
				if on && index >= 0 && index < len(skills) {
					m.selectedSkills[skills[index].Name] = skills[index]
				}
			}
			m.notify(fmt.Sprintf("%d skill(s) assigned to the next prompt.", len(m.selectedSkills)))
			return nil
		})
	}
	return nil
}

func (m *Model) enqueueModal(request modalRequest) {
	if m.modal != nil {
		m.modalQueue = append(m.modalQueue, request)
		return
	}
	title := request.title
	if strings.Contains(strings.ToLower(request.message), "trust") {
		title = "Trust workspace"
	}
	m.modal = &modalState{title: title, message: request.message, choices: request.choices, selected: recommendedChoice(request.choices), reply: request.reply}
}

func (m *Model) openChoice(title, message string, choices []contract.QuestionChoice, onSelect func(int) tea.Cmd) {
	m.modal = &modalState{title: title, message: message, choices: choices, selected: recommendedChoice(choices), onSelect: onSelect}
}

func (m *Model) openMulti(title, message string, choices []contract.QuestionChoice, checked map[int]bool, onMulti func(map[int]bool) tea.Cmd) {
	if checked == nil {
		checked = make(map[int]bool)
	}
	m.modal = &modalState{title: title, message: message, choices: choices, multi: true, checked: checked, onMulti: onMulti}
}

func (m *Model) openText(title, message string, secret bool, onText func(string) tea.Cmd) {
	m.modal = &modalState{title: title, message: message, input: true, secret: secret, onText: onText}
}

// openInfo shows a read-only modal (no choices) that closes on Enter or Esc.
func (m *Model) openInfo(title, message string) {
	m.modal = &modalState{title: title, message: message}
}

func (m *Model) handleModalKey(key tea.KeyPressMsg) tea.Cmd {
	modal := m.modal
	if !modal.input && len(modal.choices) == 0 {
		// Info modal: any of Enter/Esc/q closes it.
		switch key.String() {
		case "enter", "esc", "q":
			m.closeModal(-1)
		}
		return nil
	}
	if modal.input {
		switch key.String() {
		case "esc":
			m.closeModal(-1)
		case "enter":
			value, callback := modal.value, modal.onText
			m.closeModal(0)
			if callback != nil {
				return callback(value)
			}
		case "backspace":
			if modal.value != "" {
				_, size := utf8.DecodeLastRuneInString(modal.value)
				modal.value = modal.value[:len(modal.value)-size]
			}
		default:
			if text := key.Key().Text; text != "" {
				modal.value += text
			}
		}
		return nil
	}
	switch key.String() {
	case "up", "k":
		modal.selected = max(0, modal.selected-1)
	case "down", "j":
		modal.selected = min(len(modal.choices)-1, modal.selected+1)
	case "space", " ":
		// Bubble Tea v2 stringifies the space key as "space"; accept the
		// literal form too for safety.
		if modal.multi {
			modal.checked[modal.selected] = !modal.checked[modal.selected]
		}
	case "esc":
		m.closeModal(-1)
	case "enter":
		index, callback, multiCallback := modal.selected, modal.onSelect, modal.onMulti
		selected := modal.checked
		m.closeModal(index)
		if modal.multi && multiCallback != nil {
			return multiCallback(selected)
		}
		if callback != nil && index >= 0 {
			return callback(index)
		}
	}
	return nil
}

func (m *Model) closeModal(index int) {
	if m.modal == nil {
		return
	}
	if m.modal.reply != nil {
		select {
		case m.modal.reply <- index:
		default:
		}
	}
	m.modal = nil
	if len(m.modalQueue) > 0 {
		next := m.modalQueue[0]
		m.modalQueue = m.modalQueue[1:]
		m.enqueueModal(next)
	}
}

func (m *Model) commandMatches() []commandEntry {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	var result []commandEntry
	for _, command := range commands {
		if query == "/" || strings.Contains(command.name, query) || strings.Contains(strings.ToLower(command.description), strings.TrimPrefix(query, "/")) {
			result = append(result, command)
		}
	}
	return result
}

func (m *Model) completeCommand() {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return
	}
	m.commandIndex = max(0, min(m.commandIndex, len(matches)-1))
	m.input.SetValue(matches[m.commandIndex].name)
	m.input.MoveToEnd()
}

func (m *Model) cycleAgent() {
	if len(m.agents) == 0 {
		return
	}
	if m.viewAgent == "" {
		m.viewAgent = m.agents[0].id
		return
	}
	for index, agent := range m.agents {
		if agent.id == m.viewAgent {
			if index == len(m.agents)-1 {
				m.viewAgent = ""
			} else {
				m.viewAgent = m.agents[index+1].id
			}
			return
		}
	}
	m.viewAgent = ""
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
