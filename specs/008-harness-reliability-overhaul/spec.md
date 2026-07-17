# Feature Specification: Harness Reliability & Clarity Overhaul

**Feature Branch**: `008-harness-reliability-overhaul`

**Created**: 2026-07-14

**Status**: Draft

**Input**: User description: "Harness reliability & clarity overhaul for MuhiyaCode
(feature 008): model-switch cost warning, DeepSeek-aligned subagent orchestration,
honest token display, and a Claude-Code-inspired /context & usage redesign. Changing
the model mid-session silently cold-starts the per-model provider cache (a real
session's only invalidation and most of its uncached spend came from one flash→pro
switch) — warn before applying, with proceed/cancel. The agent skips subagents most
of the time even when needed (a max-effort website overhaul ran 32 main-loop
requests, 0 subagents); orchestration must make the model actually delegate when
appropriate, with clear instructions compensating DeepSeek's weaknesses (long-horizon
reasoning, execution drift, instruction-following, applying changes correctly), and
reduce errors, incorrect results, and token-burning loops — research the current
orchestration first, comprehensive plan before implementation, no regressions
(especially byte-stable prefix-cache discipline). Show only the normal (non-cached)
token count plus cache-hit percentage in the live activity line and task summary;
cached-token detail stays in /context. Redesign /context and session usage inspired
by Claude Code's designs (screenshots provided): cost, API vs active time, lines
added/removed, per-model breakdown, cache hit %, and a usage-by-category context
table — adapted to MuhiyaCode's visual system, not copied. Client-side only."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The agent delegates like a senior engineer (Priority: P1)

A developer gives MuhiyaCode a large task at high or max effort — a multi-file
overhaul, a broad audit, or a change that needs independent verification. Instead of
grinding through everything in one ever-growing main conversation (32 requests, zero
helpers, in a real session), the agent recognizes the parallelizable, exploratory,
and verification-shaped parts and hands them to subagents within its effort
allowance — keeping the main conversation small, the results independently checked,
and the work finished with fewer wrong turns. The guidance that produces this
behavior is written for the model actually being driven (DeepSeek), compensating its
known weak spots: it drifts on long tasks, follows loose instructions imperfectly,
and mis-applies changes without explicit, redundant direction and verification.

**Why this priority**: This is the core value of the feature — fewer errors, fewer
incorrect results, less wasted token spend on runaway main-loop growth. Every other
story is display or guard-rail polish around it.

**Independent Test**: Run the same scripted delegation-appropriate workload (multi-
file overhaul with independent sub-parts) at max effort before and after the change,
with model, gateway, and effort held constant. Delivers value alone: the after-run
uses subagents for the independent parts, completes correctly, and does not grow the
main conversation to the before-run's size.

**Acceptance Scenarios**:

1. **Given** a max-effort task containing independent, parallelizable sub-parts,
   **When** the agent plans its approach, **Then** it delegates at least the
   independent exploration/verification portions to subagents (subagent runs > 0)
   instead of doing everything in the main loop.
2. **Given** a low-effort quick fix (single file, single concern), **When** the agent
   works, **Then** it does NOT spawn subagents merely because they are allowed —
   delegation follows the task's shape, not the allowance.
3. **Given** a delegated sub-task's report arrives, **When** the main agent
   continues, **Then** it uses the report's findings (no re-doing the delegated work
   in the main loop).
4. **Given** the model attempts an edit that does not match the file (a known
   DeepSeek failure mode), **When** the harness detects the mismatch, **Then**
   corrective guidance is applied and the task recovers without entering a
   token-burning retry loop (bounded retries, then a clear stop with reasons).
5. **Given** the orchestration guidance changes, **When** two consecutive turns are
   compared at the wire level, **Then** the stable prompt prefix remains
   byte-identical across turns (the guidance is part of the fixed prompt, never
   per-request variance).

---

### User Story 2 - No surprise bills from a mid-session model switch (Priority: P2)

