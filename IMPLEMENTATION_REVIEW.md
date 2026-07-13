# IMPLEMENTATION REVIEW & FIX DIRECTIVE — OVERHAUL_PLAN.md

**Executor:** GLM 5.2 — execute this document directly, top to bottom. Every item has: problem, affected files, expected vs current behavior, root cause, exact fix instructions, and required tests.
**Reviewed change set:** the uncommitted working tree of `F:\MuhiyaCode Agent Go` (branch `001-prompt-cache-optimization`) vs HEAD, claimed as a "full implementation" of `OVERHAUL_PLAN.md`. Reviewed by four independent verification passes with file:line evidence. `go build`, `go vet`, `go test ./...` all pass — but passing tests do NOT mean the plan was implemented: several required mechanisms are missing or wrong, and the missing tests are exactly where the bugs are.
**Reference project (copy patterns from here):** `C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2` ("Reasonix"). All Reasonix file:line references below are into that tree.

---

## 0. RULES — READ FIRST

### 0.1 The prime invariant (unchanged, and the implementation respected it — keep it that way)

> The cached prompt prefix (system prompt + tools array + settled history) is sacred. Composed once per session, mutated only at explicit event-recorded boundaries. ALL per-turn dynamics ride the tail of the newest user message. Never insert mid-history, never mutate settled messages, never filter/reorder the tools array by mode.

The review confirmed the implementer preserved this: `prompt.go` untouched, `exit_plan_mode` added unconditionally in a fixed position (`engine.go:897-906`), all new notices tail-appended, no mid-history writes, prefix-shape guard strengthened (byte-hash comparison, `prefixshape.go:95`). **Every fix below must also preserve it.** When Part A tells you to add cache-discipline text to the system prompt, that is a STATIC, session-invariant addition (one-time break at upgrade, then byte-stable forever) — it must contain no dynamic values.

### 0.2 Confirmed good — do not touch except where an item below says so

C1 (session header, fully correct: `types.go:229`, `provider.go:151-153`, `engine.go:593,683`, `subagent.go:158`, `onboarding.go:31`, tests `gateway_test.go:78-133`), C5, G2, G4, G6, P1, P3, P4, P5, H3, H5, H7, H8, M3, M4, M6, M7, T1 (code), the evidence/benchmark file changes (verified honest — metrics were made STRICTER, not fabricated), and the strengthened `prefixshape.go`.

### 0.3 Verification protocol (run after EVERY part, not just at the end)

```
go build ./... && go vet ./... && go test ./...
```
Plus, after Part B: `CGO_ENABLED=1 go test -race ./internal/orchestrator/... ./internal/mcpclient/...` on a cgo-capable host (the B1 data race and the new concurrency in manager.go demand it).

---

## SCORECARD (review verdicts, all 30 plan items)

| Item | Verdict | Item | Verdict | Item | Verdict |
|---|---|---|---|---|---|
| C1 | ✅ DONE | G4 | ✅ DONE | H5 | ✅ DONE (1 weak test) |
| C2 | ⚠️ DONE, test gap | G5 | ⚠️ PARTIAL (2 bugs) | H6 | ❌ **MISSING** |
| C3 | ⚠️ PARTIAL, test gap | G6 | ✅ DONE | H7 | ✅ DONE |
| C4 | ❌ **DEFECTIVE** | P1 | ✅ DONE | H8 | ✅ DONE |
| C5 | ✅ DONE | P2 | ⚠️ PARTIAL (1 wrong branch) | M1 | ⚠️ PARTIAL (no 30s bound) |
| G1 | ⚠️ PARTIAL (4 gaps) | P3 | ✅ DONE | M2 | ⚠️ PARTIAL |
| G2 | ✅ DONE | P4 | ✅ DONE | M5 | ⚠️ PARTIAL (M5.3 missing) |
| G3 | ⚠️ PARTIAL + **data race** | P5 | ✅ DONE | M8 | ❌ **BROKEN** (reconnect dead) |
| M3/M4/M6/M7 | ✅ DONE | H1 | ⚠️ DONE, enum bug + no tests | T1 | ✅ code done, test weak |
| H2 | ⚠️ PARTIAL | H3 | ✅ DONE | T2 | ⚠️ PARTIAL (3 gaps) |
| H4 | ⚠️ PARTIAL (new hole) | | | T3 | ⚠️ PARTIAL |

New defects introduced by the implementation (fixed in Parts A–D below): the C4 suppressed-invalidation trap (A1), the goal-state data race (B1), unconditional `exit_plan_mode` + batch orphaning (B2), goal markers leaking to display (B3), cumulative-not-consecutive idle guard (B4), `/goal` flipping plan mode mid-task (B5), numeric-enum validation bug (B8), M8 stale-tools loop (D1), `/mcp` modal infinite non-terminating tick with lazy servers (D2).

---

# PART A — CACHE SYSTEM (TOP PRIORITY)

