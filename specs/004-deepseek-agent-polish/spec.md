# Feature Specification: Coding Agent Quality Polish & DeepSeek V4 Optimization

**Feature Branch**: `004-deepseek-agent-polish`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: "Create a feature to overhaul and optimize the MuhiyaCode agent orchestration, mode-awareness, plan lifecycle, tool execution UI, and DeepSeek V4 integration. The feature must ensure that completed plans are correctly marked as finished and never continue displaying a message suggesting that they are still executable. When a Write or Edit tool is shown, display the affected file path immediately after the tool name and before the lines-added and lines-removed UI. Make every agent fully aware of the capabilities, restrictions, available tools, and allowed actions in its current mode. Prevent agents from attempting unsupported actions, calling unavailable tools, or using capabilities that are disabled in the active mode. Perform a complete audit of the coding-agent harness to identify and fix common orchestration failures, invalid tool calls, incorrect mode transitions, delegation problems, unfinished task states, repeated work, unnecessary tool usage, and other reliability issues commonly found in coding agents. Optimize the existing system prompt and prompt-delivery architecture specifically for DeepSeek V4 models. Do not increase the system prompt unnecessarily. Improve its structure, ordering, clarity, stability, cache compatibility, and instruction priority while removing duplication, ambiguity, and conflicting instructions. Research and account for DeepSeek V4 reasoning behavior, strengths, weaknesses, tool-use patterns, instruction-following behavior, and delegation performance. Adapt MuhiyaCode's orchestration, task decomposition, tool selection, subagent delegation, context management, validation, and recovery behavior to guide DeepSeek models as effectively as possible. The initial production launch will use only DeepSeek models through the MuhiyaLLM gateway. Therefore, ensure that the entire MuhiyaCode harness is deeply optimized for DeepSeek, including system prompting, tool schemas, tool-result formatting, context construction, mission delegation, retry behavior, error recovery, completion detection, and final-response generation. The final result must maximize DeepSeek model quality, reliability, token efficiency, cache stability, tool accuracy, and coding performance without bloating the system prompt or adding unnecessary orchestration complexity. It is a polish plan for the coding-agent quality the end user experiences when they use it."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Reliable Task Execution on the Launch Model (Priority: P1)

A developer gives MuhiyaCode a coding task in the launch configuration — a DeepSeek V4 model served through the MuhiyaLLM gateway, the only configuration shipping at launch. The agent breaks the work down sensibly, calls only tools that exist and are currently available, does not redo work whose results it already has, recovers from transient failures without losing progress, recognizes when the goal is genuinely achieved, and closes with a final response that accurately reflects what was actually done. The session never stalls in a loop, never leaves work items dangling as "in progress" after the run ends, and never claims work it did not perform.

**Why this priority**: Every launch user's every interaction flows through this path, and every reliability failure — an invalid tool call, a repeated step, an unfinished task, a runaway retry — burns the user's credits and their trust. This is the core of the "polish" the feature exists to deliver; all other stories refine specific surfaces of it.

**Independent Test**: Run the benchmark suite of representative multi-turn coding tasks (feature development, bug fix, refactor, codebase Q&A) against the launch configuration, before and after. Compare invalid-tool-call rate, redundant-call rate, task completion rate, terminal-state rate, and tokens per completed task, using provider-reported measurements only.

**Acceptance Scenarios**:

1. **Given** a multi-step coding task, **When** the agent finishes its run, **Then** every work item it started is in a definite terminal state (completed, failed, or cancelled) and none remains displayed as in progress.
2. **Given** a result the agent already obtained this session (e.g., an unchanged file it already read), **When** the agent plans its next action, **Then** it does not re-issue an identical request for the same unchanged result, and any such attempt is intercepted with feedback pointing at the existing result.
3. **Given** a tool call that fails because of malformed or missing inputs, **When** the agent retries, **Then** it receives specific corrective feedback, corrects the call within a bounded number of attempts, and — if the bound is reached — reports the failure honestly instead of looping.
4. **Given** a transient provider or gateway failure mid-task, **When** the system retries within its bounded retry policy, **Then** the task resumes without redoing already-completed steps, and a permanent failure is surfaced to the user with the task left in an accurate, resumable state.
5. **Given** the agent believes the task is complete, **When** it produces its final response, **Then** the response's claims match the recorded work (files actually changed, checks actually run), and any unfinished or skipped item is disclosed rather than implied done.

---

### User Story 2 - Plans That Know When They Are Finished (Priority: P2)

