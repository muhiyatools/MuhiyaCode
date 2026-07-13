# Feature Specification: Reasonix-Aligned Agent Flow Overhaul

**Feature Branch**: `002-reasonix-agent-overhaul`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: "Refactor and enhance the current agent flow system. Read the implementation review carefully, then begin a detailed analysis of the Reasonix project, focusing on everything that enables it to achieve stable cache-hit rates of 98–99%. Perform a complete overhaul of MuhiyaCode's prompt engineering, agent orchestration, context construction, and all related systems, adapting and replicating the proven Reasonix architecture wherever suitable. Address every issue in the existing implementation review; add a dedicated phase for replicating Reasonix's agent architecture; analyze its prompt structure, orchestration flow, context management, caching strategy, tool execution, session handling, memory system, and token-efficiency techniques; ensure all prompt-contributing content is fully compatible with prompt caching; keep cacheable prefixes deterministic, consistently ordered, and unchanged between requests; prevent dynamic content from invalidating cached sections; audit the complete request-building pipeline for unnecessary differences, duplicated context, unstable ordering, timestamps, generated identifiers, or changing metadata; define clear implementation phases, affected files, migration steps, validation methods, and acceptance criteria. Inputs: F:\MuhiyaCode Agent Go\IMPLEMENTATION_REVIEW.md (defect inventory) and C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2 (reference architecture)."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Cache-Efficient Long Coding Session (Priority: P1)

A developer runs a long, multi-turn coding session (planning, file edits, shell commands, subagent research) through their gateway. Because the agent's request construction keeps every reusable portion of the conversation byte-stable, nearly all previously sent context is served from the provider's cache on every turn. The session's cost stays near the theoretical minimum for the work performed, and the measured cache efficiency matches the reference agent (Reasonix) when both perform the same scripted task.

**Why this priority**: Cache efficiency is the product's core economic promise and the stated purpose of this overhaul. Every other behavior in this feature exists to protect or extend it. The prior implementation round left one defect that can hard-fail a session mid-flight and several efficiency mechanisms unadopted — this story closes that gap.

**Independent Test**: Run the standard benchmark workload plus one matched scripted task (identical prompt sequence) through both MuhiyaCode and the reference agent against the same provider; compare prefix stability, steady-state hit rate, and cost per task from the recorded request logs.

**Acceptance Scenarios**:

1. **Given** an in-progress session whose context pressure sits in the mid band (where history maintenance is eligible), **When** the next task starts and maintenance would reclaim only a negligible amount of context, **Then** the conversation history is left byte-identical, no reclamation is recorded, and the session continues without any cache reset or failure.
2. **Given** any session, **When** history is legitimately rewritten (reclamation or compaction), **Then** the rewrite is always paired with a recorded invalidation explanation, and the session never fails with an unexplained prefix-change error.
3. **Given** a plain follow-up turn with no active goal, plan, or pending plan, **When** the request is built, **Then** the only content added beyond the user's own text is the fixed per-task brief, and the stable prefix (instructions, tool descriptions, settled history) is byte-identical to the previous request's.
4. **Given** the same scripted multi-turn task executed by MuhiyaCode and by the reference agent, **When** both request logs are compared, **Then** MuhiyaCode's steady-state cache-hit rate is within a small, defined margin of the reference agent's on that identical workload.
5. **Given** a session where the provider omits or duplicates usage-reporting frames, **When** a turn completes, **Then** the turn still succeeds and the usage is flagged as estimated rather than failing the request.

---

### User Story 2 - Correct, Predictable Agent Behavior Under All Flows (Priority: P2)

A developer exercises the agent's full surface — plan mode with the proceed-now/later flow, goals that complete on any turn shape, mid-task cancellation, parallel subagents, MCP servers that die and recover, and mid-session mode changes — and every flow behaves predictably: no crashes, no corrupted conversations, no stuck states, no invisible rule bypasses.

**Why this priority**: The implementation review found nine introduced defects and two unimplemented safety mechanisms. Several are shipping blockers (a data race, a conversation-protocol corruption, a dead reconnect path, a broken server-management dialog). Cache efficiency is worthless if sessions crash or corrupt; correctness is constitutionally prior to optimization.