The state of caching after this implementation, honestly: the byte-stability machinery is intact and slightly stronger than before (C1 session pin now closes the routing-flip hole, F7 replay decoupling landed, shape guard now hashes settled bytes). The remaining cache work is: **one introduced bug that can hard-fail a session (A1)**, one robustness risk (A2), two missing guard tests (A3), and — per the product owner's directive — **adopting Reasonix's remaining cache algorithms so the entire agent flow is built for cache hits (A4)**. Do Part A first and completely before anything else.

## A1 — [BLOCKER] C4 yield gate mutates history, then suppresses the invalidation event → fatal guard trip

- **Files:** `internal/orchestrator/engine.go:1467-1496` (maintenance path), `internal/orchestrator/history.go` (Maintain), `internal/orchestrator/maintenance_test.go`.
- **Expected (plan C4):** ESTIMATE fold yield BEFORE mutating history; if yield is below the floor, SKIP the fold entirely (no mutation, no latch slot consumed). Every applied rewrite records an `InvalidationEvent`.
- **Current:** the code calls `e.history.Maintain()` FIRST (history already rewritten), then checks `!result.Folded && result.FoldedTokens == 0 && pressure < 0.80` and returns early WITHOUT recording an invalidation event (`engine.go:1490-1496`). A trim-only change in the 0.60–0.80 band therefore mutates settled bytes with no recorded event.
- **Consequence:** the next main-loop request's shape guard compares settled-byte hashes (`prefixshape.go:95`), finds them changed with no matching event, and returns the fatal `"stable request prefix changed without an invalidation event"` (`engine.go:706`). A user in the 0.60–0.80 pressure band can have their session hard-fail. This is the single worst bug in the change set.
- **Root cause:** the yield check was bolted on after the mutation instead of gating it.
- **Fix instructions:**
  1. In `internal/orchestrator/history.go`, add a read-only estimator method (e.g. `EstimateMaintainYield() (foldTokens int, trimTokens int)`) that computes what `Maintain` WOULD reclaim without mutating anything (walk the same candidates `foldCompletedLocked`/trim logic walks, under `RLock`, summing candidate token counts).
  2. In the engine maintenance path (`engine.go:~1467`): call the estimator FIRST. If `foldTokens + trimTokens < minMaintenanceYield` where `minMaintenanceYield = contextWindow * 5 / 100` (the plan's 5% floor — compute from the model's context window already known to the engine), skip entirely: no `Maintain()` call, no latch consumption, no event. Log one debug line.
  3. If the estimate passes the floor, run `Maintain()` and record the `InvalidationEvent` for ANY `result.Changed` outcome (fold OR trim) — never return between a mutation and its event. Delete the current post-hoc early-return at `engine.go:1490-1496`.
  4. Keep the latch semantics that were correctly implemented (no reset on transient dips, reset only on `/compact` `engine.go:430` and auto-compaction success `engine.go:660`).
- **Required tests (in `maintenance_test.go` — these were required by the plan and never written):**
  - Oscillation: pressure crosses 0.60 repeatedly → fold fires at most twice total (latch), and every applied fold has a matching invalidation event.
  - Low-yield skip: pressure 0.65 with almost nothing foldable → history is byte-identical after the maintenance pass (assert settled hash unchanged) and NO event recorded and NO latch slot consumed.
  - Trap regression: construct the exact current failure (trim-only change in-band) → assert the next `Run` does NOT return the fatal shape-guard error.

## A2 — [HIGH] New hard-error on usage-frame count can fail whole requests against the live gateway

- **Files:** `internal/gateway/provider.go:181-185, 205-207, 254`.
- **Expected:** robust usage parsing; a missing/duplicated usage frame from the provider chain must not fail an otherwise-successful request.
- **Current:** `chatOnce` now errors if a stream carries anything other than exactly one usage object. The MuhiyaLLM gateway's fallback paths mark `UsageEstimated` and there are stream-recovery scenarios where usage frames can be absent or duplicated; a hard error turns a metrics imperfection into a failed turn.
- **Fix instructions:** downgrade both conditions from request-failure to degraded-usage: on zero usage frames, return the response with `Usage` zeroed and a flag the engine records as usage-estimated (mirror the existing `CacheAttribution` degraded conventions); on multiple frames, keep the LAST frame and log once. Do not fail the request. Keep `usage:null → absent` (that part is correct).
- **Required test:** stream fixture with zero usage frames → request succeeds, usage flagged estimated; fixture with two usage frames → last one wins.

## A3 — [MEDIUM] Missing cache-guard tests (the guard changes are correct but unproven)

1. **C2 degraded path untested:** every mock now implements `StableRequestMessages`, so `recordDegradedPrefixGuard` (`engine.go:1568-1583`) never executes in tests. Add one test with a provider that does NOT implement `wireRequestNormalizer` → assert the degraded marker is recorded once and the run still succeeds (relaxed guard branch).
2. **C3 subagent guard untested:** the plan required a `cachehit_guard_test.go` case where the subagent's stable inputs mutate mid-run → assert the run fails with the guard error (`subagent.go:150-157`). Write it (inject a provider that changes the system text between subagent turns).

