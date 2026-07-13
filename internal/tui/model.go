package tui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"golang.org/x/text/unicode/norm"
)

type Runtime struct {
	Engine   *orchestrator.Engine
	Session  contract.Session
	Settings *contract.Settings
}

type Skill struct {
	Name, Description, Instructions, Path string
}

// UsageData is the account usage shown by /usage (003 US5), mapped from the
// gateway GET /v1/usage response. Credits are in credits (1 credit = $0.01);
// window/spend figures are USD.
type UsageData struct {
	PlanName       string
	Windows        []UsageWindow
	ExtraTotal     float64
	ExtraRemaining float64
	SpendTodayUSD  float64
}

type UsageWindow struct {
	Name            string
	BudgetUSD       float64
	CurrentSpentUSD float64
	ResetTime       string
	DurationSeconds int
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
	// LoadSkill loads one skill's instruction body on demand (003 T036), so the
	// /skills modal opens without reading every skill file.
	LoadSkill func(context.Context, string) (string, error)
	// FetchUsage retrieves account usage from the gateway with the stored key
	// (003 US5). IsLoggedIn reports whether an API key is present, driving the
	// /login / /usage / /logout command visibility.
	FetchUsage func(context.Context) (*UsageData, error)
	IsLoggedIn func() bool
	// DiscoverModels re-queries the gateway's model catalog, merges it into
	// settings, and returns the models now available. No restart needed.
	DiscoverModels func(context.Context) ([]contract.Model, error)
	// TranscriptPage (005 US1 T020) loads a keyset page of the durable transcript
	// from SQLite for on-demand scroll-back. Nil ⇒ paging is disabled and the TUI
	// shows only the recent window it was given.
	TranscriptPage func(context.Context, contract.TranscriptPageRequest) (contract.TranscriptPage, error)
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
	// Hydrate (US1 T021), when set and Runtime.Engine is nil, enables two-stage
	// launch: the composer appears immediately and this builds the runtime in the
	// background, returning it once ready. When nil, launch is single-stage and
	// Runtime is used as-is (unchanged behavior).
	Hydrate func(context.Context) (HydratedRuntime, error)
}

// HydratedRuntime is the result of the deferred runtime build (T021): everything
// the model needs to switch from the loading shell to the live session.
type HydratedRuntime struct {
	Runtime Runtime
	Actions Actions
	Recent  []contract.Event
	Notice  string
}

// hydratedMsg delivers the deferred runtime (or an error) to the loading model.
type hydratedMsg struct {
	result HydratedRuntime
	err    error
}

// transcriptPageMsg delivers a keyset page (US1 T020). initial replaces the window
// on first load; otherwise the older entries are prepended.
type transcriptPageMsg struct {
	page    contract.TranscriptPage
	initial bool
	err     error
}

type item struct {
	kind, content, title string
	tool                 *toolView
	agentID              string
	stats                *contract.TaskStats
	// eventID is the stable SQLite id for a durable transcript item (US1 T020
	// paging); 0 for live/synthetic items. The oldest loaded item's eventID is the
	// keyset cursor for loading the previous page.
	eventID int64
	// US1 render cache: the memoized rendered block for the text kinds
	// (user/assistant/assistant_draft/system), valid while width, content length,
	// and title are unchanged. Transcript items are never edited in place except
	// the streaming draft (whose length grows), so this is collision-free and
	// makes a transcript re-render O(changed items) instead of O(all items). The
	// palette is startup-constant, so it need not enter the key.
	cachedBlock            string
	cachedWidth, cachedLen int
	cachedTitle            string
}

type toolView struct {
	name, target, output, summary string
	state                         string
	started                       time.Time
	// expanded is this tool's own detail toggle (US4 tool chip): clicking the tool
	// row opens just that tool's detail block. hovered marks the row currently under
	// the mouse so View() can render a clickable affordance.
	expanded bool
	hovered  bool
}

// chipSpan records where a clickable transcript chip's header sits in the full
// (pre-scroll) transcript content, so View() can project it to a screen rectangle
// using the live viewport offset (US4 tool/subagent chips).
type chipSpan struct {
	row     int
	tool    *toolView
	agentID string
}