A developer plans a piece of work with the agent, approves the plan, and executes it — immediately or later. From the moment execution completes, the plan reads as finished everywhere its status appears, and the reminder that the plan "can be executed" (e.g., "say 'proceed' any time") is gone for good. If the developer closes and reopens the session, the plan is still finished. A plan interrupted partway shows exactly that — partially executed — and only a genuinely pending plan ever invites execution.

**Why this priority**: A completed plan that keeps advertising itself as executable is a truthfulness bug: it misleads the user, invites accidental re-execution of finished work, and undermines confidence in every other status the product displays. It is the most visible of the reported defects and is explicitly mandated by the feature request.

**Independent Test**: Script the full plan lifecycle — approve, execute now, execute later, interrupt mid-execution, resume the session, discard, supersede with a newer plan — and verify at each step that the displayed status is truthful and the "can be executed" affordance appears only while the plan is genuinely pending.

**Acceptance Scenarios**:

1. **Given** a plan saved for later execution, **When** the user tells the agent to proceed and all plan steps complete, **Then** the plan is marked finished and no message suggesting it can still be executed appears anywhere from that point on.
2. **Given** a finished plan, **When** the session is closed and later resumed, **Then** the plan still reads as finished and no executable affordance returns.
3. **Given** a plan whose execution was interrupted partway, **When** its status is displayed, **Then** it reads as partially executed / interrupted — not finished, and not indefinitely "ready to execute" as if untouched.
4. **Given** a pending plan that was never executed, **When** the user approves a newer plan, **Then** the older plan is marked superseded and its executable affordance is withdrawn.
5. **Given** a genuinely pending plan, **When** its status is displayed, **Then** the execution hint appears — pending plans must remain discoverable; the fix must not hide legitimate affordances.

---

### User Story 3 - Agents That Know Their Mode (Priority: P2)

Whatever context the agent is operating in — planning mode, normal execution, or a delegated subagent mission with a reduced toolset — it behaves within that context's boundaries because it is told, precisely and at the right moment, what it can and cannot do: which tools are available, which capabilities are disabled, which actions are allowed. The developer never watches the agent attempt a file edit while planning, call a tool that does not exist in its mission, or delegate work its delegate cannot perform.

**Why this priority**: Out-of-mode attempts are wasted turns the user pays for, and each one erodes the sense that the agent understands its own situation. Enforcement without awareness produces retry loops; awareness without enforcement produces accidents. Both halves are required.

**Independent Test**: Script mode scenarios — planning mode, execution mode, delegated missions with restricted toolsets, and a mid-session mode change — and verify zero disallowed actions take effect, every blocked attempt receives corrective feedback naming the restriction, and the agent adapts instead of repeating the attempt.

**Acceptance Scenarios**:

1. **Given** planning mode is active, **When** the agent works, **Then** it does not attempt state-changing actions (file writes, edits, mutating commands); any attempt is blocked before taking effect and answered with feedback naming the mode restriction and the allowed alternatives.
2. **Given** a subagent is delegated a mission with a restricted toolset, **When** the mission is composed, **Then** the mission requires only actions within that toolset and the subagent is explicitly informed of its exact toolset and boundaries.
3. **Given** the mode changes mid-session (e.g., planning mode is turned off), **When** the agent takes its next action, **Then** its capability awareness reflects the new mode — it neither honors stale restrictions nor assumes stale permissions.
4. **Given** the agent attempts a tool that is unavailable in its current context, **When** the attempt is rejected, **Then** the corrective feedback names what is available instead, and the same unavailable-tool attempt does not recur more than twice in the session.

---

### User Story 4 - Guidance Tuned to the Launch Model, Without Bloat (Priority: P2)

The standing guidance the agent runs on — its instructions, tool descriptions, result formatting, context assembly, delegation briefs, and recovery behavior — is restructured around documented research into how DeepSeek V4 models actually behave: their reasoning style, strengths and weaknesses, tool-use patterns, instruction-following tendencies, and delegation performance. Duplicated, ambiguous, and conflicting instructions are removed; ordering and priority are made explicit; total guidance size does not grow; and the determinism and cache stability the product already achieves are preserved. The developer experiences this as better instruction adherence, more accurate tool use, and equal-or-lower cost per task.

**Why this priority**: Launch is single-model. Guidance written generically leaves quality on the table exactly where all launch traffic lands. But this story depends on research and measurement to be done right, and its value is realized through the reliability outcomes of Story 1 — so it follows the concrete defect fixes in priority.