A developer working in a long session switches the model (e.g. from the cheap fast
model to the expensive reasoning model) to get better answers on a hard problem.
Before the switch applies, MuhiyaCode tells them plainly what it will cost them: the
provider's cache is per-model, so the whole warmed context must be re-read cold on
the new model, and the new model's prices may be several times higher. The developer
can proceed or cancel. Choosing a model when nothing has been sent yet (fresh
session) stays frictionless with no warning.

**Why this priority**: Direct money protection with tiny scope. A real session
showed one mid-session switch caused most of the session's uncached spend.

**Independent Test**: In a session with at least one completed turn, pick a
different model and observe the warning with proceed/cancel; cancel leaves
everything unchanged; proceed applies the switch. In a fresh session the same action
shows no warning.

**Acceptance Scenarios**:

1. **Given** a session with at least one completed model turn, **When** the user
   selects a different model, **Then** a warning states the cache will restart cold
   on the new model and pricing may differ, and asks proceed/cancel.
2. **Given** the warning is shown, **When** the user cancels, **Then** the active
   model, session state, and settings are unchanged.
3. **Given** the warning is shown, **When** the user proceeds, **Then** the model
   switches exactly as it does today (including the existing cache-invalidation
   event record).
4. **Given** a fresh session with no completed turns, **When** the user selects a
   model, **Then** no warning appears.
5. **Given** the user re-selects the currently active model, **When** the choice is
   confirmed, **Then** no warning appears and nothing changes.

---

### User Story 3 - The token number means what it costs (Priority: P3)

While a task runs (the live status under the thinking text) and when it finishes
(the task summary line), the developer sees a token figure that reflects what they
actually pay for: the non-cached tokens (fresh input the provider billed at full
price, plus generated output), alongside the cache-hit percentage. Cache-read tokens
— which cost ~50× less — no longer inflate the headline number. The full detail
(cache read, uncached, per-stream splits) remains one `/context` away.

**Why this priority**: Users read the current combined number as spend and find it
misleading; this is a small, high-trust fix but purely display.

**Independent Test**: Run a task whose provider usage reports cache hits; the live
line and summary show the non-cached figure + hit %, and `/context` still shows the
full split. Numbers agree with provider-reported usage exactly.

**Acceptance Scenarios**:

1. **Given** a completed turn whose usage reports cache-hit and cache-miss tokens,
   **When** the live status renders, **Then** its headline token count excludes
   cache-read tokens (non-cached input + output only) and shows the cache-hit
   percentage.
2. **Given** a finished task, **When** the summary line renders, **Then** its token
   figure uses the same non-cached definition and matches `/context`'s uncached
   total for the task.
3. **Given** a provider/turn without cache metrics, **When** the status renders,
   **Then** the count falls back to the full reported total and the cache tag shows
   unavailable — never a fabricated split.
4. **Given** any task, **When** the user opens `/context`, **Then** cache read,
   uncached, and per-stream detail are still fully visible.

---

### User Story 4 - A session report worth reading (Priority: P4)

The developer opens the context/usage view and gets a clear, scannable session
report in MuhiyaCode's own visual style, inspired by the clarity of leading tools:
what the session cost, how much time was spent waiting on the model versus total
task time, how many lines were added and removed, which models were used and in what
share, the cache-hit rate, and a per-model breakdown (input, output, cache read,
cost). The context view additionally shows where the context window actually goes —
an estimated by-category table (system prompt, tool definitions, project
instructions/memory, conversation, summary, free space) with tokens and percentages.

**Why this priority**: High daily-use value, but purely presentational and safe to
land last — it reads existing data.

**Independent Test**: After a session with at least two tasks (ideally two models),
open the usage view and verify every figure against provider-reported usage and
existing session records; the category table's parts sum to the context window.

**Acceptance Scenarios**:

1. **Given** a session with completed tasks, **When** the usage panel renders,
   **Then** it shows session cost (or the honest unavailable/priced-requests note),
   model-wait time vs total active task time, cumulative lines +added/−removed from
   applied changes, cache-hit rate, and a per-model table (input, output, cache
   read, cost) with a total row.