type agentView struct {
	id, agent, title, task, model, status string
	index                                 int
	usage                                 contract.Usage
	items                                 []item
	active                                string
	hovered                               bool // mouse is over this chip (clickable affordance)
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
	ctx       context.Context
	runtime   Runtime
	bridge    *Bridge
	actions   Actions
	version   string
	theme     Theme
	palette   palette
	glyphs    glyphs
	viewport  viewport.Model
	input     Composer
	width     int
	height    int
	items     []item
	busy      bool
	status    string
	started   time.Time
	usage     contract.Usage
	context   contract.ContextInfo
	plan      contract.Plan
	reasoning string // live 300-rune tail shown collapsed
	frame     int
	// transcriptRenders counts full transcript re-renders; the US1/US2 hot-path
	// guard asserts it does not advance on keystrokes or idle ticks.
	transcriptRenders int
	// US1 T019: draft accumulates the active assistant stream in O(1) amortized
	// (strings.Builder) instead of O(n²) content concatenation; activeTools gives
	// O(1) active-tool lookup instead of a linear item scan per chunk. activeTools
	// is an optimization only — finishTool/appendToolOutput fall back to a linear
	// scan if a lookup misses, so a stale map can never lose correctness.
	draft       strings.Builder
	activeTools map[string]*toolView
	// US5: a large paste is stashed here and shown in the input as a compact
	// [#pasteN NN lines] placeholder the user can type around; expandPastes
	// reconstructs the exact raw bytes at submit so nothing is lost or truncated.
	pastes         map[int]string
	pasteSeq       int
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
	// T1: persistent usage footer. Set when statsMsg arrives, cleared on
	// next submit() or session switch. NEVER cleared by a timer — the
	// "footer must remain visible" requirement.
	lastStats *contract.TaskStats
	// T2: thinking summary baseline. Set when the first reasoning token
	// arrives in a stream; cleared at submit. Drives the post-task
	// "thought for Ns" line.
	thoughtStart        time.Time
	lastThoughtDuration time.Duration
	// M2: track that the live MCP modal is open so the tick handler keeps
	// refreshing it until every server reports a terminal state.
	mcpModalAtRest bool
	mcpRefreshIn   bool
	// mcpServers is the server list backing the currently-open MCP modal, refreshed
	// in place on every list tick. The modal's select closure reads THIS (not a
	// captured snapshot) so a row still maps to the right server after a live
	// refresh changes the server set (M4).
	mcpServers []MCPServerInfo
	// US2 T030: the animation tick is lifecycle-scoped, not permanent. It runs
	// only while something needs it (a spinner, a notice pending expiry, or the
	// live MCP modal) and self-terminates when idle, so an idle session schedules
	// no recurring application work.
	ticking bool
	// US4 mouse: hits is the InteractionMap for the last rendered frame, rebuilt
	// by View() so click/hover coordinates resolve against exactly what is on
	// screen. Its generation comes from FrameState (advanced every frame) so a
	// click can be reasoned about relative to the frame it was issued against.
	hits *interactionMap
	// modalRows records, for the modal currently being rendered, which line index
	// inside the dialog each visible choice occupies. View() turns these into
	// absolute screen rectangles once it knows the centered dialog origin.
	modalRows []modalRowSpan
	// transcriptChips records the header row of every clickable tool/agent chip in
	// the last-rendered transcript (content-row coordinates). View() projects them
	// to screen rectangles with the live viewport offset.
	transcriptChips []chipSpan
	// needsTranscriptRefresh is set by a mouse action that changed transcript
	// content (a tool expand/collapse or opening an agent view) so the Update loop
	// re-renders the transcript this cycle.
	needsTranscriptRefresh bool
	// hoveredTool/hoveredAgent track the transcript chip currently under the mouse
	// so View() can render a clickable affordance (a hover underline). Only one row
	// is hovered at a time; setHover swaps them and asks for a transcript refresh.
	hoveredTool  *toolView
	hoveredAgent string
	// followOutput (US2 T033) is the auto-scroll intent: true means new content
	// pins to the bottom. Scrolling up (even mid-stream) clears it so the view
	// STAYS where the user put it; scrolling back to the bottom or submitting sets
	// it again. This replaces the old "always follow while busy" rule that yanked
	// the reader back down during streaming.
	followOutput bool
	// sel (US4 T058) is the in-app transcript text selection. It is highlighted at
	// View() time over the visible lines only (cheap) and copied via OSC52 on
	// Ctrl+C. transcriptContent is the exact styled string last set on the viewport,
	// kept so a copy can extract the ANSI-free plain text of the selected range.
	sel               textSelection
	transcriptContent string
	// US1/US2 T006: the resolved pane geometry and per-frame dirty-pane state.
	layoutSnapshot LayoutSnapshot
	frameState     FrameState
	// US1 T021 two-stage launch: while loading is true the runtime is still
	// hydrating in the background; the composer is live but submit is deferred.
	// hydrate builds the runtime; pendingSubmit holds a prompt entered (or the
	// initial prompt) during loading, sent once the runtime is ready.
	loading       bool
	hydrate       func(context.Context) (HydratedRuntime, error)
	pendingSubmit string
	// US1 T020/T023 scroll-back paging: the TUI holds a bounded window of the
	// durable transcript and loads older pages from SQLite on demand. oldestEventID
	// is the keyset cursor; hasOlderEvents gates the load; loadingOlder prevents
	// duplicate in-flight loads; pageGen ties a page result to the current session so
	// a stale result (after a session switch) is discarded.
	oldestEventID  int64
	hasOlderEvents bool
	loadingOlder   bool
	pageGen        uint64
	initialPaged   bool
	pendingPrepend pagePrepend
}

