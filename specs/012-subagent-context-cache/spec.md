# Feature Specification: Subagent Context Reuse & Cache-First Orchestration

**Feature Branch**: `012-subagent-context-cache`

**Created**: 2026-07-19

**Status**: Draft

**Input**: User description: "Overhaul the caching system and the way subagent contexts are saved and reused. When a new subagent is opened for work related to a previously completed subagent, it should be linked to that earlier subagent's context instead of starting from zero. Cache-hit rate must be one of the main priorities. Research the best ways to achieve this for both MiniMax through OpenRouter and DeepSeek. Analyze how different models and providers should work together inside the coding agent. The main agent should not fully read every file first and then repeat the same context to the subagent — it should assign a clear phase; the subagent reads the relevant phase from the implementation plan, inspects only the files required, and applies the changes. Clearer separation of responsibilities: main model plans, orchestrates, decomposes, coordinates, reviews; subagent model executes implementation phases. Maximize cache hits while avoiding duplicated context, unnecessary file reads, repeated planning, oversized prompts, and wasteful transfers."

## Clarifications

### Session 2026-07-19

- Q: What does "linking" physically mean by default? → A: Hybrid (continuation-first): the continuation subagent extends the predecessor's conversation stream by default (provider serves the shared prefix from cache); the system falls back to a digest-seeded fresh stream when window, provider/model, or staleness constraints block continuation — and records which form was used.
- Q: How far does linking eligibility reach? → A: Phases (and retries) within one orchestrated task, plus same-session follow-up tasks: a new user task may link to the most recent completed subagent when the orchestrator judges the work directly related (same files/area, same provider stream, passes staleness checks). Cross-session linking remains out of scope.
- Q: Which subagent kinds can chain into one another? → A: Same-kind sequential chains (e.g., implement→implement), plus one cross-kind pair: a review subagent may continue the stream of the implementer whose work it reviews. All other cross-kind pairs use the digest-seeded fallback.
- Q: How hard is the role separation enforced? → A: Phase-scoped hard gate: while implementation phases are active, the main model's implementation-file reads are denied with redirect-to-dispatch guidance. Exemptions (recorded when used): the plan file, subagent reports, and diagnosis after a subagent failure. Outside implementation phases the main model reads freely.
- Q: When is a predecessor too stale to link? → A: Majority threshold: continue the stream and re-read changed files while a minority of the predecessor's read-set changed; decline continuation (digest-seeded fallback) once more than half of the read-set has changed. The threshold is documented and every staleness decision is recorded in the ledger.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Continuation subagent inherits its predecessor's context (Priority: P1)

A user runs a multi-phase task. An implementation subagent completes phase 1 (reading the plan, inspecting several files, making edits) and finishes. When the orchestrator dispatches the next subagent for phase 2 of the same task, that subagent is linked to the completed phase-1 subagent's conversation context: it resumes on top of what the predecessor already read and did, instead of starting from an empty transcript. The provider recognizes the shared conversation prefix, so the resumed context is billed almost entirely at cached rates, and the phase-2 subagent does not re-read files or re-derive facts its predecessor already established.

**Why this priority**: This is the core of the feature and the largest single source of waste today — every subagent starts cold, repeats discovery its sibling already paid for, and writes a brand-new cache prefix. Linking contexts converts that repeated cost into cache reads and eliminates duplicate file reading in one move.

**Independent Test**: Run a task that produces two sequential implementation subagents over the same code area. Verify from provider-reported usage that the second subagent's first request is dominated by cache reads (not cache writes), and verify from the tool log that the second subagent does not re-read files the first one already read.

**Acceptance Scenarios**:

1. **Given** a completed implementation subagent for phase 1 of a task, **When** the orchestrator dispatches a subagent for phase 2 of the same task, **Then** the new subagent is linked to the phase-1 context and its first provider request reports a majority of its input tokens as cache reads.
2. **Given** a linked continuation subagent, **When** it begins work, **Then** it does not re-read any file whose content its predecessor already holds in the inherited context, unless the file changed since.
3. **Given** a file that was modified after the predecessor subagent read it, **When** the continuation subagent needs that file, **Then** it re-reads the current version rather than trusting the stale inherited copy.
4. **Given** a new task unrelated to any prior subagent's work, **When** a subagent is dispatched, **Then** it starts fresh — no unrelated context is inherited.

