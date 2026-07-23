# Feature Specification: Automatic Skill Use & Interface Polish

**Feature Branch**: `013-auto-skills-ui-polish`

**Created**: 2026-07-20

**Status**: Draft

**Input**: User description: "I want to implement a new ability in the MuhiyaCode Agent that allows it to understand what a task is about and then automatically use the skill related to the user's prompt. I want to expand the agent's capabilities in this area so that it can read all skills from different folders, handle them efficiently, read the relevant skill first, and use it throughout the workflow. It should work similarly to Claude Code, which automatically uses skills when it detects that a prompt is related to them. For example, when the agent builds a website in Claude Code, it automatically reads and uses the frontend design skill. We need something similar. However, I do not want the skill usage to feel unintelligent or rigid. The agent should use each skill properly while still applying the selected model's own style and reasoning. Also, rename the command text from 'Clear API Key' to 'Log Out of Account.' I also want to completely remove Harness Friction and its /errors command from the UI, and polish the UI further. Remove /permissions as well, because it is not needed. The keyboard shortcut will be enough for the user. Add a small hint under the current permission mode text, which appears before the thinking level. Under it, show the text: 'Shift + Tab to cycle.' Also, remove the 'DeepSeek maps to' description text from the reasoning levels. I want all displayed token numbers to show the full values, including cached tokens. The session cache-hit value should also show the complete cache-hit rate for the entire session, including all sub-agents. Simplify the data shown in the context modal significantly while keeping it well organized and displaying only the most important information. I also want to remove the Ctrl + T shortcut for todos because it is not needed. Todos should be shown permanently whenever the agent is active. I prefer replacing the Tab shortcut for switching agents with the left and right arrow keys on the keyboard. Make sure all of these features are implemented accurately and fully integrated with every related feature, function, and class throughout the MuhiyaCode Agent."

## Clarifications

### Session 2026-07-20

- Q: When should the left/right arrow keys switch between agent views (they also move the text caret)? → A: Only when the composer (input box) is empty; any typed text restores normal caret movement.
- Q: What exactly should the simplified context view keep? → A: Essentials only — context usage, session token totals (cache-inclusive), the session-wide cache-hit rate, session cost when available, and the session's models; every other currently shown item is dropped.
- Q: How should token numbers be displayed (today they abbreviate to "12.3k"/"1.2m")? → A: Full digits with thousands separators on every surface, including the one-line live status.
- Q: Should the permission mode be visible in the footer at all times, or only for non-default modes (as today)? → A: Always visible — every mode, including the default, renders before the thinking-level chip with the "Shift + Tab to cycle" hint beneath it.
- Q: How do skills reach sub-agents during delegated work? → A: The main agent equips them — it passes the relevant skill's guidance along with the delegated assignment; sub-agents perform no skill discovery or selection of their own.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The agent automatically finds and uses the right skill (Priority: P1)

A user has skills installed — some inside the project, some in their personal (home-level) skill folders. They ask the agent to do something a skill covers (for example, "build me a landing page" while a frontend-design skill is installed) **without naming any skill**. The agent understands what the task is about, recognizes that an installed skill applies, reads that skill's full instructions **before** starting the related work, and keeps applying its guidance for the rest of the task. The result reads like the selected model doing its best work *informed by* the skill — not like a template being mechanically filled in. When no installed skill relates to the request, the agent works exactly as it does today and loads nothing extra.

**Why this priority**: This is the headline new capability the feature exists for. Everything else in this feature is refinement of existing surfaces; this changes what the agent can do. It directly determines output quality for every user who installs skills.

**Independent Test**: Install a distinctive skill (one whose guidance produces recognizably different output), then run one matching prompt and one unrelated prompt. The matching task must show the skill was read before the related work began and its guidance visibly shaped the result; the unrelated task must load no skill content at all.

**Acceptance Scenarios**:

