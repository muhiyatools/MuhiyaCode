# Feature Specification: Prompt Cache Optimization

**Feature Branch**: `001-prompt-cache-optimization`

**Created**: 2026-07-11

**Status**: Draft

**Input**: User description: "Improve the coding agent's prompt-cache performance so that stable,
reusable context consistently achieves a 99–100% cache-hit rate whenever technically possible.
The current cache-hit percentage is too low and must be deeply investigated and improved. Analyze
the complete request lifecycle (system prompts, message ordering, tool definitions, conversation
history, file context, agent orchestration, request serialization). Use the DeepSeek Reasonix
project at `C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2` as the
primary reference and determine why it consistently achieves ~99% cache hits. The implementation
may redesign the cache and context architecture where necessary, but must preserve agent
correctness, reasoning quality, tool functionality, and fresh project context."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Long coding sessions reuse cached context instead of re-paying for it (Priority: P1)

A developer works through a long, multi-turn coding session. After the session's first request,
every subsequent request reuses the previously transmitted stable context — the agent's
instructions, its tool catalog, the settled conversation so far, and unchanged file context — as
cheap cache reads instead of re-billed, re-processed uncached input. Turns start faster and each
turn costs a fraction of what it costs today, without the agent losing any capability, context
freshness, or answer quality.

**Why this priority**: This is the core value of the feature. Today the cache-hit percentage is
too low, which means users repeatedly pay full price — in both money and latency — for identical
bytes the provider has already seen. Every other part of this feature exists to enable, prove, or
protect this outcome.

**Independent Test**: Run a scripted, realistic coding session of at least 20 turns (file reads,
edits, searches, tool calls, follow-up questions) against a cache-supporting endpoint. Inspect
per-request usage records: from the second request onward, stable previously-transmitted context
is served from cache at 99–100% on every request, and only genuinely new content is charged as
uncached input.

**Acceptance Scenarios**:

1. **Given** a session past its first request, **When** the user sends a follow-up message that
   adds only new conversation, **Then** all previously transmitted stable context is served from
   cache and only the genuinely new content of the turn is charged as uncached input.
2. **Given** an ongoing task with per-turn dynamic state (objectives, plan mode, budgets,
   reasoning effort, task classification), **When** requests are built across successive turns,
   **Then** that dynamic state never invalidates previously cached stable context.
3. **Given** a file already read in this session and unchanged since, **When** the agent would
   read it again, **Then** the unchanged content is not retransmitted as new uncached input.
4. **Given** genuine context pressure that forces history restructuring, **When** the
   restructuring happens, **Then** it happens at most once per pressure event, is recorded with
   its cause, and subsequent requests immediately re-stabilize into a new reusable prefix.
5. **Given** a session resumed after an application restart, **When** the next request is built,
   **Then** it reproduces the same stable context byte-for-byte, so a still-live provider cache
   continues to match.

---

### User Story 2 - Trustworthy cache and cost accounting (Priority: P2)

A developer (or maintainer validating this feature) can see exactly how many tokens each request
read from cache, wrote to cache, sent uncached, and produced as output — and these numbers match
the provider's own reported usage exactly. Cost problems and cache regressions become visible the
moment they happen instead of being discovered on an invoice.

**Why this priority**: Without honest, provider-sourced measurement, neither the P1 improvement
nor any future regression can be proven. It also delivers standalone value: cost transparency for
every user, independent of how well caching performs.

**Independent Test**: Run any session while separately capturing the provider's raw usage
payloads. Compare the agent's per-request records and session totals against the captured
payloads: they agree exactly. Repeat against an endpoint that omits cache fields: the metrics
report "unavailable" rather than zeros or invented numbers.

**Acceptance Scenarios**:

1. **Given** a provider that reports cache usage, **When** any request completes, **Then** the
   session's records show cache-read, cache-write, uncached-input, and output token counts
   identical to the provider-reported values for that request.
2. **Given** a provider that omits cache usage fields, **When** a request completes, **Then**
   those metrics are recorded and displayed as unavailable — never fabricated or defaulted to
   zero-as-fact.
3. **Given** a completed or in-progress session, **When** the user inspects usage, **Then**
   cumulative session totals and an overall cache-hit percentage are available.
4. **Given** a request whose transmitted prefix is identical to the previous request's yet
   misses cache, **When** the miss is recorded, **Then** it is distinguishable as provider-caused
   (e.g., expiry or eviction) rather than agent-caused.

---

### User Story 3 - Reference-informed design and documented limits (Priority: P3)

Maintainers receive a written analysis of the DeepSeek Reasonix reference project explaining why
it consistently achieves ~99% cache hits, with each of its cache-relevant practices marked as
adopted, adapted, or rejected for this project — plus a register of provider limitations that cap
the achievable hit rate, so any benchmark shortfall from 100% is attributable to a documented
cause rather than an unexplained defect.

**Why this priority**: This converts a one-time optimization into durable, reviewable knowledge.
It is also mandated by the project constitution (Principle VII: the reference must be studied
before significant cache-affecting design work; Principle X and VI: shortfalls must be honestly
explained, not hidden).

