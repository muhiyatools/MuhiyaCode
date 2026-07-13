# Feature Specification: MuhiyaCode TUI & Agent Experience Overhaul

**Feature Branch**: `003-tui-ux-overhaul`

**Created**: 2026-07-12

**Status**: Draft

**Input**: User description: "A comprehensive, deep improvement plan for the MuhiyaCode TUI — a full UI/UX overhaul covering visual design, layout, accessibility, and overall experience. The goal is for MuhiyaCode to feel as polished, trustworthy, and easy to use as Claude Code — a coding agent with its own unique vibe and atmosphere that makes users feel completely comfortable trusting it with their tokens. Covers: layout/coloring/responsiveness, tool output & Ctrl+O consistency, result/summary stats cleanup, input box & message area cleanup, markdown rendering bug, context modal redesign, usage & authentication (/usage via the MuhiyaWorkspace gateway at api.muhiya.com), header/top bar (full workspace path, 'Subagent' naming), and skills delivery polish without prompt-size growth."

## Clarifications

### Session 2026-07-12

- Q: Where should the per-task "credits consumed" figure come from, so the task summary and context modal are fully accurate? → A: Gateway per-request usage — the gateway returns credits/cost inside each response's usage payload; the TUI sums them across the task's turns (exact attribution, no estimates).
- Q: How should the task's cache-hit rate be calculated across all of its turns? → A: Token-weighted aggregate — sum of cache-read tokens across the task's turns divided by the sum of all input tokens (cache reads + uncached) across those turns.
- Q: What should the "total tokens" figure in the task summary count? → A: All provider-reported tokens — input (cache reads + uncached) plus output, summed across all the task's turns.
- Q: What scope of account data should the /usage command display? → A: Standard view — credits remaining plus credits consumed for the current session, today, and the current billing period.
- Q: When a task is interrupted (Esc/stop, error, or disconnect mid-task), what should the task summary show? → A: Partial summary, marked — show credits, tokens, and cache-hit rate for the turns that completed, visibly marked as interrupted; spent credits are never hidden.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Polished, Correct Rendering in Every Terminal (Priority: P1)

A developer opens MuhiyaCode in their terminal of choice — Windows Terminal, Warp, iTerm2, or a VS Code embedded terminal — and everything simply looks right: the top bar has clean padding, colors form a coherent, intentional palette, assistant responses and tables are attractive and readable, and bold text like `**this**` renders as bold instead of showing raw asterisks or garbled styling. Resizing the window never breaks the layout.

**Why this priority**: This is the single largest driver of the "weak or disorganized" impression. Broken bold text, misaligned bars, and washed-out tables destroy trust before the agent even answers. Every other improvement is invisible if the canvas itself looks broken.

**Independent Test**: Run MuhiyaCode in at least three different terminal emulators at widths from 80 to 200+ columns, exercise a session that produces headings, bold/italic text, code blocks, lists, and tables, and visually verify correct, consistent rendering with no overflow, misalignment, or raw markdown syntax leaking through.

**Acceptance Scenarios**:

1. **Given** a response containing `**bold**`, `*italic*`, inline code, and headings, **When** it is rendered in the transcript, **Then** each element displays with its intended styling and no raw markdown markers are visible.
2. **Given** a response containing a markdown table, **When** it is rendered, **Then** columns align, the table fits the current terminal width, and coloring makes header and body rows distinguishable and readable.
3. **Given** MuhiyaCode running in Warp (or any terminal with non-default rendering behavior), **When** the top bar and transcript draw, **Then** padding, borders, and background fills appear correctly with no clipped or overflowing rows.
4. **Given** a running session, **When** the user resizes the terminal narrower or wider, **Then** the layout reflows without artifacts, orphaned characters, or permanently broken lines.
5. **Given** any screen in the app (transcript, modals, pickers, input), **When** viewed together, **Then** they share one coherent color system — accents, muted text, success/error tones — rather than ad-hoc per-screen colors.

---

### User Story 2 - Calm, Honest Task Summary (Priority: P2)

