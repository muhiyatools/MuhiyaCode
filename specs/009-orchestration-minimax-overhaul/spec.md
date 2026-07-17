# Feature Specification: Enforced Orchestration Pipeline & MiniMax Provider Integration

**Feature Branch**: `009-orchestration-minimax-overhaul`

**Created**: 2026-07-14

**Status**: Draft

**Input**: User description: "The system still uses only the main model without
creating sub-agents. Enforce a phased orchestration flow: multiple research
sub-agents first, then the main agent writes a highly detailed, research-backed
implementation plan to a .md file (clear enough that a cheaper model like DeepSeek
executes it with results comparable to a more expensive model), then additional
sub-agents execute the plan's parts, with validation and review. All sub-agents use
the configured sub-agent model. Enforce via system prompt AND harness whenever the
task is complex enough; only genuinely simple requests run direct. Match Claude
Code's reliability and orchestration quality: accurate routing between agents, clear
responsibilities, correct context/instructions/tools/output-format per sub-agent,
and structured transfer of results between agents (only the relevant information).
Complete high-quality overhaul without unnecessary complexity or breaking existing
functionality. Additionally: full production-ready MiniMax provider support in the
Go proxy gateway (F:\\MuhiyaWorkspace\\MuhiyaWorkspace) per the official docs
(text generation, M3 function calling, prompt caching) with caching optimized to the
DeepSeek standard; correct routing by provider/model/main-vs-sub agent; correct
preservation of prompts, tool calls, results, and agent context in MiniMax's format.
Analyze MiniMax's Mini-Agent reference CLI (C:\\Users\\mydwa\\Downloads\\
Mini-Agent-main) fully as a reference. DeepSeek and MiniMax must work perfectly
together."

## Clarifications

### Session 2026-07-14

- Q: How deep should the enforced pipeline run for different task sizes? → A:
  Scaled depth — standard tasks run a light pipeline (research sub-agent(s) + plan
  artifact mandatory; implementation may remain in the main conversation);
  large/epic tasks run the full four phases (research → plan → implementation
  sub-agents → validation/review). Effort scales counts within each phase.
- Q: After the plan artifact is written, pause for user approval or proceed
  automatically? → A: Always pause — every pipeline-required task stops after the
  plan is written and waits for the user's go-ahead (approve / steer / cancel)
  before any implementation begins.
- Q: Default model posture once MiniMax is integrated? → A: First-time (fresh)
  setups default to MiniMax-M3 as the MAIN model and DeepSeek V4 Pro as the
  sub-agent model (graceful fallback to today's DeepSeek defaults when MiniMax is
  not available on the gateway). Existing configured settings are never changed.
  Design principle recorded: the system stays simple and logical while the
  orchestration flow itself is ultra-detailed.
- Q: Should the user have a per-task override of the pipeline decision? → A: No —
  the decision is professional and internal, like Claude Code. The agent always
  orchestrates with sub-agents when the task needs a plan; per-phase sub-agent
  COUNTS scale by effort ONLY; the only direct path is a task that needs no plan,
  where the agent simply answers without entering the agent flow. No override
  surface (per-task or global) is added.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Complex work runs a real pipeline, not a monologue (Priority: P1)

A developer gives MuhiyaCode a complex task (multi-file feature, audit, overhaul).
Instead of one long main-model monologue, the harness drives a phased pipeline:
**Research** — parallel sub-agents investigate the relevant scopes; **Plan** — the
main agent synthesizes their findings into a detailed implementation plan written to
a Markdown file; **Implement** — sub-agents execute the plan's independent parts;
**Validate & Review** — sub-agents verify the changes and review the diff before the
final answer. For tasks the system judges complex, this flow is *enforced by the
harness*, not merely suggested: implementation cannot begin before a plan artifact
grounded in completed research exists AND the user has approved it (the pipeline
always pauses at the plan). Genuinely simple requests (a question, a
one-line fix) skip orchestration entirely and run direct — no ceremony where none is
warranted. Every sub-agent runs on the configured sub-agent model.

