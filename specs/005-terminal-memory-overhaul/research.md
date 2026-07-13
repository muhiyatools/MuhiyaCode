# Phase 0 Research: Terminal Experience and Project Memory

**Feature**: `005-terminal-memory-overhaul`  
**Date**: 2026-07-13

This research combines direct code inspection, the repository's earlier TUI and cache artifacts, and the DeepSeek Reasonix reference required by Constitution Principle VII. Each decision records its rationale and rejected alternatives. No clarification remains open.

## Current-State Evidence

### Terminal runtime

- The existing stack is sufficient: Go 1.25 with Bubble Tea v2.0.8, Bubbles v2.1.1, Lip Gloss v2.0.5, `x/ansi`, and `go-runewidth` (`go.mod`). Replacing the framework or adding another TUI runtime is unnecessary.
- `Model.Init` starts an unconditional 100 ms timer, and every timer event reschedules itself (`internal/tui/model.go`). Every `Update`, including every keypress and timer event, calls `layout` and `refreshViewport`; `refreshViewport` invokes `renderTranscript` and `viewport.SetContent` (`internal/tui/view.go`). `renderTranscript` walks and rerenders every retained item, wraps Markdown, and rebuilds one large string. Input work is therefore proportional to retained transcript size, and idle is never quiet.
- `m.items`, agent item lists, input history, and completed assistant content are not bounded. Streaming repeatedly concatenates strings; tool completion/output searches backward through all items. The bridge already coalesces assistant stream callbacks for 35 ms, but not shell/tool chunks (`internal/tui/bridge.go`).
- While busy, `forceBottom` always repins the viewport, so an intentional scroll away from the stream cannot stick. Resize clamps stored dimensions upward before the too-small view is evaluated, which can render beyond the physical terminal.
- Resume builds the complete application before Bubble Tea starts (`internal/command/root.go`, `internal/command/application.go`, `internal/tui/run.go`). Runtime construction synchronously reads history, inspection, knowledge, usage and invalidation sidecars, may perform a 1.5-second web probe, constructs the engine, and then loads the latest 300 SQLite events. The transcript database already has ordered integer event IDs and an index, but exposes only all-or-last-N reads (`internal/state/db.go`).
- Mouse capture is explicitly disabled in `View` to preserve native selection. No mouse event is handled, although Bubble Tea's `MouseModeCellMotion` and the Bubbles viewport already support click/release/wheel events.
- Bracketed paste is passed directly to a Bubbles textarea capped at 32,000 characters. The textarea cannot represent atomic blocks and sanitizes/truncates input, so it cannot satisfy exact large-paste preservation.
- Semantic colors and glyphs are centralized in `palette` and `glyphs` (`internal/tui/render.go`), but spacing, pane metrics, borders, and breakpoints remain scattered through `view.go`. Empty/first-run is a blank viewport; other states exist in partial form.
- `RunLine` remains a separate, straightforward line interface selected by `--simple`, `MUHIYA_SIMPLE_TUI=1`, or non-TTY input/output. It has no dedicated tests and must stay independent of full-screen paging, mouse, and composer behavior.

### Session, prompt, and memory runtime

- Durable compatibility state lives under `~/.muhiya` or `MUHIYA_HOME`. SQLite stores sessions, events, and workspace trust; per-session files store history, inspection, knowledge, transcripts, usage, invalidations, plans, and goals (`internal/state`).
- Workspaces are canonicalized with symlink/junction-aware containment (`internal/workspace/paths.go`). In-workspace reads are allowed, but automatic project-instruction loading must require root containment directly rather than use a permission path that can approve outside-workspace access (`internal/workspace/permissions.go`).
- `internal/orchestrator/knowledge.go` is session-local. It holds bounded file notes and subagent reports for reuse/compaction; it neither records project decisions nor crosses sessions.
- `SystemPrompt(PromptContext)` contains session-stable inputs. Prefix-shape guards and restart tests enforce deterministic system/tool/history bytes. Settled history is append-only except explicit, recorded maintenance boundaries (`internal/orchestrator/prompt.go`, `prefixshape.go`, `history.go`, `restart_determinism_test.go`).
- No `MUHIYA.md` loader or project-memory store exists. Skills are rediscovered during every runtime reconstruction even though docs describe a session-pinned listing; a changed skill can therefore silently alter the reconstructed prompt on resume. The new stable-context snapshot must close this same determinism gap for skills as well as instructions and memory.
- Existing session compaction asks for `GOAL/STATE/FILES/DECISIONS/COMMANDS/PENDING`, but compaction is pressure-driven and cannot guarantee that every ordinary completed task contributes cross-session memory.

