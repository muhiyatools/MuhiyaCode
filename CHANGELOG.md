# Changelog

All notable MuhiyaCode changes are documented here. Releases follow semantic versioning.

## 1.3.0 – Token-Economy Overhaul (Feature 014)

- **Lean fixed prefix (≤10,000 bytes / ≤2,500 estimated tokens):** Core system prompt and core tool definitions wire footprint reduced by >60% on zero-MCP baseline.
- **ToolBroker & Deferred Tool Hydration:** Non-core built-in tools and MCP tool schemas are handled via a deferred hydration broker, preventing prefix bloat and cache invalidation when configuring MCP servers.
- **Security & Permission Parity:** Brokered tool calls strictly preserve workspace containment, OS sandbox, secret redaction, and approval confirmation gates.
- **CLI & Config Control:** Added `--lean-prefix` and `--economy [off|observe|balanced|aggressive]` CLI flags alongside the `tokenEconomyMode` config setting.

## 1.1.0

- Version-alignment release: same code as 1.0.5, republished under 1.0.6 so the app, npm package, and published version number all read 1.0.6.

## 1.0.5

- **Cheap by default: standard tasks run direct (feature 014).** "Fix two bugs in the project" now just fixes them — no research phase, no plan ceremony, no approval pause. The pipeline is reserved for genuinely large/epic work (two corroborating size signals) and for explicit plan requests ("create a plan to…"), which still pause for your approval.
- **One subagent at a time, always.** Parallel subagent fan-out is gone at the dispatcher level: dispatches run serially, and the phase instructions teach a single-chain workflow (one explore at a time while planning; ONE general subagent executing plan steps sequentially — each dispatch inherits its predecessor's cached context, so the serial chain is also the cheapest path).
- **No more raw network errors.** `Post ".../chat/completions": context canceled` and friends can no longer reach you: one shared friendly mapping now serves the task-stop line, subagent reports, and notices — cancellations say "stopped.", timeouts and connection drops get plain-language next steps, and the budget/credit/auth cases keep their specific guidance.
- **You decide, not the harness: model-driven subagent dispatch (feature 013).** The harness no longer launches subagents on its own — no more automatic research fan-out reading your whole workspace before planning. Every pipeline phase now instructs the main model, which chooses between working directly and delegating: planning opens with "inspect only what the task needs — read files directly, or delegate scoped explores if the surface is genuinely large"; after your approval the model delegates plan steps to general subagents (or implements directly) at its own judgment; the review gate still decides whether a validation review is warranted, but the model launches it, and completion stays blocked until its `VERDICT: PASS`. Phase enforcement, approval pauses, budgets, and context linking are all unchanged — only the launching moved to the model.
- **Clearer truncated-call errors.** A tool call whose JSON was cut off mid-generation (the live `update_plan: unexpected end of JSON input` failure) now names the real cause and the one-step fix — re-emit the whole call, shorter if it was large — instead of echoing the parser error.
- **Refactor pass (no behavior change).** Dead code and duplicate logic removed across the codebase (the old fan-out/grouping machinery, an unused TUI field, misplaced comments); the deprecated `strings.Title` replaced by a byte-identical local helper; `staticcheck` now runs clean outside documented idioms.

## 1.0.4

- **Fixed MiniMax models being unusable.** The default `max_tokens` for the MiniMax family was set to the entire context window (1M on M3), and MiniMax counts requested output against the shared context budget — so every M3 request failed with a 400 "maximum context length exceeded" before producing anything. The operational default is now 16k per turn like every other family; explicit larger requests still clamp to the documented ceiling. A regression test pins the invariant.

## 1.0.3

Competitive-agent audit (feature 011): the agent stops paying a review tax on every small task, spends tokens where risk actually lives, and reports honest per-provider cache numbers.

- **Risk-aware review gating.** The validation review subagent no longer fires after almost every task. A gating engine profiles each task (files touched, lines changed, task type, risk areas like auth/billing/concurrency/migrations) and picks a tier — skip, focused, or deep. Trivial and doc-only work skips review entirely; risky changes still get a full pass (risk always outranks size). Configurable via the new `reviewGating` setting (`default` / `conservative` / `off`), and an explicit `run_subagent("review")` request is never gated.
- **Focused reviews are bounded.** Focused-tier reviews get a scoped brief (changed files + same-package neighbors + one-hop importers, capped) and a hard token ceiling; a review that hits the ceiling wraps up and reports partial coverage instead of burning unbounded tokens.
- **Calmer task classification.** Escalating a task to the full orchestration pipeline now requires two independent signals (length, breadth words, bullet count, file paths) instead of one — so a short prompt containing a single word like "audit" no longer triggers the heavyweight pipeline.
- **Per-kind cache pinning.** Each subagent kind (explore / plan / review / general) gets its own stable gateway session pin, so provider prompt caches stay warm per role instead of colliding on one shared pin. Task summaries now include per-pairing steady-state cache-hit rates (cold first write excluded — the honest number).
- **Cheaper tool habits.** The prompt and shell-tool description now steer the model to `read_file`/`grep`/`glob` for reading and searching (cheaper, cache-tracked) instead of shell `cat`/`grep`; violations are counted in task stats.
- **Benchmark harness.** `MUHIYA_BENCH_JSON=1` makes one-shot runs emit a machine-readable summary line (usage, cost, review tier/spend, violations, per-pairing cache rates); `MUHIYA_BENCH_LEGACY_REVIEW=1` restores pre-011 review behavior for honest A/B baselines. Fixture suite + runner scripts under `scripts/bench_011.*`.
- **Clearer budget-limit errors.** A gateway 429 caused by an exhausted plan budget window now explains itself ("your plan's budget window is used up…") instead of the generic "rate limited — try again in a moment."
- **Fixes.** Starting an orchestrated plan now clears a stale active goal instead of carrying it across; tool-argument validation recurses into nested arrays/objects; subagent reports that exceed the return budget are banked to knowledge with a digest note instead of truncated silently.

## Unreleased — Experience Overhaul (Tier 2, folded into 1.0.2)

A retention-focused rework of the terminal experience and the agent's memory, built on the Stability Gate below. Executed phase-by-phase, each gated on a green build + full test suite.

- **Reasoning chip + footer.** The reasoning level moved from the header to a colored chip at the bottom-right, beneath the input box next to the permission state — showing only the level name (`Low`/`Medium`/`High`/`Max`) in a distinct color per level. The footer under the input is now a clean two-column line: key hints on the left, live state (plan / goal / queued skills / permission) plus the effort chip on the right; the quiet default mode shows no badge.
- **Live to-do checklist.** The cryptic `plan 0/5` bar is gone; a Claude Code-style checklist renders under the activity zone — `☒` done / `▸` in-progress / `☐` pending, capped at five rows with a `☒ N done` roll-up and a `… N more` overflow, toggled with `Ctrl+T`, with an idle "N to-dos remaining — say 'proceed' to resume" line, retiring itself when the plan finishes. The word split is now consistent everywhere: the **plan** is the proposal you approve, the **to-dos** are the live checklist — so `update_plan` reports "To-dos updated: N/M done" and the old "Plan updated:" phrasing is a forbidden string in the gauntlet.
- **Activity zone polish.** No more "Thinking" text (the initial status is "Working…", the reasoning tail is a bare gutter line); the status line reads `spinner · verb · elapsed · tokens · cache%` with a star-pulse spinner; a blank-line rhythm gives the whole zone room to breathe; the header context meter gains an urgency color ramp. Chrome (header / activity / to-dos / footer) is memoized per frame so it renders once instead of 2–5× per cycle, enforced by a source-scan guard.
- **Project memory store.** Durable memory moved out of the repo into a per-project store at `~/.muhiya/projects/<id>/memory/` (unreachable by the model's file tools — reached only through `save_memory` / `recall_memory`). It is now an always-loaded bounded **index** (facts + topic pointers, first 200 lines / 24 KiB) plus **topic files** read on demand via a new `recall_memory` tool, so detail is fetched only when relevant instead of loaded every turn. `save_memory` gained an optional `topic`; an existing workspace `MEMORY.md` is copied into the store once (the original is never touched); an optional user-level `~/.muhiya/MUHIYA.md` now applies across every project. The prompt-cache architecture is unchanged — a single reviewed instruction refresh means one cold-start on the first session of this build, then it rides the cache as before.

## Unreleased — Stability Gate (hardening 1.0.2 before first publish)

A reliability overhaul folded into the not-yet-published 1.0.2, focused on "a stranger installs it, runs twenty tasks, and hits zero harness-caused errors — and it can never destroy their work." Executed phase-by-phase, each gated on a green build + full test suite.

- **Build truth & self-diagnosis.** `muhiyacode --version` now shows the real git commit + build date (via `scripts/build.ps1`), ending the "same error keeps showing" stale-binary confusion. `muhiyacode doctor` gained launch-shadow detection (it warns when the `muhiyacode` command runs a different build than the repo), a masked API-key check, and workspace writability/git checks. New `scripts/check.ps1` (one gate) and `scripts/swap-global.ps1` (gate → stamped build → overwrite the npm vendor exe).
- **Harness telemetry.** Every gate rejection, tool failure, provider error, and bounded recovery now records a structured event, visible via `/errors` and a quiet `⚠ N harness` marker on the task summary. Bugs are traces now, not screenshots.
- **UI-thread safety.** The "Proceed now" freeze class is closed permanently: engine mutations that emit callbacks run off the Bubble Tea event loop, enforced by an AST guard test that fails if any non-read-only engine call is added back to the Update goroutine.
- **Gate policy.** One written policy (stated-in-advance, one-step-fixable or non-blocking, bounded ≤2, telemetered, sync-tested) audited across every gate. Fixed: the read-only shell gate no longer blocks a destructive *word used as an argument* (`grep format main.go` was wrongly refused); the `update_plan` step cap is now bounded (guide ≤12, accept ≤24 with a note, never the old hard "1-12 steps" wall); a subagent that exhausts its turn budget returns a guided partial and **never** the "reached its bounded turn limit" string.
- **Work safety (verified, already present).** The checkpoint/`/rewind` system already snapshots before every edit and restores; writes are already atomic (temp + rename, mode-preserving); the catastrophic-shell block tier is comprehensive — all confirmed and pinned.
- **Panic containment.** A panic in the task goroutine becomes a calm task error (the terminal survives) with a saved crash report; a top-level backstop catches everything else.
- **Acceptance gauntlet.** A deterministic end-to-end battery (greenfield first-try, tool-failure recovery, subagent turn-budget) with a forbidden-strings meta-check that fails if any past live-failure phrasing ever returns. See `LIVE_GAUNTLET.md` for the human-driven final check.

## Unreleased — Ultimate Consolidation (feature 010)

- Unified the two overlapping task-lifecycle state machines (the legacy plan-mode flags and the feature-009 pipeline phases) into ONE 11-state machine with a predicate API, so the desync class behind several recent live failures is now structurally impossible. Older saved sessions migrate through a single bounded loader. Three real production bugs were caught and fixed by the pinned tests during the rework.
- Added a fault-injection suite (31 scenarios) that drives the whole agent through every failure class and asserts one shared recovery invariant — every fault ends in guided success, a recorded degradation, or a user decision; never a loop, stall, or silent drop. Stability is now a test, not a hope.
- Consolidated every model-facing text (system prompt, tool descriptions, phase preludes, gate/denial messages) into one audited instruction system with automated checks for contradictions, unavailable-capability references, and missing worked examples. Fixed several instruction incoherences (the read-only shell allowlist is now taught before it's enforced; the plan-step example and report-format list are single-sourced; the write_file rule is identical in the tool and the prompt).
- Restructured the codebase: the 3,100-line engine and the oversized TUI/command files split into single-responsibility units (no file over 800 lines, enforced by a test), the package layering made acyclic and enforced, dead code and duplicate mechanisms removed with a reviewable ledger.
- Every advertised tool, command, keybinding, setting, and event is now verified wired or removed, enforced by a guard test that enumerates the live surface; removed a settings field that had no effect.

## 1.0.2

- Added a harness-enforced orchestration pipeline for standard and complex coding work: scoped research, execution-grade planning, an approval pause that overrides auto-accept, dependency-aware implementation workers, independent validation, durable resume state, and phase/role/model visibility. Small tasks retain the direct fast path.
- Replaced transcript-shaped delegation with structured handoffs (role, scope, scoped context, deliverable, output format), bounded parent summaries, actionable plan gates, and safe read-only validation commands. Successful full pipelines no longer fall through to the legacy executor and repeat completed edits.
- Added MiniMax M3 and M2.x client support: million-token M3 capability profiles, `reasoning_details` replay preservation, cached-token accounting, honest unavailable cache display, per-provider session pinning, and mixed-provider usage rows. Fresh two-provider installs select M3 main + DeepSeek V4 Pro subagent while preserving explicit pins and DeepSeek-only defaults.
- Rebuilt delegation so the agent actually uses subagents: a dedicated DELEGATION prompt section with concrete criteria, a rich `run_subagent` description with a worked example, and a one-time review nudge at max effort — replacing a hedged one-liner the model reliably ignored.
- Near-miss edits (`oldString not found`, ambiguous matches with nothing applied) now count as tool failures, so the loop guards stop DeepSeek's edit-retry loop instead of letting it burn tokens invisibly.
- Fixed the mid-task silent stop: a turn that narrated its next action without calling a tool ("… Let me fix:") was accepted as the final answer and ended the task mid-thought. The engine now nudges the model to make the call (bounded to two retries), so announced work actually happens.
- Added surgical-edit discipline to the prompt: existing files change via targeted edits; whole-file writes are reserved for new files.
- Added a `save_memory` tool: durable facts save through a dedicated, deduplicated, size-bounded tool with a distinct "Memory" transcript row — no more hand-editing MEMORY.md mid-task.
- Mid-session model switches now warn before applying (the provider cache restarts cold on the new model and pricing may differ), with Cancel as the default.
- The live token figure and task summary now show what you actually pay for — uncached input + output plus a cache-hit percentage — instead of a total inflated by cache reads; full detail stays in /context.
- Redesigned /context: session cost, API vs active time, lines added/removed, a per-model usage breakdown, and an estimated context-by-category table.
- Removed the per-task cumulative token cap that could cut a long task short with a "token budget exceeded" message. Effort now scales a task purely through subagent count and parallelism, reasoning depth, and turn budget — never a hard token ceiling. (A runaway-failure terminator and per-effort turn budget remain as loop guards.)
- Fixed the plan gate that could reject `exit_plan_mode` forever ("step 1 is missing an observable Acceptance:/Verify: check"): the quality bar now gives at most two concrete guidance rounds — including the exact expected step shape — then accepts the plan with a recorded degradation, so planning can never deadlock; the user approval pause remains the real quality gate.
- Fixed three pipeline lifecycle traps found in the release audit: an unrelated task no longer inherits an abandoned plan's approval gate (the pipeline parks and the plan stays resumable with "proceed"), `/plan clear` now releases the pipeline gating along with the plan, and a superseded/discarded plan can never be revived into execution through a stale approval modal.
- A light-depth pipeline now resumes light after a restart (depth persists in the plan-state sidecar) instead of silently restarting with full-depth subagent fan-out.
- Plan grounding now counts only current research-phase findings — implementation or review reports from earlier tasks no longer satisfy the research-evidence gate.
- Trimmed a duplicated ~4k-character findings briefing that was billed on two consecutive turns of every full pipeline; the plan phase now delivers it once.
- Session line counts no longer skip added/removed code lines that start with `++`/`--` (they were misread as diff headers).
- Removed an accidental npm self-dependency (`muhiyacode` depending on `muhiyacode`) that would have made every install download the binary twice.

## 1.0.1

- Added a DeepSeek capability profile: requests now stay within the provider's documented context and output-token limits, never send deprecated parameters, and `/context` shows the active provider capabilities.
- Reworked request timeouts into a first-byte deadline so a long-queued request survives the provider's keep-alive window instead of aborting early, while a genuinely stalled stream is still cancelled.
- Fixed right-to-left input: the first (rightmost) word of a long Arabic prompt is no longer truncated off the composer's right edge.
- Rewrote the README as a concise, production-grade landing page.

## 1.0.0 - Unreleased

- Rebuilt the complete terminal coding agent in Go with no TypeScript, Bun, Node.js, or npm runtime dependency.
- Added a responsive Bubble Tea TUI, streamed tool activity, subagent views, command palette, modal approvals, steering, session switching, and Arabic/RTL fallback rendering.
- Added an OpenAI-compatible streaming gateway with model discovery, virtual model IDs, reasoning profiles, retries, idle timeouts, usage/cache accounting, and text tool-call rescue.
- Added guarded filesystem, search, exact edit, atomic multi-edit, unified patch, shell, Git, checkpoint, rewind, web-search, and skill tools.
- Added durable SQLite sessions plus compatible history, inspection, knowledge, plan, transcript, trust, MCP, and secret storage under `~/.muhiya`.
- Added bounded effort-aware orchestration, cache-stable compact prompts, context folding/compaction, range-aware read deduplication, live steering, plan completion, and parallel subagents.
- Added stdio and Streamable HTTP MCP clients, OAuth authorization and refresh persistence, namespaced tools, deadlines, and per-server failure isolation.
- Added static cross-platform release archives, checksums, provenance attestations, install scripts, and Windows/Linux/macOS CI.