2. **Given** a session that used two models, **When** the per-model table renders,
   **Then** each model has its own row and the shares/totals are consistent with the
   sum of its recorded requests.
3. **Given** the context view opens, **When** the by-category table renders,
   **Then** categories (system prompt, tools, project instructions/memory,
   conversation, compact summary, free space) each show tokens and a percentage, and
   together account for the whole context window, with estimated figures labeled as
   estimates.
4. **Given** any metric whose source is unavailable (no provider cost, no cache
   fields), **When** the panel renders, **Then** that metric shows an explicit
   unavailable state rather than a guessed number.

---

### User Story 5 - Memory that feels like a first-class ability (Priority: P5)

When the agent learns something durable — a project convention, a decision, a user
preference — it saves it through a dedicated memory tool, and the transcript shows a
distinct, polished "Memory" entry (like leading agent tools do), not a generic
file-write row. The saved fact lands in the existing durable memory file (format
unchanged, still human-editable), is immediately known to the agent for the rest of
the session, and loads automatically next session. The agent is taught to use this
tool — sparingly and for durable facts only — instead of hand-editing the memory
file with normal file tools.

**Why this priority**: High perceived quality ("cool like Claude Code") and better
memory hygiene, but additive — no other story depends on it.

**Independent Test**: Ask the agent to remember a preference; observe the distinct
Memory row in the transcript (not a Write row), verify the memory file gained a
clean entry, restart the session, and confirm the agent knows the fact.

**Acceptance Scenarios**:

1. **Given** the agent learns a durable fact, **When** it saves it, **Then** it uses
   the dedicated memory tool (not a raw file write) and the transcript renders a
   distinct Memory entry showing what was saved.
2. **Given** a memory save completes, **When** the session continues, **Then** the
   agent treats the fact as known (the in-session memory view updates without
   re-reading files), and after a restart the fact loads with project context.
3. **Given** the memory file does not exist yet, **When** the first memory is
   saved, **Then** the file is created in the established format; an identical
   duplicate save changes nothing.
4. **Given** the session's permission mode requires approval for mutations,
   **When** a memory save is attempted, **Then** it follows the same approval flow
   as other mutations (no security weakening), while still rendering as a Memory
   entry.
5. **Given** the memory file already has entries, **When** a save lands, **Then**
   existing content and format are preserved (the file stays human-readable and
   hand-editable).

---

### Edge Cases

- Re-selecting the already-active model, or switching in a session with zero
  completed turns: no warning, no state change beyond the normal selection.
- Switching the subagent model (not the main model) mid-session: the same warning
  dialog names the subagent model as the affected one; canceling changes nothing.
- Multiple model switches in one session: each is warned, each recorded; the usage
  panel shows one row per model actually used.
- A task interrupted mid-stream (estimated usage): headline figures for that task
  are labeled estimated; estimated requests are counted in the "unpriced/estimated"
  note, never silently mixed into exact totals.
- Provider without cache metrics (generic gateway): headline falls back to the full
  reported total with the cache tag unavailable; delegation and displays must not
  assume DeepSeek-only fields exist.
- Subagent allowance exhausted mid-task: the agent finishes remaining work in the
  main loop and the summary reflects the denied runs — no retry loop against the
  denial.
- A delegated subagent fails or returns an unusable report: the main agent absorbs
  the failure with bounded recovery (it may finish that part itself) — one failed
  delegate must not cascade into repeated re-delegation of the same sub-task.
- All subagent slots used but the task turns out simple: delegation criteria are
  shape-based, so a trivially linear task at max effort legitimately uses zero
  subagents — the success measure is appropriateness, not raw count.
- Very narrow terminals: the new tables must degrade (wrap/truncate) without
  corrupting the layout.
