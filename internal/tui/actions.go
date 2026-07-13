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
		allowed := command == "/reasoning" || command == "/effort" || command == "/goal" || command == "/context"
		if !allowed {
			m.notify(command + " is unavailable while a task is running. Press Esc to stop it, or send plain text to steer it.")
			return nil
		}
	}
	switch command {
	case "/context":
		m.openInfo("Context usage", formatContextReport(m.runtime.Engine.ContextReport()))
	case "/compact":
		if m.busy {
			m.notify("Stop the running task before compacting.")
			return nil
		}
		m.status = "Compacting conversation…"
		m.notify("Compacting conversation — summarizing earlier turns…")
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
	case "/usage":
		if m.actions.FetchUsage == nil {
			m.notify("Usage data is unavailable in this build.")
			return nil
		}
		if m.actions.IsLoggedIn != nil && !m.actions.IsLoggedIn() {
			m.notify("Sign in with /login to view account usage.")
			return nil
		}
		m.notify("Fetching usage…")
		return actionCommand("usage", func() (any, error) { return m.actions.FetchUsage(m.ctx) })
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
	case "/paste":
		m.openPasteManager()
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
		} else if last, ok := m.runtime.Engine.LastGoalResult(); ok {
			m.openInfo("Last goal result", fmt.Sprintf("%s\n\nStatus: %s%s", last.Text, last.Status, suffix(last.Blocked)))
		} else {
			m.notify("No active goal. Use /goal <objective> to set one.")
		}
	case "clear", "off", "stop", "none", "done":
		m.runtime.Engine.ClearGoal()
		m.notify("Goal cleared.")
	default:
		// B5/T023: busy-guard setting a goal mid-task. SetGoal flips plan mode
		// off (G3), so allowing it while a task runs reintroduces the exact
		// mid-turn mode change P5 guards /plan against. Status and clear stay
		// allowed while busy (a query is read-only; clearing only removes tail
		// content next task).
		if m.busy {
			m.notify("Cannot set a goal while a task is running.")
			return nil
		}
		// G3 + G6: SetGoal returns a notice when it has to disable plan mode
		// or when it replaces an active goal.
		if notice := m.runtime.Engine.SetGoal(rest); notice != "" {
			m.notify(notice)
		} else {
			m.notify("Goal set — the agent will keep working toward it until it is met.")
		}
	}
	return nil
}

func suffix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "\n\nReason: " + value
}

