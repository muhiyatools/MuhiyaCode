# Feature Specification: Competitive Agent Audit & Transformation

**Feature Branch**: `011-competitive-agent-audit`

**Created**: 2026-07-19

**Status**: Draft

**Input**: User description: "Conduct a full, deep, and ultimate audit of the MuhiyaCode Agent — the entire agent system, its weak points, its system prompts, how it instructs the LLM, and the connection between different LLMs (e.g., MiniMax M3 as main model with DeepSeek as sub-agent model, across different providers). Deep audit of prompt engineering and token efficiency. The automatic code-review sub-agent opens after almost every small task — the system must understand when a review is actually necessary and choose review depth based on the task, project, and codebase size, without becoming excessively expensive on large codebases. Ensure all tool calls and workflow steps instruct the LLM correctly without unnecessary token usage, including terminal usage versus cheaper dedicated tools. An exemplar system prompt will be attached for reference. Ensure ultra-high integration between different LLMs in the same session — model-specific and provider-specific caching, communication between models, and output transfer between main agent and sub-agents. Produce an exceptionally deep and comprehensive plan for transforming MuhiyaCode into a genuinely competitive coding agent that can compete with Claude Code, Codex, and Grok Build."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Evidence-Backed Audit & Transformation Roadmap (Priority: P1)

The product owner receives a comprehensive, evidence-backed audit of the entire agent: how each part of the workflow behaves today, where it wastes tokens, where its instructions to the model are weak or contradictory, how it behaves when the main model and sub-agent model come from different providers, and how it compares to competitor-class coding agents. Every weakness is documented with evidence (a transcript excerpt, a measurement, or a configuration reference), a severity, and a concrete remediation. The audit concludes with a prioritized, phased transformation roadmap in which every item traces back to a finding and forward to a measurable success criterion.

**Why this priority**: Every other improvement in this feature depends on knowing — with evidence, not impressions — what is actually wrong and how expensive each problem is. The roadmap is also the deliverable the owner explicitly asked for.

**Independent Test**: Can be fully tested by reviewing the audit document against the coverage list (every enumerated subsystem has findings with evidence, severity, and remediation) and confirming baseline measurements exist for token spend, review overhead, and cache behavior taken from real sessions before any change.

**Acceptance Scenarios**:

1. **Given** the current agent on real multi-turn coding sessions, **When** the audit is executed, **Then** baseline measurements exist for per-task token spend by task category, review-overhead share of total spend, per-provider cache hit rates, and trivial-task auto-review rate — all taken from provider-reported usage, not estimates.
2. **Given** the completed audit document, **When** the owner checks any enumerated subsystem (orchestration loop, instruction set, tool catalog and guidance, review policy, sub-agent handoff, terminal usage, caching behavior, context/history management), **Then** that subsystem has at least one finding with evidence, severity, and a remediation, or an explicit "no defect found" verdict with the evidence examined.
3. **Given** the transformation roadmap, **When** any roadmap item is inspected, **Then** it references the finding(s) it fixes and the success criterion it moves.
4. **Given** the exemplar system prompt provided by the owner, **When** the instruction-set audit runs, **Then** the agent's instructions are compared against it section by section, and each deviation is classified as keep (justified), adopt, or adapt.

---

### User Story 2 - Right-Sized Code Review (Priority: P2)

A user completes a small task — fixing a typo, renaming a variable, editing documentation, or a tiny low-risk change — and the agent finishes without launching an automatic review sub-agent. When the user completes a substantial or risky change (touching authentication, billing, concurrency, or a large surface), the agent runs a review whose depth matches the risk and size of the change — and tells the user, briefly, why that depth was chosen. On very large codebases, a review examines the changed surface and its direct blast radius, never the whole repository, and always stays within a spending ceiling.

**Why this priority**: This is the owner's most concrete pain today: a review sub-agent opens after almost every small task, multiplying the cost of trivial work. It is also the most user-visible quality-of-life change.

**Independent Test**: Can be fully tested by running a fixed suite of tasks spanning trivial → risky categories and asserting which ones triggered a review, at what depth, at what cost, and with what user-visible rationale.

**Acceptance Scenarios**:

1. **Given** a documentation-only or comment-only change, **When** the task completes, **Then** no automatic review runs and the completion summary notes that review was skipped and why.
2. **Given** a one-line change that removes an authentication check, **When** the task completes, **Then** a review DOES run despite the small diff, because risk signals — not diff size alone — drive the decision.
3. **Given** a multi-file logic change in a large codebase, **When** the review runs, **Then** it examines only the changed files and their direct dependents, reports which surface it covered, and its token spend stays under the configured ceiling.
4. **Given** any task where the user explicitly requests a review, **When** the request is made, **Then** the review always runs at the requested (or default) depth regardless of gating.
5. **Given** the gating configuration, **When** the user sets it to "off" or "conservative", **Then** automatic review behavior follows the configured mode.