1. **Given** a project with a frontend-design skill installed, **When** the user asks the agent to build a web page without mentioning any skill, **Then** the agent reads that skill's full instructions before beginning the design work and applies its guidance throughout the task.
2. **Given** skills installed across the project-level folders, the user's home-level folders, and any user-configured extra folders, **When** a session starts, **Then** all of them appear in one deduplicated skill catalog (name and short description) available to the agent, without any full skill bodies being loaded up front.
3. **Given** a prompt unrelated to every installed skill, **When** the task runs, **Then** no skill instructions are loaded and the task proceeds exactly as it would without the feature.
4. **Given** a task that falls in the territory of two different installed skills, **When** the task runs, **Then** the agent may use both, reading each skill's instructions before applying that skill's guidance.
5. **Given** the user manually queued a skill through the existing manual selection flow, **When** the task runs, **Then** the manual choice is honored and the same skill is never loaded twice into the same task.
6. **Given** a long multi-step task whose later steps drift into an installed skill's territory, **When** the agent reaches those steps, **Then** it can pick the skill up mid-task rather than only at the first response.
7. **Given** the same task run with two different selected models, **When** both use the same skill, **Then** each output still reflects that model's own reasoning and style — the skill informs the work, it does not replace the model's voice.
8. **Given** the agent delegates part of a skill-relevant task to a sub-agent, **When** the sub-agent works, **Then** the skill guidance chosen by the main agent accompanies the delegated assignment, and the sub-agent performs no skill discovery of its own.

---

### User Story 2 - Token and cache numbers tell the whole truth (Priority: P2)

A user watches the token and cache figures while the agent works and reviews them afterwards. Every token number they see anywhere — the live status line, per-agent views, end-of-task summaries, and the context view — is the **complete** figure, cached tokens included, written out in full (no "12.3k"-style abbreviation). The session cache-hit figure reflects the **entire** session: the main conversation *and* every sub-agent. Opening the context view shows a short, well-organized card with only the information that matters, instead of the current long diagnostic dump.

**Why this priority**: Users make cost and trust decisions from these numbers. A total that silently excludes cached tokens, or a "session" rate that ignores sub-agents, actively misleads. This is the highest-value correction to existing behavior.

**Independent Test**: Run a session that spawns at least one sub-agent and produces cache activity. Verify every displayed token figure equals the full provider-reported count (cached + uncached), verify the session cache-hit rate changes when sub-agent traffic differs from main-conversation traffic, and verify the context view shows only the reduced, organized set.

**Acceptance Scenarios**:

1. **Given** a running task with cache activity, **When** the live status shows a token count, **Then** it is the complete count including cached tokens, displayed as a full number with thousands separators.
2. **Given** a session that spawned sub-agents, **When** the session cache-hit rate is displayed, **Then** it is computed over every request in the session — main conversation and all sub-agents.
3. **Given** the user opens the context view, **Then** they see a compact, clearly grouped card limited to the most important items: context usage, session token totals (cache-inclusive), the session-wide cache-hit rate, session cost when available, and the session's models — and nothing else.
4. **Given** a provider that reports no cache metrics, **When** cache figures would display, **Then** they read as unavailable rather than showing zero or a fabricated rate.
5. **Given** the same scope (one task, one agent, or the session), **When** its token figures appear on more than one surface, **Then** the figures agree exactly.

---

### User Story 3 - A decluttered command surface with clear mode hints (Priority: P3)

A user browses the command palette and the footer. The logout command now describes itself as "Log Out of Account". The `/errors` command and every mention of "harness friction" are gone. `/permissions` is gone too — instead, the current permission mode is visible in the footer area before the thinking-level indicator, with a small hint directly beneath it reading "Shift + Tab to cycle", so mode switching stays discoverable without a command. The reasoning-level chooser describes each level plainly, with no "DeepSeek maps to…" text.

**Why this priority**: Pure clarity work on existing surfaces. Valuable — the interface stops advertising internal diagnostics and provider trivia — but nothing new becomes possible.

**Independent Test**: Open the command palette and confirm the removed commands are absent and the renamed description is present; view the footer and confirm the permission mode plus its hint render; open the reasoning chooser and confirm no provider-mapping text appears; type the removed commands and confirm the standard unknown-command response.

**Acceptance Scenarios**:

1. **Given** the command palette, **When** the user reads the logout entry, **Then** its description is "Log Out of Account".
2. **Given** any screen in the application, **When** the user looks for `/errors`, harness-friction panels, or friction mentions in summaries, **Then** none exist anywhere in the interface.
3. **Given** the command palette and autocomplete, **When** the user searches for `/permissions` (or its alternate spelling), **Then** it is absent; **and** cycling the permission mode via Shift+Tab still works.
4. **Given** the footer, **When** the user looks at the permission-mode text shown before the thinking level, **Then** a small hint reading "Shift + Tab to cycle" appears directly beneath it.
5. **Given** the reasoning-level chooser, **When** the user reads the level descriptions, **Then** no level or explanatory text mentions a DeepSeek mapping.
6. **Given** a user types a removed command, **When** they submit it, **Then** they get the standard unknown-command response — no crash, no partial behavior.