**Why this priority**: This is the user's core frustration. The measured baseline
(feature 008's live before-leg) showed a four-module, max-effort task completing
with ZERO sub-agent runs, and prompt-level inducement alone leaves delegation to
the model's discretion — a discretion the target model family exercises poorly.
Harness enforcement, with a simplicity escape hatch, removes that dependence and
is the product's headline behavior change.

**Independent Test**: Run the existing delegation benchmark (max effort, four
independent modules): observe research sub-agents before any file edit, a plan file
on disk before implementation, implementation sub-agents for independent parts, and
a validation/review pass — then run the simple control task and observe zero
orchestration. Delivers value alone: complex tasks become structured and reviewable.

**Acceptance Scenarios**:

1. **Given** a task classified complex, **When** the agent starts working, **Then**
   research sub-agents run before any file-changing tool call, and a plan artifact
   exists on disk before the first implementation edit.
2. **Given** the plan is written, **When** the pipeline reaches the approval
   pause, **Then** no implementation occurs until the user approves (or steers/
   cancels); **and given** approval on a large/epic task, implementation work is
   performed by sub-agents for the plan's independent parts (within the effort
   allowance), while on a standard task research + plan + approval remain
   mandatory and implementation may proceed in the main conversation (scaled
   depth).
3. **Given** implementation completes on a complex task, **When** the task
   approaches its final answer, **Then** a validation/review sub-agent pass has run
   and its verified findings were addressed or explicitly reported.
4. **Given** a genuinely simple request (question, single-concern small fix),
   **When** the agent works, **Then** no sub-agents and no plan file are created and
   the answer is direct.
5. **Given** any pipeline phase, **When** a sub-agent is launched, **Then** it runs
   on the configured sub-agent model (never silently the main model).
6. **Given** the pipeline is enforced, **When** a phase cannot proceed (research
   returns nothing usable, no allowance left), **Then** the harness degrades
   gracefully to direct work with the reason recorded — never a stuck task.

---

### User Story 2 - A plan a cheaper model can execute like an expensive one (Priority: P2)

The plan produced between research and implementation is a durable Markdown artifact
in the workspace: research-backed findings with exact file/function references,
ordered implementation steps with per-step acceptance checks, risks, and
verification commands. It is written so a cheaper executor model can follow it
mechanically and reach results comparable to a premium model doing the whole task —
the plan carries the intelligence so execution doesn't have to. The file persists
across session restarts and is resumable.

**Why this priority**: The plan artifact is the load-bearing wall of the pipeline —
enforcement (US1) is only valuable if what's enforced produces execution-grade
plans.

**Independent Test**: On the benchmark task, inspect the generated plan file:
grounded references for every step, per-step acceptance checks, and completeness
such that executing ONLY the plan's steps passes the task's full expected-outcome
checklist.

**Acceptance Scenarios**:

1. **Given** a complex task's plan phase completes, **When** the plan file is
   inspected, **Then** every implementation step names its exact target
   (file/function/scope), is grounded in a cited research finding, and carries an
   observable acceptance check.
2. **Given** the plan exists, **When** implementation sub-agents execute only what
   the plan specifies, **Then** the task's expected-outcome checklist passes in
   full.
3. **Given** a session restart mid-pipeline, **When** the user resumes, **Then**
   the plan file and phase progress are recovered and the pipeline continues from
   where it stopped.

---

### User Story 3 - Handoffs are contracts, not vibes (Priority: P3)

Every sub-agent launch carries a clear contract: its role, only the context it needs
(including the relevant, structured extracts of prior phases' outputs — never a raw
transcript dump), the tools appropriate to its role, and the exact output format
expected back. Every sub-agent's result is returned in that structured form and the
next consumer receives only the relevant portion. The developer can always see, in
the session, which agent is running, on which model, in which phase, and what it
was asked to deliver.

**Why this priority**: Routing and handoff fidelity is what makes the pipeline
*accurate* rather than merely busy — it is also what keeps token costs sane.

**Independent Test**: Audit the benchmark run's transcripts: each sub-agent prompt
contains role + scoped context + deliverable format; each dependent phase's input
embeds the predecessor's structured summary; no phase wholesale re-reads a scope a
predecessor covered.

**Acceptance Scenarios**:

1. **Given** any sub-agent launch, **When** its prompt is inspected, **Then** it
   states the role, the deliverable, the expected output format, and only the
   context relevant to its scope.