**Independent Test**: Execute the review's scenario walkthroughs — plan proceed-now/later with restart, goal completion on a final turn, mid-batch cancellation followed by a new prompt, server kill-and-recover, concurrent goal changes during a running task — and verify each completes without error, corruption, or stuck state, with the concurrency checker clean.

**Acceptance Scenarios**:

1. **Given** a running task, **When** the model signals plan completion in the same turn as other tool activity, or on the last allowed turn, **Then** every tool call still receives a matching result, the conversation remains well-formed for the next request, and the plan-ready handoff still occurs.
2. **Given** an active goal, **When** the goal's completion is declared on the final governed turn or alongside tool calls, **Then** the goal is detected as complete, removed from active state, and its markers never appear in user-visible output.
3. **Given** a user issuing goal commands while a task is running, **When** the engine is simultaneously reading goal state, **Then** no data race exists (verified by the concurrency checker) and mode protections are never silently dropped mid-task.
4. **Given** a connected external tool server that crashes mid-session, **When** the agent next calls one of its tools, **Then** the failure is detected, exactly one automatic reconnection is attempted, and on repeated failure the model receives a clear "unavailable — do not retry" instruction instead of raw transport errors.
5. **Given** the server-management dialog opened with servers not yet connected, **When** it is left open, **Then** connections are initiated, states update live, and the refresh activity terminates once all servers settle.
6. **Given** subagents running any task, **When** they execute tools, **Then** the same validation, mode restrictions, repeat protection, and failure protection that govern the main loop govern them.

---

### User Story 3 - Reference-Grade Context Management and Token Efficiency (Priority: P3)

As sessions grow, the agent manages its context the way the reference agent does: stale tool results are reclaimed with content-aware geometry before any expensive summarization, compaction is rare and preserves the task statement and prior digests verbatim, the model itself is instructed (statically) to behave cache-frugally, and the user can watch the live session cache-hit rate and cost at all times.

**Why this priority**: These are the adopted reference mechanisms that push efficiency from "correct" to "reference-grade." They deliver compounding savings on long sessions but depend on Stories 1–2 being solid first.

**Independent Test**: Drive a session past the reclamation and compaction thresholds with scripted filler work; verify reclamation geometry, archival of pruned originals, compaction rarity and digest preservation, and the live efficiency readout, all while the Story 1 stability guarantees continue to hold.

**Acceptance Scenarios**:

1. **Given** context pressure entering the reclamation band, **When** stale tool results are reclaimed, **Then** older read-style results keep a large head and small tail, action-style results keep balanced head and tail, tiny results are left untouched, and every pruned original is archived and recoverable.
2. **Given** repeated growth to the compaction threshold, **When** compaction runs more than once, **Then** earlier digests are preserved verbatim rather than re-summarized, the original task statement survives, and back-to-back compactions are prevented.
3. **Given** any session, **When** the user looks at the interface during or after a task, **Then** a live session cache-hit rate and token/cost readout is visible, persists after task completion, and clears only on a new prompt, session switch, or exit.
4. **Given** the model's standing instructions, **When** any session starts, **Then** they include fixed guidance to avoid cache-hostile behavior (redundant re-reads, verbatim failed retries, oversized reads), and these instructions are byte-identical across all sessions with the same configuration.

---

### Edge Cases

- Context pressure oscillates around the maintenance threshold across many task boundaries → reclamation must not re-arm repeatedly; total rewrites stay bounded and each is explained.
- The provider streams zero or multiple usage frames in one response → turn succeeds with usage marked estimated; no hard failure.
- Plan completion signal arrives outside plan mode (stray call) → treated as a harmless no-op; no phantom saved plan, no premature task end.
- Cancellation lands between parallel tool executions → every announced tool call still gets a result (real or synthetic); next request is accepted by the provider.
- A goal reply omits its status marker twice in a row → agent stops and asks instead of looping; a productive tool turn between text replies does not count toward the idle stop.
- An external server's authorization expires without a refresh path → surfaced as "needs authorization," never as a generic error.
- Session restart mid-plan or mid-goal → mode flags, pending plan, and active goal are restored (or their loss is explicitly announced); a later "proceed" still executes a saved plan.
- Right-to-left or multi-byte reasoning text → live thinking display truncates on character boundaries and renders shaped text; no corruption.
- Two agents (MuhiyaCode and reference) run the same scripted task → comparison uses identical prompt sequences; conclusions about parity are only drawn from matched workloads.

## Requirements *(mandatory)*