**Independent Test**: Review artifacts confirm the research notes exist and every model-specific adaptation traces to a documented finding; measurement confirms guidance size ≤ baseline, cache efficiency ≥ baseline, and before/after benchmark sessions show no quality regression, holding model, gateway, effort, and workload constant.

**Acceptance Scenarios**:

1. **Given** the restructured guidance, **When** compared to the pre-change baseline, **Then** its total size is equal or smaller and instances of duplication, ambiguity, or conflicting instructions identified in review are eliminated.
2. **Given** an unchanged session configuration, **When** consecutive requests are made, **Then** the stable portion of each request remains identical across the session and measured cache efficiency is at or above the pre-change baseline.
3. **Given** the research notes on launch-model behavior, **When** any model-specific adaptation is inspected, **Then** it traces to a specific documented finding rather than folklore.
4. **Given** before/after benchmark runs under identical conditions, **When** results are compared, **Then** task quality and completion rate are at or above baseline while tokens and cost per completed task are at or below baseline.
5. **Given** a provider other than the launch configuration, **When** the same runtime is used, **Then** behavior degrades gracefully — nothing hard-depends on launch-model-specific traits.

---

### User Story 5 - Tool Activity That Names Its Target (Priority: P3)

As the agent writes and edits files, each activity row tells the developer at a glance exactly what happened and where: the tool name, then the affected file path, then the lines added and removed — in that order. The developer never has to expand an entry just to learn which file a write or edit touched.

**Why this priority**: A small, precisely specified display improvement. It sharpens moment-to-moment transparency but does not change agent behavior, so it ranks below the reliability and truthfulness stories.

**Independent Test**: Drive sessions that produce file writes, edits, multi-edits, and patch applications — including new files, long paths, and failed calls — and verify each rendered row shows tool name, then path, then added/removed counts in order, with graceful handling of missing or oversized paths.

**Acceptance Scenarios**:

1. **Given** the agent edits an existing file, **When** the activity row renders, **Then** it shows the tool name, then the affected file path, then the added/removed line counts, in that order.
2. **Given** the agent writes a new file, **When** the activity row renders, **Then** the new file's path appears in the same position, followed by the added/removed counts.
3. **Given** a file path too long for the available width, **When** the row renders, **Then** the path is shortened in a way that preserves the file name, without pushing the counts out of view or breaking the row layout.
4. **Given** a failed write or edit where no affected path can be determined, **When** the row renders, **Then** the path segment is omitted cleanly — no placeholder text or artifacts.

---

### Edge Cases