2. **Given** a phase depends on an earlier phase's output, **When** its input is
   inspected, **Then** it contains the predecessor's structured summary (findings/
   steps relevant to it) rather than the predecessor's full conversation.
3. **Given** a running pipeline, **When** the user watches the session, **Then**
   each sub-agent's phase, role, and model are visible, and the final answer
   attributes what each phase contributed.
4. **Given** a sub-agent returns an unusable or failed result, **When** the
   pipeline continues, **Then** the failure is absorbed with bounded recovery (the
   part is re-scoped once or absorbed into direct work) — never silent loss of a
   plan part.

---

### User Story 4 - MiniMax as a first-class provider through the gateway (Priority: P4)

An operator adds MiniMax to the gateway and MuhiyaCode users can select MiniMax
models (main or sub-agent role) exactly like DeepSeek ones. Text generation,
streaming, function calling, and multi-turn tool workflows work correctly in
MiniMax's documented format; prompt caching is exploited with the same care as
DeepSeek's (stable prefixes, cached-token accounting, observable hit rates); usage,
cost, and cache metrics appear in the same per-request records, session panels, and
billing rows as DeepSeek's. The integration is robust (bounded retries, clean
error relay), secure (keys handled like existing providers), and observable.

**Why this priority**: Independent of the orchestration stories and deliverable on
its own; it unlocks the mixed-provider posture of US5.

**Independent Test**: A live conformance session through the gateway against a
MiniMax model — multi-turn with tool calls — completes with zero wire-shape
rejections; usage records show cached-token counts; a repeated-prefix probe shows
warm cache hits; billing rows price the traffic with MiniMax rates.

**Acceptance Scenarios**:

1. **Given** MiniMax models are configured, **When** a client requests one (by
   model or via main/sub-agent routing), **Then** the gateway routes to MiniMax
   with the request correctly shaped for its documented format (messages, tools,
   tool results, streaming).
2. **Given** a multi-turn tool-calling session, **When** tool calls and results
   round-trip, **Then** call IDs, arguments, and results are preserved exactly and
   the model's reasoning continuity is maintained per MiniMax's documented
   requirements.
3. **Given** a session with a stable prefix, **When** requests repeat the prefix,
   **Then** provider-reported cached-token counts appear in usage records and the
   steady-state hit behavior is comparable in quality to the DeepSeek integration.
4. **Given** MiniMax traffic, **When** billing and panels render, **Then** costs
   use MiniMax pricing (cached vs uncached input distinguished) and every metric
   lands in the same observability surfaces as DeepSeek's.
5. **Given** MiniMax is unavailable or rate-limits, **When** requests fail,
   **Then** errors relay cleanly (status, retry guidance) without breaking the
   session, matching the DeepSeek path's behavior.

---

### User Story 5 - DeepSeek and MiniMax, perfect together (Priority: P5)

A user runs the main agent on one provider and sub-agents on the other (e.g.
DeepSeek main + MiniMax sub-agents, or MiniMax M3 main + DeepSeek flash subs). The
pipeline works end-to-end across the mix: each stream routes to its configured
provider/model, each provider's prompt cache stays warm within its own streams,
capability differences are respected automatically, and the session's usage panel
attributes tokens/cost per model accurately across both providers.

**Why this priority**: The combination is the product's differentiator (best model
per role at the best price), but it depends on US1–US4 landing first.

**Independent Test**: Run the delegation benchmark with main and sub-agent models on
different providers: the pipeline completes correctly, per-model usage rows show
both providers with sane cache metrics, and neither provider's steady-state cache
behavior regresses versus its single-provider baseline.

**Acceptance Scenarios**:

1. **Given** main and sub-agent models on different providers, **When** the
   pipeline runs, **Then** every request routes to the correct provider for its
   stream and the task completes correctly.
2. **Given** the mixed run completes, **When** the usage panel renders, **Then**
   per-model rows show both providers' tokens, cache reads, and costs accurately.
3. **Given** repeated tasks in the mixed session, **When** cache metrics are
   compared to single-provider baselines, **Then** neither provider's steady-state
   hit behavior regresses (each stream's prefix stays byte-stable within its
   provider).

---

### Edge Cases

- Research finds nothing actionable (empty/failed research phase): the plan phase
  proceeds on direct investigation with the degradation recorded; the pipeline
  never deadlocks on an empty phase.
