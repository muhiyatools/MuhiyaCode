# Feature Specification: Terminal Experience and Project Memory

**Feature Branch**: Not created (no branch hook configured)

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: "Overhaul the MuhiyaCode terminal experience so typing and streaming remain fast and stable at any session length, mouse interaction is first-class, large pastes stay compact without losing content, the interface is consistently polished, and project instructions plus durable decisions and findings carry across sessions. Preserve the agent loop, tools, model routing, prompt-cache determinism, simple line mode, local-only operation, and cross-platform support."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Instant Input at Any Session Length (Priority: P1)

A developer can type, edit, scroll, and resume work without feeling the transcript's size. Input echoes immediately in a new session and remains equally immediate after thousands of messages or hours of activity. Resuming a large session becomes interactive quickly, and the first keystroke is not delayed while older content is prepared.

**Why this priority**: Input lag is the most direct failure in the current experience. If typing degrades with history, every long-running or returning user eventually loses confidence in the terminal.

**Independent Test**: Replay the standard 10-message and 5,000-message session fixtures, type the same scripted sequence in each, and compare measured input-to-echo latency, launch-to-interactive time, scrolling responsiveness, and memory use over a 60-minute run.

**Acceptance Scenarios**:

1. **Given** a session with 10 messages, **When** the user types or edits input, **Then** each input event is reflected within one display frame at the 95th percentile.
2. **Given** a session with at least 5,000 messages, **When** the same input sequence is performed, **Then** input latency remains within one display frame at the 95th percentile and its 95th-percentile result differs from the 10-message result by no more than 2 milliseconds.
3. **Given** a saved session with at least 5,000 messages, **When** the user resumes it, **Then** the input is usable within 2 seconds at the 95th percentile and the first keystroke appears within one display frame.
4. **Given** a representative 60-minute session that grows beyond 5,000 messages, **When** memory and responsiveness are sampled throughout, **Then** the application remains responsive, does not crash, and stays within the defined working-memory envelope.

---

### User Story 2 - Smooth Updates and Quiet Idle Operation (Priority: P1)

While an answer streams, only the changing content appears to update; completed messages remain visually still. Scrolling and resizing feel stable, without torn lines, flashes, or repeated reflow. When no state changes, the application becomes quiet instead of continuously consuming processing time.

**Why this priority**: A terminal that flickers, stutters, or burns resources while idle feels unreliable even when its output is correct. These behaviors also compound the long-session latency in Story 1.

**Independent Test**: Stream a long response into both short- and large-history fixtures, scroll during output, resize repeatedly, then leave the application untouched for 60 seconds while recording visual changes, update latency, and idle processing use.

**Acceptance Scenarios**:

1. **Given** completed transcript messages and an actively streaming final message, **When** new chunks arrive, **Then** completed content remains visually unchanged and the new content appears smoothly without whole-screen flashes.
2. **Given** a 5,000-message session, **When** output streams, **Then** its 95th-percentile chunk-to-visible latency is no more than 5 milliseconds slower than the same stream in a 10-message session.
3. **Given** an unchanged idle screen, **When** the application is observed for 60 seconds, **Then** it initiates no repeated full-screen refresh and averages no more than 1% of one processor core, excluding user or terminal-generated events.
4. **Given** any supported application state, **When** the terminal is resized, **Then** the visible layout reflows in the next frame with no stale fragments, duplicated rows, or flicker.

---

### User Story 3 - Return to a Project with Its Context Intact (Priority: P1)

A developer opens MuhiyaCode in a workspace and the agent begins with that project's optional instructions. Decisions and durable findings established during earlier work are available in later turns and after closing and reopening the application, so the developer does not have to repeatedly explain the same conventions, chosen approaches, or discovered facts.

**Why this priority**: Persistent orientation is the other core continuity problem. Fast rendering alone does not help if a resumed session has forgotten why the work looks the way it does.

**Independent Test**: Create a workspace instructions file, establish a set of decisions and findings across multiple turns, close the application, start a new session in the same workspace, and ask questions that require each item; repeat in a different workspace to verify isolation.

**Acceptance Scenarios**:

1. **Given** a workspace containing a readable `MUHIYA.md`, **When** any interactive or simple-line session starts in that workspace, **Then** the agent follows its applicable project instructions from the first turn without the user pasting them into the conversation.
2. **Given** no `MUHIYA.md` in the workspace, **When** a session starts, **Then** startup continues normally with no warning or failure caused by the absent optional file.
3. **Given** a durable project decision or finding recorded in one turn, **When** it is relevant in a later turn or a newly opened session in the same workspace, **Then** the agent can correctly state or apply it without the user repeating it.
4. **Given** two different workspaces with different instructions and memories, **When** sessions alternate between them, **Then** neither workspace receives the other's project context.
5. **Given** a later decision that replaces an earlier one, **When** the topic is recalled, **Then** the current decision is used and the superseded decision is not presented as current.
6. **Given** identical configuration, workspace instructions, project memory, and settled history, **When** equivalent requests are assembled, **Then** the existing byte-stability and deterministic prompt-cache guarantees remain unchanged.