---

### User Story 3 - Token-Efficient Task Execution (Priority: P3)

A user runs everyday coding tasks and the agent completes them with visibly leaner resource usage: it reads files with the cheapest adequate tool instead of shelling out to the terminal, never re-reads content it already holds, keeps tool outputs bounded, and carries no bloated or redundant instructions. The same tasks complete with the same or better quality at measurably lower cost.

**Why this priority**: Token efficiency is the economic foundation of a competitive agent — it compounds across every session — but it depends on the audit (P1) to locate the waste precisely.

**Independent Test**: Can be fully tested by replaying a fixed benchmark suite of multi-turn coding tasks before and after the change and comparing total spend, tool-selection choices, and task completion rates.

**Acceptance Scenarios**:

1. **Given** a task requiring the content of a file, **When** the agent accesses it, **Then** it uses the dedicated file-reading capability rather than a terminal command, except where the terminal is genuinely required (e.g., filtered/derived output).
2. **Given** a file already read this session and unchanged since, **When** the model attempts to read it again, **Then** the duplicate expenditure is prevented or redirected to the content already in context.
3. **Given** the benchmark suite, **When** run after the transformation, **Then** median small-task total spend drops by the target percentage with no loss in completion rate.
4. **Given** the stable instruction content, **When** any change adds instruction text, **Then** the addition fits within the defined instruction budget or carries a written justification.

---

### User Story 4 - First-Class Mixed-Model Sessions (Priority: P4)

A user configures the main agent on one model (e.g., MiniMax M3) and the sub-agent on another from a different provider (e.g., DeepSeek). The session behaves as one coherent system: each model receives instructions in the dialect it understands best, each provider's caching keeps working independently at full effectiveness, work handed to the sub-agent arrives as a focused brief rather than a transcript dump, and results return in a structured form the main agent integrates without duplication. If a sub-agent fails, the main flow says so explicitly and continues; nothing is silently lost.

**Why this priority**: Mixed-model operation is a differentiating capability and the owner's explicit ambition, but it builds on the caching, handoff, and instruction improvements grounded in P1–P3.

**Independent Test**: Can be fully tested by running the benchmark suite with a mixed-provider pairing and comparing each model's cache behavior, handoff sizes, and task outcomes against same-model baselines.

**Acceptance Scenarios**:

1. **Given** a session with main and sub-agent models on different providers, **When** multiple turns and sub-agent tasks execute, **Then** each provider's steady-state cache hit rate stays within the defined tolerance of its single-model baseline.
2. **Given** a sub-agent task dispatch, **When** the handoff is inspected, **Then** it contains a structured brief (goal, constraints, selected relevant context) whose size is bounded, not the full session transcript.
3. **Given** a completed sub-agent task, **When** its result returns, **Then** the main agent receives a structured outcome (result, artifacts changed, follow-ups) and does not re-ingest content it already holds.
4. **Given** a sub-agent that fails or times out, **When** the main flow continues, **Then** the failure and any partial results are explicitly surfaced in the session, labeled as partial.

---

### User Story 5 - Competitive Benchmark Standing (Priority: P5)

The owner runs a repeatable benchmark suite of realistic multi-turn coding tasks and sees where MuhiyaCode stands — task completion rate, cost per completed task, turns per task, review precision — before and after the transformation, with results stored so any run can be reproduced. The suite doubles as the regression gate for all future prompt and workflow changes.

**Why this priority**: "Competitive with Claude Code, Codex, and Grok Build" only means something if it is measured; the suite turns ambition into a number. It is last because it validates the work of P1–P4.

**Independent Test**: Can be fully tested by running the suite twice on an unchanged build (results must be stable) and once after any change (results must be attributable to that change).

**Acceptance Scenarios**:

1. **Given** an unchanged build, **When** the suite runs twice under identical configuration, **Then** headline metrics are stable within a defined variance band.
2. **Given** the transformed agent, **When** the suite runs against the pre-transformation baseline, **Then** completion rate is equal or better and cost per completed task is lower.

---

### Edge Cases