// pagePrepend captures the transcript height and scroll offset just before an
// older page is prepended, so the viewport can be re-anchored to the same content
// after the re-render (US1 T023 anchor compensation).
type pagePrepend struct {
	active               bool
	beforeLines, beforeY int
}

// textSelection is a transcript selection in content coordinates (full-transcript
// rows/columns, independent of scroll). dragging is true between press and release.
type textSelection struct {
	active               bool
	dragging             bool
	anchorRow, anchorCol int
	endRow, endCol       int
}

// ordered returns the selection bounds normalized so (r0,c0) precedes (r1,c1).
func (s textSelection) ordered() (r0, c0, r1, c1 int) {
	r0, c0, r1, c1 = s.anchorRow, s.anchorCol, s.endRow, s.endCol
	if r1 < r0 || (r1 == r0 && c1 < c0) {
		r0, c0, r1, c1 = r1, c1, r0, c0
	}
	return
}

// modalRowSpan maps a modal choice index to the dialog-relative screen row it
// renders at (row 0 is the dialog's top border), so View() can offset it by the
// centered dialog origin to get an absolute click region.
type modalRowSpan struct{ choice, row int }

// needsAnimation reports whether the 100ms tick has any work to do: a live
// spinner (busy), a notice awaiting expiry, or the live MCP modal.
func (m *Model) needsAnimation() bool {
	return m.loading || m.busy || (m.flash.text != "" && !m.flash.expires.IsZero()) || m.mcpModalAtRest
}

// ensureTick starts the animation tick if it is needed and not already running.
// Called at the end of every Update so any transition into an animated state
// (task start, a new expiring notice, the MCP modal opening) restarts it.
func (m *Model) ensureTick() tea.Cmd {
	if m.ticking || !m.needsAnimation() {
		return nil
	}
	m.ticking = true
	return tick()
}

var commands = []commandEntry{
	{"/reasoning", "Set reasoning effort (low–max)"}, {"/goal", "Set an autonomous goal"}, {"/plan", "Plan before editing"},
	{"/resume", "Resume a workspace session"}, {"/new", "Start a new session"},
	{"/context", "Inspect context usage"}, {"/compact", "Compact conversation"},
	{"/model", "Choose or refresh models"}, {"/login", "Store API key"}, {"/logout", "Clear API key"},
	{"/usage", "View account usage"},
	{"/permissions", "Change permission mode"}, {"/skills", "Assign skills to next prompt"}, {"/mcp", "Manage MCP servers"},
	{"/paste", "Inspect or remove pasted blocks"},
	{"/diff", "Summarize git diff"}, {"/rewind", "Restore latest checkpoint"},
}