func (m *Model) handlePlanCommand(rest string) tea.Cmd {
	if rest == "" {
		if m.busy {
			// P5: the bare /plan toggle is now busy-guarded. Half-applying a
			// mode change mid-task turns some calls blocked and some allowed
			// in the same turn and is the source of the inconsistent reads
			// the audits flagged. Effort remains the only intentionally-live
			// knob (engine.go:214).
			m.notify("Cannot toggle plan mode while a task is running.")
			return nil
		}
		on := !m.runtime.Engine.PlanMode()
		if notice := m.runtime.Engine.SetPlanMode(on); notice != "" {
			m.notify(notice)
		}
		if on {
			m.notify("Plan mode on — the agent researches and proposes a plan without editing. Run /plan again to resume editing.")
		} else {
			m.notify("Plan mode off — editing is allowed again.")
		}
		return nil
	}
	// 004 US2 (T12): /plan clear discards the current plan — a terminal state
	// that withdraws every executable affordance.
	if strings.EqualFold(strings.TrimSpace(rest), "clear") {
		if m.busy {
			m.notify("Finish the running task before clearing the plan.")
			return nil
		}
		m.runtime.Engine.DiscardPlan()
		m.notify("Plan discarded.")
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
	if role != "subagent" {
		role = "main"
	}
	if m.actions.SetModel != nil {
		return actionCommand("settings", func() (any, error) { return nil, m.actions.SetModel(m.ctx, role, id) })
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

// formatUsage renders the /usage modal: the account plan's credit allowance as
// a progress bar per budget window (total / used / remaining / percentage), plus
// any extra credit balance. Everything is shown in credits — never dollars —
// converted at the plan rate (2,500 credits = $25, contract.USDToCredits). The
// modal is account-level only; session usage lives in /context.
func formatUsage(data *UsageData) string {
	lines := []string{}
	if data.PlanName != "" {
		lines = append(lines, "Plan: "+data.PlanName, "")
	}
	for i := range data.Windows {
		w := data.Windows[i]
		total := contract.USDToCredits(w.BudgetUSD)
		used := contract.USDToCredits(w.CurrentSpentUSD)
		reset := w.ResetTime
		if t, err := time.Parse(time.RFC3339, w.ResetTime); err == nil {
			reset = t.Local().Format("Jan 2 15:04")
		}
		lines = append(lines, strings.Title(w.Name))
		lines = append(lines, creditMeterLines(used, total)...)
		if reset != "" {
			lines = append(lines, "  Resets "+reset)
		}
		lines = append(lines, "")
	}
	if data.ExtraTotal > 0 || data.ExtraRemaining > 0 {
		lines = append(lines, "Extra credits")
		lines = append(lines, creditMeterLines(data.ExtraTotal-data.ExtraRemaining, data.ExtraTotal)...)
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		lines = append(lines, "No plan credit data was returned by the gateway.")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// creditMeterLines renders one credit allowance as a progress bar plus the
// total / used / remaining / percentage breakdown, all in credits.
func creditMeterLines(used, total float64) []string {
	if used < 0 {
		used = 0
	}
	if used > total {
		used = total
	}
	remaining := total - used
	percent := 0.0
	if total > 0 {
		percent = used / total * 100
	}
	return []string{
		fmt.Sprintf("  %s %5.1f%% used", renderBar(used, total, 24), percent),
		fmt.Sprintf("  Total:     %s credits", formatCredits(total)),
		fmt.Sprintf("  Used:      %s credits", formatCredits(used)),
		fmt.Sprintf("  Remaining: %s credits", formatCredits(remaining)),
	}
}

// renderBar draws a fixed-width block-character progress bar for used/total.
func renderBar(used, total float64, width int) string {
	fraction := 0.0
	if total > 0 {
		fraction = used / total
	}
	fraction = min(1, max(0, fraction))
	filled := int(fraction*float64(width) + 0.5)
	// A non-zero balance always shows at least one filled cell, and a bar only
	// fills completely when the allowance is truly exhausted.
	if used > 0 && filled == 0 {
		filled = 1
	}
	if fraction < 1 && filled == width {
		filled = width - 1
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// formatCredits renders a credit amount: whole credits without decimals,
// fractional amounts with two.
func formatCredits(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

// formatContextReport renders the context modal as labeled, scannable groups
// (003 T032/FR-017): Context window (with a fill bar), This session, Streams,
// Cache health. Every datum available before the overhaul is preserved.
func formatContextReport(report orchestrator.ContextReport) string {
	aggregate := report.UsageAggregate
	pressureSource := "provider-reported"
	if report.PressureEstimated {
		pressureSource = "estimated bootstrap"
	}
	free := max(0, report.ContextLimit-report.HistoryTokens)
	lines := []string{
		"Context window",
		fmt.Sprintf("  %s %5.1f%% full", renderBar(float64(report.HistoryTokens), float64(report.ContextLimit), 24), report.Percent),
		fmt.Sprintf("  In use:   %s of %s tokens", formatTokens(report.HistoryTokens), formatTokens(report.ContextLimit)),
		fmt.Sprintf("  Free:     %s tokens", formatTokens(free)),
		fmt.Sprintf("  Pressure: %s tokens (%.1f%%, %s)", formatTokens(report.PressureTokens), report.PressurePercent, pressureSource),
		"",
		"This session",
		fmt.Sprintf("  Prompt / output tokens: %s / %s", formatTokens(aggregate.SumPrompt), formatTokens(aggregate.SumCompletion)),
	}
	// Session credits (contract.USDToCredits) under the member-set honesty rule.
	if report.SessionCreditsUSD != nil {
		prefix := ""
		if report.SessionCreditsEstimated {
			prefix = "~"
		}
		lines = append(lines, fmt.Sprintf("  Credits used: %s%.2f", prefix, contract.USDToCredits(*report.SessionCreditsUSD)))
	} else if report.SessionCreditsEligible > 0 {
		lines = append(lines, fmt.Sprintf("  Credits used: unavailable (%d of %d requests priced)", report.SessionCreditsPriced, report.SessionCreditsEligible))
	} else {
		lines = append(lines, "  Credits used: unavailable")
	}
	if aggregate.CacheAvailable > 0 {
		lines = append(lines, fmt.Sprintf("  Cache read / uncached: %s / %s", formatTokens(aggregate.SumCacheRead), formatTokens(aggregate.SumCacheMiss)))
	} else {
		lines = append(lines, "  Cache read / uncached: unavailable")
	}
	lines = append(lines,
		"",
		"Streams",
		fmt.Sprintf("  Requests: main %d · aux %d · subagent %d", aggregate.MainRequests, aggregate.AuxRequests, aggregate.SubagentRequests),
	)
	if aggregate.UnavailableRequests > 0 {
		lines = append(lines, fmt.Sprintf("  Cache metrics unavailable: %d request(s)", aggregate.UnavailableRequests))
	}
	lines = append(lines,
		"",
		"Cache health",
		"  Session hit rate:      "+formatRate(aggregate.SessionHitRate),
		"  Steady-state hit rate: "+formatRate(aggregate.SteadyStateHitRate),
		"  Prefix stability rate: "+formatRate(aggregate.PrefixStabilityRate),
	)
	if report.MaintenanceLatched {
		lines = append(lines, "  Automatic maintenance: paused by anti-thrash latch")
	}
	if len(report.Invalidations) > 0 {
		lines = append(lines, "  Recent cache invalidations:")
		start := max(0, len(report.Invalidations)-5)
		for _, event := range report.Invalidations[start:] {
			lines = append(lines, fmt.Sprintf("    - %s: %s (%s)", event.Cause, event.Scope, event.At.Local().Format("15:04:05")))
		}
	}
	return strings.Join(lines, "\n")
}

func formatRate(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}

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
		m.warn(action.kind + " failed: " + action.err.Error())
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
		m.notify("API key saved.")
	case "logout":
		m.notify("Signed out. The stored API key was cleared.")
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
			m.thoughtStart = time.Time{}
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
		{Label: "Proceed now", Description: "Turn plan mode off and execute the approved plan immediately", Recommended: true},
		{Label: "Proceed later", Description: "Save the plan; say 'proceed' any time to execute it"},
		{Label: "Keep planning", Description: "Stay in plan mode and keep refining the plan"},
	}
	m.openChoice("Plan is ready", "The plan is complete. How do you want to proceed?", choices, func(index int) tea.Cmd {
		switch index {
		case 0: // Proceed now
			if m.runtime.Engine != nil {
				m.runtime.Engine.SetPendingPlan(false)
				m.runtime.Engine.SetPlanMode(false)
				m.runtime.Engine.SetPlanPhase(contract.PlanPhaseExecuting) // 004 US2 (T3)
			}
			m.notify("Proceeding with the approved plan now.")
			return m.submit("Proceed with the approved plan. Work through the plan steps in order, keeping update_plan current.")
		case 1: // Proceed later
			if m.runtime.Engine != nil {
				m.runtime.Engine.SetPendingPlan(true)
				m.runtime.Engine.SetPlanMode(false)
				m.runtime.Engine.SetPlanPhase(contract.PlanPhasePending) // 004 US2 (T4)
			}
			m.notify("Plan saved. Say 'proceed' (or 'go ahead') any time to execute it.")
			return nil
		default: // Keep planning
			if m.runtime.Engine != nil {
				m.runtime.Engine.SetPlanPhase(contract.PlanPhaseDrafting) // 004 US2 (T5): plan mode stays on
			}
			m.notify("Plan mode stays on — keep refining the plan.")
			return nil
		}
	})
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
}

func (m *Model) commandMatches() []commandEntry {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	var result []commandEntry
	for _, command := range commands {
		if !m.commandVisible(command.name) {
			continue
		}
		if query == "/" || strings.Contains(command.name, query) || strings.Contains(strings.ToLower(command.description), strings.TrimPrefix(query, "/")) {
			result = append(result, command)
		}
	}
	return result
}

// commandVisible applies the sign-in-aware palette filter (003 T031/FR-019):
// /login shows only when signed out; /usage and /logout show only when signed in.
func (m *Model) commandVisible(name string) bool {
	loggedIn := m.actions.IsLoggedIn != nil && m.actions.IsLoggedIn()
	switch name {
	case "/login":
		return !loggedIn
	case "/logout", "/usage":
		return loggedIn
	case "/paste":
		return len(m.activePastes()) > 0
	default:
		return true
	}
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
