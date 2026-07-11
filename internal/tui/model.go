package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

type Runtime struct {
	Engine   *orchestrator.Engine
	Session  contract.Session
	Settings *contract.Settings
}

type Skill struct {
	Name, Description, Instructions string
}

type Actions struct {
	SaveSettings  func(context.Context, *contract.Settings) error
	SetModel      func(context.Context, string, string) error
	SetPermission func(context.Context, contract.PermissionMode) error
	Rewind        func(context.Context) (string, error)
	NewSession    func(context.Context) (Runtime, []contract.Event, error)
	ListSessions  func(context.Context) ([]contract.Session, error)
	Resume        func(context.Context, string) (Runtime, []contract.Event, error)
	SetAPIKey     func(context.Context, string) error
	Logout        func(context.Context) error
	MCP           MCPActions
	ListSkills    func(context.Context) ([]Skill, error)
	// DiscoverModels re-queries the gateway's model catalog, merges it into
	// settings, and returns the models now available. No restart needed.
	DiscoverModels func(context.Context) ([]contract.Model, error)
}

type Options struct {
	Runtime       Runtime
	Bridge        *Bridge
	Actions       Actions
	Version       string
	InitialPrompt string
	Recent        []contract.Event
	Notice        string
	Context       context.Context
	Simple        bool
}

type item struct {
	kind, content, title string
	tool                 *toolView
	agentID              string
}

type toolView struct {
	name, target, output, summary string
	state                         string
	started                       time.Time
}

type agentView struct {
	id, agent, title, task, model, status string
	index                                 int
	usage                                 contract.Usage
	items                                 []item
	active                                string
}

type modalState struct {
	title, message string
	choices        []contract.QuestionChoice
	selected       int
	reply          chan int
	onSelect       func(int) tea.Cmd
	multi          bool
	checked        map[int]bool
	onMulti        func(map[int]bool) tea.Cmd
	input          bool
	secret         bool
	value          string
	onText         func(string) tea.Cmd
}

type resultMsg struct {
	answer string
	err    error
}
type actionMsg struct {
	kind  string
	value any
	err   error
}
type initialMsg string
type tickMsg time.Time

// commandEntry is one selectable slash command in the palette.
type commandEntry struct{ name, description string }

// noticeState is a transient, dismissible message shown in the status zone.
// System actions (settings, permissions, MCP, task summaries) surface here
// rather than in the transcript, which stays limited to user and assistant
// turns. A zero expires time means the message is sticky until replaced.
type noticeState struct {
	text    string
	level   string // "info" | "warn"
	expires time.Time
}

type Model struct {
	ctx            context.Context
	runtime        Runtime
	bridge         *Bridge
	actions        Actions
	version        string
	palette        palette
	viewport       viewport.Model
	input          textarea.Model
	width          int
	height         int
	items          []item
	busy           bool
	status         string
	started        time.Time
	usage          contract.Usage
	context        contract.ContextInfo
	plan           contract.Plan
	reasoning      string
	verbose        bool
	frame          int
	modal          *modalState
	modalQueue     []modalRequest
	agents         []*agentView
	agentByID      map[string]*agentView
	viewAgent      string
	selectedSkills map[string]Skill
	commandIndex   int
	history        []string
	historyIndex   int
	initialPrompt  string
	flash          noticeState
}

var commands = []commandEntry{
	{"/reasoning", "Set reasoning effort (low–max)"}, {"/goal", "Set an autonomous goal"}, {"/plan", "Plan before editing"},
	{"/resume", "Resume a workspace session"}, {"/new", "Start a new session"},
	{"/context", "Inspect context usage"}, {"/compact", "Compact conversation"},
	{"/model", "Choose or refresh models"}, {"/login", "Store API key"}, {"/logout", "Clear API key"},
	{"/permissions", "Change permission mode"}, {"/skills", "Assign skills to next prompt"}, {"/mcp", "Manage MCP servers"},
	{"/diff", "Summarize git diff"}, {"/rewind", "Restore latest checkpoint"}, {"/stop", "Stop running task"}, {"/exit", "Exit MuhiyaCode"},
}