---

### User Story 4 - Always-on to-dos and arrow-key agent switching (Priority: P4)

While the agent works, its to-do checklist is simply there — permanently visible whenever the agent is active and has checklist items, with no shortcut needed to reveal or hide it (Ctrl+T is gone). To move between agent views, the user presses the left and right arrow keys instead of Tab; Tab keeps its command-autocomplete role, and typing or moving the caret in the composer is completely unaffected.

**Why this priority**: Interaction refinements on existing behavior. Improves flow but changes no capability and corrects no misleading data.

**Independent Test**: Run a task that produces a to-do list and confirm the panel stays visible for the task's whole active life with no way (and no need) to toggle it; run two agents and confirm the left/right arrow keys cycle between agent views while composer editing behaves exactly as before.

**Acceptance Scenarios**:

1. **Given** an active task with checklist items, **When** the user watches the screen, **Then** the to-do panel is visible the entire time the agent is active, and no shortcut toggles it.
2. **Given** an idle session, **Then** the to-do panel does not linger (existing retire behavior preserved).
3. **Given** two or more agent views exist and the composer is empty, **When** the user presses the right or left arrow key, **Then** the view switches to the next or previous agent respectively.
4. **Given** the composer contains any text, **When** the user presses left/right arrows, **Then** the caret moves within their text exactly as before — agent switching never steals those keys from editing.
5. **Given** the keyboard hints in the footer, **Then** they reflect the new reality: arrow keys for agents, no Ctrl+T hint, no Tab-for-agents hint.

---

### Edge Cases

