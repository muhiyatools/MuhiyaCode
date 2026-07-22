# Feature Specification: Token Economy Overhaul

**Feature Branch**: `014-token-economy-overhaul`
**Created**: 2026-07-22
**Status**: Landed (All phases completed and validated)
**Input**: Reduce MuhiyaCode's total cached and uncached token consumption to coding-agent best-in-class levels without weakening correctness, continuity, cache stability, safety, or user trust.

## Problem statement

MuhiyaCode's prompt cache is healthy, but its token economy is not. The supplied tic-tac-toe session ended with 34,209 live context tokens yet accumulated 494,156 input tokens and 19,810 output tokens. Of the input, 444,941 tokens were cache reads and 49,215 were uncached, for a 90.04% hit rate and 6.46 credits. The cache reduced the price of replay; it did not eliminate replay. The approximate replay amplification is `494,156 / 34,209 = 14.44x`, which means the session paid to process roughly fourteen current-context equivalents.

Repository evidence confirms that this is systemic rather than a one-off. The frozen feature-011 baseline completed trivial documentation and comment tasks in 7-10 main turns with 59,927-89,240 prompt tokens. Current default budgets still allow 10 tiny turns, 16 small turns, 26 standard turns, and much higher ceilings after escalation. The current wire-prefix golden is 22,005 bytes, including a 5,966-character system prompt and a substantially larger always-on tool-definition block. Tool results, assistant reasoning, skill bodies, prior-task history, and governor messages then grow the replayed tail.

The optimization target is therefore total economic work, not cache-hit percentage alone. A successful system may report fewer cached tokens because it avoids retransmitting them. That is an improvement when quality is unchanged.

## User scenarios and testing

### User Story 1 - Small changes remain small (Priority: P1)

A user requests a typo fix, label change, style adjustment, one-file bug fix, or small UI component. MuhiyaCode inspects only the required evidence, performs the change, runs one proportionate check, and finishes without unnecessary planning, onboarding, review, rereads, or synthesis turns.

**Independent test**: Run the same 20-task trivial/small fixture matrix twice against the same model and gateway before and after the feature. The optimized build must preserve completion and correctness while meeting the budgets in SC-001 through SC-005.

**Acceptance scenarios**:

1. Given a one-line change with a named file, when the file is already known, then the task finishes in no more than three main model requests unless a tool or provider failure occurs.
2. Given a small single-file UI change, when no project test harness exists, then the agent does not invent a runtime, scratch harness, broad audit, tasks.md file, or review pass.
3. Given a greenfield single-page tic-tac-toe fixture, when the task is clear, then total provider-reported input is at most 120,000 tokens, output is at most 6,000 tokens, and completion quality matches the baseline rubric.
4. Given a tool failure, when one corrected retry can recover, then the retry is allowed and its cost is separately attributed; the normal budget is not silently enlarged into an open-ended loop.

### User Story 2 - Long sessions do not replay unrelated work (Priority: P1)

A user keeps one visible MuhiyaCode session open across multiple tasks. The model remains fixed for the session, but the context compiler distinguishes a follow-up from unrelated work. Follow-ups retain the active task's working evidence. Unrelated tasks start a new context epoch containing only stable instructions, project state, relevant prior task capsules, and the new task.

**Independent test**: Run a six-task session containing two follow-up chains and two unrelated task transitions. Compare output and repository state with a full-history control. The optimized build must retrieve every required prior fact while reducing cumulative prompt tokens by at least 55%.

**Acceptance scenarios**:

1. Given a direct follow-up such as "make the button blue instead," then the current task epoch continues and the affected file/diff evidence remains available.
2. Given an unrelated task in the same visible session, then a new context epoch begins automatically without changing the selected model or upstream affinity.
3. Given a new task that depends on an older decision, then the relevant signed task capsule is retrieved; unrelated transcript and raw tool output are not.
4. Given uncertain relatedness, then the system keeps the current epoch or asks one concise question; it never silently discards potentially required state.