func NewModel(options Options) *Model {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	input := textarea.New()
	input.Placeholder = "Ask MuhiyaCode to inspect, build, fix, or explain…"
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 32_000
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = 5
	input.SetVirtualCursor(true)
	colors := newPalette()
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Text = colors.text
	styles.Focused.Placeholder = colors.faint
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Prompt = colors.brand
	styles.Cursor.Color = lipgloss.Color("#43D17D")
	input.SetStyles(styles)
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	view.SoftWrap = false
	view.FillHeight = true
	m := &Model{ctx: ctx, runtime: options.Runtime, bridge: options.Bridge, actions: options.Actions, version: options.Version, palette: colors, viewport: view, input: input, width: 100, height: 34, status: "Ready", agentByID: make(map[string]*agentView), selectedSkills: make(map[string]Skill), historyIndex: -1, initialPrompt: options.InitialPrompt}
	m.plan = options.Runtime.Engine.CurrentPlan()
	report := options.Runtime.Engine.ContextReport()
	m.context = contract.ContextInfo{HistoryTokens: report.HistoryTokens, ContextLimit: report.ContextLimit, Percent: report.Percent}
	m.loadEvents(options.Recent)
	if strings.TrimSpace(options.Notice) != "" {
		m.flash = noticeState{text: options.Notice, level: "warn"}
	}
	m.refreshViewport(true)
	return m
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.input.Focus(), tick()}
	if strings.TrimSpace(m.initialPrompt) != "" {
		prompt := m.initialPrompt
		commands = append(commands, func() tea.Msg { return initialMsg(prompt) })
	}
	return tea.Batch(commands...)
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(value time.Time) tea.Msg { return tickMsg(value) })
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	wasBottom := m.viewport.AtBottom()
	beforeOffset := m.viewport.YOffset()
	var commandsOut []tea.Cmd
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(40, value.Width), max(14, value.Height)
	case tickMsg:
		m.frame++
		m.expireNotice()
		commandsOut = append(commandsOut, tick())
	case initialMsg:
		commandsOut = append(commandsOut, m.submit(string(value)))
	case statusMsg:
		m.status = simplifyStatus(string(value))
	case streamMsg:
		m.appendStream(value)
	case toolStartMsg:
		m.items = append(m.items, item{kind: "tool", tool: &toolView{name: value.name, target: toolTarget(value.name, value.input), state: "running", started: time.Now()}})
	case toolEndMsg:
		m.finishTool(value.name, value.output)
	case toolOutputMsg:
		m.appendToolOutput(value.name, value.output)
	case planMsg:
		m.plan = contract.Plan(value)
	case usageMsg:
		m.usage = contract.Usage(value)
	case contextMsg:
		m.context = contract.ContextInfo(value)
	case agentMsg:
		m.applyAgent(contract.AgentEvent(value))
	case statsMsg:
		m.notify(taskSummary(contract.TaskStats(value)))
	case mcpStatusMsg:
		m.warn("MCP · " + string(value))
	case modalRequest:
		m.enqueueModal(value)
	case resultMsg:
		m.busy = false
		m.status = "Ready"
		m.reasoning = ""
		if value.err != nil {
			m.warn("Task stopped: " + value.err.Error())
		} else {
			m.finishAssistant(value.answer)
		}
	case actionMsg:
		commandsOut = append(commandsOut, m.handleAction(value))
	case tea.KeyPressMsg:
		command := m.handleKey(value)
		if command != nil {
			commandsOut = append(commandsOut, command)
		}
	case tea.PasteMsg:
		// Bracketed paste arrives as its own message type; route it to
		// whichever text field has focus or clipboard paste does nothing.
		if m.modal != nil && m.modal.input {
			m.modal.value += value.Content
		} else if m.modal == nil {
			var command tea.Cmd
			m.input, command = m.input.Update(value)
			if command != nil {
				commandsOut = append(commandsOut, command)
			}
		}
	}
	// Recompute the layout every cycle so the transcript viewport shrinks to
	// make room for the plan, command palette, and notice line instead of
	// overflowing the screen.
	m.layout()
	// A keyboard scroll (PageUp/Down, arrow-scroll) that moved the viewport
	// this round must stick: while busy we still force the bottom to
	// auto-follow a live stream, but otherwise only re-pin to the bottom if
	// the user was already there and didn't just scroll away.
	scrolled := m.viewport.YOffset() != beforeOffset
	forceBottom := (wasBottom && !scrolled) || m.busy
	if _, isKey := message.(tea.KeyPressMsg); !isKey || m.modal == nil {
		m.refreshViewport(forceBottom)
	}
	return m, tea.Batch(commandsOut...)
}

