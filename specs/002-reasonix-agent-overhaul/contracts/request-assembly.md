# Contract: Request Assembly (Stable Prefix & Dynamic Tail)

**Feature**: `002-reasonix-agent-overhaul` · **Owner**: `internal/orchestrator` (composition) + `internal/gateway` (wire) · **Reference**: research.md §1–2, Reasonix `REASONIX.md:14-16` invariant

This contract freezes what every model request looks like. Any change to a MUST here requires a contract update in the same change, plus the conformance tests in §6. It exists so no future feature can silently regress cache behavior.

## 1. Prefix composition (byte-identical within a session)

**System prompt** — composed once per engine construction from configuration + persisted snapshots only. Section order is FIXED:
1. Core identity/instructions (static constants)
2. Static behavioral policies, including the **cache-discipline section** (this feature's one sanctioned addition; compile-time constant text: never re-read unchanged files; read narrowly; never repeat a failed call verbatim; minimal stable tool arguments; state what changed when retrying)
3. Workspace/config-derived stable facts (model addendum, workspace line)
4. Capability sections derived from persisted snapshots only (e.g. web-probe result) — never from live probing at compose time

Prohibited anywhere in the system prompt: timestamps, dates, session IDs, per-request or per-turn values, locale/environment-sensitive formatting, mode-dependent text (plan/goal/effort), MCP liveness state. Determinism guard: two engine constructions with identical config MUST produce byte-identical prompts (`prompt_stability_test.go`, extended).

**Tools array** — composed once per task from: base workspace definitions → synthetic tools (`update_plan`, `ask_user`, `propose_changes`) → `run_subagent` (sorted enum) → `exit_plan_mode` → MCP-pinned definitions (sorted by name). Rules:
- Every group's internal order deterministic; schemas canonicalized once (Go `json.Marshal` map-key sorting; no map-iteration order may reach the wire).
- The array is IDENTICAL for every request within a task, and changes only at task boundaries via the recorded boundary-change path. Mode toggles (plan/goal/effort) MUST NOT add, remove, reorder, or reword any entry (enforcement is dispatch-time; see dispatch-gate contract).
- Composition order is MuhiyaCode's existing fixed order, NOT re-sorted alphabetically (research decision #8: re-sorting would bust every existing session once for zero gain).

**Settled history** — append-only between recorded rewrites. No code path mutates an individual settled message in place; rewrites happen only through the context-lifecycle contract's operations, each paired with an InvalidationRecord BEFORE the next request is assembled.

## 2. Steady-state diff invariant (the headline guarantee)

For consecutive main-stream requests N, N+1 with no recorded invalidation between them:

```
bytes(request N+1) == bytes(request N)
                      + serialized(assistant msg of turn N)
                      + serialized(tool result msgs of turn N, in call order)
                      + serialized(new user msg)
```

Nothing else may differ: no header changes, no body-parameter changes, no re-rendered sections, no counters. Per-request parameters are session-stable: `temperature` (fixed), `tool_choice:"auto"` (fixed, kept per research decision), reasoning tier via header only (`X-Muhiya-Effort` — never rewrites settled bytes), `max_tokens` policy unchanged from current verified behavior. Conformance: a mock-provider test reconstructs request N+1 from request N + turn messages and asserts byte equality (mirrors Reasonix `cachehit_e2e_test.go:166-172`).

## 3. Wire message shapes (DeepSeek path through the gateway)

- user: `{"role":"user","content":"<text>"}`
- assistant (text): `{"role":"assistant","content":"<text>"}`
- assistant (tool calls): includes `tool_calls` array; `reasoning_content` key emitted UNIFORMLY on settled assistant tool-call turns (empty string when none) so effort/thinking toggles never flip settled bytes (present behavior, review-verified — keep).
- tool result: `{"role":"tool","content":"<raw output>","tool_call_id":"...","name":"..."}` — content is the raw tool output with zero wrapper text.
- Local-only fields (usage bookkeeping, display metadata) MUST never serialize to the wire (marshal-determinism tests).
- Phase C audit item (inventory #14) — RESOLVED (T002/T003): the four message wire shapes (user, assistant-text, assistant-tool-call, tool-result) are now frozen by `TestWireMessageShapesFrozen` in `internal/gateway/marshal_determinism_test.go`, which captures the ACTUAL current serialization and asserts each role's structure. The current `content` value on pure tool-call assistant turns is kept as-is (not "aligned" to Reasonix's `null`), since changing it would bust every live session once. Any deliberate future change must update that test in the same commit.

**Headers** (per stream, constant for the session): `Content-Type`, `Authorization`, `X-Client-App`, `X-Muhiya-Effort` (tier only), `X-Muhiya-Session: <session>:{main|sub|aux}` — main loop and compaction share `:main`; subagents and onboarding share `:sub`. The session value never changes within a session. (MuhiyaCode-specific addition vs the reference; required by the multi-model gateway.)

## 4. Dynamic tail — CLOSED allow-list

The ONLY content that may vary per turn, and it rides exclusively on the newest user message (or is a tail-appended user-role notice mid-task):

| Tag/block | Condition | Budget |
|---|---|---|
| Task classification brief | first message of a task | ≈38 tokens |
| `[active-goal: …]` block | goal Active | ≈85 tokens |
| Plan-mode block | plan mode on | ≈75–380 tokens (strengthened P1 text) |
| `[executing saved plan]` + plan text | pending-plan continuation turn only | plan-sized, once |
| Governor/steering/loop-guard notices | near caps, failure streaks, user steers | ≈30–45 tokens each, event-driven |
| Onboarding Q&A fold | first turn after onboarding | once |
| Compaction-failure notice (user-role, `[governor]` prefix) | compaction failure | once |
| `<project-instructions-update sha256=… supersedes=…>` block (005 US3) | root `MUHIYA.md` hash changed since the applied cursor | instructions-sized, one-shot per change |
| `<memory-update from=… through=…>` block (005 US3) | project-memory ledger advanced past the applied cursor | ≈15–40 tokens per changed item, one-shot per change |

Canonical top→bottom tail order (user text first): user text → `<project-instructions-update>` → `<memory-update>` → `[active-goal]`/plan-mode → `[executing saved plan]` → task classification brief. The two 005 update blocks are baked into the newest user message before first transmission and frozen verbatim into history, so they are ordinary append-only tail growth — they never mutate a settled prefix and owe no invalidation event.

Rules: adding ANY new tag requires updating this table and the tail-budget guard test in the same change. A plain follow-up turn (no goal/plan/pending/steer/onboarding/project-context change) carries ONLY the user text + at most the brief — guarded at ≤ ~50 system-added tokens (SC-004); the two 005 update blocks are absent unless the file/ledger actually changed since the applied cursor. Mid-history insertion is prohibited absolutely; a steer costs exactly one appended message (one accepted miss).

## 5. Request-producing paths covered

Main loop, subagent runs, compaction summarizer, onboarding, classification. Every path: (a) sends the correct `X-Muhiya-Session` suffix; (b) is either covered by the prefix-shape guard or explicitly documented as an intentionally cold isolated stream at its call site; (c) any guard degradation (provider lacking the normalizer) is recorded loudly, never silent.

## 6. Conformance tests (minimum set)

1. System-prompt byte stability across two constructions (existing, extended for the cache-discipline section).
2. Tools array byte-identical across all requests of a scripted multi-task session including a plan-mode toggle and a goal set/clear mid-session.
3. Steady-state diff reconstruction test (§2) over ≥3 consecutive turns.
4. Tail allow-list guard: plain follow-up turn total system-added tokens ≤ budget; every tag in the table appears only under its condition.
5. Marshal determinism: SessionID and all local-only fields absent from the body; header present and stable; per-stream suffixes correct (existing gateway tests, kept green).
6. Degraded-guard loudness: provider without normalizer ⇒ recorded degraded marker (REV A3 test).
7. Wire-shape freeze: golden-file the serialized forms of the four message shapes in §3; any diff fails.