- Lines +/− when an edit is later reverted by another edit: counts reflect the sum
  of applied changes (a revert adds to both sides); failed tool calls contribute
  nothing.
- The agent rewrites a whole existing file when a two-line change was needed: the
  edit-discipline guidance and the benchmark audit must catch this (whole-file
  writes are reserved for new files or genuine full regeneration).
- A memory save with content identical to an existing entry: no duplicate is
  appended; the tool reports it as already known.
- A memory save while the memory file's content is already in the boot context: the
  existing one-shot in-session update mechanism carries the change without
  re-sending the boot block (prefix stays stable).
- Oversized memory content (pasted walls of text): the tool enforces a sane entry
  size and asks for a concise fact instead — memory loads into every session's
  context, so bloat is a standing cost.

## Requirements *(mandatory)*

### Functional Requirements

**Delegation & orchestration reliability (US1)**

- **FR-001**: The orchestration MUST give the model explicit, unambiguous delegation
  criteria — when to hand work to subagents (independent/parallelizable exploration,
  broad reads, verification of applied changes) and when not to (small linear tasks)
  — phrased to be reliably followed by the actually-driven model family, and these
  criteria MUST live in the fixed instruction prefix (one-time upgrade), never in
  per-request text.
- **FR-002**: At high and max effort, for tasks whose shape contains independent
  sub-parts, the agent MUST actually use its subagent allowance for those parts (the
  before-case — 32 main-loop requests, 0 subagents on a max-effort overhaul — must
  not reproduce on the benchmark workload).
- **FR-003**: At low effort or for single-concern linear tasks, the agent MUST NOT
  delegate gratuitously; delegation decisions follow task shape, not allowance.
- **FR-004**: The harness MUST compensate documented weaknesses of the driven model
  family with explicit guidance and verification: (a) applied-change verification
  after edits the model is prone to fumbling, (b) redundant, imperative phrasing for
  instructions it tends to drop, (c) drift-arresting reminders anchored to the
  task's stated goal — all within the byte-stable prefix discipline.
- **FR-005**: Failure handling MUST be bounded: a failing edit/tool pattern gets
  corrective guidance and a limited number of retries, then a clear stop with the
  reason — never an unbounded retry loop (the existing repeated-failure terminator
  and turn budgets remain in force; no token-count ceiling may be reintroduced).
- **FR-006**: A delegated sub-task's report MUST be consumed by the main loop (its
  findings referenced, its files not redundantly re-read wholesale) — measured on
  the benchmark by the absence of duplicate full re-exploration of delegated scopes.
- **FR-007**: Every orchestration-guidance change MUST keep the stable prompt prefix
  byte-identical across consecutive turns at steady state, verified by the existing
  prefix-stability checks; the change itself is a single recorded cache-epoch
  upgrade.

**Edit discipline (US1)**

- **FR-018**: The guidance MUST direct the model to make surgical edits to existing
  files (targeted single/batched edits) rather than rewriting whole files; whole-file
  writes are reserved for creating new files or genuine full regeneration. The
  guidance lives in the fixed prefix; compliance is audited on the delegation
  benchmark (zero whole-file rewrites of existing files where a targeted edit
  sufficed).

**Model-switch cost warning (US2)**

- **FR-008**: A model change (main or subagent) requested in a session with at least
  one completed model turn MUST present a warning before applying, stating: the
  provider cache restarts cold on the new model, and pricing may differ; with
  explicit proceed/cancel.
- **FR-009**: Cancel MUST leave settings, session, and cache state untouched;
  proceed MUST apply the change exactly as today (including the existing
  invalidation-event record).
- **FR-010**: No warning appears when: the session has no completed turns, the
  selected model equals the current one, or the selection happens during initial
  session setup.

**Honest token display (US3)**

- **FR-011**: The live activity status and the end-of-task summary MUST show, as
  their headline token figure, the provider-reported non-cached tokens (uncached
  input + output) for the task, plus the cache-hit percentage when cache metrics are
  available.