## A4 — REASONIX CACHE ALGORITHM ADOPTION (build the whole agent flow for cache hits)

Honest framing so you don't break what works: **MuhiyaCode already implements Reasonix's core invariant** (compose-once prefix, tail-riding dynamics, pinned MCP surface, event-recorded rewrites) — that was feature 001, and this review confirms it held. What MuhiyaCode does NOT yet have are Reasonix's four supporting algorithms. Adopt them as follows, in this order. Each is cache-additive; none may violate 0.1.

### A4.1 — Static cache-discipline section in the system prompt (the "system prompting" directive)

- **File:** `internal/orchestrator/prompt.go` (currently untouched — good).
- **What:** append ONE static section to the system prompt teaching the model cache-friendly behavior. It must be a compile-time constant — zero dynamic values — so it is byte-identical every request of every session (one-time cache break on upgrade, then permanent hits).
- **Content (adapt to the prompt's existing voice):**
  ```
  ## Token & cache discipline
  - Never re-read a file you already read unless a tool result told you it changed.
  - Read narrowly: prefer offset/limit or a targeted search over whole-file reads.
  - Never repeat a tool call that just failed with the same arguments; change approach instead.
  - Keep tool arguments minimal and stable; do not add decorative fields.
  - When you must retry after an error, state in one short clause what you changed.
  ```
- **Why this is safe:** static text in the prefix costs one upgrade-time miss and then rides the cache forever, while steering the model away from the behaviors (redundant reads, verbatim failed retries) that inflate the uncached tail every turn. This is how "cache priority" enters the agent's own instructions without touching per-turn bytes.
- **Test:** `prompt_stability_test.go` — assert the system prompt is byte-identical across two engine constructions with identical config (extend the existing stability test to cover the new section).

### A4.2 — Two-tier stale-tool-result reclamation BEFORE compaction (Reasonix `internal/agent/prune.go`)

- **What Reasonix does:** between snip-ratio 0.6 and compact-ratio 0.8, it reclaims context WITHOUT paying for a summarizer call and with minimal prefix damage: tier 1 `SnipStaleToolResults` keeps head+tail of old tool results (per-tool geometry via `tool.SnipHinter`: read-only tools `{head:80,tail:12}` lines, side-effecting `{head:40,tail:40}`, `minPruneBytes=1024`); tier 2 `PruneStaleToolResults` elides to a placeholder. Originals are archived to `.jsonl` first so nothing is lost (Reasonix `prune.go:187-209`, trigger logic `compact.go:100-130`).
- **What MuhiyaCode has:** fold/trim in `history.Maintain` — same purpose, cruder geometry, and (pre-A1) mis-gated. KEEP Maintain as the mechanism; ADOPT Reasonix's parameters and archival:
  1. Apply per-tool head/tail geometry when trimming aged tool results: read-only tool results (read_file, grep, glob, list_files, search_text, git_status, git_diff) keep head 80 lines / tail 12; mutating-tool results keep head 40 / tail 40; never touch results under 1024 bytes (not worth the rewrite).
  2. Before any fold/trim mutates a message, append the original to a session-local archive file (`<session>/pruned.jsonl`, one JSON object per pruned message: task index, tool name, original content) via `internal/state/session.go`. This makes every reclamation reversible/inspectable and matches Reasonix's re-derivability guarantee.
  3. Ordering stays: snip/fold band 0.60–0.80 (with the A1 yield floor), full compaction only ≥0.80.
- **Cache note:** each applied pass is one prefix reset (already event-recorded). The Reasonix win is that better geometry reclaims MORE per reset, so resets are RARER. That is the metric to preserve: fewer, higher-yield rewrites.
- **Tests:** geometry unit test (read-only vs mutating trim shapes); archive test (pruned original lands in pruned.jsonl verbatim); the A1 tests already cover event pairing.

### A4.3 — Compaction geometry alignment (Reasonix `internal/agent/compact.go:19-36, 94-146, 377-418`)

- **What Reasonix does:** compaction only at 0.8 of window; keeps a 16,384-token verbatim tail; PINS the system prompt + the first user turn (if <1500 tokens) + all prior digests; new digests ACCUMULATE (fold the middle into one more digest) instead of re-summarizing old digests; a `consecutiveCompacts` latch prevents back-to-back compaction loops; at 0.5 it only posts a soft notice ("approaching compaction") without rewriting anything.
- **Fix instructions:** compare MuhiyaCode's compaction (engine.go `Compact`/auto path around `:660,1433`) parameter-by-parameter against that list and align: (a) trigger ratio 0.8 (verify current constant), (b) verbatim tail ≥16k tokens, (c) pin first user turn <1500 tokens so the task statement survives, (d) digest accumulation — a second compaction must NOT re-summarize the first digest, only append a new one, (e) consecutive-compacts latch, (f) 0.5 soft notice with zero rewrite. Where MuhiyaCode already matches, leave it; where it differs, adopt Reasonix's value and note it in `docs/prompt-caching.md`.
- **Test:** two consecutive compactions → first digest byte-identical in the second result; soft-notice band performs zero history mutation (settled hash unchanged).

### A4.4 — Zero-overhead steady-state tail (verify, don't build)

Reasonix sends the raw user text unwrapped when no goal/plan/memory is pending — zero per-turn tail overhead (`internal/control/input.go:157-207`). MuhiyaCode's audited tail overhead is already small (brief ≈38 tokens once per task; goal ≈85 and plan ≈75 only when active; notices only near caps). ACTION: add one regression test (`prompt_stability_test.go`) asserting that a plain follow-up turn with no active goal/plan/pending-plan appends the user text with NOTHING but the classification brief — so future changes can't quietly grow the tail. No production change.

### A4.5 — Session cache-hit-rate on the status line (Reasonix `internal/cli/chat_tui.go:80-81, 315-319`, `agent.go:742-746`)

- **What:** Reasonix shows a running session cache-hit rate and `↓tokens` readout continuously. MuhiyaCode has the data (`AggregateUsage`, `contract/cache.go:93-141`) and T1's persistent footer.
- **Fix instructions:** extend the T1 usage footer and the busy status line to include the session-cumulative cache-hit rate (`Σcache_read / Σprompt` for the main stream) and the current task's prefix-stability rate. Read from the existing usage aggregation — no new accounting. This gives the user (and future audits) a live view of exactly the number this whole program optimizes.
- **Test:** extend the T1 footer test to assert the rate string renders from a seeded usage snapshot.

---

# PART B — ORCHESTRATOR CORRECTNESS FIXES

## B1 — [HIGH — data race] Goal state guarded by two different mutexes

- **Files:** `internal/orchestrator/goal.go` (all of it), `internal/orchestrator/plan.go`.
- **Expected (plan G3.3):** a SINGLE mutex guarding the mode/goal state ("single modeMu preferred").
- **Current:** `e.goal`/`e.lastGoal` are written under `modeMu` in `setGoalLocked`/`clearGoalLocked` (`goal.go:122-156`, reached from `SetGoal`/`ClearGoal`/`SetPlanMode`) but read/written under `goalMu` in `goalBlock`/`scanGoalMarker`/`advanceGoal`/`GoalSnapshot`/`goalSnapshotForPersist` (`goal.go:201-357`). Two mutexes on one field = no mutual exclusion. Reachable: `/goal` is allowed while a task runs (`internal/tui/actions.go:22-23`), so the TUI's `SetGoal` races the engine loop's `scanGoalMarker` on a live task. Torn reads / lost updates / potential nil-deref.
- **Fix instructions:** eliminate `goalMu` entirely; guard ALL goal and plan-mode state (`goal`, `lastGoal`, `planMode`, `pendingPlan`) with the single `modeMu`. Audit every method in goal.go/plan.go for lock coverage after the merge; keep critical sections short (persistence writes stay off-lock via the existing `writeMu` pattern, `goal.go:362-369`). Locks are currently never nested — keep it that way (no calls to other locking methods while holding `modeMu`).
- **Required test:** a `-race` test: run `engine.Run` with a scripted provider while a second goroutine calls `SetGoal`/`ClearGoal` in a loop; `CGO_ENABLED=1 go test -race` must pass.

## B2 — [HIGH] `exit_plan_mode` fires unconditionally and orphans batch-mates

Two related defects, fix together:

**(a) Unconditional exit (violates plan P2.2).**
- **File:** `internal/orchestrator/engine.go:1277-1278`.
- **Expected:** `exit_plan_mode` called while plan mode is OFF returns the harmless result "not in plan mode" (Failed:false) and the loop continues.
- **Current:** dispatch returns `ErrPlanModeExited` regardless of plan-mode state, so any stray call ends the task; in the one-shot path it even sets `pendingPlan=true` — a phantom saved plan.
- **Fix:** in the `exit_plan_mode` case of `executeOne`, check `e.PlanMode()` first; if false, return output "not in plan mode — continue with the task" with no error.

**(b) Batch orphaning (breaks H4's guarantee).**
- **File:** `internal/orchestrator/engine.go:828-846` (outcomes loop).
- **Expected:** every `tool_call` in an appended assistant message gets a `RoleTool` result — always.
- **Current:** when an outcome carries `ErrPlanModeExited`, the loop appends THAT result and immediately `return e.finalize(...)`. Calls ordered after it in the same batch never get results → assistant message with N tool_calls but <N results persisted → the next request fails with a provider 400 about unmatched tool_calls.
- **Fix:** restructure: first append ALL outcomes' results (for calls after the exit, if they were skipped, append a synthetic result "skipped: plan mode exited"), THEN handle the planReady finalize. The invariant "assistant tool_calls and their results are appended atomically" must hold on every return path — audit the whole outcomes loop for other early returns while you're there.
- **Required tests:** batch of [read_file, exit_plan_mode, write_file] in plan mode → history has 3 tool results, then planReady; `exit_plan_mode` with plan mode off → task continues, no pendingPlan set.

## B3 — [HIGH] Goal completion still missed on final turns; markers leak to the user

- **Files:** `internal/orchestrator/engine.go:746-757` (isFinal path), `:1644-1656` (finalize), `internal/orchestrator/goal.go:219-253`, TUI display boundary (`bridge.go` / wherever final text is emitted).
- **Expected (plan G1):** markers scanned on EVERY assistant text including the isFinal early-return and a last-chance sweep in `finalize`; completion with incomplete plan intercepted ONCE; `StripGoalMarkers` applied to displayed text while markers stay in history.
- **Current:** tool-call branch and no-tool branch scan correctly (`engine.go:812`, `:786`), but the isFinal path returns via `finalize` without scanning — the ORIGINAL bug (goal finishing on the last governed turn stays active) is still alive on this path. `finalize` has no sweep. `StripGoalMarkers` (`goal.go:253`) has ZERO callers — `[goal:complete]` renders verbatim to the user. The intercept-once integrity check was never implemented.
- **Fix instructions:**
  1. Add `e.scanGoalMarker(assistantText)` in the isFinal branch before the `finalize` return (`engine.go:~746`).
  2. In `finalize`, as a last-chance sweep, scan the final text if a goal is still active.
  3. Wire `StripGoalMarkers` at the display boundary: strip from the text passed to TUI callbacks (final answer AND streamed tokens if feasible — at minimum the final answer), never from what's appended to history.
  4. Intercept-once: in `scanGoalMarker`, when the marker is `complete` and `e.hasIncompletePlan()` and this goal hasn't been intercepted before (add an `Intercepted bool` to the Goal struct), do NOT complete — return a continuation instructing "plan steps N,M incomplete — finish them or mark [goal:blocked:reason]" and set `Intercepted=true`. Second complete claim passes.
- **Required tests:** goal completes on the final governed turn → goal cleared; complete-with-incomplete-plan → intercepted exactly once; displayed final text contains no `[goal:` substring while history does.

## B4 — [MEDIUM] Idle guard counts cumulatively; no-marker nudge logic incoherent

- **File:** `internal/orchestrator/goal.go:273, 290-311`, call site `engine.go:791`.
- **Expected (plan G5):** idle guard = 2 CONSECUTIVE auto-continued turns without tool calls → stop; a productive tool turn resets the count. No-marker reply → nudge once ("end with exactly one goal marker"), second consecutive markerless reply → stop and ask.
- **Current:** `advanceGoal`'s only call site passes `madeToolCall=false` always, so the reset branch (`goal.go:299-301`) is dead — the count is cumulative per task and can block a goal that did real work between two text replies. The nudge condition `LastMarker=="" || AutoTurns==1` misfires: `LastMarker` is only set when a marker EXISTS, so it nudges on marker-ed turns and never properly tracks "missing marker".
- **Fix instructions:** thread the real signal: the engine knows whether the just-ended turn had tool calls — pass it (`e.advanceGoal(text, len(calls)>0)` from the appropriate branch, or track `lastTurnHadToolCalls` on the engine and read it inside). Fix the reset branch to actually run. Replace the nudge logic with an explicit `MissingMarkerStreak int` on the Goal: no marker → streak++; streak==1 → continue once with the nudge appended; streak>=2 → stop and ask the user; any valid marker → streak=0.
- **Required tests:** text-turn, tool-turn, text-turn sequence → NOT blocked (consecutive semantics); two consecutive markerless replies → stops with question; markerless then marker-ed → streak resets.

## B5 — [MEDIUM] `/goal` mid-task silently flips plan mode (defeats P5)

- **Files:** `internal/tui/actions.go:22-23` (busy allow-list), `internal/orchestrator/goal.go:66-69`.
- **Expected:** mode changes take effect at task boundaries; P5 busy-guards `/plan` for exactly this reason.
- **Current:** `/goal` is allowed while busy, and `SetGoal` (per G3) turns plan mode off — so a running plan-mode task loses its read-only protection mid-turn through the goal path.
- **Fix:** busy-guard `/goal <text>` the same way `/plan` is guarded (`actions.go:177-187` pattern): refuse with "Cannot set a goal while a task is running." Keep `/goal` (status query) and `/goal clear` allowed while busy — clearing is safe (it only ever REMOVES tail content next task) but text-setting is not, because of the plan-mode side effect.
- **Required test:** TUI action test mirroring the P5 one.

## B6 — [HIGH] H6 shared dispatch gate was never implemented — subagents bypass every new guard

- **Files:** `internal/orchestrator/engine.go` (executeCall gate stack: validation `:1140`, failed-cache `:1149`, plan gate `:1158`, repeat limiter `:1185`, duplicate guard), `internal/orchestrator/subagent.go:195-201`.
- **Expected (plan H6):** one shared gate method used by BOTH loops; subagent gets per-run counter instances.
- **Current:** grep confirms no `gatedExecute` exists. `subagent.go:201` still calls `e.registry.Execute` raw; only the P3 plan-mode gate and H7 were bolted on inline. Subagents get: no H1 validation (raw Go JSON errors), no H2 failed-cache/storm-breaker (can loop on an identical failing call for all 24 turns), no repeat limiter, no duplicate guard.
- **Fix instructions:**
  1. Extract from `executeCall` a method: `func (e *Engine) gatedExecute(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, allowed map[string]bool, counters *callCounters) (output string, failed bool)` — where `callCounters` is a new small struct holding the per-scope maps currently living as per-task engine fields consulted by the gates (`callCounts`, `failedCalls`, `failedClassCounts`, plan-violation counter). Gate order inside: H1 validation → plan-mode/mutation gate (including P4's read-only-shell passthrough) → repeat limiter → duplicate guard (main loop only — pass nil ledger for subagents if the inspection ledger is main-scoped) → H2 failed-cache → dispatch → record failure/success into the counters.
  2. Main loop: `executeCall` becomes a thin wrapper passing the engine's per-task `callCounters`.
  3. Subagent loop (`subagent.go:201`): construct a fresh `callCounters` per subagent RUN and dispatch through `gatedExecute`. Remove the now-redundant inline P3/H7 code ONLY if the shared gate reproduces both behaviors exactly (P3's general-kind rejection stays where it is — it's a pre-run check, not a per-call gate).
  4. Watch locking: the counters are per-scope so subagent goroutines never share maps with the parent → no new contention on `taskMu`. The H2 storm-breaker's escalation notice appends to the PARENT history — for subagent scope, put the escalation text into the subagent's own transcript instead (its report), never `e.history` (that's also the fix for the pre-existing risk of concurrent `history.Append` from parallel batches — verify `History` has an internal mutex; if not, this is mandatory).
- **Required tests:** subagent repeating an identical failing call → short-circuited by the shared failed-cache on attempt 2; subagent sending malformed args → H1's actionable error text (not a raw Go error); parallel read-only subagents failing simultaneously → `-race` clean.

## B7 — [MEDIUM] H2's consecutive all-failed-turns detector is missing

- **File:** `internal/orchestrator/engine.go` (turn epilogue, near the existing `consecutiveFailures` logic `:868`).
- **Expected (plan H2.2):** a counter of consecutive TURNS in which every tool call failed; at 2, inject the storm-breaker escalation notice.
- **Current:** only the per-call `(tool, normalizedError)` breaker and the old per-call `consecutiveFailures>=3` nudge exist; a model alternating two different failing tools across turns evades both longer than it should.
- **Fix:** after each turn's outcomes are appended: if `len(outcomes)>0` and all have `Failed:true`, increment `allFailedTurnStreak`; any success resets it. At 2, append the loop-guard notice (reuse the H2 escalation text) and reset. Field reset at `Run` start with the other per-task state (`engine.go:519-524`).
- **Required test:** scripted provider alternating write_file-fail / run_shell-fail turns → notice appears after turn 2.

## B8 — [LOW] H1 validator: numeric/boolean enums always reject valid values + zero H1 tests

- **File:** `internal/orchestrator/engine.go:636-679` (`checkPrimitiveType`).
- **Current:** for non-string values it stringifies the value (`fmt.Sprintf("%v")`) and compares against the raw enum element (`float64` for numeric enums) — `float64(1) == "1"` is never true, so `{"type":"integer","enum":[1,2,3]}` rejects everything.
- **Fix:** compare enum membership on normalized values: if both are numbers compare as float64; if both strings compare as strings; if both bools compare as bools. Only stringify when the enum element is itself a string.
- **Required tests (the plan's mandated H1 suite, still absent):** one test per failure class — unparseable JSON, missing required field, wrong primitive type, enum violation (STRING enum and NUMERIC enum), valid call untouched, unknown extra field ignored — asserting the exact model-facing error strings.

## B9 — [LOW] Token breaker ignores subagent usage

- **Files:** `internal/orchestrator/engine.go:366-374` (`taskTokens`), `:1779` (`recordIsolatedUsage`).
- **Current:** only main-loop usage counts toward `maxTaskTokens`; a runaway subagent's burn is invisible to the breaker.
- **Fix:** in `recordIsolatedUsage` (or where subagent usage returns to the parent), add the subagent's TotalTokens into `taskTokens` under the same lock. No double-count exists today (verified) — this is adding the missing count, not fixing one.
- **Required test:** extend the (currently weak) token-breaker test: a scripted run whose subagent burns past the cap → forced finalize. While there, strengthen `TestTokenBreakerFinalizesOnBudgetExceeded` so the rigged response EMITS TOOL CALLS (the current fixture has none, so it finalizes regardless and proves nothing about the breaker).

---

# PART C — REMAINING PLAN-MODE/GOAL POLISH

## C1 — G3 defensive both-blocks invariant (one small gap)

- **File:** `internal/orchestrator/engine.go:582-587`.
- **Expected (plan G3.4):** if both `planBlock` and `goalBlock` are somehow non-empty, log and drop the goal block (plan wins).
- **Fix:** wrap the two `if` statements: when both non-empty, keep plan, skip goal, `log.Printf("[mode] plan+goal both active — dropping goal block (plan wins)")`. Two lines; it's the backstop for any future exclusion bug.

---

# PART D — MCP FIXES

## D1 — [HIGH] M8 reconnect is dead: stale tools map short-circuits `EnsureLive` forever

- **File:** `internal/mcpclient/manager.go:667-678` (`markServerError`), `:599-620` (`forwardingTool.Execute`/`EnsureLive`), compare `:399-401` (refresh's correct cleanup).
- **Expected:** transport error → status `error`, connection dropped, NEXT call reconnects once via the lazy path; second consecutive failure stays `error`.
- **Current:** `markServerError` removes the connection but NOT the server's entries in `m.tools` — so the next `forwardingTool.Execute` finds the stale live tool at `manager.go:599-602` and calls it on the CLOSED session → transport error → repeat forever. `EnsureLive` is never reached. The model gets raw transport errors on every call.
- **Root cause:** asymmetry with `refresh(onlyName)`, which correctly does `delete(m.tools, tool.exposedName)` for each of the dropped connection's tools.
- **Fix:** in `markServerError`, before dropping the connection, iterate `connection.tools` and `delete(m.tools, tool.exposedName)` (same loop as `manager.go:399-401`), under the already-held lock. Then the next Execute falls through to `EnsureLive` → bounded reconnect (15s `lazyStartTimeout`) → forward. Add the one-reconnect-per-task bound the plan asked for: a per-server `reconnectAttempted` flag cleared on successful connect; if set and the reconnect fails again, return the H8 unavailable message without another attempt.
- **Required test (the plan's, never written):** fake server killed between calls → next call flips `error`, reconnects once, succeeds; kill again + fail reconnect → stays `error`, returns unavailable message, no third attempt.
- **Cache note:** this must keep respecting the boundary rule — the registered forwarding tools array never changes mid-task (only `m.tools` liveness bookkeeping does). The current code respects it; keep it that way.

## D2 — [HIGH] M5.3 missing: `/mcp` modal never connects lazy servers and its tick never terminates

- **Files:** `internal/tui/mcp.go:54-55` (openMCP), `internal/command/application.go:315` (MCP.List), `mcp.go:117-123` (`hasMCPPendingStates`), `internal/tui/model.go:248-252`.
- **Expected (plan M5.3):** opening the modal triggers a full refresh — the user is looking at the list; connecting is expected there.
- **Current:** with M5 lazy-connect, cached-surface servers have NO status entry → `mcpServerInfos` reports "not connected" → `hasMCPPendingStates` counts that as pending → the 500ms tick re-lists FOREVER while the modal is open, and nothing ever connects (List is a pure status read). The modal is functionally broken for exactly the lazy servers M5 created.
- **Fix instructions:**
  1. In the openMCP action, before the first List, fire a non-blocking full `Refresh` (the normal 900ms-budget one) in the background — the M2 tick will then observe servers move `connecting → connected/error` and terminate naturally.
  2. Treat `"not connected"` as TERMINAL in `hasMCPPendingStates` (it only becomes non-terminal after the refresh flips it to `connecting`) — this alone stops the infinite tick even if the refresh fails to start.
- **Required test:** modal opened with a lazy (no-status) server → refresh triggered, tick terminates after states settle; assert no tick messages scheduled after `mcpModalAtRest` flips false.

## D3 — [MEDIUM] M1's missing 30-second bound on blocking refreshes

- **Files:** `internal/mcpclient/manager.go:333-335,366-369`, `internal/command/application.go:371-372,383-384`.
- **Current:** `RefreshBlocking` waits on the session-lived ctx — a wedged server stalls the Authorize/Test action indefinitely.
- **Fix:** in the Authorize and Test call sites, wrap: `ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second); defer cancel()` and pass that to `RefreshBlocking`. On timeout, surface "server did not respond within 30s" and leave status as whatever the connect goroutine eventually sets (it holds its own refresh ctx — verify it isn't the same wrapped ctx, else the connect itself dies at 30s; if it is, give the connect goroutine the session ctx and only the WAIT the 30s one).
- **Required test:** server stalled 40s → Authorize returns at ~30s with the timeout message; the background connect may still complete and set `connected` afterward (tick from D2 will show it).

## D4 — [LOW] M2 tick scheduling burst

- **File:** `internal/tui/model.go:248-252, 302`.
- **Current:** `mcpRefreshIn` is set only when the tick FIRES, so the 100ms base tick schedules ~5 overlapping 500ms one-shots in the first window — bunched List calls.
- **Fix:** set `mcpRefreshIn = true` at SCHEDULE time (inside the `tickMsg` branch when it emits `mcpRefreshTick()`), clear it when the refresh tick completes its List. One flag move.

---

# PART E — TUI COMPLETIONS

## E1 — T2 gaps: `reasoningFull` buffer, verbose expansion, blank-line separation

- **Files:** `internal/tui/model.go:588-592`, `internal/tui/view.go:159-173`.
- **Current:** rune-safe live truncation ✅, `▎` left rule + RenderRTL + `thought for Ns` ✅. But: (a) `reasoningFull` does not exist — nothing accumulates the full reasoning; (b) the Ctrl+O verbose branch is a literal no-op — both branches call `oneLine(text, width)` identically (`view.go:161-165`); (c) the thinking block joins with bare `"\n"` — no blank-line separation from the status line above or input below.
- **Fix instructions:**
  1. Add `reasoningFull strings.Builder` (or string) to the Model; append every reasoning token in `appendStream`; cap at 64 KiB by dropping the HEAD (keep the tail) — rune-safe.
  2. Verbose branch: when `m.verbose`, render the last ~12 wrapped lines of `reasoningFull` (wrapped at `contentWidth()`, each line RTL-shaped); else the current one-liner.
  3. Insert a blank line above the thinking block and below it in the parts assembly.
  4. Clear `reasoningFull` where `reasoning` is cleared (submit path — and note the plan wants the `thought for Ns` summary line to PERSIST after resultMsg; keep that behavior, clear only on next submit).
- **Required tests:** verbose ON renders >1 line from a multi-line reasoning fixture; 64 KiB cap keeps the tail; Arabic/emoji fixture stays valid UTF-8 through both the live 300-rune cap and the full-buffer cap.

## E2 — T3: actually unify the width formula

- **File:** `internal/tui/view.go:126-128 (helper), :197 (input), :417 (transcript)`.
- **Current:** `contentWidth()` exists but only the thinking block calls it; input still hardcodes `m.width-2`, transcript still inlines the viewport formula — the plan's goal (aligned edges via ONE formula) is unmet.
- **Fix:** make `contentWidth()` the single source (decide the canonical formula — the transcript's viewport-based value per the plan — and have input + thinking + footer derive from it; if the input box must remain `m.width-2` for border reasons, document WHY in a comment and align the others to it instead; the point is one deliberate decision, not three accidental ones).

## E3 — Strengthen the weak/superficial tests the plan mandated

1. **T1 footer** (`tui_test.go` `TestFootprintFooterSurvivesNoticesAndClearsOnSubmit`): add — advance 70+ `tickMsg`s (>6s simulated) → footer still present; a `warn()` (not just notify) → still present; session-switch action → cleared.
2. **M2 modal** (`TestMCPModalRefreshesUntilTerminal`): drive through the REAL scheduler (`tickMsg` → `mcpRefreshTick`) instead of injecting `mcpRefreshTickMsg` directly, with a fake List that does NOT self-advance (pair it with D2's refresh trigger); delete the dead `for _, choices := range []contract.QuestionChoice(nil)` loop.
3. **H5 token breaker**: covered in B9's test note.
4. **Cosmetic:** remove the stale `"ready"` in the doc comment `mcp.go:20` and the old fixture reference `tui_test.go:147`.

---

# PART F — EXECUTION ORDER & FINAL VERIFICATION

Execute: **A1 → A2 → B1 → B2 → D1 → D2** (these six are the shipping blockers: session hard-fail, request hard-fail, data race, protocol corruption, dead reconnect, broken modal) → rest of Part B → Part C → rest of Part D → Part E → Part A3/A4 (A4 last: it's additive feature work and must land on a correct base).

Final gate, all mandatory:
1. `go build ./... && go vet ./... && go test ./...` clean.
2. `CGO_ENABLED=1 go test -race ./internal/orchestrator/... ./internal/mcpclient/...` clean (B1's test must exist first).
3. Cachebench before/after vs the live gateway: prefix-stability ≥ 99.3% band held; raw steady-state not regressed. Confirm `X-Muhiya-Session` on every logged request.
4. Manual scenarios (from OVERHAUL_PLAN.md Phase 7 V3): plan proceed-now/later + restart; goal complete on final turn + marker-free display; MCP authorize → connected without reopening; kill server → one reconnect; usage footer survives 6+ minutes; cancel mid-batch → no 400 on next turn.
5. Report results honestly per scenario — "tests pass" without the scenario walkthroughs is not done. Do not edit any file under `specs/001-prompt-cache-optimization/benchmarks/` except to APPEND new measured results.