- A one-line change in a security- or billing-critical area: small diff, high risk — gating must review it (risk signals outrank size).
- The user explicitly requests a review of a trivial change: always honored, gating never blocks an explicit request.
- A greenfield/near-empty project: heavyweight review depths must not fire on initial scaffolding.
- A provider that reports no cache-usage fields: the session proceeds normally and reporting shows "not reported" rather than fabricated numbers.
- Main and sub-agent configured to the same model: handoff and caching behave correctly in the degenerate single-model case.
- The user switches the main model mid-session: caching expectations reset honestly (one cold turn), and reporting reflects the reset rather than misattributing a miss.
- A sub-agent produces an oversized result: the return package is bounded/summarized with the full artifact reachable, never dumped raw into the main context.
- The exemplar system prompt is never provided: the instruction-set audit proceeds against publicly documented competitor patterns and says so.
- The review ceiling is hit mid-review on a huge change: the review reports what it covered and what it skipped, rather than silently stopping or overspending.

## Requirements *(mandatory)*

### Functional Requirements

**Audit & Roadmap**

- **FR-001**: The audit MUST cover every one of these subsystems: the orchestration loop; the full instruction set (system prompts and per-tool guidance); the tool catalog and its cost characteristics; the automatic review policy; sub-agent dispatch, handoff, and result integration; terminal usage policy; per-provider caching behavior; mixed-model session handling; context/history management; and token accounting. Each subsystem receives findings with evidence (transcript excerpt, measurement, or configuration reference), a severity, and a remediation — or an explicit "no defect found" verdict with the evidence examined.
- **FR-002**: The audit MUST establish measured baselines before any change: per-task token spend by task category, review overhead as a share of task spend, trivial-task auto-review rate, per-provider cache hit rates, and cheapest-tool violation rate — all from provider-reported usage on real multi-turn sessions, never from estimates presented as measurements.
- **FR-003**: The instruction-set audit MUST compare the agent's instructions against the owner-provided exemplar system prompt (when supplied) and against publicly documented competitor-class patterns; every material deviation is classified keep (with justification), adopt, or adapt.
- **FR-004**: The audit MUST conclude with a prioritized, phased transformation roadmap in which every item traces to at least one finding and at least one success criterion of this specification.

**Review Gating & Depth**

- **FR-005**: The system MUST decide whether an automatic review runs from a task profile — at minimum: change size, risk category of the touched areas, task type (documentation/comment/rename/formatting versus logic), test outcome, and codebase size — never unconditionally.
- **FR-006**: Defined trivial categories (documentation-only, comment-only, formatting-only, pure renames with passing tests, and equivalently low-risk micro-changes) MUST NOT trigger an automatic review.
- **FR-007**: Review depth MUST scale across at least three tiers (skip / focused / deep), selected from the same task profile, and the chosen tier with a one-line rationale MUST be visible to the user.
- **FR-008**: Review spending MUST be bounded by both a proportional cap relative to the task's own spend and an absolute per-tier ceiling; on large codebases the review MUST scope to the changed surface and its direct blast radius, never a whole-repository sweep, and MUST report the surface it covered.
- **FR-009**: An explicit user request for review MUST always run regardless of gating, and the user MUST be able to configure gating behavior (off / conservative / default).

**Instruction & Token Efficiency**

- **FR-010**: The instruction set MUST direct the model to prefer the cheapest adequate capability for each action — dedicated file reading over terminal reads, targeted search over exhaustive listing — and violations MUST be detectable in session transcripts for measurement.
- **FR-011**: The system MUST prevent redundant expenditure: duplicate reads of unchanged content already in context are blocked or redirected; unchanged content is not retransmitted when avoidable; oversized tool outputs are bounded with the full artifact reachable on demand.
- **FR-012**: The stable instruction content MUST fit a defined token budget; any addition beyond the budget requires written justification recorded with the change.
- **FR-013**: Per-turn dynamic content (task classification, budgets, goals, mode hints) MUST ride in per-turn positions and MUST NOT be interleaved into stable instruction content or settled history.

**Mixed-Model Integration**

- **FR-014**: Sessions MUST support a main model and sub-agent model from different providers, with each request formed in the receiving model's dialect (reasoning/effort conventions, formatting expectations) and no leakage of one provider's dialect into another's requests.
- **FR-015**: Caching MUST be tracked and optimized per model+provider independently within a session; sub-agent activity on one provider MUST NOT degrade the main model's cache effectiveness on another, and per-model cache metrics MUST be visible.
- **FR-016**: Main-to-sub handoff MUST be a structured, bounded brief (goal, constraints, selected relevant context); sub-to-main return MUST be a structured outcome (result, artifacts changed, follow-ups) integrated without re-ingesting content the main context already holds.
- **FR-017**: Sub-agent failure or timeout MUST surface explicitly in the main flow with any partial results labeled as partial; no silent loss.