---

### User Story 4 - Mouse-Native Terminal Navigation (Priority: P2)

A developer can use the mouse naturally: the wheel scrolls the transcript, clicking the input focuses it and positions the caret, and visible menus, activity chips, and modal buttons respond to clicks. The developer can still select and copy transcript text. In terminals without mouse support, every action remains reachable from the keyboard.

**Why this priority**: Mouse interaction removes unnecessary friction and makes discoverable controls behave like the controls they visually resemble, while keyboard fallback preserves terminal portability.

**Independent Test**: Exercise every mouse target and text-copy path in Windows Terminal, then repeat the action matrix in representative macOS and Linux terminals and with mouse reporting unavailable, confirming equivalent keyboard completion.

**Acceptance Scenarios**:

1. **Given** a transcript longer than the viewport, **When** the user rotates the mouse wheel over it, **Then** the transcript scrolls in the expected direction without moving or corrupting the input caret.
2. **Given** text in the input, **When** the user clicks a visible character position, **Then** the input gains focus and the caret moves to the closest valid text position.
3. **Given** an open slash-command menu, **When** the user clicks a visible command item, **Then** that item is selected exactly as if chosen by keyboard.
4. **Given** a tool or subagent activity chip, **When** the user clicks it, **Then** its details expand or collapse in place.
5. **Given** a modal with actionable buttons, **When** the user clicks a button, **Then** the corresponding action occurs once and the modal state updates correctly.
6. **Given** transcript text, **When** the user performs the documented copy gesture, **Then** the selected text reaches the system clipboard without triggering an unrelated application action.
7. **Given** a terminal with no usable mouse capability, **When** the user operates the application entirely by keyboard, **Then** all scroll, focus, menu, chip, modal, and copy tasks remain available.

---

### User Story 5 - Large Pastes Stay Compact and Complete (Priority: P2)

A developer pastes a long prompt, log, or code sample into the input and sees one compact labeled block rather than a wall of text. They can type before and after it, inspect it when needed, remove it if pasted by mistake, and send the exact original content as part of the message. Ordinary small pastes continue to feel like normal typing.

**Why this priority**: Large pasted text currently disrupts both layout and composition. Preserving it as a compact, editable part of the input solves the visual problem without sacrificing content fidelity.

**Independent Test**: Paste multiline, Unicode, mixed-newline, and near-threshold samples; compose text on both sides; expand, collapse, remove, and send; compare the submitted message byte-for-byte after normal text-input newline handling.

**Acceptance Scenarios**:

1. **Given** pasted text below the large-paste threshold, **When** it enters the input, **Then** it appears inline as ordinary editable text.
2. **Given** pasted text at or above the threshold, **When** it enters the input, **Then** it appears as one compact block labeled with an accurate line count and does not cause the input or surrounding layout to overflow.
3. **Given** a compact paste block, **When** the user types or navigates around it, **Then** ordinary text can be added before and after the block without changing the stored pasted content.
4. **Given** a compact paste block, **When** the user expands and collapses it, **Then** the full content can be inspected and the compact layout can be restored without modifying the message.
5. **Given** a compact paste block, **When** the user removes it, **Then** only that pasted content is removed and adjacent typed text remains intact.
6. **Given** a message containing typed text and one or more compact paste blocks, **When** it is sent, **Then** the receiver gets the complete contents in their original order with all pasted text preserved.

---

### User Story 6 - Clear, Coherent States on Every Screen (Priority: P3)

The interface feels like one product rather than a collection of panels. Header, transcript, activity, command menu, input, mode line, and modals share consistent spacing, alignment, and color semantics. Empty, idle, busy, streaming, tool/subagent activity, error, and undersized-terminal states are immediately understandable.

**Why this priority**: Consistency and explicit states complete the sense of polish and reduce confusion, but the core usability gains remain independently deliverable through the preceding stories.

**Independent Test**: Capture each required state at supported widths and color capabilities, compare shared visual semantics across surfaces, and verify the undersized state preserves a usable recovery path.

**Acceptance Scenarios**:

1. **Given** any two application surfaces, **When** comparable information or status appears, **Then** spacing, alignment, emphasis, and color meaning are consistent and derive from the same authoritative theme.
2. **Given** a first run or an empty transcript, **When** the application opens, **Then** it presents a clear starting state and the input is immediately usable.
3. **Given** idle, busy, streaming, tool/subagent-running, or error state, **When** the state changes, **Then** the user can distinguish the current state without relying on color alone.
4. **Given** a terminal too small for the normal layout, **When** the application renders, **Then** it shows a stable, readable size warning and recovery guidance rather than a corrupted partial interface.

### Edge Cases

- A session contains very large individual messages or tool outputs as well as thousands of ordinary messages; scrolling and input must remain responsive whether those entries are collapsed or expanded.
- A user scrolls away from the bottom while output is streaming; the viewport must not jump to the newest output until the user explicitly returns or invokes the follow-output action.
- A resize occurs during a paste, modal interaction, mouse drag, or stream update; focus, selection, paste content, and interaction state must survive the reflow.
- Mouse coordinates land on wrapped wide characters, combining characters, emoji, borders, or empty padding; the nearest valid target is chosen and no panic or invalid caret position occurs.
- The terminal reports wheel or click events incompletely or not at all; keyboard behavior remains unchanged and the UI does not become stuck in a mouse-only state.
- Native selection conflicts with application mouse capture; a documented, cross-platform copy path remains available and does not lose selected text.
- A paste is empty, contains a single extremely long line, has mixed newline conventions, contains tabs or Unicode, or occurs multiple times in one draft; content order and fidelity are preserved.
- A large paste is removed, the draft is cleared, or the application closes before sending; no orphaned paste data is later attached to another message.
- `MUHIYA.md` is empty, unreadable, larger than the accepted project-instructions limit, changes during a session, or resolves outside the trusted workspace; the session remains usable and existing path-containment and trust rules are enforced.
- Project memory contains duplicate, conflicting, stale, or superseded items; recall favors the current item and does not present contradictory items as simultaneously active.
- A session or project-memory write is interrupted; existing session data and previously committed memory remain readable on restart, without partial entries being treated as valid.
- The same saved session is opened after the workspace moved or was renamed; the user receives clear, non-destructive behavior and context is never silently borrowed from an unrelated workspace.
- A terminal supports only limited colors or no mouse reporting; required states remain distinguishable and all functionality remains keyboard accessible.

## Requirements *(mandatory)*

### Functional Requirements

**Performance and transcript stability**

- **FR-001**: Input handling MUST remain independent of total transcript length so that typing, deletion, caret movement, and draft editing meet the latency targets for both short and 5,000-message sessions.
- **FR-002**: A resumed large-history session MUST accept and display input before all non-visible history has been prepared for display.
- **FR-003**: The application MUST keep only the transcript content and presentation state needed for the active view in working memory, while preserving access to the full saved transcript.
- **FR-004**: Streaming MUST update changing content without visibly refreshing or altering completed transcript regions.
- **FR-005**: Scrolling MUST remain responsive during streaming and MUST preserve the user's viewport when they have intentionally scrolled away from the newest content.
- **FR-006**: When no application state or external event changes, the application MUST NOT initiate recurring full-screen refreshes or other continuous work.
- **FR-007**: A terminal resize MUST produce a coherent layout in the next frame without stale fragments, duplicated rows, torn content, or loss of input state.
- **FR-008**: The 60-minute, 5,000-message benchmark workload MUST stay within 300 MiB of application working memory and MUST show no upward trend greater than 10% during its final 15 minutes.

**Mouse and keyboard interaction**

- **FR-009**: Mouse-wheel events over the transcript MUST scroll it without changing input content or caret position.
- **FR-010**: Clicking within the input MUST focus it and place the caret at the closest valid text position, including on wrapped and wide-character lines.
- **FR-011**: Every visible slash-command item MUST be clickable and perform the same selection behavior as its keyboard equivalent.
- **FR-012**: Every visible tool or subagent activity chip MUST be clickable to expand or collapse its associated details.
- **FR-013**: Every visible modal button MUST be clickable and invoke its action no more than once per click.
- **FR-014**: Users MUST be able to select and copy transcript text through native terminal selection or an in-application copy interaction with a documented gesture on each supported platform.
- **FR-015**: Every mouse-enabled action MUST retain an equivalent keyboard path, and the application MUST remain fully usable when mouse events are unavailable or unreliable.

**Input and paste handling**

