package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// commandLayout is the single source of truth for the command-palette window: the
// filtered matches, the scroll-window start, and how many rows are shown. Both the
// renderer and the mouse target recorder consume it so a click always resolves to
// the exact row the user sees. ok is false when the palette is not showing.
func (m *Model) commandLayout() (matches []commandEntry, start, limit int, ok bool) {
	if !strings.HasPrefix(m.input.Value(), "/") {
		return nil, 0, 0, false
	}
	matches = m.commandMatches()
	if len(matches) == 0 {
		return nil, 0, 0, false
	}
	limit = min(m.commandWindow(), len(matches))
	selected := max(0, min(m.commandIndex, len(matches)-1))
	start = max(0, min(selected-limit/2, len(matches)-limit))
	return matches, start, limit, true
}

func (m *Model) renderCommands() string {
	matches, start, limit, ok := m.commandLayout()
	if !ok {
		return ""
	}
	selected := max(0, min(m.commandIndex, len(matches)-1))
	var lines []string
	for index := start; index < start+limit; index++ {
		marker, style := "  ", m.palette.muted
		if index == selected {
			marker, style = "› ", m.palette.brandSoft
		}
		lines = append(lines, fitLine(" "+style.Render(marker+matches[index].name)+"  "+m.palette.faint.Render(matches[index].description), m.width))
	}
	if len(matches) > limit {
		lines = append(lines, fitLine(" "+m.palette.faint.Render(fmt.Sprintf("   %d/%d — ↑/↓ to browse, Tab to complete, Enter to run", selected+1, len(matches))), m.width))
	}
	return strings.Join(lines, "\n")
}

type runtimeActionValue struct {
	runtime Runtime
	events  []contract.Event
}

func (m *Model) runSlash(value string) tea.Cmd {
	args := strings.Fields(value)
	command := strings.ToLower(args[0])
	if m.busy {
		allowed := command == "/reasoning" || command == "/effort" || command == "/context" || command == "/errors"
		if !allowed {
			m.notify(command + " is unavailable while a task is running. Press Esc to stop it, or send plain text to steer it.")
			return nil
		}
	}
	switch command {
	case "/context":
		report := formatContextReport(m.runtime.Engine.ContextReport())
		if m.runtime.Settings != nil {
			report += "\n\n" + formatCapabilityProfile(gateway.ResolveModelProfile(m.runtime.Settings.Provider.ActiveModelID))
		}
		m.openInfo("Context usage", report)
	case "/errors":
		// HarnessEvents() is a read-only, lock-guarded ring copy — safe on the
		// Update goroutine (see uithread_guard allowlist, T022).
		m.openInfo("Harness events", formatHarnessEvents(m.runtime.Engine.HarnessEvents()))
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
	case "/permissions", "/mode":
		if len(args) > 1 {
			return m.setPermission(contract.PermissionMode(args[1]))
		}
		choices := []contract.QuestionChoice{{Label: "normal", Description: "Confirm mutations and shell commands", Recommended: m.runtime.Settings.PermissionMode == contract.PermissionNormal}, {Label: "auto-accept", Description: "Automatically allow safe workspace actions", Recommended: m.runtime.Settings.PermissionMode == contract.PermissionAutoAccept}}
		m.openChoice("Permission mode", "Destructive commands and credential paths remain blocked in every mode.", choices, func(index int) tea.Cmd { return m.setPermission(contract.PermissionMode(choices[index].Label)) })
	case "/login":
		// `/login <key>` pastes a key directly; bare `/login` opens the browser
		// sign-in (loopback + PKCE) through the Muhiya platform.
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			if m.actions.SetAPIKey == nil {
				m.notify("Use `muhiyacode config set apiKey <key>` in this build.")
				return nil
			}
			key := strings.TrimSpace(args[1])
			return actionCommand("login", func() (any, error) { return nil, m.actions.SetAPIKey(m.ctx, key) })
		}
		if m.actions.LoginViaBrowser != nil {
			m.notify("Opening your browser to sign in… approve the request, then return here.")
			return actionCommand("login", func() (any, error) { return m.actions.LoginViaBrowser(m.ctx) })
		}
		if m.actions.SetAPIKey == nil {
			m.notify("Run `muhiyacode login` in a terminal to sign in.")
			return nil
		}
		m.openText("Muhiya API key", "Paste the API key, or run `muhiyacode login` for browser sign-in. It is stored with user-only permissions.", true, func(value string) tea.Cmd {
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

// engineOp runs a void engine mutation in a tea.Cmd goroutine (off the Update
// loop) and surfaces a fixed notice when it completes (T021).
func engineOp(op func(), notice string) tea.Cmd {
	return func() tea.Msg {
		op()
		return engineNoticeMsg{notice: notice}
	}
}

func suffix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "\n\nReason: " + value
}

// Ultimate Polish P1: the /plan command is removed. Planning is now an internal
// agent flow — the classifier routes "create a plan …" into the pipeline (proposes
// then pauses for proceed-now/later/discard) and "execute the plan in X.md" into
// direct plan-document execution, so there is no user-facing plan toggle to keep in
// sync with the lifecycle. A pending plan is dropped via the plan-ready modal's
// "Discard plan" choice or a natural-language "discard the plan".

func (m *Model) setEffort(value string) tea.Cmd {
	level, ok := orchestrator.NormalizeEffort(strings.ToLower(value))
	if !ok {
		m.notify("Effort must be low, medium, high, or max.")
		return nil
	}
	m.runtime.Engine.SetEffort(level)
	m.notify("Reasoning set to " + effortLabel(level))
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

func (m *Model) resumeCommand(id string) tea.Cmd {
	if m.actions.Resume == nil {
		return nil
	}
	return actionCommand("resume", func() (any, error) {
		runtime, events, err := m.actions.Resume(m.ctx, id)
		return runtimeActionValue{runtime: runtime, events: events}, err
	})
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