### Functional Requirements

**Group A — Defect closure (the implementation review is the authoritative inventory)**

- **FR-001**: The system MUST resolve every defect and gap recorded in `IMPLEMENTATION_REVIEW.md` (Parts A–E), including all six shipping blockers: the mid-band maintenance trap that can hard-fail a session, the usage-frame hard error, the goal-state data race, the unconditional plan-exit signal and its batch-result orphaning, the dead server-reconnect path, and the non-terminating server-management dialog.
- **FR-002**: Every conversation rewrite (reclamation, trim, compaction, window drop) MUST be paired with a recorded invalidation explanation; the system MUST never fail a session for a prefix change it caused itself.
- **FR-003**: All tool executions — main loop and subagents alike — MUST pass through one shared gate providing argument validation with model-actionable errors, mode enforcement, repeat/duplicate protection, and failed-call short-circuiting, with per-scope counters so concurrent work does not interfere.
- **FR-004**: The conversation protocol MUST remain well-formed on every path: each announced tool call receives exactly one result (real or synthetic) regardless of cancellation, early exit signals, or batch composition.
- **FR-005**: Goal completion MUST be detected on every turn shape (final turn, tool-call turn, plain turn); completed or blocked goals MUST leave active state automatically; goal control markers MUST never appear in user-visible output.
- **FR-006**: All defect fixes MUST be covered by the tests the review names, including concurrency verification for the shared state introduced by mode exclusivity.

**Group B — Reference architecture analysis and adoption (dedicated phase)**

- **FR-007**: The plan for this feature MUST include a dedicated analysis phase that traces the reference agent's complete request and execution flow — prompt structure, orchestration loop, context management, caching strategy, tool execution, session handling, memory/steering placement, and token-efficiency techniques — and produces a mechanism inventory with source references.
- **FR-008**: For every inventoried reference mechanism, the design MUST record an explicit decision — adopt as-is, adapt (with the deviation and its reason), or reject (with the reason) — so no mechanism is silently skipped. Mechanisms already equivalently present in MuhiyaCode MUST be marked as such with evidence rather than rebuilt.
- **FR-009**: The system MUST adopt the reference agent's context-reclamation approach: a low-cost reclamation tier ahead of summarization, content-aware head/tail geometry per tool-result kind, a minimum-size floor below which results are untouched, and archival of every pruned original for recoverability.
- **FR-010**: The system MUST align its compaction behavior with the reference agent's: a high trigger threshold, a substantial verbatim recent tail, preservation of the original task statement, accumulation (never re-summarization) of prior digests, prevention of consecutive compactions, and a no-rewrite advisory notice at the soft threshold.
- **FR-011**: The system's standing model instructions MUST include a fixed cache-discipline section teaching the model to avoid cache-hostile behavior; this content MUST be identical for every request and every session with the same configuration.
- **FR-012**: The interface MUST display a live, persistent session cache-hit rate and token/cost readout sourced from existing usage accounting, following the already-specified persistence rules (cleared only by new prompt, session switch, or exit).

**Group C — Cache compatibility of all prompt-contributing content**

- **FR-013**: Every category of prompt-contributing content — standing instructions, tool definitions, skill definitions, subagent definitions, external-server tool surfaces, configuration-derived text — MUST render deterministically: stable ordering, no timestamps, no generated identifiers, no environment-sensitive or locale-sensitive formatting, and byte-identical output for identical configuration.
- **FR-014**: Dynamic, session-specific, or per-turn content (briefs, goals, plan instructions, steering, notices, pending-plan injections) MUST ride only on the newest user turn or in request metadata, never inside standing instructions, tool definitions, or settled history.
- **FR-015**: The tool surface presented to the model MUST remain identical across a session regardless of mode changes; mode restrictions are enforced at execution time. Changes to the available surface (e.g., server tools appearing) MUST land only at task boundaries with a recorded explanation.
- **FR-016**: A complete audit of the request-building pipeline MUST verify — with automated guards, not one-time inspection — that no duplicated context, unstable ordering, or changing metadata enters any cacheable region, covering every request-producing path (main loop, subagents, compaction, onboarding, classification).
- **FR-017**: The prefix-stability guard MUST cover all request-producing paths loudly: any path that cannot be normalized for guarding MUST degrade visibly (recorded and reportable), never silently.

**Group D — Validation and evidence**

