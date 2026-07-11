# Agent and token-efficiency design

MuhiyaCode treats model turns—not individual tool bytes—as the dominant cost because the conversation prefix is resent on every request.

## Prompt compiler

- The full prompt is stable across turns and held below a regression ceiling of roughly 1,900 estimated tokens.
- Conversational and toolful tasks share one stable full prompt and pinned tool surface; task-specific policy stays in the user-message brief.
- Workspace, shell, model profile, effort directives, and feature availability are session-stable substitutions. The current date rides in the task brief.
- Task class and budgets live in a compact final user-message brief, preserving the system prefix for provider caching.
- Provider family addenda are short and target known tool-call failure modes; leaked DSML or a single JSON call can be rescued without accepting unknown tool names.

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

## Subagents

`explore`, `plan`, and `review` receive read-only registries. `general` receives the full tool registry for isolated implementation. Runs have independent contexts, lower reasoning tiers, strict turn limits, and shared durable knowledge. Multiple independent subagent calls execute in parallel only at effort levels that permit it; ordinary tools remain ordered.

## Regression gates

Tests enforce prompt-size ceilings, stable prompt bytes, lite-tool isolation, history folding/intactness, range coverage and migration fixtures, effort/class budgets, bounded outputs, subagent limits/reuse, steering, final landing, and real streamed integration turns.