- The sub-agent allowance is smaller than the plan's independent parts: parts are
  batched/serialized within the allowance; the plan is not silently truncated.
- The user interrupts mid-phase (Esc) or steers mid-pipeline: the current phase
  stops cleanly; steering re-enters the pipeline at the appropriate phase rather
  than abandoning the plan.
- The user never responds at the plan-approval pause: the task ends cleanly with
  the plan persisted as pending; a later "proceed" resumes implementation from the
  approved plan (no work is lost, nothing implements without approval).
- An active autonomous goal reaches the plan-approval pause: the pause still
  applies (approval is universal); the goal resumes after the user's go-ahead.
- A borderline task (medium complexity): the classifier's decision is visible in
  the session (why orchestrated / why direct), so surprising routing is explicable.
- Plan file already exists from a previous task: the new task versions or clearly
  supersedes it — never silently overwrites an unrelated plan.
- The configured sub-agent model equals the main model: the pipeline still runs;
  "sub-agent model" is whatever is configured, identity with main is allowed.
- Sub-agent output exceeds reasonable handoff size: summaries are bounded; overflow
  goes to durable artifacts referenced by path, not into the next prompt wholesale.
- MiniMax's reasoning interleaving (thinking between tool calls) on replayed
  history: replay must preserve what the provider requires for continuity without
  breaking the byte-stable prefix discipline.
- MiniMax minimum cacheable size (short sessions below the threshold): cache
  metrics honestly show zero/unavailable rather than fabricated hits.
- A provider outage on ONE side of a mixed session: the affected stream fails with
  clean errors; the other provider's streams continue unaffected.
- Existing single-provider users: default behavior without MiniMax configured is
  byte-identical to today (no regression for DeepSeek-only deployments).

## Requirements *(mandatory)*

### Functional Requirements

**Enforced pipeline (US1)**

- **FR-001**: The system MUST decide, internally and automatically (no user
  override surface, per-task or global — clarified 2026-07-14), whether a task
  needs a plan: tasks that need no plan (conversational turns, trivial
  single-concern requests) are answered directly with no agent flow; every task
  that needs a plan enters the pipeline. The decision MUST be recorded and
  visible in the session.
- **FR-002**: For pipeline-required tasks, the harness MUST gate implementation on
  the pipeline order: no file-changing work in the main conversation before (a) a
  research phase using sub-agents has completed, (b) a plan artifact exists, and
  (c) the user has explicitly approved the plan (clarified 2026-07-14: the
  pipeline ALWAYS pauses after the plan is written — approve / steer / cancel —
  before any implementation begins); the gate is harness-enforced, not
  prompt-suggested.
- **FR-003**: The pipeline MUST comprise: research (parallel sub-agents per
  independent scope), planning (main agent synthesizes findings into the plan
  artifact), implementation (sub-agents execute the plan's independent parts
  within the effort allowance), and validation/review (sub-agent verification of
  changes) — with phase transitions recorded. Depth scales with task class
  (clarified 2026-07-14): standard tasks run the light pipeline (research
  sub-agent(s) and the plan artifact are mandatory; implementation may remain in
  the main conversation); large/epic tasks run all four phases with sub-agents at
  each phase. Per-phase sub-agent COUNTS scale by effort ONLY (clarified
  2026-07-14) — task size selects which phases are mandatory; effort selects how
  many sub-agents each phase may use.
- **FR-004**: Every sub-agent MUST run on the configured sub-agent model; the
  routing MUST be verifiable per request in the session's usage records.
- **FR-005**: Each phase MUST degrade gracefully when it cannot proceed (empty
  research, exhausted allowance, failed sub-agent): bounded recovery into direct
  work with the degradation reason recorded — the pipeline can never deadlock or
  silently drop plan parts.
- **FR-006**: Existing loop guards, effort allowances, denial texts, and the
  simple-task fast path MUST remain in force; the pipeline adds NO token ceiling
  and does not change effort→allowance numbers.

**Plan artifact (US2)**

- **FR-007**: The planning phase MUST write a durable Markdown plan in the
  workspace containing: research findings with exact references, ordered
  implementation steps each naming its target scope and an observable acceptance
  check, verification commands, and risks. The artifact MUST persist and be
  resumable across session restarts (phase progress included).