- A plan's execution is interrupted (user stop, error, or disconnect): status must show partial execution — neither finished nor freshly executable — and resuming the session must preserve that state.
- The session crashes or is killed mid-plan or mid-task: on resume, plan and task states reflect reality, with no resurrected "executable" hints and no phantom in-progress items.
- The user never executes a pending plan and simply moves on: the pending plan stays pending until executed, discarded, or superseded by a newer approved plan — the hint must not nag on every unrelated turn, and must not vanish while the plan is legitimately pending.
- Mode is toggled while the agent is mid-turn: the boundary rules of the mode in effect when an action is attempted govern that action; awareness updates before the next action.
- The agent insists on an unavailable tool or disallowed action repeatedly: bounded corrective attempts, after which the situation is surfaced honestly to the user rather than silently looping.
- Two identical tool calls are issued within one turn: the duplicate is intercepted; the agent is pointed at the first call's result.
- A delegated mission is composed at the exact moment tool availability changes: the mission reflects the delegate's actual granted toolset at dispatch time.
- The provider omits usage or cache-reporting fields: measurement and display degrade gracefully — no fabricated numbers (consistent with the constitution's honest-measurement principle).
- Research findings suggest a guidance ordering that would destabilize the cached request prefix: cache stability and correctness principles win; the conflict and the decision are documented in the research notes.
- Retry storms on gateway errors: retries are bounded with backoff; the user is never charged for unbounded repetition without progress.
- File paths with unusual characters (spaces, non-Latin scripts) in activity rows: rendered correctly, truncated safely.

## Requirements *(mandatory)*

### Functional Requirements

**Plan lifecycle truthfulness**

- **FR-001**: The system MUST track every plan through explicit lifecycle states — at minimum: being drafted, ready for decision, pending later execution, executing, finished, interrupted (partially executed), superseded, and discarded — and every place a plan's status is shown MUST reflect its current state truthfully.
- **FR-002**: The system MUST mark a plan finished when its execution completes, and from that moment MUST NOT display any message or affordance suggesting the plan can still be executed.
- **FR-003**: Any "plan can be executed" hint MUST appear only while the plan is genuinely pending execution, and MUST be withdrawn the moment execution starts, completes, is interrupted, or the plan is discarded or superseded.
- **FR-004**: Plan lifecycle state MUST persist accurately across session save, resume, and abnormal termination; a finished, discarded, or superseded plan MUST never reappear as executable.
- **FR-005**: Approving a new plan MUST mark any older unexecuted plan as superseded and withdraw its execution affordance.

**Tool activity display**

- **FR-006**: Activity rows for file-writing and file-editing tools MUST display the affected file path immediately after the tool name and before the lines-added / lines-removed counts.
- **FR-007**: Path display MUST degrade gracefully: paths exceeding available width are shortened while preserving the file name; rows for calls whose affected path cannot be determined omit the path cleanly with no placeholder artifacts.

**Mode awareness and enforcement**

- **FR-008**: Every agent — the primary agent and every delegated subagent — MUST be informed, before it acts, of its active mode's exact boundaries: the tools available to it, the capabilities disabled, the actions allowed and disallowed, and (for delegates) its mission scope.
- **FR-009**: The system MUST block any attempt to use a tool or capability not available in the active mode before it takes effect, and MUST respond with corrective feedback that names the violated restriction and the allowed alternatives — not a generic error.
- **FR-010**: When the active mode or tool availability changes, each affected agent's capability awareness MUST be updated before its next action.
- **FR-011**: Delegated missions MUST be composed only from capabilities actually granted to the delegate; a mission requiring unavailable tools MUST be adjusted or rejected at composition time, not discovered as failures mid-mission.

**Harness reliability (audit and fixes)**

- **FR-012**: A documented audit of the coding-agent harness MUST be produced covering, at minimum: orchestration failures, invalid tool calls, incorrect mode transitions, delegation problems, unfinished task states, repeated work, and unnecessary tool usage. Every confirmed issue MUST be either fixed within this feature or explicitly deferred with a recorded rationale.
- **FR-013**: Every task or work item the agent starts MUST reach an explicit terminal state (completed, failed, or cancelled) by the end of its run; the system MUST NOT leave items displayed as in progress after the run ends.
- **FR-014**: The system MUST intercept repeated identical tool invocations and requests for results already available and unchanged, steering the agent to the existing result with corrective feedback; persistent repetition MUST be bounded and then surfaced honestly to the user.
- **FR-015**: Invalid tool calls — unknown tools, malformed or missing inputs — MUST produce specific, actionable corrective feedback, with a bounded correction policy per call site.
- **FR-016**: Transient provider or gateway failures MUST be retried within bounded limits without losing task progress or redoing completed steps; permanent failures MUST be reported honestly with the task left in an accurate, resumable state.
- **FR-017**: Before presenting work as complete, the system MUST verify the claim against recorded activity (work items' states, actions actually performed); final responses MUST accurately reflect performed work and disclose anything unfinished or skipped.

**Launch-model optimization**

- **FR-018**: The standing guidance delivered to the model MUST be restructured with explicit instruction priority and stable ordering, and reviewed to remove duplication, ambiguity, and conflicting instructions; its total size MUST NOT exceed the pre-change baseline.
- **FR-019**: Documented research into launch-model behavior — reasoning patterns, strengths and weaknesses, tool-use patterns, instruction-following behavior, and delegation performance — MUST be captured as a feature artifact, and every model-specific adaptation MUST trace to a documented finding.
- **FR-020**: Tool descriptions, tool-result formatting, context construction, delegation briefs, retry and error-recovery behavior, completion detection, and final-response generation MUST each be reviewed against the research findings and adapted where the findings indicate improvement.
- **FR-021**: All restructuring MUST preserve the determinism and cache stability of the stable request prefix — identical within a session, deterministic across sessions given identical configuration — and measured cache efficiency MUST remain at or above the pre-change baseline.
- **FR-022**: Launch-model optimization MUST degrade gracefully on other OpenAI-compatible providers: no hard dependence on launch-model-specific behavior or reporting fields.

**Verification**

- **FR-023**: Every improvement claim made by this feature MUST be verified with realistic before-and-after multi-turn agent sessions on the launch configuration, holding model, gateway, effort level, and workload constant, using provider-reported measurements; the evidence MUST be stored with the feature's artifacts.

### Key Entities

- **Plan**: A user-approved outline of intended work. Carries a lifecycle state (drafted → ready → pending / executing → finished, or interrupted / superseded / discarded) and the completion status of its steps. Its displayed status and affordances must always match its actual state.
- **Mode**: A named operating context (e.g., planning vs. execution; primary session vs. delegated mission) that defines the set of available tools, disabled capabilities, and permitted actions.
- **Capability statement**: The declaration of an agent's current boundaries — tools, restrictions, allowed actions, mission scope — delivered to that agent before it acts.
- **Task / work item**: A tracked unit of agent work with a lifecycle that must end in a terminal state (completed, failed, or cancelled).
- **Tool invocation record**: One tool call — its validity, outcome, and rendered activity row (tool name, target such as a file path, outcome measure such as lines added/removed).
- **Delegated mission**: A scoped assignment handed to a subagent: goal, boundaries, and the exact toolset granted.
- **Audit finding**: A documented harness defect — symptom, root cause, and either its fix or a deferral rationale.
- **Research finding**: A documented observation about launch-model behavior that motivates a specific adaptation, enabling traceability from change back to evidence.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across a scripted plan-lifecycle suite covering execute-now, execute-later, interrupt, session-resume, discard, and supersede paths, 100% of finished plans display a finished status, and zero finished, discarded, or superseded plans display an executable hint — including after session resume.
- **SC-002**: Across scripted mode scenarios (planning, execution, delegated missions, mid-session mode change), zero disallowed actions take effect, 100% of blocked attempts receive corrective feedback naming the restriction, and no identical disallowed attempt recurs more than twice in a session.
- **SC-003**: Across the benchmark suite of representative multi-turn coding sessions on the launch configuration, invalid tool calls per session and redundant (already-answered) calls per session each decrease by at least 50% relative to the recorded baseline.
- **SC-004**: 100% of benchmark and scripted runs end with every started work item in a terminal state; zero runs end with an item still displayed as in progress.
- **SC-005**: The standing guidance's measured size is at or below the pre-change baseline, session cache efficiency is at or above baseline, and median provider-reported tokens and cost per completed benchmark task are at or below baseline.
- **SC-006**: Task completion rate and reviewed output quality on the benchmark suite are at or above baseline — no acceptance scenario in Stories 1–5 regresses — under identical model, gateway, effort, and workload conditions.
- **SC-007**: 100% of file-write and file-edit activity rows with a determinable path display the path between the tool name and the line counts; zero rows render placeholder text or layout artifacts for missing or oversized paths.
- **SC-008**: At least 95% of benchmark tasks run to a terminal state without manual intervention (no user rescue from loops, stalls, or runaway retries).
- **SC-009**: The audit report and launch-model research notes exist as feature artifacts; 100% of confirmed audit findings are resolved or carry a written deferral rationale, and 100% of model-specific adaptations trace to a documented research finding.

## Assumptions

- The launch configuration is the DeepSeek V4 model family (including its reasoning variant) served exclusively through the MuhiyaLLM gateway; other OpenAI-compatible providers remain supported with graceful degradation but are not optimization targets for this feature.
- "Every agent" means the primary orchestrating agent and all delegated subagents within MuhiyaCode; agents outside the product are out of scope.
- The benchmark harness and recorded baselines from the prompt-cache-optimization feature (001) serve as the measurement baseline; where a needed baseline does not yet exist, it is captured before changes are made, per the constitution's verification principles.
- Research into launch-model behavior draws on public documentation and empirical probing through the gateway; no privileged vendor information is assumed. The constitution's reference-architecture study (DeepSeek Reasonix) is part of this research input.
- The existing plan interaction flow (approve, then proceed now / proceed later / keep planning) is retained; this feature corrects status truthfulness and lifecycle accuracy, not the interaction's design.
- The duplicate-read blocking, prefix determinism, and cache-efficiency levels achieved by feature 001 are the floor; this feature must not regress them.
- An unexecuted pending plan remains pending until it is executed, explicitly discarded, or superseded by a newer approved plan; pending is a legitimate long-lived state.
- "Displayed" refers to the product's terminal interface — transcript, status, and summary surfaces.
- This is a polish of existing subsystems: improvements are made through the smallest change that achieves the measured goal, and no new orchestration subsystems are introduced (per the constitution's improve-don't-rewrite principle).