### Validation gaps

- Direct `Model.Update`/`View` tests exist and the current targeted suites pass, but there is no 5,000-event fixture, terminal performance harness, mouse suite, large-paste fidelity suite, PTY/ConPTY frame recorder, or dedicated simple-line test.
- Model-only tests can prove invalidation and bounded rendering but cannot, alone, prove physical terminal echo latency, OS working set, idle process CPU, or visible flicker. Release evidence needs both an automated deterministic harness and real-terminal recordings.

## Decisions

### D1. Keep Bubble Tea; make rendering event-driven and bounded

**Decision**: Retain Bubble Tea/Bubbles/Lip Gloss. Introduce pane dirty flags, cached frame strings, stable entry IDs, O(1) active tool/agent lookup, and a bounded transcript window. An input event dirties only input/command panes; a stream event dirties only the active transcript block/activity; a resize invalidates visible blocks and geometry once. Let Bubble Tea's differential renderer update changed rows.

**Rationale**: The dependency supports all required events. The defect is MuhiyaCode's unconditional full-history composition, not a framework limitation. This is the smallest change consistent with Constitution VIII.

**Alternatives considered**: Replacing Bubble Tea was rejected as an unjustified rewrite. Caching one full rendered transcript was rejected because memory and keypress work would still grow with history. Merely slowing the timer was rejected because keypresses would remain O(history).

### D2. Page transcript events by stable SQLite ID

**Decision**: Add keyset-paginated event reads around SQLite event IDs. The TUI retains the visible page plus bounded overscan under both entry and byte limits, with a viewport anchor of event ID + intra-entry row + follow-output state. Older/newer pages load asynchronously near a window edge; results carry session ID and generation so stale responses cannot populate a switched session. The full transcript remains durable in SQLite.

**Rationale**: SQLite already owns ordered events and an index. Paging the presentation copy bounds TUI memory without changing model history or session compatibility. Stable anchors prevent jumps when wrapped heights change.

**Alternatives considered**: Loading all events and virtualizing only the final string was rejected because raw entries would remain unbounded. Replacing SQLite with a new transcript store was rejected because the existing compatibility boundary is sufficient.

### D3. Use lifecycle-scoped timers only

**Decision**: Remove the permanent 100 ms timer. Schedule animation ticks only while a spinner or elapsed counter is visible, notice expiry as one deadline command, MCP refresh only while its modal is live, and stream/tool flushes only when buffered chunks exist. Idle schedules no application refresh command.

**Rationale**: Every recurring idle message currently triggers layout and transcript work. Event-scoped timers meet the quiet-idle requirement without losing active feedback.

**Alternatives considered**: A lower-frequency global timer was rejected because it still consumes CPU and causes redraw opportunities while idle.

### D4. Start the interactive shell before non-visible session hydration

**Decision**: Split interactive startup into a lightweight shell phase and an asynchronous runtime/page hydration phase. Settings/session identity and the composer create the first frame; input editing works immediately while submission waits for a generation-tagged runtime-ready message. Use a persisted web-probe result for the current session and refresh it only for a future boundary rather than blocking startup.

**Rationale**: `OpenApplication` currently blocks before Bubble Tea can receive a key. Two-stage composition is required to make the first keystroke independent of large saved state and the network probe without changing the engine loop.

**Alternatives considered**: Optimizing JSON decoding alone was rejected because the synchronous network probe still threatens the 2-second P95 and non-visible transcript preparation still precedes input.

### D5. Enable cell-motion mouse input with a frame-owned interaction map

**Decision**: Set `tea.View.MouseMode` to `MouseModeCellMotion`. Each composed frame owns an immutable interaction map of screen rectangles/caret stops for transcript, input, command items, tool/subagent chips, paste blocks, and modal buttons. Target precedence is modal > command menu > input/paste > chip > transcript. Actions fire on left-button press only; release cannot double-invoke. Wheel events scroll only over the transcript. Keyboard handlers and mouse handlers invoke the same semantic action.

**Rationale**: Cell motion supplies clicks, release, wheel, and pressed drag without all-motion's idle pointer traffic. A map produced from the same geometry as the frame prevents hit testing from drifting from what the user sees.

**Alternatives considered**: All-motion mode was rejected because it generates needless movement traffic. Ad-hoc coordinate checks in each handler were rejected because pane heights and wrapped content are dynamic.