### User Story 3 - Tool output becomes addressable evidence, not permanent transcript (Priority: P1)

Large file reads, diffs, test logs, build logs, web results, and MCP responses are stored in a local evidence store. The model receives a compact, truthful observation with an artifact handle, fingerprints, salient failures, and bounded excerpts. It can fetch a range or expand the artifact only when needed.

**Independent test**: Feed successful and failing outputs from Go, npm, pytest, Git, shell, file-read, grep, and MCP fixtures. Verify lossless artifact storage, faithful summaries, bounded inline tokens, and exact range retrieval.

**Acceptance scenarios**:

1. A successful test run returns status, duration, package/test counts, and warnings in a bounded result; full output remains retrievable.
2. A failing test run preserves every failure name, file/line location, error class, and a bounded diagnostic neighborhood.
3. A file read returns requested lines exactly. A structural or search query may return only relevant symbol/range excerpts with source coordinates.
4. No reducer may turn an incomplete scan, skipped file, truncated process, or partial test run into an unqualified success.

### User Story 4 - The fixed prompt and tool surface are lean (Priority: P1)

Every session loads only a small core ACI. Rare tools, MCP schemas, memory mutation tools, and specialized skill bodies are discoverable on demand without changing the stable core prefix. The main prompt contains universal rules only; specialized workflow guidance is task-scoped or tool-scoped.

**Independent test**: Serialize the actual wire request for clean sessions with no MCP, many MCP servers, zero skills, and many skills. Verify byte-stable core prefixes and the budgets in SC-006.

**Acceptance scenarios**:

1. Unused MCP tool schemas consume zero model tokens before discovery.
2. The full installed-skill catalog and skill bodies are not injected into every request.
3. Common file/search/edit/check operations remain direct tools; rare tools use a stable discovery/invocation broker.
4. Tool discovery adds information only at the newest task tail and never mutates settled prefix bytes.

### User Story 5 - Reasoning and output scale with the next decision (Priority: P1)

The runtime selects a per-request output cap and reasoning tier from the current execution phase, task class, remaining budget, provider requirements, and truncation history. It does not give every request the task's maximum possible output allowance.

**Independent test**: Replay deterministic response fixtures for inspect, edit, verify, final, truncation, and malformed-tool-call phases across MiniMax, DeepSeek, and generic OpenAI-compatible profiles.

**Acceptance scenarios**:

1. Tiny and small tasks use low reasoning by default even when the global user effort is medium, unless a risk or observed failure justifies escalation.
2. A tool-selection turn has a smaller output ceiling than a final architecture explanation.
3. `finish_reason=length` triggers one phase-specific bounded escalation; it never resets the entire task budget.
4. Required MiniMax interleaved-thinking blocks remain intact within a tool-use chain; the optimizer never strips provider-required reasoning replay.

### User Story 6 - Cache behavior is provider-aware and economically rational (Priority: P2)

MuhiyaCode uses a normalized cache-capability contract. MiniMax may use its Anthropic-compatible explicit cache interface where configured and verified; DeepSeek uses automatic prefix caching; generic endpoints degrade safely. Cache decisions minimize credits or monetary cost, not merely maximize hit-rate.

**Independent test**: Run cold, warm, idle-expired, prefix-change, tool-discovery, task-epoch, and resume cases through provider simulators plus paid canaries.

**Acceptance scenarios**:

1. Cache read, creation/write, uncached input, output, TTL, and attribution fields are reported separately when the provider supplies them.
2. A context reset occurs only when its predicted replay savings exceed cold-prefix and quality-risk costs, except at explicit user boundaries.
3. The model selector remains outside the main execution context and runs at most once before the first main request.
4. No optimization changes the selected model mid-session.

### User Story 7 - Operators can see where tokens went (Priority: P2)