---

### User Story 2 - Main model orchestrates; subagents implement (Priority: P2)

A user asks for a feature that requires planning and several implementation phases. The main model decomposes the work, writes the implementation plan to a file, and then hands each phase to an implementation subagent by reference — "execute phase 2 of the plan file" — with only the minimal facts the subagent cannot discover on its own. The main model does not read the implementation files itself first, and it does not paste file contents or long context dumps into the handoff. Each subagent reads its phase from the plan file, inspects only the files that phase requires, applies the changes, and returns a compact structured result. The main model coordinates, tracks progress, and reviews outcomes — it does not duplicate the subagent's reading or editing work.

**Why this priority**: Today the main model front-loads file reading and then repeats that same context into every handoff — paying for the content twice (once in the main transcript, once in each subagent's prompt) and bloating both caches. Role separation removes the duplication at its source and makes the P1 context-linking maximally effective, but it depends on P1's handoff mechanics to land safely.

**Independent Test**: Run a standard multi-phase task and audit the transcript: count file-read operations performed by the main model versus subagents, and measure handoff size. The main model's reads should be limited to orchestration needs (plan file, subagent reports), and handoffs should reference the plan rather than embedding file contents.

**Acceptance Scenarios**:

1. **Given** a task that the orchestrator classifies as multi-phase, **When** the plan is approved and implementation begins, **Then** every implementation file read is performed by a subagent, and the main model's file reads are limited to the plan file, reports, and review needs.
2. **Given** a phase handoff to an implementation subagent, **When** the handoff is constructed, **Then** it identifies the phase by reference to the plan file plus a bounded task statement, and contains no pasted file bodies.
3. **Given** a completed phase, **When** the subagent returns, **Then** its result is a bounded structured report (what changed, where, verification status, facts the next phase needs), not a full transcript replay.
4. **Given** the main model receives a phase report, **When** it dispatches the next phase, **Then** it forwards only the facts the next subagent cannot read from the plan or the repository itself.

---

### User Story 3 - Provider-aware cache discipline across MiniMax and DeepSeek (Priority: P3)

A user runs the recommended mixed configuration — MiniMax M3 as the main model (via OpenRouter) and DeepSeek as the subagent model. The system knows how each provider's caching actually works — what makes a prefix cacheable, what invalidates it, how cache reads are reported and priced — and structures every conversation accordingly: stable byte-identical prefixes, append-only transcript growth, context linked only where the provider can actually honor it, and no accidental prefix-breaking edits (reordered tools, injected timestamps, rewritten history). Cache behavior is verified per provider pairing, and the displayed hit-rate numbers come from provider-reported usage, never estimates.

**Why this priority**: Context linking (P1) and lean handoffs (P2) only pay off if the provider actually returns cache hits. Getting each provider's rules right is the enabling layer; it matters most in the mixed-provider setup the product recommends, where main and subagent caches are independent and each must be managed on its own terms.

**Independent Test**: Run the same task suite once on a DeepSeek-only configuration and once on the mixed MiniMax + DeepSeek configuration. For each provider stream, verify steady-state cache-hit rates meet target and that no request unexpectedly restarts a cold prefix mid-session.

**Acceptance Scenarios**:

1. **Given** an ongoing session on either provider, **When** consecutive requests are sent on the same conversation stream, **Then** each request's prefix extends the previous one and the provider reports cache hits on the shared portion.
2. **Given** a linked continuation subagent, **When** its provider cannot honor cross-conversation prefix reuse under the linking method used, **Then** the system uses the linking method that provider does support, and never silently pays cold-write prices while displaying "linked".
3. **Given** a mixed-provider session, **When** usage is displayed, **Then** cache hit rates are reported per provider pairing from provider-reported figures, and unavailable data is shown as unavailable rather than estimated.

---

### User Story 4 - Lean handoffs and structured returns (Priority: P4)

Every dispatch to a subagent and every return from one follows a compact contract. Outbound: role, phase reference, bounded task statement, and only non-discoverable facts. Inbound: a structured result with what changed, what was verified, what failed, and a bounded set of facts worth carrying forward. Oversized returns are banked to durable task knowledge and summarized in place. The orchestrator never replays a subagent's full transcript into the main conversation, and repeated dispatches never re-send static material that is already part of a stable cached prefix.

**Why this priority**: This locks in the gains of P1–P3 by bounding the per-hop cost of communication in both directions. It is last among the mechanics because its value compounds the others rather than standing alone.

**Independent Test**: Instrument a representative task and measure handoff payload sizes and return sizes across all dispatches; verify each stays within its documented bound and that no dispatch embeds previously-sent static context.

**Acceptance Scenarios**:

1. **Given** any subagent dispatch, **When** the handoff is assembled, **Then** its size stays within a documented bound and contains no content the subagent can read from the plan file or repository.
2. **Given** a subagent produces an oversized report, **When** it returns, **Then** the full report is banked to task knowledge, and the main conversation receives a bounded digest plus a pointer.
3. **Given** a task with several sequential dispatches, **When** their combined transcript cost is measured, **Then** communication overhead (handoffs + returns, excluding actual work) stays within the documented share of total task tokens.

---

### User Story 5 - The user can see reuse working (Priority: P5)

After a task, the user can see — from the task summary and session statistics — whether subagent context linking happened, how much of each subagent's input was served from cache, and how much reading was avoided. When a continuation subagent could not be linked (stale context, provider switch, incompatible predecessor), the reason is visible rather than silent.

**Why this priority**: Trust and verifiability. The user has been burned by invisible inefficiency before; surfacing reuse honestly is how the feature proves it works — and it is required for honest measurement of the other stories. It is P5 because it observes the mechanism rather than being the mechanism.

**Independent Test**: Run a linking-eligible task and a linking-ineligible one; verify the summary distinguishes them and shows per-subagent cache figures and a link/no-link reason.

**Acceptance Scenarios**:

1. **Given** a task in which a continuation subagent was linked, **When** the task summary is shown, **Then** it identifies the link and the subagent's cache-read share.
2. **Given** a dispatch where linking was possible but declined, **When** the summary is shown, **Then** the reason (e.g., predecessor stale, provider changed) appears.

---

### Edge Cases

- **Stale predecessor context**: files changed (by the user, the main model, or another subagent) after the predecessor read them — the continuation must detect this and re-read changed files; inherited knowledge of changed files must not be trusted silently. When more than half of the predecessor's read-set changed, continuation is declined in favor of the digest-seeded fallback (see Clarifications).
- **Predecessor failed or was cancelled**: linking to a failed subagent's context may inherit a broken train of thought; the system must decide (and record) whether a failed predecessor is linkable, and never inherit from a cancelled run mid-tool-call.
- **Provider or model switched between phases**: a context built on one model/provider generally cannot be cache-reused on another; the link must be declined with a visible reason and the new subagent started fresh (or re-seeded cheaply).
- **Inherited context near the window limit**: a long predecessor context plus a new phase may not fit; the system must bound inherited context (e.g., prefer plan-file reference + banked knowledge over full inheritance) rather than overflow or blindly truncate the prefix and destroy cacheability.
- **Concurrent subagents of the same kind**: two parallel implementers must not both extend the same conversation stream and corrupt each other's prefixes; linking is for sequential continuation, and parallel dispatches need distinct streams.
- **First subagent of a task**: nothing to link to; must start fresh without error and still establish a linkable context for successors.
- **Plan file edited between phases**: the phase reference must resolve against the current plan; a subagent must not execute a phase description that no longer exists.
- **Session resumed after restart**: previously banked subagent contexts may or may not still be cache-warm at the provider (caches expire); the system must not assume warmth after long gaps and must not display stale "linked/cached" promises it cannot verify from provider-reported usage.
- **Legacy/one-shot mode**: linking must not break single-shot runs or older session files; absence of any linkable history degrades to today's behavior.
- **Main model gated mid-diagnosis**: a subagent fails and the main model must inspect implementation files to decide what to do next — the post-failure exemption applies and the read succeeds, recorded as an exempt read; the gate must never leave the orchestrator blind after a failure.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST durably retain, for each completed subagent, the material needed to continue its work: its conversation context (or a faithful bounded representation), the files it read with their identities/versions, and its structured result.
- **FR-002**: When dispatching a subagent whose assigned work continues or directly depends on a previously completed subagent's work within the same task or session, the system MUST link the new subagent to that predecessor's retained context instead of starting from zero, whenever linking is compatible with the provider, model, and context-window constraints. Linking is continuation-first: the new subagent extends the predecessor's conversation stream so the provider serves the shared prefix from cache; when continuation is blocked (window overflow, provider/model change, heavy staleness), the system MUST fall back to a digest-seeded fresh stream and record the fallback as the link form used.
- **FR-003**: The system MUST decide linkability by explicit, recorded criteria (same task lineage, same model/provider stream, predecessor completed in a linkable state, inherited size within bounds, predecessor not stale beyond repair — defined as at most half of the predecessor's read-set changed since it ran) and MUST record the decision and reason for every dispatch. Stream continuation is permitted for same-kind sequential chains and for a review subagent continuing the stream of the implementer whose work it reviews; all other cross-kind links use the digest-seeded fallback.
- **FR-004**: A linked continuation MUST reuse the predecessor's conversation prefix in a form the provider can serve from cache, and the system MUST verify reuse through provider-reported cache figures rather than assuming it.
- **FR-005**: A linked continuation MUST NOT re-read files whose inherited content is current, and MUST re-read files that changed after the predecessor read them; file currency MUST be checked, not assumed.
- **FR-006**: The main model's responsibilities in orchestrated tasks MUST be planning, decomposition, coordination, dispatch, and review; implementation-phase file reading and editing MUST be performed by subagents. The system MUST enforce this with a phase-scoped gate: while implementation phases are active, the main model's implementation-file reads are denied with guidance to dispatch a subagent instead. Exempt (and recorded when used): the plan file, subagent reports, and post-failure diagnosis reads. Outside implementation phases the main model reads without restriction.
- **FR-007**: The main model MUST be able to delegate a phase by reference — pointing the subagent at the relevant phase of the persisted implementation plan — without embedding file contents in the handoff; the subagent MUST read its phase from the plan and gather its own file context.
- **FR-008**: Handoffs MUST carry only non-discoverable facts (decisions made, constraints, results of prior phases not yet in the plan) within a documented size bound; content the subagent can read itself (plan text, file bodies, repository state) MUST NOT be duplicated into handoffs.
- **FR-009**: Subagent returns MUST be structured and bounded (outcome, changes made, verification status, carry-forward facts); oversized reports MUST be banked to durable task knowledge with a bounded digest returned in their place.
- **FR-010**: The system MUST maintain per-provider cache behavior profiles covering at minimum DeepSeek (direct) and MiniMax via OpenRouter: what constitutes a cacheable prefix, what invalidates it, minimum cacheable sizes, how cache usage is reported, and how linking must be performed on that provider — and MUST structure requests accordingly.
- **FR-011**: All conversation streams — main and subagent — MUST grow append-only with byte-stable prefixes within a session; no system-injected content (timestamps, reordered tools, rewritten history, variable boilerplate) may break an established prefix. (Reaffirms the existing stable-prefix guarantee and extends it to linked continuations.)
- **FR-012**: In mixed-provider configurations, the system MUST manage each provider's cache independently and MUST NOT apply one provider's caching assumptions to another; per-pairing cache performance MUST be tracked separately.
- **FR-013**: When linking is declined or impossible, the system MUST fall back to the most cache-efficient available start (e.g., stable shared subagent prefix plus plan reference) and MUST surface the declined-link reason in task visibility.
- **FR-014**: Repeated dispatches within a task MUST NOT re-transmit static material (instructions, tool definitions, plan reference) outside the stable cached prefix; per-dispatch variable content MUST be limited to the phase-specific handoff.
- **FR-015**: Task summaries and session statistics MUST report, per subagent dispatch: whether it was linked and to what, provider-reported cache read/write shares, and files read versus files inherited; estimates MUST be labeled as such and provider-reported figures preferred. Absence of data MUST be shown as unavailable, never fabricated.
- **FR-016**: The feature MUST NOT degrade task quality: linked continuations must complete their phases at least as reliably as cold-start subagents, and any conflict between reuse and correctness MUST resolve in favor of correctness (stale context re-read, link declined) per the project constitution.
- **FR-017**: Existing session files, one-shot mode, and configurations without linkable history MUST continue to work unchanged; the feature degrades to current behavior in the absence of linkable context.
- **FR-018**: All improvement claims for this feature MUST be measured against baselines captured on the unchanged system using the existing benchmark harness and provider-reported usage, under like-for-like conditions.

### Key Entities

- **Subagent Context Record**: the durable artifact of a completed subagent — bounded conversation context, identity of the model/provider stream it ran on, the set of files read with version identities, its structured result, and its completion state. The unit that continuation linking consumes.
- **Context Link**: the recorded relationship between a new dispatch and a predecessor's Context Record — including the linkability decision, its reason, the link form used (stream continuation or digest-seeded fallback), and the verification outcome (provider-reported cache share on first request).
- **Phase Handoff**: the bounded outbound contract of a dispatch — role, plan-phase reference, task statement, non-discoverable facts — and its inbound counterpart, the structured return.
- **Provider Cache Profile**: the per-provider description of caching reality — prefix rules, invalidation triggers, minimum sizes, reporting fields, linking method — that request construction must obey.
- **Delegation Ledger**: the per-task record of dispatches, links, declined links with reasons, and per-dispatch cache outcomes that feeds task summaries and measurement.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In multi-phase tasks, a continuation subagent's first request reports at least 60% of its input served from cache (provider-reported), versus approximately 0% for today's cold starts, measured across the benchmark suite's multi-phase tasks.
- **SC-002**: Duplicate file reading is eliminated for linked continuations: across benchmark multi-phase tasks, the number of file reads repeating a predecessor's still-current read is zero, and total file-read operations per task drop by at least 30% versus baseline.
- **SC-003**: In orchestrated tasks, at least 90% of implementation file reads are performed by subagents rather than the main model (baseline to be captured; today the main model performs a substantial share).
- **SC-004**: Total provider-billed cost for the benchmark's multi-phase tasks drops by at least 25% versus the pre-feature baseline at equal task-completion quality.
- **SC-005**: Steady-state cache-hit rate on subagent streams reaches at least 80% on DeepSeek and at least 70% on MiniMax-via-OpenRouter across the benchmark suite (cold first writes excluded, provider-reported).
- **SC-006**: Communication overhead — handoff plus return tokens as a share of total task tokens — stays at or below 10% across benchmark orchestrated tasks.
- **SC-007**: Task-completion quality does not regress: benchmark completion rate and validation outcomes are at least equal to baseline, and no correctness incident is attributable to stale inherited context in the acceptance run.
- **SC-008**: Every dispatch in every benchmark task has a recorded link decision with reason, and 100% of displayed cache figures trace to provider-reported usage or are labeled unavailable.

## Assumptions

- The gateway continues to provide sticky per-stream routing and passes through provider-reported cache usage; those figures remain the sole source of displayed cache metrics.
- The recommended mixed configuration (MiniMax M3 main via OpenRouter + DeepSeek subagents) and the DeepSeek-only configuration are the two pairings this feature must serve well; other providers inherit the generic profile and degrade gracefully.
- The existing benchmark harness and fixture suite (feature 011) is the measurement vehicle; live measured runs incur real provider cost and require the owner's explicit go-ahead, so baseline capture is planned but scheduled at the owner's discretion.
- "Related work" for linking purposes covers (a) phases and retries within one orchestrated task, and (b) a same-session follow-up task that the orchestrator judges directly related to the most recent completed subagent's work — same files/area, same provider stream, staleness checks passed. Cross-session linking is out of scope for this feature beyond honest handling of expired provider caches.
- Provider cache lifetimes are outside the system's control; the system verifies warmth from provider-reported usage rather than promising it.
- The existing stable-prefix and byte-determinism guarantees (project constitution, Principle III) remain in force and this feature builds on them rather than renegotiating them.
- No exemplar or competitor system prompt is available; comparisons are against the project's own baselines.