- **FR-016**: Text pastes smaller than 1,024 characters and fewer than 6 lines MUST insert as ordinary inline input.
- **FR-017**: A paste of at least 1,024 characters or at least 6 lines MUST appear as a compact input block that shows an accurate line count and occupies no more than two input rows while collapsed.
- **FR-018**: A compact paste block MUST preserve the complete pasted text, its position relative to typed text and other paste blocks, Unicode content, tabs, and logical line breaks until sent or removed.
- **FR-019**: Users MUST be able to place and edit ordinary text before and after a compact paste block without implicitly expanding or modifying the block.
- **FR-020**: Users MUST be able to expand a compact paste block for inspection, collapse it again, and remove it independently from surrounding input.
- **FR-021**: Sending a draft MUST reconstruct one message containing all typed and pasted content in composition order, with no truncation, duplication, placeholder labels, or other presentation-only text.
- **FR-022**: Clearing, abandoning, or successfully sending a draft MUST release any paste content belonging only to that draft and MUST NOT attach it to a later message.

**Visual structure and application states**

- **FR-023**: Header, transcript, status/activity area, command menu, input, mode line, modals, and size warnings MUST share one authoritative theme for spacing, alignment, emphasis, and color semantics.
- **FR-024**: The application MUST present distinct, readable states for first-run/empty, idle, busy, streaming, tool running, subagent running, error, and terminal-too-small conditions.
- **FR-025**: State meaning MUST NOT rely on color alone, and required content MUST remain readable with limited color support and on both light and dark terminal backgrounds.
- **FR-026**: When the terminal is too small for the full layout, the application MUST replace unstable content with a readable minimum-size message while preserving a keyboard-accessible way to exit or recover.

**Persistent project context**

- **FR-027**: Every session MUST look for an optional `MUHIYA.md` at the trusted workspace root and make its readable, valid instructions available as project context from the first agent turn.
- **FR-028**: A missing optional project-instructions file MUST NOT delay, warn, or prevent startup; an unreadable, invalid, oversized, or untrusted-path file MUST be handled with a clear non-fatal explanation.
- **FR-029**: Project instructions MUST be scoped to their workspace, loaded in deterministic order, and refreshed predictably when the file changes without leaking content into other workspaces.
- **FR-030**: The system MUST persist durable project decisions and findings locally so they remain available in later turns and newly created or resumed sessions for the same workspace.
- **FR-031**: Each persisted memory item MUST retain enough provenance to distinguish its statement, project scope, originating session or turn, creation/update time, and whether it is current or superseded.
- **FR-032**: When a decision or finding is corrected or replaced, the memory layer MUST preserve the audit relationship while presenting only the latest applicable item as current context.
- **FR-033**: Project memory MUST exclude raw conversation replay and transient activity by default, MUST NOT become a codebase semantic index, and MUST NOT persist detected credentials or secrets as durable memory.
- **FR-034**: Users MUST be able to inspect and clear locally stored project memory without deleting the project or unrelated saved sessions.
- **FR-035**: Project instructions and memory MUST be available in interactive, resumed, and non-interactive/simple-line sessions without changing the reasoning process, tool availability, model selection, or model routing.

**Compatibility and verification**

- **FR-036**: The feature MUST preserve existing agent-loop behavior, tool contracts, permission and real-path containment rules, local-only data handling, OpenAI-compatible provider behavior, and the established `~/.muhiya` session/state compatibility boundary.
- **FR-037**: The stable prompt prefix and settled history MUST retain their byte-stability, deterministic ordering, append-only behavior, and cache guarantees; dynamic memory updates MUST NOT rewrite previously settled request content.
- **FR-038**: The interactive experience MUST work on Windows Terminal as the primary target and on supported macOS and Linux terminals, with graceful fallback for missing mouse or advanced color capabilities.
- **FR-039**: Existing non-interactive/simple-line workflows MUST continue to accept input, load project context, send messages, and produce output without requiring the full-screen terminal interface.
- **FR-040**: All performance, resource-use, responsiveness, and cache-preservation claims MUST be verified against recorded before-and-after workloads that hold session content, terminal dimensions, machine conditions, model, gateway, and workload constant where applicable.
- **FR-041**: Codebase semantic indexing, inline per-hunk diff review, `@`-file context mentions, and multi-model fast-apply MUST NOT be introduced as part of this feature.

### Key Entities