The context and task reports expose provider-reported totals plus clearly labeled local attribution by category: stable prefix, current task, prior-task capsules, raw/compacted observations, reasoning replay, auxiliary calls, retries, and final output.

**Independent test**: Inject exact usage fixtures and compare task, session, JSON benchmark, and TUI renderings for consistency.

**Acceptance scenarios**:

1. Reports distinguish live context size from cumulative tokens and replay amplification.
2. Reports celebrate lower total cache reads when caused by fewer replays; they do not call it a cache regression.
3. Estimated category splits reconcile to the exact provider total within the documented calibration tolerance and are visibly labeled estimated.
4. Every budget override has a reason code and the request sequence that caused it.

## Edge cases

- A new prompt is short but semantically depends on an old task whose capsule has gone stale after external file changes.
- The user alternates rapidly between two related tasks; epoch thrashing must be prevented with hysteresis.
- The provider reports cache reads but not uncached tokens, or reports OpenAI and Anthropic usage fields with different semantics.
- A MiniMax tool-use response contains thinking/signature blocks that must be replayed verbatim.
- A tool output is binary, invalid UTF-8, secret-bearing, enormous, or changes while a range is being fetched.
- A reducer does not recognize the test runner; it must use a generic bounded head/tail/error extractor and mark the result generic.
- The model asks for an artifact that was garbage-collected, belongs to another workspace, or has a stale fingerprint.
- The workspace has thousands of MCP tools or skills; discovery indexes must stay bounded without hiding exact matches.
- A context checkpoint or task capsule cannot be persisted; the system must keep the existing context and fail closed rather than discard history.
- A budget would be exceeded while required correctness verification is still pending; correctness wins and the override is attributed.
- A low output cap truncates a write payload or tool call. The partial call is never executed, and one bounded recovery is permitted.
- A task starts as tiny but reveals broad or risky scope. The state machine may escalate once per evidence-backed reason, not on vague model preference.
- The user explicitly requests exhaustive analysis, a long document, maximum reasoning, or a full audit. Explicit scope changes the budget but not the accounting.

## Functional requirements

### Measurement and budgets

- **FR-001**: Persist one `RequestEconomyRecord` per provider request with exact provider-reported prompt, output, cache-read, cache-write/creation, uncached-input, duration, model, transport, pin, task epoch, phase, retry cause, and finish reason where available.
- **FR-002**: Compute replay amplification as cumulative main-stream prompt tokens divided by the maximum main-stream prompt tokens for the measured scope. If either member is unavailable, report unavailable.
- **FR-003**: Attribute local context categories from the exact serialized request manifest. Attribution that depends on token estimation MUST be labeled estimated and MUST reconcile to provider totals with an explicit residual bucket.
- **FR-004**: Enforce independent budgets for main requests, auxiliary requests, cumulative prompt tokens, cache-miss tokens, output tokens, tool-result inline tokens, and wall-clock time.
- **FR-005**: Budget overruns MUST use enumerated reason codes. Correctness, safety, explicit user scope, provider recovery, and required verification are valid overrides; convenience is not.
- **FR-006**: A shadow mode MUST calculate proposed decisions and savings without changing requests before enforcement ships.

### Execution control

- **FR-007**: Replace generic turn ceilings as the primary controller with a deterministic phase state machine: `orient -> inspect -> change -> verify -> finish`, with evidence-based transitions and bounded recovery edges.
- **FR-008**: The task classifier MUST remain local and non-billable. No LLM classification or onboarding request may run for clear tiny/small tasks.
- **FR-009**: The model advisor MUST remain isolated, pre-main-session, once-only, and optional. Its tokens MUST be separately attributed.
- **FR-010**: The runtime MUST select reasoning and output ceilings per request phase. The existing user effort becomes a maximum quality envelope, not a mandate to spend the maximum on every turn.
- **FR-011**: The runtime MUST prefer one batched independent inspection request, one batched mutation request, and one proving verification over serial one-tool turns when tool independence and safety allow.
- **FR-012**: Runtime-injected governor messages MUST be deduplicated, state-derived, and bounded. Repeated prose nudges MUST NOT accumulate in history.