### D6. Provide in-app selection/copy plus terminal fallback

**Decision**: Support drag selection over visible plain transcript text under cell-motion mode and copy via Bubble Tea's clipboard command (OSC52). When a selection exists, Ctrl+C copies it before existing cancel/exit behavior. Keep a documented terminal-native modifier-selection fallback and keyboard-only selection/copy path when OSC52 or mouse reporting is unavailable.

**Rationale**: Enabling mouse capture makes unmodified native selection terminal-specific. An in-app path satisfies the contract consistently, while native selection remains a graceful fallback.

**Alternatives considered**: Native selection alone was rejected as unreliable once capture is enabled. All-motion hover selection was rejected as unnecessary.

### D7. Replace only the main textarea with a segmented composer

**Decision**: Add a composer whose draft is ordered `TextPart | PastePart` data with a grapheme-aware cursor. Small pastes merge into text. A paste at least 1,024 Unicode code points or 6 logical lines becomes an immutable atomic paste part holding the exact received string, derived line count, and collapsed/expanded state. Cursor stops exist before/after paste atoms; sending walks parts once and writes exact contents in order. Modal and line-mode input remain unchanged.

**Rationale**: The current textarea's sanitizer, private renderer, char limit, and flat string cannot safely implement atomic paste identity or fidelity. Replacing this one widget is the smallest defensible component change under Principle VIII.

**Alternatives considered**: Embedding editable sentinel labels in the textarea was rejected because deletion, cursor mapping, sanitation, and placeholder mutation can corrupt the hidden content. Treating the paste as an attachment outside the draft was rejected because it would not support text before and after the block.

### D8. Extend the existing visual system into one complete Theme

**Decision**: Preserve feature 003's palette/glyph semantics and wrap them with shared spacing, pane metrics, borders, and breakpoints in one Theme value. Add explicit first-run/empty and runtime-loading states, preserve non-color markers, and use actual terminal dimensions for the 60x20 floor.

**Rationale**: Color and glyph centralization already exists; migrating remaining layout constants completes rather than replaces it.

**Alternatives considered**: A new visual design was rejected as duplicate scope. Keeping scattered geometry was rejected because mouse hit regions and one-frame resizing require a single layout source.

### D9. Load and pin a contained `MUHIYA.md`

**Decision**: Add a workspace loader for `<canonical-root>/MUHIYA.md` with a 32 KiB raw limit. Require a regular, contained UTF-8 file with no NUL or detected secrets; normalize BOM and newline forms deterministically before hashing/rendering. Missing/empty is silent; outside-root symlink/junction, invalid, unreadable, oversized, or secret-bearing files produce a typed non-fatal diagnostic. Check only at runtime construction and user-submit boundaries—never by idle polling.

**Rationale**: The file is user-authored project context but remains subject to the security model. A deterministic canonical form and bounded size protect prefix stability and prompt size.

**Alternatives considered**: Generic permission approval was rejected because it can permit outside-workspace reads. Filesystem watchers were rejected because they add cross-platform complexity and idle work; submit-boundary detection is predictable.

### D10. Store project memory as an append-oriented workspace ledger

**Decision**: Add an additive SQLite `project_memory_events` ledger keyed by canonical workspace. Events record `record`, `supersede`, or `forget` with stable item ID, kind (`decision` or `finding`), normalized topic, statement, provenance, sequence, and timestamps. Corrections append and reference the old item; only explicit clear-all physically deletes a workspace's rows. The current projection is bounded to 64 items, 1,024 characters per statement, and 32 KiB rendered context; new entries are rejected with a visible diagnostic rather than silently evicting current memory. Secrets are rejected, not stored as redacted placeholders.

**Rationale**: An append ledger gives atomic recovery, supersession, provenance, inspectability, and cross-session workspace scope while preserving the existing SQLite compatibility boundary.

**Alternatives considered**: Reusing session `knowledge.json` was rejected because its file notes/subagent reports have different scope and lifecycle. A workspace-side memory file was rejected because the agent would mutate the user's repository and require trust/approval. Silent LRU eviction was rejected because it violates reliable recall.

### D11. Capture durable memory through a hidden final-answer trailer

**Decision**: Add one fixed, byte-stable output-contract paragraph permitting a final answer to end with a reserved `<project-memory>` JSON trailer containing at most four decision/finding candidates. A completion-boundary observer strips a valid reserved trailer from user-visible/body content, validates strict fields and bounds, rejects secrets or malformed candidates, and appends accepted ledger events. Intermediate turns, reasoning, tool output, and arbitrary prose cannot create memory. Explicit `/memory remember|list|forget|clear` and equivalent CLI/line-mode commands provide user control and fallback.