- **Transcript Entry**: One saved user, assistant, tool, subagent, status, or error item. It has stable content and identity independent of whether it is currently visible, collapsed, or prepared for display.
- **Viewport State**: The user's current visible transcript range, scroll anchor, follow-output preference, terminal dimensions, and focused interaction target.
- **Input Draft**: The unsent ordered composition of ordinary text and zero or more paste blocks, including caret and focus state.
- **Paste Block**: A compact input element backed by the complete pasted text, with derived line count, collapsed/expanded state, composition position, and removal lifecycle.
- **Project Instructions**: Optional workspace-scoped guidance sourced from `MUHIYA.md`, with workspace identity, content identity, validity, and last observed change.
- **Project Memory Item**: A locally persisted decision or finding with statement, workspace scope, provenance, timestamps, current/superseded status, and an optional relationship to the item it replaces.
- **Application State**: One of the user-visible operating conditions—first-run/empty, idle, busy, streaming, tool running, subagent running, error, or terminal too small—with consistent semantic presentation.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In both 10-message and 5,000-message fixtures, 95% of input events appear within 16 milliseconds; the two 95th-percentile results differ by no more than 2 milliseconds.
- **SC-002**: Across 30 large-session resume trials on the reference Windows environment, 95% become input-ready within 2 seconds and 95% display the first keystroke within 16 milliseconds of receipt.
- **SC-003**: A representative 60-minute workload exceeding 5,000 messages completes with zero crashes, no increasing input-latency trend, no more than 300 MiB working memory, and no more than 10% working-memory growth during the final 15 minutes.
- **SC-004**: During a 60-second unchanged idle interval, the application performs zero recurring full-screen refreshes and averages no more than 1% of one processor core, excluding terminal-generated cursor behavior and external events.
- **SC-005**: Visual capture tests for typing, streaming, scrolling, and resizing show zero completed-message changes, torn frames, stale fragments, duplicated rows, or visible full-screen flashes across the supported terminal matrix.
- **SC-006**: Every mouse action in the interaction matrix succeeds in Windows Terminal and the selected representative macOS and Linux terminals; the keyboard-only matrix achieves 100% of the same tasks when mouse input is disabled.
- **SC-007**: Across the paste-fidelity suite—including multiline, Unicode, tabs, mixed logical line breaks, multiple paste blocks, and threshold boundaries—100% of sent messages preserve the intended full content and order, and zero collapsed pastes cause layout overflow.
- **SC-008**: In scripted cross-session tests, 100% of current project instructions, decisions, and findings are correctly available when relevant after restart; 0% leak into a different workspace; and 0% of superseded items are represented as current.
- **SC-009**: Existing deterministic-prefix and settled-history stability checks remain byte-identical under unchanged context, and recorded cache efficiency is no worse than the pre-feature baseline under identical workloads.
- **SC-010**: All required application states are correctly identified by 100% of accessibility review participants without relying on color alone, and all remain usable at the documented minimum terminal size or show a stable recovery message below it.
- **SC-011**: Existing non-interactive/simple-line acceptance tests complete with no regression in input, project-context loading, message submission, output, or exit behavior on Windows, macOS, and Linux.

## Assumptions

- Feature `003-tui-ux-overhaul` remains the source for previously specified visual and transcript-polish behavior; this feature may refine shared surfaces but does not duplicate unrelated account, usage, authentication, or markdown work.
- The standard long-session benchmark contains at least 5,000 mixed user, assistant, tool, subagent, status, and error entries, representative payload sizes, collapsed and expanded activity, active streaming, scrolling, editing, and repeated resizes. Its exact fixture and machine profile will be recorded during planning so results are reproducible.
- The 300 MiB working-memory target covers the MuhiyaCode application during the standard benchmark and excludes the terminal emulator, model gateway, and external tools. Workloads containing intentionally opened unbounded external output are measured separately.
- "One frame" means 16 milliseconds for acceptance measurement, even when the physical display refresh rate differs.
- A large paste is defined as at least 1,024 characters or at least 6 logical lines. These defaults can later become user-configurable without changing the required compact-block behavior.
- Paste fidelity applies to terminal text input after the terminal's normal text decoding and newline normalization; arbitrary binary clipboard payloads are outside scope.
- `MUHIYA.md` is optional, local, workspace-root scoped, and subject to existing workspace trust, real-path containment, size, and secret-handling protections. No network synchronization or shared/team memory is introduced.
- Durable project memory includes decisions (chosen conventions, approaches, constraints) and findings (facts learned about the project that remain useful). Raw transcripts, temporary progress, speculative thoughts, and a semantic code index are not durable memory.
- Memory recall supplies relevant project context but does not alter the agent's reasoning algorithm, tools, permissions, model choice, routing, or completion behavior.
- Existing session and state data remain readable. If new sidecar data is needed, absence of that data is treated as an empty memory rather than a corrupt or incompatible session.
- Windows Terminal is the primary validation environment; at least one mainstream terminal on macOS and one on Linux form the cross-platform acceptance matrix.
- Native terminal selection may require a platform-specific modifier when mouse reporting is active. A documented in-application copy path is acceptable where native selection cannot coexist with application mouse capture.
