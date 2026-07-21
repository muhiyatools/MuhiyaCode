package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/app"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// The frontend-service types live in internal/app so a second frontend (the
// planned desktop app) can be written against the same core without importing
// this terminal UI. They are aliases, not copies: tui.Runtime IS app.Runtime,
// so internal/command satisfies both names with one value.
type Runtime = app.Runtime

type Skill = app.Skill

// UsageData is the account usage shown by /usage (003 US5), mapped from the
// gateway GET /v1/usage response. Credits are in credits (1 credit = $0.01);
// window/spend figures are USD.
type UsageData = app.UsageData

type UsageWindow = app.UsageWindow

type Actions = app.Actions

type Options struct {
	Runtime Runtime
	Bridge  *Bridge
	Actions Actions
	Version string
	// LatestVersion is the newest published version, when a background check
	// found one. Empty means unknown or up to date — the header shows nothing.
	LatestVersion string
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
type HydratedRuntime = app.HydratedRuntime

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
// using the live viewport offset (US4 tool chips).
type chipSpan struct {
	row  int
	tool *toolView
}

// agentView was one subagent's card: its identity, status, tokens, and its own
// tool/text rows, rendered as a transcript chip that could be opened into a
// full-screen view. It is gone with the subagent system.

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
	ctx     context.Context
	runtime Runtime
	bridge  *Bridge
	actions Actions
	version string
	// latestVersion is the newest published version when an update is available;
	// empty renders nothing.
	latestVersion string
	theme         Theme
	palette       palette
	glyphs        glyphs
	viewport      viewport.Model
	input         Composer
	width         int
	height        int
	items         []item
	busy          bool
	status        string
	started       time.Time
	usage         contract.Usage
	context       contract.ContextInfo
	plan          contract.Plan
	frame         int
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
	pastes     map[int]string
	pasteSeq   int
	modal      *modalState
	modalQueue []modalRequest
	// onboardingPending records that the first-run sign-in modal wants to open but
	// another modal (e.g. the workspace-trust reply prompt) is currently showing.
	// closeModal reopens it once the queue drains, so onboarding is deferred rather
	// than silently dropped by openChoice's reply-modal guard.
	onboardingPending bool
	selectedSkills    map[string]Skill
	commandIndex      int
	history           []string
	historyIndex      int
	initialPrompt     string
	flash             noticeState
	// lastStats carries the just-finished task's stats so update() can seed ONE
	// end-of-task summary transcript item. It is not a live footer — no render
	// path reads it (H-5); it is set when statsMsg arrives and cleared on the next
	// submit() or session switch.
	lastStats *contract.TaskStats
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
	// content (a tool expand/collapse) so the Update loop re-renders the
	// transcript this cycle.
	needsTranscriptRefresh bool
	// hoveredTool tracks the transcript chip currently under the mouse so View()
	// can render a clickable affordance (a hover underline). setHover swaps it
	// and asks for a transcript refresh.
	hoveredTool *toolView
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
	// chrome memoizes the header/activity/to-do/mode-line strings for one frame so
	// layout() and View() render them at most once per Update+View cycle (Experience
	// Overhaul A5). Invalidated at the top of every Update.
	chrome chromeCache
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

// commands is the palette. /errors and /permissions were removed in 013: the
// first exposed harness self-diagnostics that are telemetry, not user business;
// the second duplicated a two-state toggle that Shift+Tab already does, with the
// current mode and its shortcut now named in the footer.
var commands = []commandEntry{
	{"/reasoning", "Set reasoning effort (low–max)"},
	{"/resume", "Resume a workspace session"}, {"/new", "Start a new session"},
	{"/context", "Inspect context usage"}, {"/compact", "Compact conversation"},
	{"/login", "Store API key"}, {"/logout", "Log Out of Account"},
	{"/usage", "View account usage"},
	{"/skills", "Assign skills to next prompt"}, {"/mcp", "Manage MCP servers"},
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
	m := &Model{ctx: ctx, runtime: options.Runtime, bridge: options.Bridge, actions: options.Actions, version: options.Version, latestVersion: options.LatestVersion, theme: theme, palette: colors, glyphs: marks, viewport: view, input: input, width: 100, height: 34, status: "Ready", activeTools: make(map[string]*toolView), selectedSkills: make(map[string]Skill), historyIndex: -1, initialPrompt: options.InitialPrompt, followOutput: true}
	// US1 T021: when the runtime is not yet built and a Hydrate func is provided,
	// start in the loading shell; the engine-dependent state is populated once
	// hydration completes. Otherwise this is the unchanged single-stage path.
	if options.Runtime.Engine == nil && options.Hydrate != nil {
		m.loading = true
		m.hydrate = options.Hydrate
		m.status = "Loading workspace…"
		m.pendingSubmit = strings.TrimSpace(options.InitialPrompt)
	} else {
		m.plan = options.Runtime.Engine.CurrentChecklist()
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