- **FR-008**: Plan quality MUST be execution-grade for a cheaper model: on the
  benchmark, implementation sub-agents following only the plan's steps pass the
  task's full expected-outcome checklist.
- **FR-009**: A new pipeline run MUST version or clearly supersede a pre-existing
  plan file, never silently overwrite unrelated content.

**Structured handoffs & routing visibility (US3)**

- **FR-010**: Every sub-agent launch MUST carry a structured contract: role,
  deliverable, expected output format, and only the context relevant to its scope;
  contracts MUST be inspectable in session records.
- **FR-011**: Cross-phase data flow MUST pass structured, bounded summaries of
  predecessor outputs (with durable artifacts referenced by path when large) —
  never full transcripts; consumers MUST NOT wholesale re-explore scopes a
  predecessor covered (measured via the duplicate-read audit).
- **FR-012**: The session UI MUST show, for each running sub-agent: phase, role,
  and model; the final answer MUST attribute the pipeline's phases.

**MiniMax provider (US4)**

- **FR-013**: The gateway MUST support MiniMax as a provider: model records for the
  current documented models with real context/output limits and pricing
  (cached vs uncached input distinguished), request shaping per MiniMax's
  documented format for text generation, streaming, function calling, and
  multi-turn tool workflows.
- **FR-014**: Tool-call round-trips MUST preserve call IDs, argument payloads, and
  results exactly; replayed history MUST satisfy MiniMax's documented continuity
  requirements for reasoning models.
- **FR-015**: Prompt caching MUST be exploited to the DeepSeek standard: stable
  prefixes preserved end-to-end, provider-reported cached-token counts recorded in
  per-request usage records and surfaced in the same panels/billing as DeepSeek's,
  with honest zero/unavailable states below the provider's cacheable threshold.
- **FR-016**: Provider errors and rate limits MUST relay cleanly with the same
  bounded-retry and error-surface behavior as the DeepSeek path; MiniMax traffic
  MUST be as observable (logs, usage rows, billing) as DeepSeek's.
- **FR-017**: Reasoning/thinking controls for MiniMax MUST be normalized
  gateway-side to MiniMax's documented forms (no raw client value passthrough),
  consistent with the existing per-provider normalization policy.
- **FR-018**: Deployments without MiniMax configured MUST behave byte-identically
  to today (zero regression to DeepSeek-only setups).

**Cross-provider operation (US5)**

- **FR-019**: Main and sub-agent streams MUST route independently to their
  configured provider/model; per-stream routing MUST remain stable within a
  session (cache affinity preserved per provider).
- **FR-020**: Client capability handling MUST cover the MiniMax family (limits,
  reasoning parsing, tool-call rescue behavior) so requests never use
  capabilities the target provider lacks, regardless of the main/sub mix.
- **FR-021**: Session accounting MUST attribute usage, cache metrics, and cost per
  model across providers accurately in mixed sessions (per-model rows, honest
  unavailable states).
- **FR-022**: First-time (unconfigured) setups MUST default to MiniMax-M3 as the
  main model and DeepSeek V4 Pro as the sub-agent model when both providers are
  available through the gateway, falling back gracefully to today's DeepSeek
  defaults when MiniMax is not available; existing configured settings MUST never
  be altered by an upgrade (clarified 2026-07-14).

### Key Entities

- **Task Complexity Verdict**: The recorded pipeline-required vs direct decision
  with its reason; visible in the session.
- **Orchestration Phase**: One of research / plan / implement / validate-review,
  with recorded transitions, per-phase sub-agent runs, and degradation notes.
- **Plan Artifact**: The durable workspace Markdown plan — findings, referenced
  steps with acceptance checks, verification, risks; versioned; resumable.
- **Handoff Contract**: A sub-agent's role, deliverable, output format, and scoped
  context; plus the structured summary it returns.
- **Agent Route**: The stream→provider/model binding for a request (main vs
  sub-agent), verifiable in usage records.
- **Provider Profile (MiniMax)**: Documented models, limits, pricing (cached/
  uncached), caching threshold, reasoning and tool-call dialect requirements.