**Benchmarking**

- **FR-018**: A repeatable benchmark suite of realistic multi-turn coding tasks MUST exist covering the trivial→risky task spectrum and both single-model and mixed-model configurations; it MUST measure completion rate, cost per completed task, turns per task, review trigger precision, and per-provider cache rates, and its procedures and raw results MUST be stored with the feature artifacts so any reviewer can rerun them.

### Key Entities

- **Audit Finding**: One evidenced weakness or verdict — subsystem, evidence reference, severity, remediation, and links to roadmap items.
- **Task Profile**: The signals describing a completed task — change size, touched-area risk category, task type, test outcome, codebase size — that drive review gating and depth.
- **Review Decision**: The gating outcome for one task — tier chosen (skip/focused/deep), rationale, spend cap applied, surface covered.
- **Model Pairing**: The session's main-model and sub-agent-model assignment, including provider identity and per-model dialect/caching expectations.
- **Handoff Brief / Return Package**: The structured content passed main→sub (goal, constraints, selected context) and sub→main (outcome, artifacts, follow-ups), each with size bounds.
- **Baseline & Benchmark Run**: A recorded measurement set — configuration, task suite version, provider-reported usage, outcomes — comparable across runs.
- **Transformation Roadmap Item**: One prioritized change — finding(s) addressed, success criteria moved, phase assignment.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Automatic review rate on trivial-category tasks drops from the audited baseline (approximately every task today) to under 10% on the benchmark suite and sampled real sessions.
- **SC-002**: At least 95% of high-risk changes in the benchmark suite still receive an automatic review, and 100% of explicit user review requests run — no under-review regression.
- **SC-003**: Median total spend for small tasks falls by at least 30% versus the audited baseline at an equal or better task completion rate.
- **SC-004**: Review overhead is at most 20% of total task spend at the median for small and medium tasks, and the per-tier ceiling is honored in 100% of large-codebase benchmark runs.
- **SC-005**: In mixed-provider sessions, each model's steady-state cache hit rate stays within 5 percentage points of its single-model baseline.
- **SC-006**: Cheapest-tool violations (terminal reads where a dedicated read suffices, duplicate reads of unchanged content) fall below 2% of file-access actions in sampled sessions, from the audited baseline.
- **SC-007**: The audit covers 100% of the subsystems enumerated in FR-001, every finding carries evidence + severity + remediation, and every roadmap item traces to at least one finding and one success criterion.
- **SC-008**: The benchmark suite is stable — two runs on an unchanged build agree within a defined variance band — and the post-transformation run completes at least the baseline completion rate at a lower cost per completed task.
- **SC-009**: Every gated review decision presents a user-visible one-line rationale (100% of decisions on the benchmark suite).

## Assumptions

- **Scope spans the agent and its gateway where necessary**: mixed-model caching and per-provider behavior involve both the agent and the platform gateway; the agent is the primary subject, and gateway-side work is in scope only where a requirement (caching visibility, per-model metrics) cannot be met agent-side.
- **The exemplar system prompt arrives separately**: the owner stated it will be attached. The audit proceeds regardless; FR-003's exemplar comparison activates when it is provided, and until then the comparison uses publicly documented competitor-class patterns (the spec treats the exemplar as an input dependency, not a blocker).
- **"Competitive with Claude Code, Codex, and Grok Build" is operationalized internally**: via the FR-018 benchmark suite and a capability-parity checklist produced by the audit — not via third-party leaderboards, which are out of scope.
- **Baselines precede changes**: all "reduction" criteria are measured against baselines captured on the current build per the project constitution's honest-measurement and verified-improvement principles.
- **Model catalog**: the models involved are those the platform's gateway currently offers (DeepSeek family, MiniMax family, and models reachable via aggregator providers); no new provider integrations are required by this feature.
- **Gating defaults are refined during design**: the trivial-category list and tier thresholds in FR-005–FR-008 start from the definitions here and are tuned during the design phase using audit data; the user-facing configuration surface (off/conservative/default) is fixed by FR-009.
- **Incremental transformation**: per the project constitution, existing behavior is improved through smallest-sufficient changes; no subsystem rewrite is assumed, and any rewrite proposal must carry written justification in the plan.
- **Existing capabilities are preserved**: session durability, permission and security models, Arabic/RTL support, and current caching guarantees must not regress; the benchmark suite guards completion quality alongside cost.