func NewModel(options Options) *Model {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	input := newComposer()
	input.Placeholder = "Type a request · / for commands"
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 32_000
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = 5
	input.SetVirtualCursor(true)
	theme := newTheme(options.Runtime.Settings)
	colors := theme.Palette
	marks := theme.Glyphs
	// The textarea's base style set must match the resolved theme — feeding the
	// dark defaults to a light terminal leaves near-invisible sub-styles (line
	// numbers, end-of-buffer) that the palette overrides below don't reach.
	styles := textarea.DefaultDarkStyles()
	if !resolvePaletteDark(options.Runtime.Settings.Theme) {
		styles = textarea.DefaultLightStyles()
	}
	styles.Focused.Text = colors.text
	styles.Focused.Placeholder = colors.faint
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Prompt = colors.brand
	styles.Blurred.Text = colors.text
	styles.Blurred.Placeholder = colors.faint
	styles.Cursor.Color = colors.brand.GetForeground()
	input.SetStyles(styles)
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	view.SoftWrap = false
	view.FillHeight = true
	m := &Model{ctx: ctx, runtime: options.Runtime, bridge: options.Bridge, actions: options.Actions, version: options.Version, theme: theme, palette: colors, glyphs: marks, viewport: view, input: input, width: 100, height: 34, status: "Ready", agentByID: make(map[string]*agentView), activeTools: make(map[string]*toolView), selectedSkills: make(map[string]Skill), historyIndex: -1, initialPrompt: options.InitialPrompt, followOutput: true}
	// US1 T021: when the runtime is not yet built and a Hydrate func is provided,
	// start in the loading shell; the engine-dependent state is populated once
	// hydration completes. Otherwise this is the unchanged single-stage path.
	if options.Runtime.Engine == nil && options.Hydrate != nil {
		m.loading = true
		m.hydrate = options.Hydrate
		m.status = "Loading workspace…"
		m.pendingSubmit = strings.TrimSpace(options.InitialPrompt)
	} else {
		m.plan = options.Runtime.Engine.CurrentPlan()
		report := options.Runtime.Engine.ContextReport()
		m.context = contract.ContextInfo{HistoryTokens: report.HistoryTokens, ContextLimit: report.ContextLimit, Percent: report.Percent}
		m.loadEvents(options.Recent)
	}
	if strings.TrimSpace(options.Notice) != "" {
		m.flash = noticeState{text: options.Notice, level: "warn"}
	}
	m.refreshViewport(true)
	return m
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.input.Focus()}
	// US2 T030: start the tick only if something already needs animating (e.g. an
	// initial expiring notice). An initial prompt starts a task, whose submit sets
	// busy and restarts the tick via ensureTick at the end of that Update.
	if m.needsAnimation() {
		m.ticking = true
		commands = append(commands, tick())
	}
	if m.loading {
		// US1 T021: build the runtime in the background; the initial prompt (if any)
		// is held in pendingSubmit and sent once hydration completes.
		commands = append(commands, m.hydrateCmd())
		return tea.Batch(commands...)
	}
	if strings.TrimSpace(m.initialPrompt) != "" {
		prompt := m.initialPrompt
		commands = append(commands, func() tea.Msg { return initialMsg(prompt) })
	}
	// US1 T020: establish the paging cursor + has-older flag for the loaded session.
	if cmd := m.loadInitialPageCmd(); cmd != nil {
		commands = append(commands, cmd)
	}
	return tea.Batch(commands...)
}

// handleLoadingKey handles input during two-stage launch (T021): the composer is
// live so the user can type ahead, but submit is deferred — Enter stashes the
// prompt to send once the runtime is ready. No engine-dependent key is honored.
func (m *Model) handleLoadingKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "ctrl+c", "ctrl+d":
		return tea.Quit
	case "enter":
		if value := strings.TrimSpace(m.input.Value()); value != "" {
			m.pendingSubmit = value
			m.input.Reset()
			m.notify("Will send once the workspace finishes loading…")
		}
		return nil
	case "esc":
		m.input.Reset()
		return nil
	default:
		updated, cmd := m.input.Update(key)
		m.input = updated
		return cmd
	}
}

// hydrateCmd runs the deferred runtime build off the UI goroutine (T021).
func (m *Model) hydrateCmd() tea.Cmd {
	hydrate, ctx := m.hydrate, m.ctx
	return func() tea.Msg {
		result, err := hydrate(ctx)
		return hydratedMsg{result: result, err: err}
	}
}