After the agent finishes a task, the developer sees a single, quiet summary line beneath the final message showing only what they care about: credits consumed, total tokens, and the cache-hit rate for that task (all turns from the task's start to its end). While the agent is thinking, a subtle inline indicator shows thinking is happening; the moment thinking ends, the indicator disappears completely, leaving no leftover "thought for Ns" text, no effort label, no "chat" word, and no wall of token/tool/session percentages.

**Why this priority**: The user's money is on the line every turn. A cluttered, jargon-heavy stats dump reads as noise and hides the numbers that matter; a precise, minimal cost line is what makes users "comfortable trusting it with their tokens."

**Independent Test**: Run a multi-turn task, observe the live thinking indicator during generation, and verify that after completion exactly three metrics (credits, total tokens, cache-hit rate) appear after the latest message, computed across the whole task — and that no thinking residue, effort label, "chat" text, or token breakdown remains anywhere.

**Acceptance Scenarios**:

1. **Given** the agent is reasoning, **When** thinking is in progress, **Then** an unobtrusive inline indicator with elapsed time is visible, **and When** thinking completes, **Then** the indicator disappears entirely with no residual text.
2. **Given** a completed task that spanned multiple turns and tool calls, **When** the summary renders, **Then** it shows credits consumed, total tokens, and cache-hit rate aggregated from the task's first request to its last — not session-wide figures.
3. **Given** a completed task, **When** the summary renders, **Then** it appears directly after the latest assistant message (not above the input box) and contains no effort level, no "chat" label, no tool counts, and no cached/new token breakdown.
4. **Given** a provider or gateway that does not report cache usage fields, **When** the summary renders, **Then** the cache-hit rate is omitted gracefully rather than shown as zero or a fabricated number.

---

### User Story 3 - Consistent Tool Activity, Collapsed and Expanded (Priority: P3)

As the agent works, each tool action appears as one compact line: the tool name, its target (e.g., a file path), and a minimal outcome measure (e.g., lines added/removed for an edit). When the developer wants depth, they press Ctrl+O and every tool entry expands in place to full detail — a complete diff for an edit, full command output for a shell run, matched results for a search — following one consistent visual pattern across all tools.

**Why this priority**: Tool activity is the heartbeat of an agent session. Today's mixed detail levels make the transcript feel disorganized; a strict collapsed/expanded contract makes the agent feel disciplined and auditable.

**Independent Test**: Run a task that triggers every tool category (read, edit, write, shell, search, delegate, web), verify each collapsed entry shows exactly name + target + outcome measure, then toggle Ctrl+O and verify each entry shows its full detail in a uniform layout.

**Acceptance Scenarios**:

1. **Given** the default (collapsed) view, **When** an edit tool completes, **Then** its entry shows the tool name, the file path, and the count of lines added/removed — and no diff body or file content.
2. **Given** the user presses Ctrl+O, **When** the expanded view renders, **Then** the same edit entry shows the full diff, and every other tool entry shows its equivalent full detail (output, results, parameters) in the same visual pattern.
3. **Given** repeated Ctrl+O presses, **When** toggling between views, **Then** the transcript switches cleanly between the two detail levels with no mixed or intermediate states.

---

### User Story 4 - Quiet Input and Message Area (Priority: P4)

The developer's eye rests on a clean input box whose placeholder quietly teaches the essentials (e.g., that `/` opens commands). There is no "Enter to send" hint, no "Ctrl+P" text under the input. Their own messages are identified by a distinct background color alone — no arrow prefix — and the transcript never exposes internal machinery such as which web-search provider served a query.

**Why this priority**: Chrome and hints multiply visual noise on the most-viewed screen region. Removing them is low effort and immediately raises perceived polish, but the app is usable without it.

**Independent Test**: Open the app, confirm the hint line below the input is gone and the placeholder communicates command discovery; send a message and confirm it renders with background color only; trigger a web search and confirm no provider name appears anywhere in the UI.

**Acceptance Scenarios**:

1. **Given** an empty input box, **When** the app is idle, **Then** the placeholder text hints how to discover commands (e.g., "/" for commands) and no "Enter to send" or "Ctrl+P" hint text is displayed.
2. **Given** a sent user message, **When** it renders in the transcript, **Then** it is distinguished by background color only, with no arrow or other prefix character.
3. **Given** the agent performs a web search, **When** its activity renders (collapsed or expanded), **Then** the search provider's name is never shown to the user.

---

### User Story 5 - Account Usage and Sign-in Clarity (Priority: P5)

A signed-in developer types `/usage` and sees their account usage — fetched live from their gateway account using the API key they logged in with. Because they are signed in, `/login` no longer appears in the command list. If they log out, `/login` returns and they can re-enter an API key to get back in.

**Why this priority**: Cost visibility builds the trust the overhaul aims for, and hiding irrelevant commands reduces confusion — but it depends on gateway account data and is separable from the visual overhaul.

**Independent Test**: With a valid API key stored, run `/usage` and verify live account data appears; verify `/login` is absent from the command palette; log out, verify `/login` reappears, and log back in with a key.

**Acceptance Scenarios**:

1. **Given** a user signed in with an API key, **When** they run `/usage`, **Then** the app fetches and displays their usage data (credits consumed/remaining and related account usage) from their gateway account using that key.
2. **Given** a signed-in user, **When** they open the command palette, **Then** `/login` is not listed; **and Given** a signed-out user, **Then** `/login` is listed and accepts an API key.
3. **Given** the gateway is unreachable or the key is invalid, **When** `/usage` runs, **Then** the user sees a clear, friendly explanation rather than raw errors or a hang.

---

### User Story 6 - Organized Context Modal with Session Credits (Priority: P6)

The developer opens the context view and finds the same information as today — context composition, token budget, cache state — but organized into scannable groups with clear visual hierarchy, plus one new figure: credits used in the current session.

**Why this priority**: The modal already exists and works; this is a presentation upgrade plus one added metric, valuable but not blocking anything else.

**Independent Test**: Open the context modal mid-session and verify all previously available information is still present, grouped logically, visually consistent with the new design system, and includes session credits used.

**Acceptance Scenarios**:

1. **Given** an active session, **When** the user opens the context modal, **Then** all information available today remains available, arranged in organized, labeled groups.
2. **Given** an active session with completed turns, **When** the context modal opens, **Then** it shows credits used for the current session.

---

### User Story 7 - Honest Header (Priority: P7)

The top bar shows the full workspace path — not just the folder name — so the developer always knows exactly where the agent is operating, and the secondary model indicator is labeled "Subagent", matching what the model actually is.

**Why this priority**: Small, self-contained accuracy fixes; they polish trust at the edges but nothing depends on them.

**Independent Test**: Launch MuhiyaCode in a nested directory and verify the header shows the complete path; verify the secondary model label reads "Subagent".

**Acceptance Scenarios**:

1. **Given** a workspace at a deep path, **When** the header renders, **Then** the full path is shown (sensibly truncated from the left only when the terminal is too narrow to fit it).
2. **Given** the header's model indicators, **When** rendered, **Then** the delegated-model label reads "Subagent" (not "Agents" or "agents").

---

### User Story 8 - Skills That Just Work (Priority: P8)

When the developer's workspace contains skills, the agent reliably notices the right skill at the right moment, applies it correctly, and otherwise stays silent about them. Skill availability does not inflate the prompt beyond the minimal listing needed for discovery, full skill instructions are loaded only when a skill is actually used, and none of this disturbs the stability of the cached prompt prefix.

**Why this priority**: This is agent-behavior polish rather than UI polish; it rounds out the "trustworthy agent" goal but is independent of every visual change.

**Independent Test**: Place skills with clear trigger descriptions in the workspace, run tasks that should and should not trigger them, and verify: matching tasks use the right skill, non-matching tasks ignore skills, measured prompt size does not grow beyond the compact skill listing, and session cache-hit rate does not regress versus a baseline run.

**Acceptance Scenarios**:

1. **Given** a workspace with discoverable skills, **When** the user's request matches a skill's stated purpose, **Then** the agent applies that skill's instructions during the task.
2. **Given** a request unrelated to any skill, **When** the agent works, **Then** no skill instructions are loaded into the conversation and no skill-related noise appears in the transcript.
3. **Given** a baseline session without the change and an identical session with it, **When** both are measured, **Then** the stable prompt grows by no more than the compact skill listing and the cache-hit rate shows no regression.

---

### Edge Cases

- Terminal narrower than 80 columns: layout degrades gracefully (wrapping/truncation), never corrupts.
- Terminals with limited color support (no truecolor) or light backgrounds: the palette falls back to readable colors; no invisible text.
- Very long workspace paths: header truncates from the left, preserving the most specific (rightmost) segments.
- Task with zero tool calls or zero measurable cost: summary shows only applicable metrics without placeholders like "0 tools".
- Provider/gateway omits cache or cost fields: affected metrics are hidden, never fabricated (consistent with honest-measurement rules).
- `/usage` while signed out: command is hidden or, if invoked directly, explains that sign-in is required.
- Gateway timeout or malformed usage response: friendly error, UI remains responsive.
- Ctrl+O on an entry with very large detail (huge diff, long shell output): expanded view remains scrollable/bounded and does not freeze rendering.
- Resize during active streaming: in-progress content reflows without duplicated or torn lines.
- Task interrupted mid-turn (Esc/stop, error, disconnect): the summary shows metrics for the turns that completed, marked as interrupted; a turn with no returned usage payload is excluded from totals, never guessed.
- Markdown edge cases: bold inside tables, nested bold/italic, bold spanning line wraps — all render correctly.
- More skills discovered than the listing limit: selection is deterministic and stable across turns of a session (no listing churn between requests).
- Logging out mid-session: session continues locally where possible; commands relying on the key degrade with clear messaging.

## Requirements *(mandatory)*

### Functional Requirements

**Visual system, layout & responsiveness**

- **FR-001**: The application MUST present one coherent, intentional color system applied consistently across the top bar, transcript, tool entries, modals, pickers, summaries, and input area, readable on both dark and light terminal backgrounds and degrading gracefully on terminals without full color support.
- **FR-002**: The top bar MUST render with correct, consistent padding and alignment at all supported terminal widths.
- **FR-003**: All screens MUST reflow correctly on terminal resize and render without misalignment, overflow, or artifacts across mainstream terminal emulators (including Warp, Windows Terminal, iTerm2, and VS Code's integrated terminal).
- **FR-004**: Assistant responses MUST render markdown correctly — including bold (`**text**`), italic, inline code, code blocks, headings, and lists — with no raw markdown syntax visible to the user.
- **FR-005**: Markdown tables MUST render with aligned columns, width-aware sizing, and a color treatment that clearly distinguishes headers from body rows.

**Tool activity display**

- **FR-006**: In the default (collapsed) view, each tool action MUST display exactly: the tool's display name, its primary target (e.g., file path, command, query), and a minimal outcome measure (e.g., lines added/removed for edits, match counts for searches) — and MUST NOT display detailed content.
- **FR-007**: In the expanded (Ctrl+O) view, each tool action MUST display its full detail (e.g., complete diff for an edit, full output for a shell command) using one uniform visual pattern shared by all tool types.
- **FR-008**: The collapsed and expanded views MUST be two detail levels of the same visual design, and toggling between them MUST apply consistently to all tool entries in the transcript.

**Thinking indicator & task summary**

- **FR-009**: While the agent is reasoning, an inline, unobtrusive thinking indicator with elapsed time MUST be shown; when reasoning completes, the indicator MUST disappear entirely, leaving no residual text in the transcript or status area.
- **FR-010**: The post-task summary MUST NOT display: thinking-effort level, the word "chat", tool counts, cached/new token breakdowns, session totals, or percentage walls.
- **FR-011**: The post-task summary MUST display exactly: credits consumed, total tokens, and cache-hit rate, each aggregated across all turns of the just-completed task (from the user's triggering prompt to the final response), not across the whole session.
- **FR-012**: Task summary metrics MUST be derived from provider-reported usage data; any metric the provider does not report MUST be omitted rather than estimated or shown as zero.
- **FR-012a**: Credits consumed MUST be sourced from per-request credit/cost fields in the gateway's usage payload and summed across the task's turns; credits MUST NOT be estimated from local price tables or inferred from account-balance differences.
- **FR-012b**: The task's cache-hit rate MUST be token-weighted: the sum of provider-reported cache-read tokens across all the task's turns divided by the sum of all input tokens (cache reads + uncached) across those turns — never an average of per-turn rates or a single turn's rate.
- **FR-012c**: The task's total tokens MUST count all provider-reported tokens — input tokens (cache reads + uncached) plus output tokens — summed across all the task's turns.
- **FR-013**: The task summary MUST render directly after the latest assistant message in the transcript, not above the input box.
- **FR-013a**: When a task ends early (user stop, error, or connection loss), the summary MUST still render with the metrics for the turns that completed, visibly marked as interrupted; turns that never returned a usage payload are excluded from the totals rather than estimated.

**Input & message area**

- **FR-014**: The input area MUST NOT display "Enter to send" or "Ctrl+P" hint text; the input placeholder MUST instead hint at command discovery (e.g., that `/` opens commands).
- **FR-015**: User messages MUST be identified by background color alone, with no arrow or other prefix marker.
- **FR-016**: The UI MUST never reveal the web-search provider's identity in any view.

**Context modal**

- **FR-017**: The context modal MUST present all information it shows today reorganized into clearly labeled, scannable groups consistent with the new visual system, and MUST additionally show credits used in the current session.

**Usage & authentication**

- **FR-018**: Users MUST be able to run a `/usage` command that fetches and displays their account usage from their gateway account, authenticated with the API key they logged in with, showing: credits remaining, and credits consumed for the current session, today, and the current billing period.
- **FR-019**: The `/login` command MUST be hidden from the command list while a user is signed in with an API key, and MUST reappear after logout so the user can sign in again by entering an API key.
- **FR-020**: Usage and authentication failures (unreachable gateway, invalid/expired key, malformed response) MUST produce clear, friendly guidance and MUST NOT block or corrupt the rest of the UI.

**Header**

- **FR-021**: The header MUST display the full workspace path, truncating from the left only when width requires it so the most specific path segments stay visible.
- **FR-022**: The header's delegated-model indicator MUST be labeled "Subagent".

**Skills delivery**

- **FR-023**: The agent MUST be made aware of available skills through a compact listing (per-skill name and purpose only), sufficient for it to recognize when a skill applies; full skill instructions MUST be loaded only when a skill is actually used for the task at hand.
- **FR-024**: Skill awareness MUST NOT grow the prompt beyond that compact listing and MUST NOT introduce per-turn variance into stable, cacheable content; the skill listing MUST be deterministic and stable across the turns of a session.
- **FR-025**: When a user request matches a skill's stated purpose, the agent MUST apply that skill; when no skill matches, skills MUST add no content to the conversation and no noise to the transcript.

**Cross-cutting**

- **FR-026**: All changes MUST preserve existing functional behavior (commands, keybindings other than removed hints, session handling); this feature changes presentation, metrics, and skill delivery — not agent capabilities.
- **FR-027**: Any change that affects prompt composition or history handling (notably skills delivery and per-task metric aggregation) MUST NOT regress the session cache-hit rate, verified by before/after measurement on realistic multi-turn sessions.

### Key Entities

- **Task**: One unit of user-requested work — from the user's triggering prompt through all intermediate turns and tool rounds to the final response. The scope over which summary metrics are aggregated.
- **Task Summary**: The three-metric record shown after a task: credits consumed, total tokens, cache-hit rate; sourced from provider-reported usage across the task's turns.
- **Tool Activity Entry**: A transcript record of one tool action, holding a display name, primary target, minimal outcome measure (collapsed view), and full detail (expanded view).
- **Account Usage**: Gateway-side usage data for the signed-in user's API key — credits consumed/remaining and related figures — displayed by `/usage` and (session credits) in the context modal.
- **Skill**: A workspace-discoverable capability with a name, a stated purpose (used for compact advertisement), and full instructions (loaded only on use).
- **Visual System**: The shared palette, spacing, and component styling rules applied uniformly across every screen and state.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across at least three mainstream terminal emulators (including Warp) at widths from 80 to 200 columns, a standardized rendering exercise (headings, bold, tables, code, tool entries, modals) produces zero visual defects — no raw markdown markers, misaligned tables, clipped bars, or overflow.
- **SC-002**: 100% of bold/italic/inline-code markdown in a regression corpus renders with correct styling; the `**xxx**` defect is unreproducible.
- **SC-003**: After any completed task, a user can identify what the task cost (credits, tokens, cache-hit rate) in under 5 seconds without reading any other numbers — the summary contains exactly those three metrics and nothing else.
- **SC-004**: Task summary figures match provider-reported usage for the task's turns exactly (100% agreement); no estimated or fabricated values appear anywhere in the UI.
- **SC-005**: In collapsed view, every tool action occupies a single line with name, target, and outcome measure; in expanded view, 100% of tool types show full detail in the uniform pattern.
- **SC-006**: Zero occurrences of removed elements after the overhaul: leftover thinking text, effort labels, the word "chat", token breakdowns, "Enter to send"/"Ctrl+P" hints, user-message arrow prefixes, or web-search provider names.
- **SC-007**: A signed-in user gets `/usage` results in under 3 seconds on a healthy connection, and 100% of failure modes (offline, bad key) end in a friendly message with the UI still responsive.
- **SC-008**: On identical realistic multi-turn benchmark sessions run before and after the change, session cache-hit rate shows no regression and total prompt tokens grow by no more than the compact skill listing.
- **SC-009**: In a skills trial with matching and non-matching tasks, the agent applies the correct skill on at least 9 of 10 matching tasks and loads zero skill instructions on non-matching tasks.
- **SC-010**: In qualitative review, testers rate the interface as "polished and trustworthy" (top-two-box) at a rate of at least 80%, and none of the pre-overhaul complaints (cluttered stats, broken bold, misaligned top bar) recur.

## Assumptions

- The screenshots referenced in the request (Warp rendering issues, broken bold text) were not attached; the defects were confirmed by inspecting the current interface behavior and code, and the spec describes them from the request's text.
- "Credits" are the billing unit of the user's gateway account (the MuhiyaWorkspace gateway hosted at api.muhiya.com, source at `F:\MuhiyaWorkspace\MuhiyaWorkspace`). The gateway is assumed to expose usage/credit data for a given API key; the exact endpoints and response shapes will be confirmed against the gateway codebase during planning.
- The gateway's per-request usage payload is the authoritative source for credits. If the field is not yet present in gateway responses, adding it is in scope for the gateway side of this feature (the gateway codebase is available at `F:\MuhiyaWorkspace\MuhiyaWorkspace`); until it is present, the credits metric is omitted per FR-012 rather than estimated.
- A "task" is bounded by one user prompt submission and the agent's final response to it, including all intermediate model turns and tool rounds.
- Cache-hit rate shown to users is computed from provider-reported cache usage fields only, consistent with the project constitution's honest-measurement principle; when absent it is hidden.
- The "Subagent" rename applies to user-visible labels; it does not require renaming internal concepts.
- Skills polish covers the delivery mechanism (discovery, compact advertisement, on-demand loading of full instructions) — not authoring new skills and not changing which skills exist.
- All prompt-affecting work in this feature is bound by the project constitution: deterministic stable prefix, dynamic content kept out of cached content, no redundant retransmission, and verified before/after measurement.
- The design process for the visual overhaul will follow the project's designated TUI design guidance (the "impeccable"/"tui-design" craft process) during planning and implementation; this affects how the work is done, not what the user-facing outcome must be.
- Existing keybindings (including Ctrl+O for detail toggle and Ctrl+P for the command palette) keep working; only their on-screen hint text is removed.
