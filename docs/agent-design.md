# Agent and token-efficiency design

MuhiyaCode treats model turns—not individual tool bytes—as the dominant cost because the conversation prefix is resent on every request.

## Prompt compiler

- The full prompt is stable across turns and held below a regression ceiling of roughly 1,900 estimated tokens.
- Conversational and toolful tasks share one stable full prompt and pinned tool surface; task-specific policy stays in the user-message brief.
- Workspace, shell, model profile, effort directives, and feature availability are session-stable substitutions. The current date rides in the task brief.
- Task class and budgets live in a compact final user-message brief, preserving the system prefix for provider caching.
- Provider family addenda are short and target known tool-call failure modes; leaked DSML or a single JSON call can be rescued without accepting unknown tool names.
- Workspace-resident skills (`.agents/skills`, `.codex/skills`) are advertised as a deterministic `SKILLS` section appended to the stable prompt: one `- name (workspace-relative path): description` line per skill, sorted by lowercased name, discovered once per session. Identical configuration yields byte-identical bytes, so the section never invalidates the prefix within a session. The model loads a skill's full instructions on demand via `read_file`; skill bodies never enter the prompt. External-root skills are excluded from the advertised listing (they stay reachable through the manual `/skills` flow) so on-demand reads stay inside the workspace containment boundary.

## Runtime governor

Prompts are classified as chat, tiny, small, standard, large, or epic. The class and effort ceiling determine turns, approximate tool budget, subagent cap, reasoning tier, and verification expectation. Tool budgets are convergence signals rather than hard stops. A growing task can escalate once; the final landing turn disables tools so the model must return a factual result.

An incomplete plan causes bounded continuation instead of a premature "continue?" question. Mid-task steering is inserted at the next safe model boundary and extends runway without restarting valid work.

## Context memory

- Structured assistant tool calls and their results are always kept paired.
- Completed older tasks fold large tool payloads while retaining their compact truth.
- Read signatures and stitched line ranges prevent unchanged re-reads only while the earlier result remains intact.
- File fingerprints revoke stale coverage after an external edit. Agent mutations invalidate one path when possible and searches globally; conservative read-only shell commands preserve coverage.
- Knowledge stores bounded file notes and subagent reports. Identical read-only delegations at the same edit epoch reuse the report with zero model calls.
- Automatic compaction asks the model for a fixed `GOAL/STATE/FILES/DECISIONS/COMMANDS/PENDING` summary and includes durable knowledge facts. A deterministic fallback remains available if summarization fails.

## Project memory (cross-session)

- Project context is two root-local Markdown files, loaded with identical containment, UTF-8/size (≤32 KiB), and secret-screening rules: **`MUHIYA.md`** (user-authored instructions, like `CLAUDE.md`) and **`MEMORY.md`** (the agent's durable memory). On startup MuhiyaCode composes both into one byte-stable `## PROJECT CONTEXT` boot block that rides the first user message. Because the block is composed once and persisted as the session's `RenderedBootContext` and reused verbatim on resume, it never recomputes mid-session and never disturbs the prefix cache. A pristine (comment-only) file is treated as empty, so an untouched template costs zero prefix tokens.
- On first use MuhiyaCode writes a clear `MUHIYA.md` template (never overwriting an existing file); the user edits it by hand. `MEMORY.md` is agent-managed: the model reads it from the boot context and **keeps it current with its ordinary file tools** (`write_file` / `edit_file` / `apply_patch`) — there is no separate memory tool, trailer, or database, which keeps the prompt lean and the write path on the normal execution flow (the Reasonix/Claude-Code model). The system prompt tells the model to record only durable, non-secret project truth and to prune entries that go stale.
- A mid-session edit to either file — the agent's own or a manual user edit — is surfaced exactly once as a one-shot `<memory-update>` / `<project-instructions-update>` tail on the next user message, gated on the file's content hash, then folds into the next session's cached prefix at no per-turn cost. None of this changes tools, model routing, or the deterministic prompt guarantees.

## Subagents

`explore`, `plan`, and `review` receive read-only registries. `general` receives the full tool registry for isolated implementation. Runs have independent contexts, lower reasoning tiers, strict turn limits, and shared durable knowledge. Multiple independent subagent calls execute in parallel only at effort levels that permit it; ordinary tools remain ordered. Each delegated mission carries an explicit capability statement — its exact, sorted toolset, the boundary that nothing else is available, and a report contract to flag ambiguity or architectural choices back to the caller rather than deciding them (tuned to DeepSeek's mechanical-worker strengths). For the launch configuration, routing `general` workers to `deepseek-v4-flash` via `SubagentModelID` while the orchestrator holds `deepseek-v4-pro` is the recommended posture.

The delegation budget is the smaller of the effort ceiling and the task-class cap; every effort tier (including `low`) grants at least one run for classes that support delegation. Because plan mode advertises `explore`/`plan`/`review` delegation in its brief, a plan-mode task guarantees a floor of one run so the advertised capability can never contradict a zero budget. When a real budget is exhausted mid-task, each report states the remaining count and denials escalate from a redirect ("work directly") to a hard stop; when the task has no budget at all (`agents=0`), the very first call is hard-closed, so a task converges on direct work instead of retrying a closed door.

## Plan lifecycle

An approved plan moves through an explicit phase machine — drafting, ready, pending, executing, then finished, interrupted, superseded, or discarded — persisted in the `plan_state.json` sidecar (additive `phase` field; legacy sidecars derive it from the older booleans on load). An executable hint ("say 'proceed'") appears only while a plan is genuinely pending; the moment execution begins — detected by the user's go-ahead or, phrasing-independently, by the first plan step that starts — the hint is withdrawn, and finishing all steps marks the plan finished so it never re-advertises as executable, even across resume. A run that ends with steps still open records the plan interrupted (resumable). When the model finalizes an executing plan with steps still open, the answer carries a harness-attributed disclosure of the incomplete steps so it never implies work it did not do.

## Regression gates

Tests enforce prompt-size ceilings, stable prompt bytes, lite-tool isolation, history folding/intactness, range coverage and migration fixtures, effort/class budgets, bounded outputs, subagent limits/reuse, steering, final landing, and real streamed integration turns.