- **FR-012**: Cache-read token counts MUST NOT be folded into any headline figure;
  they remain visible in the context view (cache read, uncached, per-stream splits
  unchanged).
- **FR-013**: When cache metrics are unavailable for a task, the headline MUST fall
  back to the full reported total and mark cache state unavailable — figures are
  provider-reported or labeled estimated, never synthesized.

**Session usage & context redesign (US4)**

- **FR-014**: The session usage view MUST show: total session cost (with the
  existing honest unavailable/unpriced-requests fallback), cumulative model-wait
  time vs total task time, cumulative lines added/removed from applied file changes,
  overall cache-hit rate, and a per-model breakdown table (uncached input, output,
  cache read, cost per model, plus a total row) covering every model used in the
  session across main and subagent streams.
- **FR-015**: The context view MUST show an estimated usage-by-category breakdown of
  the current context window — at minimum: system prompt, tool definitions, project
  instructions/memory, conversation messages, compact summary (when present), and
  free space — each with tokens and percentage, summing to the window size, with
  estimates labeled.
- **FR-016**: All new displays MUST use MuhiyaCode's existing visual system (palette
  tokens, glyph table, alignment rules) and degrade gracefully on narrow terminals;
  the designs are inspirations, not copies.
- **FR-017**: Figures shown in the live line, task summary, usage panel, and context
  view MUST be mutually consistent — one source of truth per metric, no view showing
  a number another view contradicts.

**First-class memory (US5)**

- **FR-019**: The agent MUST have a dedicated memory-save tool, distinct from the
  generic file tools, that records a durable fact into the established memory file
  (existing format preserved, file remains human-editable); the prompt guidance
  MUST direct memory writes through this tool instead of hand-editing the file.
- **FR-020**: Memory-tool activity MUST render in the transcript as a distinct
  Memory entry (its own label and collapsed summary of what was saved), never as a
  generic file-write row; a completed save MUST be reflected in the agent's
  in-session view of memory via the existing update mechanism (no boot-block
  resend, no full-file re-read).
- **FR-021**: The memory tool MUST respect the session's mutation-approval flow
  exactly like other mutating tools, MUST de-duplicate identical entries, MUST
  create the file in the established format when absent, and MUST bound entry size
  (rejecting oversized content with guidance to summarize).

### Key Entities

- **Delegation Guidance**: The fixed-prefix instruction block defining when/how the
  model delegates; versioned as a one-time prompt upgrade (cache epoch).
- **Task Shape**: The classification of a task (linear vs containing independent
  sub-parts) that drives whether delegation is appropriate; derived from the
  existing task-class assessment.
- **Model Switch Event**: A requested model change with prior-turn context: old
  model, new model, warned/confirmed/cancelled outcome, and the existing
  invalidation record on proceed.
- **Billed-Token Figure**: The headline metric — provider-reported uncached input +
  output for a task; distinct from total processed tokens (which include cache
  reads).
- **Session Usage Ledger**: Per-request records (already captured) aggregated per
  model and per stream: uncached input, output, cache read, cost, estimated flags,
  request durations.
- **Context Category Estimate**: A labeled-estimated split of the current window
  (system prompt, tools, project memory, messages, summary, free) produced by the
  existing calibrated estimator.
- **Memory Entry**: One durable fact saved through the memory tool into the
  established memory file: concise content, deduplicated, size-bounded, rendered in
  the transcript as a distinct Memory item and reflected in-session via the
  existing update mechanism.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On the scripted delegation benchmark (a max-effort multi-file overhaul
  with independent sub-parts, matching the real 32-request/0-subagent session's
  shape), the after-build completes the task correctly using at least 2 subagent
  runs, and the main conversation's final size (prompt tokens at task end) is at
  least 25% smaller than the before-build's on the identical workload.
- **SC-002**: Across the before/after benchmark pair (model, gateway, effort,
  workload held constant), billed (non-cached) tokens for the after-run do not
  exceed the before-run's by more than 10%, and steady-state cache-hit rate shows
  no regression.