- **FR-018**: The feature MUST define and execute a matched-workload comparison: the same scripted task sequence run by MuhiyaCode and the reference agent against the same provider, with efficiency conclusions drawn only from the matched logs.
- **FR-019**: Benchmark evidence files MUST only ever be appended with newly measured results; recorded historical measurements MUST never be altered.
- **FR-020**: All existing behavioral guarantees confirmed by the review (session routing pin, settled-byte guard, honest metric definitions, evidence-file integrity) MUST be preserved; regression of a confirmed-good item is a failed acceptance.

### Key Entities

- **Stable Prefix**: the byte-identical portion of every request — standing instructions, tool definitions, settled conversation history. The unit whose stability the entire feature protects.
- **Dynamic Tail**: per-turn content appended to the newest user turn (brief, goal block, plan block, notices). The only place request-to-request variation is permitted.
- **Invalidation Record**: the explanation paired with every legitimate stable-prefix change; the difference between an intended cache reset and a defect.
- **Mechanism Inventory**: the per-mechanism adopt/adapt/reject decision matrix produced by the reference-analysis phase, with source references and evidence for "already present" claims.
- **Reclamation Tier**: the low-cost context recovery pass (head/tail pruning with per-kind geometry and archival) that runs before compaction is considered.
- **Efficiency Readout**: the persistent, live session view of cache-hit rate, tokens, and cost shown to the user.
- **Matched Workload**: an identical scripted prompt sequence executed by both agents, the only basis on which parity claims are made.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Prefix-stability rate (share of eligible settled bytes served cache-stable) is at least 99.5% across the benchmark suite and a live 20+ turn session, with zero unexplained prefix changes (100% of rewrites carry invalidation records).
- **SC-002**: On the matched scripted workload, MuhiyaCode's steady-state cache-hit rate is within 0.5 percentage points of the reference agent's on the same workload, and cost per completed task is within 15%.
- **SC-003**: Cache-read volume never decreases between consecutive requests of a session except at recorded invalidation points, across the entire benchmark suite and validation sessions.
- **SC-004**: A plain follow-up turn adds at most ~50 tokens of system-added content beyond the user's own text in steady state (no active goal/plan), verified by an automated guard.
- **SC-005**: All defects and gaps from the implementation review are closed with their named tests passing; the concurrency checker reports zero races across the orchestration and server-management components.
- **SC-006**: The review's scenario walkthroughs (plan proceed-now/later with restart, goal completion on every turn shape, mid-batch cancellation, server kill-and-recover, authorization-expiry surfacing, persistent usage readout) all complete without error, corruption, or stuck state.
- **SC-007**: Sessions running in the mid pressure band for 10+ task boundaries experience at most the bounded number of history rewrites (latch respected) and zero session failures attributable to maintenance.
- **SC-008**: The mechanism inventory covers 100% of the reference areas named in this feature (prompt structure, orchestration, context management, caching, tool execution, session handling, memory/steering, token efficiency) with an explicit decision per mechanism.

## Assumptions

- The provider's caching remains implicit byte-prefix matching with per-model namespaces; no request-side cache-control markers are available or needed. Reference mechanisms tied to explicit cache-control markers will be recorded as "not applicable — provider difference" in the mechanism inventory rather than ported.
- The deployed gateway's session-stickiness (honoring the client's session header, already implemented and verified) remains in place; this feature builds on it and does not modify the gateway.
- `IMPLEMENTATION_REVIEW.md` is the authoritative, complete defect inventory for the prior round; no re-audit of that round is in scope beyond verifying its fixes.
- The reference project tree at `C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2` is available read-only during planning and implementation; its patterns are adapted, not copied verbatim, and MuhiyaCode's existing architecture is improved in place (no rewrite from scratch), per the constitution.
- "Reference parity" claims are only meaningful on matched workloads; raw hit rates on dissimilar sessions are expected to differ with workload shape and are not acceptance evidence.
- The already-specified interface behaviors from the prior round (persistent usage footer rules, thinking-section design) remain requirements; this feature completes their gaps rather than redesigning them.
- Existing session artifacts (saved plans, goals, cached tool surfaces) from prior versions remain readable; where formats change, migration preserves user data or clearly announces a reset.
- Constitution v1.0.0 governs: correctness precedes optimization, and every cache-affecting change must be validated by before/after measurement.