**Rationale**: This supplies automatic, structured capture without a new agent tool, auxiliary model call, routing change, semantic index, or prose heuristic. It does not alter turn budgets or tool execution; it only observes reserved metadata on the existing final response.

**Alternatives considered**: A `record_memory` tool was rejected by the no-tool-change constraint and would change the stable tool surface. An auxiliary extraction call was rejected because it changes request count, cost, and routing/cache behavior. Prose inference was rejected as unreliable and likely to persist transient content. Compaction-only extraction was rejected because short sessions may never compact.

### D12. Follow the Reasonix boot-snapshot/tail-update pattern

**Decision**: Add a typed per-session `project_context.json` sidecar containing canonical workspace identity, exact rendered boot context, instruction hash/state, base memory sequence, and the deterministically sorted skills snapshot. New sessions compile current instructions/memory/skills into the stable boot prefix. Resume restores the exact snapshot first. Changes detected after boot ride exactly once on the newest user turn as `<project-instructions-update>` or `<memory-update>` and then become settled append-only history. Update the feature 002 closed tail allow-list and its budget/stability tests.

**Rationale**: Reasonix compiles memory into the stable prefix once and places mid-session changes in a one-shot tail block. This preserves cache warmth, avoids history rewrites, and makes resume deterministic even if workspace files changed. It also fixes the current skill-resume drift class.

**Alternatives considered**: Rebuilding the system prompt mid-session was rejected because it invalidates the prefix and violates Principles III/IV. Repeating full project context on every user message was rejected as redundant retransmission. Ignoring file/memory changes until a new session was rejected because resumed sessions must reflect current project context.

### D13. Keep prompt/model/tool boundaries intact and verify the one-time upgrade reset

**Decision**: The agent loop, registered tool definitions, active/subagent model selection, gateway routing, and task budgets remain unchanged. The stable prompt receives only the fixed memory-output contract and boot project-context block, causing one documented upgrade-boundary prefix reset. Identical session snapshots render byte-identically; timestamps/provenance remain on disk and are omitted from prompt rendering. Tail updates have deterministic key/sequence order and never cause an invalidation event because they are normal append-only growth.

**Rationale**: This is the minimum prompt change required for memory while retaining all established cache guarantees after the upgrade boundary.

**Alternatives considered**: Dynamic prefix content, per-turn memory summaries, and provider-specific structured-output APIs were rejected for determinism, retransmission, and compatibility reasons.

### D14. Add deterministic terminalbench plus real-terminal evidence

**Decision**: Create `benchmarks/terminalbench` with a fixed-seed generator that populates all real resume surfaces (SQLite events, transcript JSONL, realistic bounded model history, knowledge/usage/invalidation sidecars) for 10- and 5,000-event workloads. Record input/stream percentiles, resume readiness, render count/bytes, process CPU seconds, Go heap, OS WorkingSet/RSS, fixture hash, build/machine metadata, and cache benchmark links. Run identical baseline/improved commands. Add row-hash frame tests and manual/PTY recordings in Windows Terminal plus representative macOS/Linux terminals.

**Rationale**: Direct tests give reproducible fast gates; OS/process metrics and real-terminal recordings are needed for the success criteria that unit snapshots cannot prove. The current dirty working tree must not be used as a baseline; use clean worktrees at recorded commits.

**Alternatives considered**: Go heap alone was rejected because it cannot prove the process working-set limit. Visual inspection alone was rejected because latency/memory claims must be reproducible. Synthetic single-shot input was rejected by Constitution X.

## Resolved Unknowns

- **TUI dependency**: existing Bubble Tea stack; no new runtime dependency.
- **Mouse mode**: cell motion with frame-owned hit testing and in-app copy.
- **Paste representation**: segmented composer; no textarea sentinel.
- **Transcript source**: existing SQLite events with keyset pagination.
- **Idle strategy**: no permanent tick; lifecycle-scoped commands only.
- **Project instruction scope**: root-only `MUHIYA.md`, canonical and contained, 32 KiB.
- **Memory capture**: hidden structured final-answer trailer plus explicit controls; no tool or auxiliary call.
- **Prompt placement**: stable boot snapshot, one-shot deterministic tail updates, per Reasonix.
- **Verification**: terminalbench + existing cachebench + cross-platform real-terminal matrix.