- **SC-003**: On a scripted low-effort single-file task, the after-build uses zero
  subagents (no gratuitous delegation).
- **SC-004**: 100% of mid-session model changes in test scenarios show the warning
  first; 100% of cancels leave state byte-identical; fresh-session and same-model
  selections show zero warnings.
- **SC-005**: For every task with provider cache metrics, the headline figure equals
  provider-reported uncached input + output exactly (0 discrepancy), the cache-hit
  percentage matches the provider-reported ratio, and `/context` retains the full
  split; for tasks without cache metrics, the fallback total equals the provider-
  reported total.
- **SC-006**: The usage panel renders all FR-014 metrics for a two-model session,
  every figure reconciling exactly with the session's recorded usage (or showing an
  explicit unavailable state); the context view's category percentages sum to 100%
  (±1 point rounding) of the window.
- **SC-007**: The complete existing test suite (all packages) and the prefix-
  stability/marshal-determinism checks pass unchanged after the feature; the
  one-time prompt upgrade is the only recorded cache epoch.
- **SC-008**: On the delegation benchmark, zero whole-file rewrites of existing
  files occur where a targeted edit sufficed (audited from the run's tool calls:
  file-creating writes and genuine full regenerations are the only whole-file
  writes).
- **SC-009**: Saving a memory renders as a distinct Memory entry (not a file-write
  row) in 100% of saves; the fact survives a session restart and loads with project
  context; the memory file remains valid in the established format after 10
  consecutive tool-driven saves mixed with manual edits.

## Out of Scope

- Gateway/server changes of any kind (client-repo work only).
- Re-introducing any per-task token ceiling (explicitly removed and staying
  removed).
- New subagent kinds or changes to subagent tool surfaces beyond delegation
  guidance and result-consumption behavior.
- Changing effort→allowance numbers (low 1 / medium 2 / high 4 / max 8 stay).
- Live per-token cost estimation on the client (cost remains gateway-reported).
- Copying Claude Code's UI verbatim (its designs inform layout and content only).
- A separate memory-READ tool or memory browser UI: memory already auto-loads with
  project context at session start and updates in-session via the existing
  mechanism; only the save path becomes a first-class tool.
- Changing the memory file's location or format (feature 006 compat boundary).

## Assumptions

- "Normal (non-cached) token count" = provider-reported cache-miss input tokens +
  completion tokens, summed over the task's requests. When cache fields are absent,
  the headline falls back to the provider-reported total (labeled, cache tag
  unavailable). Estimated-usage requests render with the existing estimate marker.
- Model-wait time ("API time") = the summed wall-clock duration of provider requests
  in the session; total task time ("active time") = summed task start→finish
  durations. Both derive from timestamps the client already has; idle time between
  tasks counts toward neither.
- Lines added/removed aggregate the per-tool diff counts of applied (successful)
  edit/write/patch calls across the session, as already computed for tool rows.
- Per-model cost rows use the gateway's per-request cost reports; requests without a
  cost report are counted and disclosed via the existing "N of M requests priced"
  honesty pattern.
- The context-category table uses the existing calibrated token estimator and is
  labeled estimated; exact per-category tokenization is not required.
- Delegation benchmarks reuse the constitution's Measurement & Benchmarking
  Standards and the canonical multi-turn workload approach from features 001/002,
  extended with a delegation-appropriate scripted workload checked into the
  feature's benchmarks directory.
- DeepSeek-weakness compensations are grounded in the documented behaviors already
  cataloged in this repo (tool-call rescue for DSML drift, empty-`reasoning_content`
  tool-call turns, edit-mismatch failure modes seen in session records) plus the
  research phase's findings; the reference architecture (Reasonix) is consulted per
  the constitution before prompt changes.
- The screenshots provided by the user (session breakdown; usage-by-category
  context table) serve as layout inspiration; MuhiyaCode's palette, glyphs, and
  RTL-safe rendering rules apply unchanged.
