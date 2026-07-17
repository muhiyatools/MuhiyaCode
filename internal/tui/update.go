package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

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
	// Experience Overhaul A5: the chrome (header / activity / to-dos / mode line) is
	// memoized per frame. Invalidate at the very top of every Update so the first
	// accessor call this cycle (layout() or View()) renders it once and the rest
	// reuse that exact string — no more 2–3× redundant chrome renders per frame.
	m.chrome.valid = false
	wasBottom := m.viewport.AtBottom()
	beforeOffset := m.viewport.YOffset()
	beforeItems := len(m.items)
	beforeWidth := m.width
	beforeViewAgent := m.viewAgent
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
		tv := &toolView{name: value.name, target: contract.ToolTarget(value.name, value.input), state: "running", started: time.Now()}
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
		// so the modal choices can drive it. Clear busy/status HERE, not on the
		// resultMsg that follows: stats arrive first, and if the user answers
		// the modal in that window while busy is still true, submit() silently
		// queues the "Proceed with the approved plan…" prompt instead of
		// running it — a dead click. The resultMsg handler re-assigns the same
		// idle values, so the double-clear is idempotent.
		if value.PlanReady {
			m.busy = false
			m.status = "Ready"
			m.openPlanReadyModal()
		}
		// H5: surface a force-finalization reason (token breaker / failure
		// terminator) as a warn notice so the user knows why the task stopped
		// short of the turn ceiling.
		if value.TerminatedReason != "" {
			m.warn("Task terminated: " + value.TerminatedReason)
		}
	case planProceedResultMsg:
		// The engine transition already ran off-thread (modals.go). Do the UI-thread
		// follow-up here: a false result is the dead-click guard (plan terminal or
		// not awaiting approval); otherwise submit the execution prompt.
		if !value.ok {
			m.notify("Could not start implementation — the plan is not awaiting approval (say 'proceed' later to run it).")
			break
		}
		m.notify("Proceeding with the approved plan now.")
		if cmd := m.submit("Proceed with the approved plan. Work through the plan steps in order, keeping update_plan current."); cmd != nil {
			commandsOut = append(commandsOut, cmd)
		}
	case engineNoticeMsg:
		// An off-thread engine mutation (T021) finished; show its notice.
		m.notify(value.notice)
	case planDeferredMsg:
		m.notify("Plan saved. Say 'proceed' (or 'go ahead') any time to execute it.")
	case planKeepPlanningMsg:
		if value.err != nil {
			m.notify("Could not return to planning: " + value.err.Error())
			break
		}
		m.notify("Plan mode stays on — keep refining the plan.")
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
		if value.err != nil {
			m.warn("Task stopped: " + friendlyTaskError(value.err))
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
	// 009 polish: an agent-view switch (Tab, Alt+N, Esc, or a chip click)
	// changes which transcript body renders — repaint NOW. Keyboard switches
	// previously set no dirty signal, so on an idle session (no content events
	// coming) the header changed but the body never did: the switch looked
	// dead. Scroll resets deliberately on a switch: a detail view opens at its
	// top (the "Agent task:" header), and returning to the main session
	// resumes following the live bottom.
	viewSwitched := m.viewAgent != beforeViewAgent
	if viewSwitched {
		dirty = true
		m.followOutput = m.viewAgent == ""
	}
	forceBottom := m.followOutput
	if dirty {
		// New content re-renders and pins to the bottom only while following, so a
		// reader who scrolled up mid-stream is never yanked down.
		m.frameState.markDirty() // US2 T032: record that the transcript pane changed
		m.refreshViewport(forceBottom)
		if viewSwitched && m.viewAgent != "" {
			m.viewport.GotoTop()
		}
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
