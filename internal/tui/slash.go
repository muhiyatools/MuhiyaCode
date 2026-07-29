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

type checkpointRestoreValue struct {
	runtime Runtime
	events  []contract.Event
	message string
}

func (m *Model) runSlash(value string) tea.Cmd {
	args := strings.Fields(value)
	command := strings.ToLower(args[0])
	if m.busy {
		allowed := command == "/reasoning" || command == "/effort" || command == "/context" || command == "/requests"
		if !allowed {
			m.notify(command + " is unavailable while a task is running. Press Esc to stop it, or send plain text to steer it.")
			return nil
		}
	}
	switch command {
	case "/context":
		m.openInfo("Context usage", formatContextReport(m.runtime.Engine.ContextReport(), m.sessionModelName()))
	case "/requests":
		m.openInfo("Request diagnostics", formatRequestTrace(m.runtime.Engine))
	case "/model":
		if len(args) > 1 {
			targetID := strings.TrimSpace(args[1])
			return m.switchActiveModel(targetID)
		}
		return m.openModelSelector()
	case "/compact":
		if m.busy {
			m.notify("Stop the running task before compacting.")
			return nil
		}
		m.status = "Compacting conversation…"
		m.notify("Compacting conversation — summarizing earlier turns…")
		return actionCommand("compact", func() (any, error) { return m.runtime.Engine.Compact(m.ctx) })
	case "/rewind":
		if len(args) > 1 && m.actions.RestoreCheckpoint != nil {
			scope := "both"
			if len(args) > 2 {
				scope = args[2]
			}
			return m.restoreCheckpointCommand(args[1], scope)
		}
		if m.actions.ListCheckpoints != nil {
			return actionCommand("checkpoints", func() (any, error) {
				return m.actions.ListCheckpoints(m.ctx)
			})
		}
		if m.actions.Rewind == nil {
			m.notify("Rewind is unavailable in this interface.")
			return nil
		}
		return actionCommand("rewind", func() (any, error) { return m.actions.Rewind(m.ctx) })
	case "/processes":
		if m.actions.ListProcesses == nil {
			m.notify("Background process management is unavailable.")
			return nil
		}
		return actionCommand("processes", func() (any, error) {
			return m.actions.ListProcesses(), nil
		})
	case "/reasoning", "/effort":
		if m.runtime.Settings == nil { // H-2: guard the Effort deref below
			return nil
		}
		if len(args) > 1 {
			return m.setEffort(args[1])
		}
		choices := []contract.QuestionChoice{}
		for _, level := range []contract.EffortLevel{contract.EffortLow, contract.EffortMedium, contract.EffortHigh, contract.EffortMax} {
			choices = append(choices, contract.QuestionChoice{Label: string(level), Description: reasoningSummary(level), Recommended: level == m.runtime.Settings.Effort})
		}
		m.openChoice("Reasoning effort", "How hard the model thinks.", choices, func(index int) tea.Cmd { return m.setEffort(choices[index].Label) })
	// /permissions and /mode are gone (013 FR-014): Shift+Tab cycles the mode and
	// the footer names the current one with the shortcut beneath it, so a command
	// for the same two-state toggle was pure surface area.
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

func (m *Model) restoreCheckpointCommand(id, scope string) tea.Cmd {
	return actionCommand("checkpoint-restore", func() (any, error) {
		runtime, events, message, err := m.actions.RestoreCheckpoint(m.ctx, id, scope)
		return checkpointRestoreValue{runtime: runtime, events: events, message: message}, err
	})
}

// reasoningSummary describes each level in terms of the user's choice — how
// hard the model thinks — with no provider-mapping trivia (013 FR-016). Which
// internal thinking tier a given model receives is the gateway's business, and
// naming one vendor in a multi-provider tool only invited the question of what
// the other providers do.
func reasoningSummary(level contract.EffortLevel) string {
	switch level {
	case contract.EffortLow:
		return "Lightest thinking, fastest — the default."
	case contract.EffortMedium:
		return "Balanced thinking."
	case contract.EffortHigh:
		return "Deep thinking for tricky work."
	case contract.EffortMax:
		return "Maximum thinking for the hardest problems."
	default:
		return ""
	}
}

// sessionModelName resolves the display name of the session's model for the
// context card (013 FR-023). The name comes from the configured model list; an
// unlisted ID falls back to the ID itself so the card is never blank.
func (m *Model) sessionModelName() string {
	if m.runtime.Settings == nil {
		return ""
	}
	provider := m.runtime.Settings.Provider
	if provider.ActiveModelID == "" {
		return ""
	}
	for _, model := range provider.Models {
		if model.ID == provider.ActiveModelID {
			return model.Name
		}
	}
	return provider.ActiveModelID
}

// engineOp and suffix went with the interactive engine mutations (/model and
// friends) that used them; nothing drives a void engine op from the TUI now.

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
	if mode == contract.PermissionAutoAccept && m.runtime.Settings != nil && m.runtime.Settings.PermissionMode != contract.PermissionAutoAccept {
		m.openChoice(
			"Unsafe full access",
			"Auto-accept lets tools edit files and run unrestricted host shell commands without asking. It is not sandboxed. Enable it for this session?",
			[]contract.QuestionChoice{
				{Label: "Keep normal", Description: "Continue requiring approval for risky actions", Recommended: true},
				{Label: "Enable full access", Description: "Allow unrestricted unattended execution"},
			},
			func(index int) tea.Cmd {
				if index != 1 {
					m.notify("Permission mode remains normal.")
					return nil
				}
				return m.applyPermission(contract.PermissionAutoAccept)
			},
		)
		return nil
	}
	return m.applyPermission(mode)
}

func (m *Model) applyPermission(mode contract.PermissionMode) tea.Cmd {
	if m.runtime.Engine != nil {
		// Route through the engine's synchronized setter so a mid-task change does
		// not race the task goroutine reading permission mode (F-1). This writes
		// the same shared settings field, under liveSettingsMu.
		m.runtime.Engine.SetPermissionMode(mode)
	} else if m.runtime.Settings != nil {
		m.runtime.Settings.PermissionMode = mode
	}
	if m.actions.SetPermission != nil {
		return actionCommand("permission", func() (any, error) { return mode, m.actions.SetPermission(m.ctx, mode) })
	}
	if m.actions.SaveSettings != nil {
		return actionCommand("permission", func() (any, error) { return mode, m.actions.SaveSettings(m.ctx, m.runtime.Settings) })
	}
	return nil
}

func (m *Model) cyclePermission() tea.Cmd {
	if m.runtime.Settings == nil { // H-2: no settings → nothing to toggle, never panic
		return nil
	}
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

func (m *Model) openModelSelector() tea.Cmd {
	if m.runtime.Engine == nil {
		m.notify("Engine not initialized.")
		return nil
	}
	models := m.runtime.Engine.DiscoveredModels()
	if len(models) == 0 {
		m.notify("No gateway models discovered yet. Use `/model <id>` to set a model ID directly.")
		return nil
	}
	choices := make([]contract.QuestionChoice, 0, len(models))
	activeID := m.runtime.Settings.Provider.ActiveModelID
	for _, model := range models {
		label := model.Name
		if strings.TrimSpace(label) == "" {
			label = model.ID
		}
		desc := fmt.Sprintf("ID: %s", model.ID)
		if model.ContextLimit > 0 {
			desc += fmt.Sprintf(" | Context: %d tokens", model.ContextLimit)
		}
		rec := model.ID == activeID
		if rec {
			desc += " (active)"
		}
		choices = append(choices, contract.QuestionChoice{
			Label: label, Description: desc, Recommended: rec,
		})
	}
	m.openChoice(
		"Select Model",
		"Choose a model discovered from the gateway catalog:",
		choices,
		func(index int) tea.Cmd {
			if index >= 0 && index < len(models) {
				return m.switchActiveModel(models[index].ID)
			}
			return nil
		},
	)
	return nil
}

func (m *Model) switchActiveModel(modelID string) tea.Cmd {
	if m.busy {
		m.notify("Stop the running task before switching models.")
		return nil
	}
	if m.runtime.Engine == nil {
		m.notify("Engine not initialized.")
		return nil
	}
	return actionCommand("switch-model", func() (any, error) {
		name := m.runtime.Engine.CatalogModelName(modelID)
		profile := gateway.ResolveModelProfile(modelID + " " + name)
		if err := m.runtime.Engine.SwitchModel(m.ctx, "main", modelID, name, profile.PromptAddendum); err != nil {
			return nil, err
		}
		m.runtime.Settings.Provider.ActiveModelID = modelID
		return fmt.Sprintf("Active session model switched to %s (%s). Cache epoch is now %d; the next request starts a new cache lineage.", name, modelID, m.runtime.Engine.CacheEpoch()), nil
	})
}