**Independent Test**: Review the produced analysis against the reference project: every
cache-relevant mechanism found there (prompt structure, context management, request building,
invalidation policy, measurement) appears with an adopt/adapt/reject decision and rationale.
Cross-check benchmark results: every observed cache miss maps to either a recorded invalidation
event or a documented provider limitation.

**Acceptance Scenarios**:

1. **Given** the reference project at the provided local path, **When** the analysis is complete,
   **Then** a written report identifies the principles, structures, and behaviors responsible for
   its high hit rate, each with an explicit adopt/adapt/reject decision and rationale for this
   project.
2. **Given** benchmark sessions with any request below a 100% hit rate on eligible context,
   **When** results are reviewed, **Then** each shortfall is attributed to a documented provider
   or protocol limitation, a recorded invalidation event, or is filed as a defect to fix.
3. **Given** the limitations register, **When** a user or maintainer asks why 100% is not always
   achievable, **Then** the register answers it with observed evidence (e.g., cache lifetime,
   minimum cacheable granularity, per-model cache scoping, request routing effects).

---

### Edge Cases

- **Cold start**: the first request of any session, and the first request after provider-side
  cache expiry (e.g., a long-idle session), is inherently uncached. These are excluded from the
  steady-state target but must be reported, not hidden.
- **Explicit user resets**: user-initiated history compaction or switching models mid-session
  legitimately invalidates cached context. The system must re-stabilize immediately afterward and
  label the cause; the one-time miss must be attributable to the user action.
- **Toolset changes mid-session**: connecting, removing, or authorizing an external tool server
  changes the available tool catalog. The stable context may change only when the effective
  toolset actually changes, must re-stabilize afterward, and the event must be recorded.
- **Session resume across restarts**: rebuilding requests from persisted session state must
  reproduce the identical stable context; resume must not silently reorder, reformat, or
  re-serialize previously transmitted content.
- **Retries and cancelled streams**: a retried or resumed request must resend the identical
  stable prefix; retry paths must not introduce drift (fresh timestamps, re-rendered content).
- **Provider-side misses despite identical bytes**: providers may evict or expire caches at any
  time. Measurement must distinguish these from agent-caused invalidation (see US2 scenario 4).
- **Routing that fragments caches**: virtual model IDs that re-select an underlying model per
  request prevent stable caching. This must be documented as a limitation with user guidance,
  not silently absorbed.
- **Concurrent subagent traffic**: subagent requests interleaved with the main session must not
  degrade the main session's cache reuse, and each subagent's own context must follow the same
  stability rules.
- **File changes on disk**: when a previously read file changes externally, the agent must see
  fresh content — cache reuse must never serve stale project state (correctness beats caching).
- **Very short sessions**: one- or two-turn sessions have little reusable context; reporting must
  not misrepresent them as cache failures.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001** (Stable prefix): For every request after a session's first, all previously
  transmitted stable context — agent instructions, tool catalog, and settled conversation
  history — MUST be byte-identical to its most recent transmission, except at attributable
  invalidation events (FR-005).
- **FR-002** (Dynamic placement): Per-turn dynamic content (task classification, budgets,
  objectives, mode instructions, effort level, time-sensitive values) MUST be positioned so it
  never alters previously transmitted stable context — carried with the newest turn or outside
  message content as request parameters.
- **FR-003** (Determinism): Request construction MUST be deterministic: given identical session
  state and configuration, the system produces byte-identical request payloads — including after
  process restart and session resume.
- **FR-004** (No redundant retransmission): Unchanged file contents, tool outputs, and
  conversation history MUST NOT be retransmitted as new uncached content; repeated or
  already-covered reads MUST reuse the in-context results while those remain intact and current.
- **FR-005** (Attributable invalidation): Any event that rewrites previously transmitted context
  (compaction, folding, redaction, toolset change, model switch) MUST occur only under genuine
  context pressure, an actual toolset change, or explicit user action — never speculatively —
  and each occurrence MUST be recorded with its cause.
- **FR-006** (Re-stabilization): After any legitimate invalidation event, the newly composed
  context MUST immediately become the new stable prefix, byte-identical on all subsequent
  requests until the next legitimate event.
- **FR-007** (Honest metrics): The system MUST capture per request — exactly as reported by the
  provider — cache-read tokens, cache-write tokens, uncached input tokens, and output tokens;
  MUST aggregate them per session; and MUST expose an overall session cache-hit percentage.
  Provider-omitted fields MUST surface as unavailable, never fabricated.
- **FR-008** (Miss attribution): The system MUST record enough information (at minimum, whether
  the transmitted stable prefix changed since the prior request) to attribute each cache miss to
  an agent-side change or to provider-side behavior.
- **FR-009** (Reference analysis): A documented analysis of the DeepSeek Reasonix reference
  project MUST be produced, identifying its cache-relevant architecture, prompt organization,
  context-management strategy, and request-building behavior, with an adopt/adapt/reject decision
  and rationale recorded for each identified practice.