// applyHydration swaps the loading shell for the live session once the runtime is
// ready (T021): it adopts the runtime/actions, loads the recent transcript, seeds
// the context readout, and sends any prompt entered during loading.
func (m *Model) applyHydration(msg hydratedMsg) tea.Cmd {
	m.loading = false
	if msg.err != nil {
		m.status = "Ready"
		m.warn("Failed to load the workspace: " + msg.err.Error())
		return nil
	}
	m.runtime = msg.result.Runtime
	if msg.result.Actions.SaveSettings != nil || msg.result.Actions.NewSession != nil {
		m.actions = msg.result.Actions
	}
	m.status = "Ready"
	m.loadEvents(msg.result.Recent)
	if m.runtime.Engine != nil {
		m.plan = m.runtime.Engine.CurrentPlan()
		report := m.runtime.Engine.ContextReport()
		m.context = contract.ContextInfo{HistoryTokens: report.HistoryTokens, ContextLimit: report.ContextLimit, Percent: report.Percent}
	}
	if strings.TrimSpace(msg.result.Notice) != "" {
		m.warn(msg.result.Notice)
	}
	m.refreshViewport(true)
	var cmds []tea.Cmd
	if cmd := m.loadInitialPageCmd(); cmd != nil { // US1 T020: establish the paging cursor
		cmds = append(cmds, cmd)
	}
	if prompt := strings.TrimSpace(m.pendingSubmit); prompt != "" {
		m.pendingSubmit = ""
		// Route through the same resolution as Enter so a slash command typed during
		// loading runs as a command rather than being sent as a text prompt.
		if strings.HasPrefix(prompt, "/") {
			cmds = append(cmds, m.runSlash(m.resolveSlash(prompt)))
		} else {
			cmds = append(cmds, m.submit(prompt))
		}
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(value time.Time) tea.Msg { return tickMsg(value) })
}

// mcpRefreshTick (M2) is the slower cadence that drives the live MCP modal
// re-render. 500ms is fast enough that a slow handshake visually progresses
// and slow enough that we never thrash the manager.
func mcpRefreshTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return mcpRefreshTickMsg{} })
}