- **Mixed-Session Ledger**: Per-model, per-provider usage/cache/cost rows for a
  session spanning both providers.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On the delegation benchmark at max effort, the pipeline is observed
  end-to-end: ≥2 research sub-agent runs complete before any file-changing call, a
  plan artifact exists on disk before the first implementation edit, ≥2
  implementation sub-agent runs execute plan parts, and ≥1 validation/review run
  completes before the final answer.
- **SC-002**: The simple control task produces zero sub-agents, zero plan file,
  and completes correctly with turn count and wall time no worse than the current
  build's control baseline (+10% tolerance).
- **SC-003**: Executing only the plan's steps passes the benchmark's expected-
  outcome checklist 8/8, with the final main conversation ≤50% of the size of the
  no-pipeline baseline run on the same workload.
- **SC-004**: Handoff audit over the benchmark transcripts: 100% of sub-agent
  prompts contain role + deliverable + output format + scoped context; 0 wholesale
  re-reads of predecessor-covered scopes (duplicate-read counter not above the
  baseline).
- **SC-005**: A live MiniMax conformance session through the gateway (≥15 requests,
  multi-turn with tool calls) completes with zero provider rejections for request
  shape; 100% of its usage records carry provider-reported token counts and, when
  the prefix exceeds the provider's cacheable threshold, cached-token counts; a
  repeated-prefix probe shows a warm-turn cached share ≥50% on the second
  identical request.
- **SC-006**: A mixed-provider benchmark run (main and subs on different
  providers) completes the checklist 8/8 with per-model usage rows for both
  providers, and each provider's steady-state cache metric is within 5 points of
  its single-provider baseline.
- **SC-007**: Zero regressions: the complete existing test suite passes; the 008
  regression guards (denial texts, brief format, prefix stability, loop guards)
  remain byte-stable; a DeepSeek-only deployment's wire behavior is unchanged
  (conformance capture identical).
- **SC-008**: All improvement claims are backed by before/after runs on pinned
  model/gateway/effort per the constitution's measurement standards, with cold and
  steady-state figures reported separately.

## Out of Scope

- New sub-agent kinds beyond the existing four (research/plan/implement/review map
  onto explore, plan, general, review); new user-facing commands beyond phase
  visibility.
- MiniMax's Anthropic-dialect endpoint (the OpenAI-compatible path is the
  integration surface); MiniMax multimodal/audio features; MiniMax explicit
  (manual-TTL) caching — passive caching only.
- Changing effort→allowance numbers, the wire protocol between client and gateway,
  or the `~/.muhiya` state layout.
- Cross-provider failover mid-stream (a request that started on one provider
  completes or fails there).
- Re-introducing any token ceiling (stays removed).

## Assumptions

- Complexity mapping: conversational/tiny/small single-concern tasks are "genuinely
  simple" (direct); standard-and-above multi-scope tasks are pipeline-required.
  The existing task classifier is the decision point and its verdict is surfaced;
  refinements to its thresholds are in scope, its replacement is not.
- The plan artifact extends the existing workspace plan-file mechanism (one
  canonical plan location with versioning/supersede semantics), not a new parallel
  planning system.
- "Configured sub-agent model" is the existing sub-agent model setting; pipeline
  phases use the existing four sub-agent kinds with role-specific contracts.
- Phase state and handoff summaries ride dynamic surfaces (task tail, sidecars,
  artifacts) — never the byte-stable prefix — per the constitution; any static
  prompt text lands as one recorded upgrade epoch.
- MiniMax integration facts (automatic caching ≥512 tokens, cached-token usage
  fields, model set incl. the 1M-context coding flagship, OpenAI-compatible
  endpoint, documented function-calling and reasoning-continuity requirements)
  are taken from the official docs fetched 2026-07-14 and will be snapshotted in
  the feature's research notes; MiniMax's official Mini-Agent reference CLI is
  analyzed read-only as the reference architecture for MiniMax-native behavior
  (recorded alongside the constitution's Reasonix reference study).
- The gateway's existing MiniMax awareness (thinking-dialect mapping, tool-call
  rescue family flags) is a starting point to complete, not evidence of done.
- Live verification uses the established benchmark harnesses (delegation
  workload, conformance captures) against the deployed gateway; runs respect the
  account's budget windows.