- **FR-010** (Before/after verification): A reproducible benchmark of realistic multi-turn coding
  sessions MUST be executed on the unmodified baseline and on the improved system under identical
  conditions (same provider, model, workload, and settings), with raw results preserved alongside
  the feature artifacts.
- **FR-011** (Limitations register): Provider and protocol limitations that prevent 100% caching
  MUST be documented, covering at minimum cache lifetime and eviction behavior, minimum cacheable
  granularity, cache scoping (e.g., per model or per account), and any request-routing behavior
  that fragments caching.
- **FR-012** (Quality preservation): The improvement MUST NOT reduce agent correctness, reasoning
  quality, tool functionality, or freshness of project context. When a file changes on disk, the
  agent MUST observe the fresh content. The full pre-existing automated test suite MUST pass.
- **FR-013** (Provider compatibility): All behavior MUST degrade gracefully on endpoints without
  prefix caching or without cache usage reporting: full agent functionality is retained and cache
  metrics are marked unavailable.
- **FR-014** (Subagent conformance): Requests issued for subagent runs MUST follow the same
  stable-prefix, dynamic-placement, and measurement rules for their own contexts, and MUST NOT
  degrade the main session's cache reuse.

### Key Entities

- **Stable Context Prefix**: the reusable leading portion of a request — agent instructions,
  tool catalog, settled conversation history. Key attributes: content identity (byte-level),
  last transmission, current stability status.
- **Dynamic Turn Content**: per-turn additions that legitimately differ on every request — the
  new user message, per-turn task state, request parameters. Defined by never being part of the
  stable prefix.
- **Invalidation Event**: a recorded rewrite of previously transmitted context. Key attributes:
  cause (context pressure, toolset change, user action), time, scope of what changed.
- **Usage Record**: per-request provider-reported accounting — cache-read, cache-write, uncached
  input, output tokens — plus derived hit rate; aggregated into a session total.
- **Benchmark Workload**: a scripted, realistic multi-turn session definition with captured
  baseline and post-improvement results, reproducible by a reviewer.
- **Reference Finding**: one cache-relevant practice identified in the reference project, with
  its adopt/adapt/reject decision and rationale.
- **Provider Limitation**: a documented, evidence-backed cap on achievable caching (lifetime,
  granularity, scoping, routing).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In realistic benchmark sessions of at least 20 turns on a cache-supporting
  provider, from each session's second request onward, 99–100% of previously transmitted stable
  context tokens are served as cache reads on every request. This is evaluated as
  `prefix_stability_rate = Σread / Σ(prompt - new_tail)`; raw `Σread/Σprompt` is reported
  separately because its ceiling depends on the workload's newly appended tail.
- **SC-002**: Rebuilding the same request from identical session state — including across a
  process restart and session resume — produces byte-identical payloads in 100% of determinism
  test cases.
- **SC-003**: In steady-state turns, uncached input is bounded by the genuinely new content of
  that turn: previously transmitted unchanged context contributes zero uncached input tokens.
- **SC-004**: Agent-reported token and cache metrics agree exactly with provider-reported usage
  on 100% of benchmarked requests; endpoints omitting cache fields display "unavailable" rather
  than invented values.
- **SC-005**: The before/after benchmark demonstrates a major, repeatable improvement: the
  improved system meets SC-001 while the recorded baseline does not, the result reproduces across
  at least 3 independent runs with session hit-rate variance within ±1 percentage point, and the
  report states the change in total session cost.
- **SC-006**: Agent quality is preserved: benchmark sessions complete their coding tasks at an
  equal or better rate than baseline with identical tool behavior, and 100% of the pre-existing
  automated test suite passes.
- **SC-007**: Every benchmark cache miss is attributable: 100% of misses map to a recorded
  invalidation event, a session cold start, or a documented provider limitation; none remain
  unexplained.

## Assumptions

- Primary validation targets the project's default hosted gateway fronting models with implicit
  prefix caching that report cache usage in responses. The 99–100% target applies only where the
  provider technically supports prefix caching — matching the user's "whenever technically
  possible" qualifier — and graceful degradation covers the rest.
- A session's first request, and the first request after provider-side cache expiry, are
  inherently cache writes. The 99–100% target is a steady-state target; cold-start behavior is
  reported separately, not counted against it.
- "Stable cache-eligible context" means content previously transmitted in the same session that
  has not legitimately changed and remains within the provider's cache lifetime.
- The DeepSeek Reasonix reference is available read-only at the provided local path for analysis.
  It informs design; it is not a runtime dependency of the shipped system, and nothing is copied
  without an explicit adopt/adapt decision.
- The baseline measurement is captured on the current, unmodified system before any behavioral
  changes, using the same benchmark workload as the post-improvement measurement.
- Existing user-facing commands and workflows remain stable; usage and metrics displays may be
  extended additively to show cache accounting.
- The internal cache and context architecture may be redesigned where necessary (per the user's
  explicit latitude), provided documented compatibility boundaries (persisted state layout, wire
  protocol) and the project constitution's principles are preserved.
- Out of scope: provider-side changes, billing or credit systems, IDE integrations, and any
  optimization that trades away answer quality for hit rate (rejected by definition under
  Principles I and II).