// ptrStats keeps a defensive copy of a TaskStats so the footer is decoupled
// from the source message value (which the bridge discards after delivery).
func ptrStats(value contract.TaskStats) *contract.TaskStats {
	snapshot := value
	return &snapshot
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	wasBottom := m.viewport.AtBottom()
	beforeOffset := m.viewport.YOffset()
	beforeItems := len(m.items)
	beforeWidth := m.width
	dirty := false
	var commandsOut []tea.Cmd
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(40, value.Width), max(14, value.Height)
	case tickMsg:
		m.frame++
		m.expireNotice()
		// US2 T030: reschedule ONLY while animation is still needed; otherwise stop
		// so an idle session issues no further ticks. A later transition into an
		// animated state restarts it via ensureTick at the end of Update.
		if m.needsAnimation() {
			commandsOut = append(commandsOut, tick())
		} else {
			m.ticking = false
		}
		// M2: drive the live MCP modal. Schedule a re-list every 500ms
		// until the modal closes or every server reaches a terminal state
		// (`connected` / `error` / `auth_required` / `disabled`).
		if m.mcpModalAtRest && !m.mcpRefreshIn {
			// D4/T035: claim the refresh slot at SCHEDULE time, not when the tick
			// fires. Otherwise the 100ms base tick queues ~5 overlapping 500ms
			// one-shots before the first clears the flag (a bunched List burst).
			m.mcpRefreshIn = true
			commandsOut = append(commandsOut, mcpRefreshTick())
		}
	case initialMsg:
		commandsOut = append(commandsOut, m.submit(string(value)))
	case hydratedMsg:
		// US1 T021: runtime finished building in the background — switch to the live
		// session and send any prompt entered while loading.
		if cmd := m.applyHydration(value); cmd != nil {
			commandsOut = append(commandsOut, cmd)
		}
		dirty = true
	case transcriptPageMsg:
		// US1 T020: a keyset page arrived — replace the initial window or prepend an
		// older page (with anchor compensation applied after the re-render).
		m.applyPage(value)
		dirty = true
	case statusMsg:
		m.status = simplifyStatus(string(value))
	case noticeMsg:
		m.notify(string(value))
	case streamMsg:
		m.appendStream(value)
	case toolStartMsg:
		tv := &toolView{name: value.name, target: toolTarget(value.name, value.input), state: "running", started: time.Now()}
		m.items = append(m.items, item{kind: "tool", tool: tv})
		m.activeTools[value.name] = tv // US1 T019: O(1) lookup for output/finish
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
		// T1: snapshot on statsMsg. The persistent per-task summary is appended
		// to the transcript on the resultMsg that follows (taskSummaryLine);
		// it is never cleared by a timer — the "footer must remain visible"
		// requirement.
		m.lastStats = ptrStats(contract.TaskStats(value))
		// 004 US1 (T013): the task has ended — no tool or subagent may keep
		// animating. Reconcile any still-"running" work item to a terminal state.
		m.sweepRunningWork()
		// P2: a plan-ready task opens the Proceed now / Proceed later / Keep
		// planning modal. The engine leaves plan mode ON in interactive runs
		// so the modal choices can drive it; the resultMsg that follows clears
		// busy so the modal is immediately operable.
		if value.PlanReady {
			m.openPlanReadyModal()
		}
		// H5: surface a force-finalization reason (token breaker / failure
		// terminator) as a warn notice so the user knows why the task stopped
		// short of the turn ceiling.
		if value.TerminatedReason != "" {
			m.warn("Task terminated: " + value.TerminatedReason)
		}
	case mcpStatusMsg:
		m.warn("MCP · " + string(value))
	case modalRequest:
		m.enqueueModal(value)
	case mcpRefreshTickMsg:
		// M2: kicked in tickMsg (which already claimed the refresh slot at
		// schedule time, D4). We re-list and re-extend the modal in place. If the
		// modal closed or reached a terminal state meanwhile, release the slot.
		if m.actions.MCP.List == nil || m.modal == nil || !m.mcpModalAtRest {
			m.mcpRefreshIn = false
			break
		}
		commandsOut = append(commandsOut, actionCommand("mcp-list", func() (any, error) { return m.actions.MCP.List(m.ctx) }))
	case resultMsg:
		m.busy = false
		m.status = "Ready"
		// 003 (T019): thinking is ephemeral — clear the live reasoning buffer on
		// completion; no "thought for Ns" residue is kept.
		m.reasoning = ""
		if value.err != nil {
			m.warn("Task stopped: " + value.err.Error())
		} else {
			m.finishAssistant(value.answer)
		}
		// 003 (T020/FR-013): append the three-metric task summary as a transcript
		// entry after the latest message, using the stats from the preceding
		// statsMsg. Interrupted tasks still get a summary (marked).
		if m.lastStats != nil {
			m.items = append(m.items, item{kind: "summary", stats: m.lastStats})
		}
	case actionMsg:
		commandsOut = append(commandsOut, m.handleAction(value))
	case tea.KeyPressMsg:
		command := m.handleKey(value)
		if command != nil {
			commandsOut = append(commandsOut, command)
		}
	case tea.MouseWheelMsg:
		// US4: route the wheel to whatever scrollable surface is under the pointer —
		// the command menu, an overflowing modal, or the transcript viewport.
		m.handleMouseWheel(value)
	case tea.MouseClickMsg:
		// US4: a left click dispatches to the InteractionMap target under the pointer
		// (command row, modal choice, or the composer) via the same path the keyboard
		// uses. Non-left buttons and empty regions are ignored.
		if cmd := m.handleMouseClick(value); cmd != nil {
			commandsOut = append(commandsOut, cmd)
		}
	case tea.MouseMotionMsg:
		// US4: a left-drag extends a transcript text selection; an unpressed move
		// hovers (highlights the command/modal row under the pointer). Both only touch
		// cheap chrome or the View()-time selection overlay, never the cached transcript.
		m.handleMouseMotion(value)
	case tea.MouseReleaseMsg:
		// US4 T058: finalize a drag selection, or focus the composer on a plain click.
		if cmd := m.handleMouseRelease(value); cmd != nil {
			commandsOut = append(commandsOut, cmd)
		}
	case tea.PasteMsg:
		// Bracketed paste arrives as its own message type; route it to
		// whichever text field has focus or clipboard paste does nothing.
		if m.modal != nil && m.modal.input {
			m.modal.value += value.Content
		} else if m.modal == nil {
			paste := value
			// US5: a large paste is stashed and replaced with a compact one-line
			// placeholder so it never floods the input; small pastes insert inline.
			if isLargePaste(value.Content) {
				if m.pastes == nil {
					m.pastes = make(map[int]string)
				}
				m.pasteSeq++
				m.pastes[m.pasteSeq] = value.Content
				paste = tea.PasteMsg{Content: fmt.Sprintf("[#paste%d %d lines]", m.pasteSeq, logicalLineCount(value.Content))}
			}
			var command tea.Cmd
			m.input, command = m.input.Update(paste)
			if command != nil {
				commandsOut = append(commandsOut, command)
			}
		}
	}
	// Recompute the layout every cycle so the transcript viewport shrinks to
	// make room for the plan, command palette, and notice line instead of
	// overflowing the screen.
	m.layout()
	// US1/US2: renderTranscript is O(all items) and is the dominant per-frame
	// cost, so re-render the transcript ONLY when it actually changed — a content-
	// mutating message, an item added (submit / action reload), or a width change
	// that forces a re-wrap. Keystrokes, the 100ms tick, status/usage/context/plan
	// updates, and modal typing all echo through View()'s chrome (renderInput,
	// header, activity) without re-rendering the transcript, so typing stays
	// instant regardless of transcript length. Presentation-only: the provider
	// request path (and therefore the prefix cache) is untouched.
	switch message.(type) {
	case streamMsg, toolStartMsg, toolEndMsg, toolOutputMsg, agentMsg, resultMsg, statsMsg, initialMsg, actionMsg:
		dirty = true
	}
	if len(m.items) != beforeItems || m.width != beforeWidth {
		dirty = true
	}
	// US4: a mouse action that expanded/collapsed a tool or opened an agent view
	// changed transcript content — re-render it this cycle.
	if m.needsTranscriptRefresh {
		dirty = true
		m.needsTranscriptRefresh = false
	}
	// US1/M1: bound the in-memory transcript when new items arrive so a marathon
	// session cannot grow render cost or memory without limit. Only runs on a
	// structural add (not on every streamed token) AND only while following the
	// bottom — never trim the front while the user is browsing older history they
	// paged in (T020), or the just-loaded page would be evicted immediately. The
	// full transcript always stays in the session DB.
	if len(m.items) > beforeItems && m.followOutput {
		m.trimTranscript()
	}
	// US2 T033: a user scroll this cycle updates the follow intent — following iff
	// they landed back at the bottom. So a scroll-up (even mid-stream) sticks and
	// scrolling back to the bottom resumes auto-follow. New content pins to the
	// bottom only while following, so streaming never yanks a reader who scrolled up.
	scrolled := m.viewport.YOffset() != beforeOffset
	if scrolled {
		m.followOutput = m.viewport.AtBottom()
	}
	forceBottom := m.followOutput
	if dirty {
		// New content re-renders and pins to the bottom only while following, so a
		// reader who scrolled up mid-stream is never yanked down.
		m.frameState.markDirty() // US2 T032: record that the transcript pane changed
		m.refreshViewport(forceBottom)
	} else if forceBottom && wasBottom {
		// Content unchanged but the layout may have shifted (input grew): keep the
		// bottom pinned only when we were already there — never pull a scrolled-up
		// reader down. Cheap: no SetContent.
		m.viewport.GotoBottom()
	}
	// US1 T023: after an older page was prepended and re-rendered, re-anchor the
	// viewport to the same content by shifting the offset by the rows added on top.
	if m.pendingPrepend.active {
		added := lineCount(m.transcriptContent) - m.pendingPrepend.beforeLines
		m.viewport.SetYOffset(max(0, m.pendingPrepend.beforeY+added))
		m.pendingPrepend.active = false
	}
	// US1 T020: near the top with older history unloaded — fetch the previous page.
	if cmd := m.maybeLoadOlder(); cmd != nil {
		commandsOut = append(commandsOut, cmd)
	}
	m.frameState.clear() // reset per-frame dirty flags and advance the frame generation
	// US2 T030: restart the lifecycle tick if this update transitioned into an
	// animated state (task started, notice raised, MCP modal opened).
	if cmd := m.ensureTick(); cmd != nil {
		commandsOut = append(commandsOut, cmd)
	}
	return m, tea.Batch(commandsOut...)
}