### Context and memory

- **FR-013**: Separate the visible user session from provider context epochs. A session has one immutable model; it may contain multiple task epochs.
- **FR-014**: Relatedness decisions MUST use deterministic lexical/path/symbol/workspace signals first, include hysteresis, and retain context on uncertainty.
- **FR-015**: Each completed task MUST emit a bounded, signed `TaskCapsule` containing goal, outcome, changed files and fingerprints, checks, durable decisions, unresolved risks, and evidence handles.
- **FR-016**: A new task epoch MUST retrieve only relevant valid capsules and project-memory entries within a token budget using hybrid lexical/path/symbol scoring plus diversity selection.
- **FR-017**: Raw tool observations MUST live in an evidence store keyed by content hash and workspace/session ownership. Model-visible results MUST be bounded observations with retrievable handles.
- **FR-018**: Compaction MUST be query-aware, preserve source coordinates and exact critical literals, and run only after lossless eviction/retrieval options are exhausted or an economic break-even rule favors it.
- **FR-019**: Context-epoch and compaction state changes MUST be transactional with the session journal or MUST leave the previous valid context active on failure.

### Prompt, tools, skills, and MCP

- **FR-020**: The stable system prompt MUST contain only universal identity, safety, editing, verification, and communication rules. Project/task/specialty guidance MUST be outside the stable core.
- **FR-021**: The always-on direct tool surface MUST be limited to high-frequency primitives and one stable discovery/invocation broker. The exact core set is decided from telemetry, not preference.
- **FR-022**: MCP tools and rare built-ins MUST be deferred until discovered. Their schemas MUST NOT be inserted into the stable tool prefix mid-epoch.
- **FR-023**: The broker MUST enforce the original tool's schema, permissions, audit identity, secret handling, and result reducer; it is not a security bypass.
- **FR-024**: Skill routing metadata MAY be loaded at session start only under a strict budget. Full skill bodies MUST be section-addressable and task-scoped. No lossy generated summary may silently replace mandatory skill rules.
- **FR-025**: Tool results MUST use semantic reducers for recognized operations and a truthful generic reducer otherwise. Full raw data remains retrievable until its retention policy expires.

### Provider cache architecture

- **FR-026**: Add a provider capability matrix covering automatic versus explicit caching, cache breakpoints, TTLs, minimum cacheable tokens, usage-field semantics, thinking replay, tool-schema order, and invalidation triggers.
- **FR-027**: Add an Anthropic-compatible transport path for MiniMax behind capability detection and feature gating; preserve the generic OpenAI-compatible path.
- **FR-028**: Explicit breakpoints MUST follow each provider's documented order and block/lookback limits. Unsupported controls MUST never be sent.
- **FR-029**: Cache reset/epoch decisions MUST use a documented break-even model with provider-specific price or credit weights and a quality-risk gate.
- **FR-030**: Prefix stability tests MUST cover serialized wire bytes, tool ordering, discovered-tool behavior, system prompt, project context, resume, upstream routing, and task epochs.

### Quality and rollout

- **FR-031**: Every efficiency benchmark MUST pair token results with execution-based correctness, changed-file diff, tests, safety violations, and user-facing completion quality.
- **FR-032**: No phase may ship if it improves tokens while regressing correctness beyond the non-inferiority margin in the benchmark contract.
- **FR-033**: Rollout MUST support `off`, `observe`, `balanced`, and `aggressive` modes, with `balanced` becoming default only after canary gates pass.
- **FR-034**: All new persistent records MUST be backward compatible or have an idempotent migration and rollback path.
- **FR-035**: Documentation and the `/context` UI MUST explain that cached-token volume is still consumption and that the goal is to reduce total replays while preserving cache efficiency.

