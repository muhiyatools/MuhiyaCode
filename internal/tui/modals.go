package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func actionCommand(kind string, run func() (any, error)) tea.Cmd {
	return func() tea.Msg {
		value, err := run()
		return actionMsg{kind: kind, value: value, err: err}
	}
}

func (m *Model) handleAction(action actionMsg) tea.Cmd {
	if action.err != nil {
		if action.kind == "compact" {
			m.status = "Ready"
		}
		message := action.kind + " failed: " + action.err.Error()
		if action.kind == "login" {
			// A failed browser sign-in (timeout, browser did not open, denied) must
			// leave an escape hatch, and the onboarding modal should be re-offered.
			message += " — you can also paste a key with /login <key>"
			m.openOnboarding()
		}
		m.warn(message)
		return nil
	}
	switch action.kind {
	case "compact", "rewind":
		if action.kind == "compact" {
			m.status = "Ready"
		}
		if text, ok := action.value.(string); ok {
			m.notify(text)
		}
	case "mcp-list":
		// M2: a refresh tick can land here while the MCP modal is open.
		// Stay in the same modal and update the choices in place so the
		// user is not bounced into a fresh selection every 500ms.
		if m.mcpModalAtRest {
			m.refreshMCPChoices(action.value)
			return nil
		}
		return m.showMCPList(action.value)
	case "mcp-action":
		return m.afterMCPAction(action.value)
	case "models-refresh":
		models, _ := action.value.([]contract.Model)
		m.notify(fmt.Sprintf("Model catalog refreshed: %d model(s) available.", len(models)))
	case "login":
		// A successful sign-in retires any pending first-run modal.
		m.onboardingPending = false
		if email, ok := action.value.(string); ok && email != "" {
			m.notify("Signed in as " + email + ".")
		} else {
			m.notify("Signed in.")
		}
	case "logout":
		m.notify("Signed out. The stored API key was cleared.")
		// Re-present the sign-in modal so the app is never left in a signed-out
		// state with no obvious way back in.
		m.openOnboarding()
	case "usage":
		if data, ok := action.value.(*UsageData); ok && data != nil {
			m.openInfo("Account usage", formatUsage(data))
		} else {
			m.notify("No usage data was returned.")
		}
	case "permission":
		m.notify("Permission mode: " + string(m.runtime.Settings.PermissionMode))
	case "settings":
		m.status = "Settings saved"
	case "new", "resume":
		payload, ok := action.value.(runtimeActionValue)
		if ok && payload.runtime.Engine != nil {
			m.runtime = payload.runtime
			m.items, m.agents, m.agentByID, m.viewAgent = nil, nil, make(map[string]*agentView), ""
			// L2: also drop the active-tool index and the streaming draft, which point
			// into the discarded session's items; a stale entry would otherwise linger.
			m.activeTools = make(map[string]*toolView)
			m.draft.Reset()
			m.followOutput = true // US2 T033: a switched-in session starts pinned to the latest
			m.input.Reset()       // US5 T067: drop the previous session's draft
			m.releasePastes()     // US5 T067: and its stashed paste blocks
			m.resetPaging()       // US1 T020: bump the page generation; cancel stale page loads
			m.loadEvents(payload.events)
			m.plan, m.usage = payload.runtime.Engine.CurrentPlan(), payload.runtime.Engine.Usage()
			// Show the restored session's real context usage immediately
			// instead of a blank "context —" until the next model turn.
			report := payload.runtime.Engine.ContextReport()
			m.context = contract.ContextInfo{HistoryTokens: report.HistoryTokens, ContextLimit: report.ContextLimit, Percent: report.Percent}
			// T1: a session switch clears the persistent usage footer so
			// the previous session's totals never bleed across.
			m.lastStats = nil
			m.notify("Session opened: " + payload.runtime.Session.ID)
			// G4: if the resumed session restored an active goal from its
			// goal.json sidecar, surface the one-shot notice here too. We are
			// already inside the payload.runtime.Engine != nil guard above.
			if restored := payload.runtime.Engine.RestoredGoalNotice(); restored != "" {
				m.notify(restored)
			}
			// P2: surface the restored plan-state notice (plan mode and/or a
			// pending plan) on a mid-TUI session switch too.
			if restored := payload.runtime.Engine.RestoredPlanNotice(); restored != "" {
				m.notify(restored)
			}
			// US1 T020: establish the paging cursor for the switched-in session.
			return m.loadInitialPageCmd()
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

// replyModalOpen reports whether a bridge-driven modal (a permission Confirm or
// ask_user prompt) with a pending reply channel is currently showing. An async
// command result must never clobber such a modal: discarding its reply would leave
// the engine goroutine parked in bridge.request() forever, wedging the running
// task until the user cancels (M2).
func (m *Model) replyModalOpen() bool {
	return m.modal != nil && m.modal.reply != nil
}

func (m *Model) openChoice(title, message string, choices []contract.QuestionChoice, onSelect func(int) tea.Cmd) {
	if m.replyModalOpen() {
		m.notify(title + " — try again after answering the current prompt.")
		return
	}
	m.modal = &modalState{title: title, message: message, choices: choices, selected: recommendedChoice(choices), onSelect: onSelect}
}

func (m *Model) openMulti(title, message string, choices []contract.QuestionChoice, checked map[int]bool, onMulti func(map[int]bool) tea.Cmd) {
	if m.replyModalOpen() {
		m.notify(title + " — try again after answering the current prompt.")
		return
	}
	if checked == nil {
		checked = make(map[int]bool)
	}
	m.modal = &modalState{title: title, message: message, choices: choices, multi: true, checked: checked, onMulti: onMulti}
}

func (m *Model) openText(title, message string, secret bool, onText func(string) tea.Cmd) {
	if m.replyModalOpen() {
		m.notify(title + " — try again after answering the current prompt.")
		return
	}
	m.modal = &modalState{title: title, message: message, input: true, secret: secret, onText: onText}
}

// openPlanReadyModal (P2) opens the Proceed now / Proceed later / Keep planning
// modal after a plan-ready task. The engine left plan mode on in interactive
// runs; each choice drives the engine's plan/pending state and either submits
// (proceed now) or notifies (proceed later / keep planning).
func (m *Model) openPlanReadyModal() {
	choices := []contract.QuestionChoice{
		{Label: "Proceed now", Description: "Execute the approved plan immediately", Recommended: true},
		{Label: "Proceed later", Description: "Save the plan; say 'proceed' any time to execute it"},
		{Label: "Keep planning", Description: "Keep refining the plan without editing"},
		{Label: "Discard plan", Description: "Drop this plan entirely (or say 'discard the plan' any time)"},
	}
	m.openChoice("Plan is ready", "The plan is complete. How do you want to proceed?", choices, func(index int) tea.Cmd {
		// 010 UL-2: one unified path per choice — the engine's lifecycle methods
		// handle the orchestrated-vs-manual distinction internally, so the TUI no
		// longer branches on a pipeline-vs-legacy discriminator that could drift.
		engine := m.runtime.Engine
		if engine == nil {
			return nil
		}
		// CRITICAL: this callback runs inside Bubble Tea's Update loop. The engine
		// lifecycle methods below synchronously emit pipeline notices/plan updates
		// via program.Send on an UNBUFFERED channel, which the Update goroutine is
		// the sole reader of — calling them here deadlocks the event loop and
		// freezes the entire terminal. Each branch therefore runs the transition in
		// a command goroutine and returns a result message; Update performs the
		// notify()/submit() follow-up back on the UI thread (see update.go).
		ctx := m.ctx
		switch index {
		case 0: // Proceed now — approve and start execution (dead-click guarded inside).
			return func() tea.Msg { return planProceedResultMsg{ok: engine.ProceedWithPlan(ctx)} }
		case 1: // Proceed later — save the plan as pending.
			return func() tea.Msg { engine.DeferPlan(ctx); return planDeferredMsg{} }
		case 2: // Keep planning — return to the planning state.
			return func() tea.Msg { return planKeepPlanningMsg{err: engine.KeepPlanning(ctx)} }
		default: // P2: Discard plan — drop it entirely.
			return func() tea.Msg { engine.DiscardPlan(); return engineNoticeMsg{notice: "Plan discarded."} }
		}
	})
}

// openOnboarding shows the first-run sign-in modal: a single option that starts
// the browser login. If another modal is currently showing (notably the
// workspace-trust reply prompt), it defers itself and closeModal reopens it once
// the queue drains, so it is never dropped by openChoice's reply-modal guard. A
// signed-in user never sees it.
func (m *Model) openOnboarding() {
	if m.actions.IsLoggedIn != nil && m.actions.IsLoggedIn() {
		m.onboardingPending = false
		return
	}
	if m.modal != nil || len(m.modalQueue) > 0 {
		m.onboardingPending = true
		return
	}
	m.onboardingPending = false
	choices := []contract.QuestionChoice{
		{Label: "Log in with Muhiya Account", Description: "Opens your browser (muhiya.com) — approve, then return here", Recommended: true},
	}
	m.openChoice(
		"Welcome to MuhiyaCode",
		"Sign in to connect this machine to your Muhiya account. Your browser will open so you can approve the sign-in.\n\nPress Esc to dismiss — you can also paste a key with /login <key>.",
		choices,
		func(int) tea.Cmd {
			if m.actions.LoginViaBrowser == nil {
				m.notify("Run `muhiyacode login` in a terminal to sign in.")
				return nil
			}
			m.notify("Opening your browser to sign in… approve the request, then return here.")
			return actionCommand("login", func() (any, error) { return m.actions.LoginViaBrowser(m.ctx) })
		},
	)
}

// openInfo shows a read-only modal (no choices) that closes on Enter or Esc.
func (m *Model) openInfo(title, message string) {
	if m.replyModalOpen() {
		m.notify(title + " — try again after answering the current prompt.")
		return
	}
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
	// M2: stop the live-refresh poll when ANY modal closes — the case we
	// care about for MCP is the common one, and keeping a stale flag
	// could re-kick the tick from a non-MCP dialog.
	m.mcpModalAtRest = false
	m.mcpRefreshIn = false
	if len(m.modalQueue) > 0 {
		next := m.modalQueue[0]
		m.modalQueue = m.modalQueue[1:]
		m.enqueueModal(next)
	}
	// Once every other modal (a trust prompt, a queued permission) is dismissed,
	// open the deferred first-run onboarding modal — unless the user has since
	// signed in (e.g. via `/login <key>`), in which case just clear the flag.
	if m.modal == nil && len(m.modalQueue) == 0 && m.onboardingPending {
		m.openOnboarding()
	}
}