- **No skills installed anywhere**: the catalog is empty; the agent behaves exactly as today, with no skill mentions and no errors.
- **Unreadable, malformed, or oversized skill file**: the skill is skipped gracefully; the catalog, the session, and the task all continue without failure.
- **Same skill name in two folders**: the catalog holds one entry per name (deterministic first-wins by folder precedence, matching today's discovery order).
- **Very many installed skills**: the catalog stays bounded and deterministic; the agent still sees a stable, useful listing rather than an unbounded dump.
- **Task changes direction mid-way** (user steers into a skill's territory): the agent may adopt the newly relevant skill at that point.
- **Skill added or removed while a session is running**: the catalog is a session-start snapshot; changes take effect on the next session.
- **Only one agent view exists**: with the composer empty, arrow-key switching is a harmless no-op.
- **To-do list empty or fully retired**: the panel shows nothing even while the agent is active (nothing to show is not an error).
- **Provider without cache reporting**: token totals still display in full; cache figures show as unavailable — never a fake 0%.
- **Session resumed from disk**: session-wide totals and the cache-hit rate rebuild to cover the resumed history, including prior sub-agent records.
- **Narrow terminal**: the permission-mode text and its "Shift + Tab to cycle" hint degrade gracefully like other footer content (truncation rules), never corrupting the layout.
- **Full token values are long** (millions of tokens): displays remain readable with thousands separators and never break adjacent layout.

## Requirements *(mandatory)*

### Functional Requirements

**Automatic skill awareness and use**

- **FR-001**: The system MUST discover installed skills from every supported skill location — project-level skill folders, the user's home-level skill folders, and any user-configured additional folders — and present them as a single deduplicated catalog with deterministic ordering.
- **FR-002**: Discovery MUST be lightweight: the catalog carries only each skill's name and short description; a skill's full instructions MUST NOT be loaded until the agent actually chooses to use that skill.
- **FR-003**: Relevance MUST be the agent's own reasoned judgment from the meaning of the request and the evolving task — not a fixed keyword or rule match imposed by the application. The application supplies the catalog; the agent decides.
- **FR-004**: When the agent judges a skill relevant, it MUST read that skill's full instructions **before** beginning the related work, and MUST keep applying that guidance through the remainder of the task.
- **FR-005**: Skill guidance MUST blend with the selected model's own style and reasoning: the skill informs the work; it does not turn output into rigid template-following.
- **FR-006**: The agent MUST be able to use multiple relevant skills within one task, and MUST load no skill content for tasks unrelated to every installed skill.
- **FR-007**: The agent MUST be able to adopt a skill mid-task when later steps enter that skill's territory — skill use is not limited to the first response.
- **FR-008**: The existing manual skill-selection flow MUST keep working; manually selected skills are honored, and a skill already provided manually is never loaded a second time in the same task.
- **FR-009**: Skills that cannot be read (missing, malformed, or over the size bound) MUST be skipped gracefully without failing the catalog, the session, or the task.
- **FR-010**: Skill awareness MUST extend to delegated work in the same session: when the agent hands part of a task to a sub-agent whose assignment falls in an installed skill's territory, the main agent passes that skill's guidance along with the delegated assignment. Sub-agents perform no skill discovery or selection of their own.
- **FR-011**: Adding skill awareness MUST NOT materially reduce session cache efficiency and MUST add no meaningful cost to tasks that end up using no skill.

**Command surface and wording**

- **FR-012**: The logout command's visible description MUST read "Log Out of Account" everywhere it appears.
- **FR-013**: The `/errors` command and every user-visible trace of "harness friction" — the command entry, its panel, and any friction mentions in task summaries or other output — MUST be removed from the interface.
- **FR-014**: The `/permissions` command, including its alternate spelling, MUST be removed from the command palette, autocomplete, and execution; permission-mode cycling remains available exclusively through the existing Shift+Tab shortcut.
- **FR-015**: The current permission mode MUST be visible in the footer area shown before the thinking-level indicator in every mode — including the default — with a small hint reading "Shift + Tab to cycle" directly beneath it, so mode switching stays discoverable without any command.
- **FR-016**: Reasoning-level descriptions MUST NOT mention DeepSeek mappings — neither the per-level description lines nor the chooser's explanatory text.
- **FR-017**: After the removals and renames, no hint, help text, autocomplete entry, notice, or summary anywhere in the interface may reference a removed command, a removed shortcut, or superseded wording.
- **FR-018**: Submitting a removed command MUST produce the application's standard unknown-command response.

**Token and cache truthfulness**

- **FR-019**: Every displayed token figure MUST be the complete count for its scope, cached tokens included.
- **FR-020**: Token figures MUST display as full numbers with thousands separators — no abbreviated forms — on every surface, including the one-line live status.
- **FR-021**: The session cache-hit figure MUST be computed over every request in the session: the main conversation and all sub-agents.
- **FR-022**: When the provider supplies no cache metrics, cache displays MUST honestly read as unavailable — never zero, never an estimated rate presented as measured.
- **FR-023**: The context view MUST be reduced to a compact, clearly grouped presentation of only the most important information: context usage, session token totals (cache-inclusive), the session-wide cache-hit rate, session cost when available, and the session's models. All other currently shown detail MUST be dropped from this view — including the per-model usage rows, per-pairing cache rows, window-category breakdown, invalidation log, pressure diagnostics, API/active time, lines added/removed, and the steady-state rate.
- **FR-024**: All surfaces showing token or cache figures for the same scope MUST agree exactly with one another.

**Interaction model**

- **FR-025**: The Ctrl+T shortcut MUST be removed, and the to-do panel MUST no longer be user-toggleable.
- **FR-026**: The to-do checklist MUST be visible whenever the agent is actively working and has checklist items, and MUST NOT linger once the agent is idle (existing retire behavior preserved).
- **FR-027**: The left and right arrow keys MUST switch to the previous/next agent view; Tab MUST no longer switch agents, while retaining its command-autocomplete role.
- **FR-028**: Arrow-key agent switching MUST occur only while the composer is empty; whenever the composer contains any text, the left/right arrow keys keep their normal caret-movement behavior, so editing is never disturbed.
- **FR-029**: On-screen keyboard hints MUST reflect the final bindings — arrow keys for agent switching, no Ctrl+T reference, no Tab-for-agents reference.

**Integration**

- **FR-030**: Every change above MUST be reflected consistently across all related surfaces of the product — command listings, footers, hints, summaries, saved-session displays, and documentation strings — so no surface contradicts another.

### Key Entities

- **Skill**: An installed unit of guidance with a name, a short description, a source location, and full instructions that are read only when the skill is used.
- **Skill Catalog**: The session's deduplicated, deterministically ordered listing of all discovered skills (names and descriptions only) from every supported location.
- **Session Usage Aggregate**: The session-wide token and cache accounting — totals including cached tokens, and a cache-hit rate spanning the main conversation and all sub-agents.
- **Context View**: The simplified card summarizing context usage, cache-inclusive totals, session-wide cache-hit rate, cost when available, and the session's models.
- **To-Do Checklist**: The agent's live task checklist, permanently visible while the agent is active and holding items.
- **Agent View**: A switchable view onto the main session or one sub-agent, navigated with the left/right arrow keys.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across a benchmark set of tasks that fall in an installed skill's territory, the agent reads the applicable skill before beginning the related work in at least 90% of runs — without the user ever naming a skill.
- **SC-002**: Across a benchmark set of tasks clearly unrelated to every installed skill, zero skill bodies are loaded in at least 95% of runs.
- **SC-003**: Session startup time with skills installed in all supported locations is unchanged within 5% of the pre-feature baseline.
- **SC-004**: In a review of at least 10 skill-guided task outputs, none consists of skill text pasted verbatim; every output shows task-specific application, and outputs produced by different models remain recognizably different in style.
- **SC-005**: 100% of token figures across every surface are cache-inclusive full numbers, and figures for the same scope match exactly across surfaces in cross-checks.
- **SC-006**: In a session whose sub-agent cache behavior differs from the main conversation's, the displayed session cache-hit rate reflects the combined traffic (demonstrably different from a main-only computation).
- **SC-007**: Zero user-visible references remain to `/errors`, harness friction, `/permissions`, Ctrl+T, Tab-for-agents, "Clear API key", or DeepSeek mappings — verified by exhaustive interface text review.
- **SC-008**: The context view presents at least 50% fewer items than before while retaining every item listed in FR-023, and a user can locate any retained item within 5 seconds.
- **SC-009**: A user relying only on what is on screen (no documentation) successfully changes the permission mode — the footer hint alone is sufficient.
- **SC-010**: During active tasks with checklist items, the to-do panel is visible 100% of the time; with two or more agents running, arrow keys cycle agent views with zero regressions to composer text editing.
- **SC-011**: On the standard multi-turn benchmark workload with no skill used, the steady-state session cache-hit rate is not lower than the pre-change baseline beyond normal run-to-run variance.

## Assumptions

- **Model-driven selection**: "Automatic" means the agent is *equipped to decide* — the catalog is always available and the agent selects skills by its own reasoning. The application never force-injects a skill from keyword rules. This matches the project's standing direction that dispatch decisions belong to the model, and it is what keeps skill use from feeling rigid.
- **Manual flow remains**: The user asked for automatic use, not removal of manual selection; the existing manual skill-queueing flow stays and takes precedence, with duplicate loads prevented.
- **All-folder scope**: Automatic awareness covers every discovery location the product already supports (project-level folders, home-level folders, and user-configured folders), not just project-level ones.
- **Permission-mode visibility** *(clarified 2026-07-20)*: the current mode is visible in that footer position in all modes — including the default — with the "Shift + Tab to cycle" hint beneath it, compensating for the removal of `/permissions`.
- **Alternate spelling included**: Removing `/permissions` also removes its alias spelling; keeping a hidden synonym would contradict "the keyboard shortcut is enough".
- **"Polish the UI further"** is interpreted as: full consistency of hints, labels, spacing, and help text after these removals and renames — not a visual redesign. Anything beyond consistency is out of scope here.
- **"Active" for to-dos** means the agent is running a task; visibility of the panel during activity requires it to have items, and completed lists retire exactly as they do today.
- **Arrow-key conflict resolution** *(clarified 2026-07-20)*: arrow keys switch agents only while the composer is empty — the same empty-input pattern up/down already follow. Composer editing regressions are unacceptable.
- **Internal diagnostics**: "Remove from the UI" governs user-visible surfaces. Internal friction recording may be deleted outright if nothing else consumes it; that choice is deferred to design.
- **Cache discipline**: The skill catalog's presentation to the agent must respect the project's session-stability and cache-efficiency principles (deterministic content, no per-turn churn); verification follows the constitution's before/after measurement rules.
- **Existing size bound for skills** (a per-skill read limit that protects the context window) remains in force for automatic loads.