// pastePlaceholderRE matches the compact stand-in inserted for a stashed paste.
var pastePlaceholderRE = regexp.MustCompile(`\[#paste(\d+) \d+ lines\]`)

// isLargePaste classifies a paste as large (US5 / input-draft contract §2): 1024+
// code points OR 6+ logical lines.
func isLargePaste(content string) bool {
	return utf8.RuneCountInString(content) >= 1024 || logicalLineCount(content) >= 6
}

// logicalLineCount counts logical lines: CRLF is one break, a lone CR or LF is
// one break, and non-empty content with no break is one line.
func logicalLineCount(content string) int {
	if content == "" {
		return 0
	}
	lines := 1
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\n':
			lines++
		case '\r':
			lines++
			if i+1 < len(content) && content[i+1] == '\n' {
				i++ // CRLF counts as a single break
			}
		}
	}
	return lines
}

// expandPastes reconstructs the exact raw content of every stashed paste in place
// of its placeholder, so the sent message equals the concatenation of typed text
// and raw paste bytes. An unknown/edited placeholder is left as literal text.
func (m *Model) expandPastes(text string) string {
	if len(m.pastes) == 0 {
		return text
	}
	return pastePlaceholderRE.ReplaceAllStringFunc(text, func(match string) string {
		sub := pastePlaceholderRE.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		id, _ := strconv.Atoi(sub[1])
		if raw, ok := m.pastes[id]; ok {
			return raw
		}
		return match
	})
}