## Key entities

- **TaskEpoch**: One related chain of work inside a visible session, bound to the session's immutable model and upstream.
- **ExecutionBudget**: Hard/soft limits and remaining balances for requests, inputs, outputs, tools, retries, and time.
- **RequestPlan**: Phase-specific reasoning tier, output cap, allowed direct tools, expected evidence, and stop conditions for the next provider request.
- **RequestEconomyRecord**: Provider-exact request accounting plus local category attribution.
- **ContextManifest**: Ordered inventory of every request segment, byte range, source, stability class, fingerprint, and estimated tokens.
- **EvidenceArtifact**: Full tool output stored off-context with content hash, metadata, security label, and retention.
- **ObservationCard**: Bounded model-facing rendering of an evidence artifact.
- **TaskCapsule**: Durable, bounded result and dependency summary for retrieval by later epochs.
- **ToolDescriptor**: Compact searchable metadata for deferred tools; full schema remains outside the prompt until invocation through the broker.
- **ProviderCacheProfile**: Cache semantics, pricing weights, breakpoints, TTL support, replay requirements, and measurement mappings.
- **EconomyDecision**: A keep/reset/compact/defer/escalate decision with inputs, predicted savings, risk gate, and actual outcome.

## Success criteria

- **SC-001**: Median trivial-task main requests <=3 and p95 <=4, excluding provider/network retry requests that are separately reported.
- **SC-002**: Median small-task main requests <=5 and p95 <=7.
- **SC-003**: Median provider-reported cumulative input falls at least 70% for trivial tasks and 60% for small tasks relative to a frozen same-model baseline.
- **SC-004**: Median output tokens fall at least 50% for trivial/small tasks without increasing incomplete or truncated responses.
- **SC-005**: The tic-tac-toe reproduction consumes <=120,000 input tokens, <=6,000 output tokens, <=2.0 credits when the same credit schedule applies, and no more than six main requests.
- **SC-006**: The no-MCP/no-skill always-on wire prefix is <=10,000 bytes and <=2,500 measured/estimated tokens; adding configured but unused MCP servers or skills changes it by <=256 bytes.
- **SC-007**: Default inline tool-result payload is <=1,500 tokens per provider turn and <=3,000 tokens for a failing diagnostic, with full artifact retrieval available.
- **SC-008**: Warm cache-hit rate remains >=85% on reporting providers, but a lower total cache-read token count is accepted when total weighted cost falls.
- **SC-009**: Replay amplification is <=4.0x for trivial/small tasks and <=6.0x for standard tasks at p95.
- **SC-010**: Unrelated-task epoch transitions reduce the next task's first prompt by >=60% versus full-history continuation while retrieving 100% of rubric-required prior facts.
- **SC-011**: Execution-based completion and correctness are non-inferior to baseline: no more than a 2 percentage-point drop overall, no drop at all for security/payment/migration fixtures, and no new safety violations.
- **SC-012**: Two repeated live runs per provider/configuration fall within the benchmark's declared variance band, with raw provider usage retained.
- **SC-013**: Prefix-shape, persistence, crash-recovery, race, and full repository gates pass on all supported operating systems before default rollout.
- **SC-014**: The TUI and benchmark JSON reconcile provider totals exactly; estimated category allocations plus residual equal the exact total.

## Assumptions

- The current immutable per-session model decision remains in force.
- Prefix caching lowers price/latency but does not make repeated cached tokens free in the user's credit model.
- The OpenAI-compatible transport remains necessary for generic providers.
- MiniMax's Anthropic-compatible endpoint is optional until live compatibility tests prove tool use, streaming, cache accounting, and thinking replay for the deployed gateway.
- Raw reasoning blocks required by a provider are preserved inside an active tool-use chain. Savings come from lower reasoning effort, fewer calls, and epoch boundaries, not unsafe deletion.
- All numerical targets are release gates to validate, not claims that the current build already meets them.
