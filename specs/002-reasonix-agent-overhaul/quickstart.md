# Quickstart: Validating the Reasonix-Aligned Agent Flow Overhaul

**Feature**: `002-reasonix-agent-overhaul` · **Date**: 2026-07-12
**References**: [spec.md](spec.md) success criteria · [contracts/](contracts/) · [research.md](research.md) §7 constants · [IMPLEMENTATION_REVIEW.md](../../IMPLEMENTATION_REVIEW.md) Part F

Run these gates in order; each assumes the previous is green. Everything is executable from the repo root `F:\MuhiyaCode Agent Go`.

## Prerequisites

- Go toolchain matching `go.mod`; a cgo-capable environment for the race gate (Linux/macOS or Windows with gcc).
- Live MuhiyaLLM gateway reachable with a valid virtual key (for cachebench + matched workload only).
- Reasonix built/runnable for the matched-workload comparison (its repo path: `C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2`).

## Gate 1 — Static & unit (every phase)

```
go build ./...
go vet ./...
go test ./...
```
Expected: clean build/vet; all tests pass including the new conformance suites (request-assembly §6, context-lifecycle §8, dispatch-gate §4).

## Gate 2 — Race (after Phase A/B, mandatory before merge)

```
CGO_ENABLED=1 go test -race ./internal/orchestrator/... ./internal/mcpclient/... ./internal/tui/...
```
Expected: zero races — specifically the B1 concurrent `/goal`-during-run test and parallel-subagent failure tests must be present and green (SC-005).

## Gate 3 — Contract conformance highlights (spot-check by name)

```
go test ./internal/orchestrator/ -run 'PromptStability|SteadyStateDiff|TailBudget|Ladder|Reclamation|Compaction|Archive' -v
go test ./internal/gateway/ -run 'SessionHeader|MarshalDeterminism|WireShape' -v
```
Expected: steady-state diff reconstruction passes over ≥3 turns; plain-turn tail ≤ budget; soft band mutates nothing; low-yield skip leaves history byte-identical and trips no guard (the REV A1 regression trap); digests accumulate byte-identically.

## Gate 4 — Cache benchmark (before/after, live gateway)

```
go run ./benchmarks/cachebench -base-url <gateway> -api-key <key> -out specs/001-prompt-cache-optimization/benchmarks/  # exact flags per benchmarks/cachebench/README or -h
```
Expected (SC-001, SC-003, SC-007): prefix-stability ≥ 99.5%; zero unexplained prefix changes; cache-read tokens monotonically non-decreasing except at recorded invalidation points; mid-band scenario shows ≤ 2 maintenance rewrites over 10+ task boundaries and zero failures. Results are APPENDED as new files — never edit existing evidence (FR-019).

## Gate 5 — Matched workload vs Reasonix (SC-002, FR-018)

1. Use one scripted prompt sequence (≥ 12 turns: plan, multi-file edits, shell runs, a subagent research step, two plain follow-ups).
2. Run it through MuhiyaCode (via the gateway) and through Reasonix (same provider/model), capturing per-request `prompt_tokens`, `prompt_cache_hit_tokens`, `prompt_cache_miss_tokens`, `completion_tokens` from request logs.
3. Compute per agent: steady-state hit rate (Σhit/Σprompt excluding request 1), prefix-stability rate, cost/task.
Expected: MuhiyaCode within 0.5 pp steady-state of Reasonix and within 15% cost on the SAME script. Parity claims from any other comparison are invalid by contract.

## Gate 6 — Scenario walkthroughs (SC-006; manual, in the TUI)

1. **Plan flow**: `/plan <task>` → at most 1–2 early blocked-mutation results, none after escalation → `exit_plan_mode` → modal → "Proceed later" → restart the app → "proceed" ⇒ saved plan executes. Stray `exit_plan_mode` outside plan mode ⇒ harmless no-op.
2. **Goal flow**: `/goal <objective>` → completes on a FINAL governed turn ⇒ goal leaves active state, no `[goal:` text visible anywhere in output; `/goal` while a task runs is refused for text-setting; goal/plan mutual exclusion notices fire both directions.
3. **Cancellation**: cancel mid-tool-batch → next prompt succeeds (no provider 400 about unmatched tool calls).
4. **MCP**: authorize an OAuth server → modal shows connecting → connected without reopening; kill the server process → next call flips to error and reconnects exactly once; second failure stays error with the "unavailable" message; expired-no-refresh token shows "needs authorization". Boot with cached surfaces spawns no child processes until first use; opening `/mcp` connects them and the refresh tick terminates.
5. **Usage readout**: after a task, the footer (tokens, cached/new, cost, session hit rate) persists through notices and ≥ 6 minutes idle; clears exactly on next prompt / session switch / exit.
6. **Thinking display**: Arabic/emoji reasoning truncates cleanly (no mojibake), RTL shaped; `thought for Ns` persists after completion; Ctrl+O expands to multi-line.

## Gate 7 — Evidence & docs

- Append matched-workload + cachebench results under `benchmarks/` with the source commit recorded.
- `docs/prompt-caching.md` updated for: ladder semantics + constants, archival, cache-discipline prompt section, steady-state diff invariant.
- Mechanism Inventory (research.md §6) rows all carry final decisions; any Phase-E deviation discovered during implementation is recorded there, not silently applied.

## Rollback / safety notes

- The cache-discipline prompt section and any wire-shape change are one-time cache breaks at upgrade: ship them together in ONE release so users pay a single cold start, not several.
- `pruned.jsonl` archives make every reclamation reversible for debugging: to inspect what a placeholder replaced, look up its path in the archive.
- If Gate 4 regresses prefix stability below the current 99.3–99.5% band, stop and bisect against the contract conformance suite before proceeding to Gate 5.