func (m *Model) releasePastes() {
	m.pastes = nil
	m.pasteSeq = 0
}

// Transcript render-window budgets (M1). These bound the in-memory items the TUI
// re-renders and joins each frame; the durable transcript in the session DB is
// never touched. The bounds are deliberately generous so a typical session never
// trims — only marathon sessions with thousands of turns/tools hit them.
const (
	maxTranscriptItems = 1500
	maxTranscriptBytes = 3 << 20 // ~3 MiB of item + tool-output content
)

// itemBytes is the cheap size measure used for the byte budget: the visible
// content plus any tool output (which the collapsed view hides but the render
// still carries).
func itemBytes(it *item) int {
	n := len(it.content)
	if it.tool != nil {
		n += len(it.tool.output)
	}
	return n
}

// trimTranscript bounds the retained transcript to the render-window budgets by
// evicting the oldest items, leaving a single leading marker so the trim is
// visible rather than silent. It is a no-op until a session actually exceeds the
// budgets. Front eviction is render-safe: items are indexed only positionally
// during rendering, while tool/agent lookups use maps keyed by name/ID.
func (m *Model) trimTranscript() {
	total := 0
	for i := range m.items {
		total += itemBytes(&m.items[i])
	}
	if len(m.items) <= maxTranscriptItems && total <= maxTranscriptBytes {
		return
	}
	drop := 0
	for drop < len(m.items) && (len(m.items)-drop > maxTranscriptItems || total > maxTranscriptBytes) {
		total -= itemBytes(&m.items[drop])
		drop++
	}
	if drop <= 0 {
		return
	}
	marker := item{kind: "system", content: "⋯ earlier messages trimmed to keep the interface responsive — the full transcript is saved in this session's log ⋯"}
	trimmed := make([]item, 0, len(m.items)-drop+1)
	trimmed = append(trimmed, marker)
	trimmed = append(trimmed, m.items[drop:]...)
	m.items = trimmed
	// US4 T058: front-eviction shifts content rows, so any selection is now stale.
	m.clearSelection()
}

func (m *Model) expireNotice() {
	if m.flash.text != "" && !m.flash.expires.IsZero() && time.Now().After(m.flash.expires) {
		m.flash = noticeState{}
	}
}

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
	// T2: a new prompt clears the "thought for Ns" line so the summary
	// from the previous run cannot leak across to the new one.
	m.lastThoughtDuration = 0
	m.thoughtStart = time.Time{}
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
		// T2: rune-safe truncation so the 300-cap cannot split a multi-byte
		// glyph mid-sequence (Arabic, emoji, CJK). thoughtStart marks the
		// first reasoning-token arrival for the post-task "thought for Ns"
		// summary.
		if m.thoughtStart.IsZero() {
			m.thoughtStart = time.Now()
		}
		m.reasoning += stream.reasoning
		const maxRunes = 300
		if runes := []rune(m.reasoning); len(runes) > maxRunes {
			m.reasoning = string(runes[len(runes)-maxRunes:])
		}
	}
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

// sweepRunningWork (004 US1, T013) is the task-end safety net: any tool or
// subagent still marked "running" when the task's stats arrive was cut short
// (user stop, error, or a crash mid-tool) and is reconciled to the terminal
// "cancelled" state so nothing keeps spinning in the transcript. Idempotent —
// items already ok/fail are left untouched.
func (m *Model) sweepRunningWork() {
	// US1 T019: the task ended, so no tool is active anymore.
	clear(m.activeTools)
	for _, entry := range m.items {
		if entry.tool != nil && entry.tool.state == "running" {
			entry.tool.state = "cancelled"
		}
	}
	for _, agent := range m.agents {
		if agent.status == "running" {
			agent.status = "cancelled"
		}
		for i := range agent.items {
			if agent.items[i].tool != nil && agent.items[i].tool.state == "running" {
				agent.items[i].tool.state = "cancelled"
			}
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
	if tool := m.activeTools[name]; tool != nil && tool.state == "running" {
		finalizeTool(tool, output)
		delete(m.activeTools, name)
		return
	}
	// Fallback: linear scan handles any missed registration (correctness first).
	for index := len(m.items) - 1; index >= 0; index-- {
		tool := m.items[index].tool
		if tool != nil && tool.name == name && tool.state == "running" {
			finalizeTool(tool, output)
			delete(m.activeTools, name)
			return
		}
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