func (m *Model) expireNotice() {
	if m.flash.text != "" && !m.flash.expires.IsZero() && time.Now().After(m.flash.expires) {
		m.flash = noticeState{}
	}
}

func (m *Model) handleKey(key tea.KeyPressMsg) tea.Cmd {
	if m.modal != nil {
		return m.handleModalKey(key)
	}
	stroke := key.String()
	switch stroke {
	case "ctrl+c":
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
	case "ctrl+o":
		m.verbose = !m.verbose
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
	display := prompt
	if m.busy {
		if m.runtime.Engine.QueueUserMessage(prompt) {
			m.items = append(m.items, item{kind: "user", content: prompt, title: "steering"})
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
			if body == "" {
				body = skill.Description
			}
			sections = append(sections, "<skill name=\""+skill.Name+"\">\n"+body+"\n</skill>")
		}
		prompt = "Follow the selected skill instructions when relevant to this turn.\n\n" + strings.Join(sections, "\n\n") + "\n\nUser prompt:\n" + prompt
		m.selectedSkills = make(map[string]Skill)
	}
	m.busy, m.status, m.started = true, "Thinking…", time.Now()
	engine := m.runtime.Engine
	ctx := m.ctx
	return func() tea.Msg {
		answer, _, err := engine.Run(ctx, prompt)
		return resultMsg{answer: answer, err: err}
	}
}

func (m *Model) appendStream(stream streamMsg) {
	if stream.reasoning != "" {
		m.reasoning += stream.reasoning
		if len(m.reasoning) > 300 {
			m.reasoning = m.reasoning[len(m.reasoning)-300:]
		}
	}
	if stream.text == "" {
		return
	}
	if len(m.items) == 0 || m.items[len(m.items)-1].kind != "assistant_draft" {
		m.items = append(m.items, item{kind: "assistant_draft"})
	}
	m.items[len(m.items)-1].content += stream.text
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

func (m *Model) finishTool(name, output string) {
	for index := len(m.items) - 1; index >= 0; index-- {
		tool := m.items[index].tool
		if tool != nil && tool.name == name && tool.state == "running" {
			tool.output, tool.summary = output, summarizeTool(output)
			tool.state = "ok"
			if isFailure(output) {
				tool.state = "fail"
			}
			return
		}
	}
}

func (m *Model) appendToolOutput(name, chunk string) {
	for index := len(m.items) - 1; index >= 0; index-- {
		tool := m.items[index].tool
		if tool == nil || tool.name != name || tool.state != "running" {
			continue
		}
		tool.output += chunk
		if len(tool.output) > 8_000 {
			tool.output = "… live output truncated …\n" + tool.output[len(tool.output)-7_000:]
		}
		tool.summary = summarizeTool(tool.output)
		return
	}
}

func (m *Model) applyAgent(event contract.AgentEvent) {
	if event.Kind == "start" {
		agent := &agentView{id: event.RunID, agent: event.Agent, title: event.Title, task: event.Task, model: event.Model, status: "running", index: len(m.agents) + 1}
		m.agents = append(m.agents, agent)
		m.agentByID[event.RunID] = agent
		m.items = append(m.items, item{kind: "agent", agentID: event.RunID})
		return
	}
	agent := m.agentByID[event.RunID]
	if agent == nil {
		return
	}
	switch event.Kind {
	case "tool_start":
		agent.active = toolLabel(event.Tool)
		agent.items = append(agent.items, item{kind: "tool", tool: &toolView{name: event.Tool, target: toolTarget(event.Tool, jsonRaw(event.Arguments)), state: "running", started: time.Now()}})
	case "tool_end":
		agent.active = ""
		for i := len(agent.items) - 1; i >= 0; i-- {
			if agent.items[i].tool != nil && agent.items[i].tool.state == "running" {
				agent.items[i].tool.output, agent.items[i].tool.summary, agent.items[i].tool.state = event.Output, summarizeTool(event.Output), "ok"
				if isFailure(event.Output) {
					agent.items[i].tool.state = "fail"
				}
				break
			}
		}
	case "text":
		agent.items = append(agent.items, item{kind: "assistant", content: event.Content})
	case "usage":
		agent.usage = event.Usage
	case "done":
		agent.status, agent.usage = event.Status, event.Usage
		if event.Report != "" {
			agent.items = append(agent.items, item{kind: "assistant", content: event.Report, title: "report"})
		}
	}
}

func jsonRaw(value string) []byte { return []byte(value) }
