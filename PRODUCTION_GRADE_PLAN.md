# PRODUCTION_GRADE_PLAN.md — Unlimited, Unbreakable, Cache-Perfect (v1.1.0 final hardening)

**Repos**: this plan spans TWO repos.
- **Agent** (this repo, `F:\MuhiyaCode Agent Go`) — phases G0–G2, G4.
- **Gateway** (`F:\MuhiyaWorkspace\MuhiyaWorkspace`) — phase G3; separate commits there, owner deploys.

**Hosting reality (owner, 2026-07-20)**: the gateway is hosted on **elest.io** and the
model catalog lives in the **live PostgreSQL DB** — rows are managed directly in prod,
not via repo seed files. Therefore NO seed/migration work: the repo's missing
`deepseek-v4-pro` seed is a non-issue for prod, and catalog corrections are a short
SQL checklist against the live DB (§G3.1). Focus is FIXES only.

**Executor**: Opus 4.8. **Trigger**: the owner says "execute".
**State**: branch `feature/native-agent-v1.1.0` at `4d7e190`, suite green, binary swapped.

Grounding: a two-explorer sweep of the gateway (wire path + business logic, file:line
evidence throughout) plus a direct audit of every remaining limit, handoff surface,
and error path in the agent. Every finding below carries its evidence location.

---

## §0 Mission and invariants

The owner's directive: remove any limits, make the agent production grade and free
from errors, make the connection between agents/subagents/models ultimate, make
prefix caching perfect, and take what is needed from the gateway.

Reading of "remove any limits" (consistent with the budget removal already shipped):
**no limit may ever stop legitimate work in progress**. Liveness terminators that stop
*pathology* (failure loops, repeats, runaways) stay — but every remaining bound must be
either progress-aware (extends while real work advances) or a genuine physical bound
(the model's context window, the provider's stream timeout) handled gracefully.

**Invariants that must survive every phase (do not renegotiate):**
1. The crown-jewel cache discipline (feature 012): byte-stable prefix, per-kind pins,
   verbatim replay, PrefixShape guard, invalidation ledger. Any prefix-byte change
   lands in ONE cache-epoch commit (§G4.1).
2. Subagent launching is the MODEL's decision; ONE subagent at a time, strictly serial.
3. The plan/execute role split: main plans, executor executes, tasks.md is the one
   file the main model writes.
4. Raw transport errors never reach the user (`gateway.FriendlyRequestError`).
5. Session-frozen models (M3 main / V4 Pro exec / V4 Flash utility).

---

## §1 Findings inventory (root-cause map)

### A. Agent repo

| # | Finding | Evidence | Verdict | Phase |
|---|---------|----------|---------|-------|
| A1 | Subagent turn caps (12/14/24 × AgentTurnScale) are the last **capacity** limit: a big executor run is forced to wrap up mid-build regardless of progress | subagent.go:129-131, :315 | remove (progress ladder) | G0.1 |
| A2 | No context-window management inside a subagent run: a long run dies on the provider's context-length 400 instead of wrapping gracefully | subagent.go (no compact/pressure path) | fix | G0.2 |
| A3 | `TokenCeiling` field is dead — never set in production, only in its own test | subagent.go:37; only subagent_ceiling_test.go:34 | delete | G0.3 |
| A4 | CLI `RequestLifetime` 10min / `IdleTimeout` 90s are the binding wire limits (tighter than the gateway's 15min/120s) — a long reasoning turn dies CLI-side first | provider.go:52-56 | raise, pair with gateway | G0.4 |
| A5 | `hardTurnCeiling` 120 is flat; at max effort the ladder can legitimately reach it | engine.go:16 | effort-scale it | G0.5 |
| A6 | **Report truncation cuts the tail**: `TruncateEllipsis(report, 2200)` deletes the END of long reports — exactly where `STATUS:` and the verification section live. Parent reads "no verification shown" → re-dispatches. The trust protocol dies for every long report | subagent.go:204; format.go:60-69 (tail-cut) | fix (status-preserving) | G1.1 |
| A7 | The handoff carries the task TWICE: `Scope` (1200-char truncation) + full `TASK:` block — every dispatch pays double | subagent.go:99 vs :224 | de-duplicate | G1.2 |
| A8 | Steering never reaches a RUNNING subagent: user input queues until the main loop resumes — on a long executor run that is minutes of silence | drainSteering exists only in turnloop.go:238 | pass through | G1.3 |
| A9 | Executor cannot ask the user anything (`ask_user` is main-loop synthetic); BLOCKED exists but no question-relay convention | definitions.go:17 vs subagentSpecs | protocol line | G1.4 |
| A10 | Role gate treats ALL `mcp__` tools as mutations — main model is refused even read-only MCP (doc search, log reader) and must dispatch an executor for a read | rolesplit.go `workspaceMutation` | honor readOnlyHint | G1.5 |
| A11 | **Mid-stream death is never retried**: `if streamed \|\| !retryable` guarantees a died stream surfaces as an error; turnloop then ABORTS the whole task (`return "", stats, err`) | provider.go:92; turnloop.go:394-400 | retry cache-warm | G2.1-2 |
| A12 | 429 handling cannot distinguish "slow down 60s" from "budget exhausted — stop" (gateway sends one shape for both; see B6) | friendly.go; gateway limiter.go:239-254 | disambiguate via /v1/usage | G2.3 |
| A13 | staticcheck: 6 × ST1005 + 80 × U1000. Real dead code among them: `(*Application).setModel` (a `/model` removal leftover), `combineBenchmarkStats`; the `*Text` var storm in instructions needs a per-var verdict (side-effecting registration vs genuinely dead) | staticcheck run 2026-07-20 | zero it | G2.4 |
| A14 | `testWriterOrStdout` returns stderr — the name lies | maintenance.go:249-254 | rename | G2.5 |
| A15 | No session-start check that the three configured models exist in the live catalog — a missing gateway row (see B1!) becomes a silent 404 loop mid-task | actions.go catalog fetch exists; no reconciliation | preflight | G2.6 |

### B. Gateway repo (explorer-verified, file:line)

| # | Finding | Evidence | Verdict | Phase |
|---|---------|----------|---------|-------|
| B1 | `deepseek-v4-pro` exists only in the live prod DB (repo has no row). Prod works; the CLI must still tolerate catalog drift (rename/deactivate/hidden) gracefully | seeds + migrations swept; db.go:917 | prod checklist + CLI preflight | G3.1 + G2.6 |
| B2 | **Visibility trap for hand-added rows**: migration 021 defaults NEW models to `muhiyacode_visible=false`. A row added to prod after 021 (like v4-pro) is hidden from `/v1/models` for the CLI — inference by name still works, but the CLI's catalog lookup misses → wrong ContextLimit fallback (128k instead of 1M) and G2.6 would flag it "missing" | 021_model_muhiyacode_visible.sql:20; handler.go:1641-1670; actions.go:185 | prod checklist (one UPDATE) | G3.1 |
| B3 | Wire path is **cache-safe and tested**: deterministic re-marshal (sorted keys), prefix byte-stability asserted (`TestDeepSeekTransformDeterministic`, `TestDeepSeekTransformPreservesPrefix`), usage passthrough VERBATIM streaming + non-streaming, muhiya_log chunk pre-`[DONE]` idempotent | handler.go:1159,1204; cache_regression_test.go:48,60 | none — verified | — |
| B4 | Session affinity: `X-Muhiya-Session` → in-process 24h router pin + forwarded as `X-Session-Id` to OpenRouter ONLY; the standard OpenRouter sticky field `user` is STRIPPED (handler.go:987) and the derived identity goes under non-standard `user_id`. Unverified that OpenRouter honors `X-Session-Id`. (Low urgency: M3/V4 are DIRECT providers, not OpenRouter) | stickysession.go:19-58; handler.go:998-1007 | verify + also set `user` | G3.3 |
| B5 | Budget-exhausted, suspended, AND rpm/tpm throttle are ALL `429 rate_limit_error` — a client retry loop hammers a permanent condition; agent path even omits `Retry-After` | limiter.go:226-254; handler.go:661-666; agent.go:81-84 | distinct semantics | G3.2 |
| B6 | `httpClient.Timeout` 15min is a hard mid-stream kill for the longest turns; `streamIdleTimeout` 120s cuts silent reasoning; 60s shutdown drain cuts streams on deploy | handler.go:22,49; main.go:350 | raise/configure | G3.4 |
| B7 | `GET /v1/usage` **landed** (auth'd, quota-exempt: windows, credits, spend) — the disambiguation surface A12 needs | main.go:481; handler.go:450-494 | consume CLI-side | G2.3 |
| B8 | CORS allow-list lacks `X-Muhiya-Session`/`X-Client-App` (future browser clients lose stickiness + visibility gating; CLI unaffected) | main.go:653 | one-line fix | G3.4 |

---

## §2 Phase G0 — Unlimited, gracefully (agent repo)

### G0.1 Subagent progress ladder (mirrors the main-loop ladder shipped in 4d7e190)
In `executeSubagent`'s turn loop (subagent.go:315-455):
- Replace the flat `maxTurns` bound with a ladder: when `turn >= currentCap` AND the
  run made progress in the last window (any successful mutation via `capture`, or ≥3
  distinct successful reads for research kinds), extend `currentCap += spec.MaxTurns/2`
  and append one rider: `"[governor] Run extended: real work is advancing. Keep going;
  wrap up only when the task is done or nothing new is being learned."`
- No progress in the window → the existing wrap-up fires unchanged (INV-3 intact).
- Absolute bound: the window-pressure guard (G0.2) — a physical limit, not a quota.
- `wrapUpTurns=2` stays. The forced wrap-up report text stays STATUS-carrying
  (tailcoherence_test.go already pins this).
- Tests: extension-on-progress; no-extension-when-idle; fault row: a permanently
  no-op executor still terminates (ladder never extends on zero progress).

### G0.2 Subagent window pressure (the graceful physical bound)
- Track `lastPromptTokens` (already recorded, subagent.go:312) against the model's
  window: `e.modelContextLimit(modelID)` (contextlink.go:339) falling back to
  `ResolveModelProfile(modelID).DefaultContextWindow`.
- At `promptTokens >= 0.85 × window`: trigger the SAME graceful wrap-up as the old
  ceiling (reuse the `ceilingHit` machinery renamed `windowPressure`), labeling the
  result `partial (context window)`. The parent's continuation dispatch (session
  chain) resumes the work in a fresh window with the digest seed — already built.
- This REPLACES A3's dead TokenCeiling as the only token-shaped bound, and it is
  physical, not budgetary.
- Tests: wrap-up at threshold with STATUS present; continuation eligibility after a
  window-pressure partial (linkDigestSeeded path).

### G0.3 Delete `TokenCeiling`
Field (subagent.go:32-37), its enforcement branch (:444-448), the `"partial (token
ceiling)"` label (:456-460), and rewrite `subagent_ceiling_test.go` into the G0.2
window-pressure test. `terminalShapeFor` keeps a ceiling shape only if the window
pressure reuses it (rename, one recorded epoch note in the contract doc).

### G0.4 Wire lifetimes (pair with gateway's bounds; see G3.5)
provider.go defaults: `RequestLifetime` 10min → **14min** (inside the gateway's 15min
total), `IdleTimeout` 90s → **110s** (inside the gateway's 120s idle watchdog, so the
side with the friendlier recovery — the CLI, after G2.1 — times out first).
Both already config-struct fields; only the defaults move. Document the pairing in a
comment referencing gateway handler.go:22,49.

### G0.5 Effort-scaled hard ceiling
`hardTurnCeiling` stays the absolute liveness backstop but scales:
`ceil(120 × AgentTurnScale)` (max effort ⇒ 168). Implement as a method, not a global,
so the fault-injection tests keep a deterministic bound (they run at medium ⇒ 120).

**Gate**: build+vet+gofmt+full tests; fault-injection suite green (ladder rows added).

---

## §3 Phase G1 — Perfect handoffs (agent repo)

### G1.1 STATUS-preserving truncation (kills the biggest silent waste)
New `contract.TruncateMiddle(value string, maxChars int) string`: keeps the FIRST
~60% and the LAST ~40% of the budget with `\n…[trimmed N chars]…\n` between. Apply to:
- `parentReport` (subagent.go:204) — the parent now ALWAYS sees the report's tail:
  the verification section and the STATUS line survive any length.
- `digestCarryForward` / CarryForward composition (contextlink.go:280,295) — same
  reasoning: the tail of a predecessor digest carries its verification state.
The 2200 bound itself stays (context hygiene; the full report is banked in knowledge
and the digest note already says so).
Tests: a 10k-char report with STATUS last → parent text contains `STATUS:`;
fieldtest E2E variant: long-report COMPLETE is NOT re-verified (extends
`TestFieldTestCompleteReportIsNotReVerified` with an oversized report).

### G1.2 De-duplicate the task in the handoff
`handoffFor` (subagent.go:99): `Scope` becomes `contract.Digest(input.Task, 160)`
(one line), because `subagentUserMessage` already appends the FULL task under
`TASK:`. Tail-only bytes — no prefix epoch. Update handoff-shape tests.

### G1.3 Steering pass-through into a running subagent
- Engine gains `takeSteering() []string` (drains the same queue turnloop's
  `drainSteering` reads, engine-internal, mutex-guarded).
- In `executeSubagent`'s loop head: drain queued steering; if any, append to
  `messages` as ONE user message:
  `"[user interjection] <text> — fold this into the current task; it is direction,
  not a new task."`, and mirror a notice into the PARENT history (so the main model
  knows on resume): `"[steering] forwarded to the running agent: <digest>"`.
- Cache note: steering rides the subagent TAIL (user message) — prefix-safe; the
  parent-history notice is tail-safe too.
- The turn-loop's own `drainSteering` semantics are unchanged for main-scope turns.
- Tests: steering delivered mid-run lands in the subagent transcript exactly once
  and the parent history carries the notice; serial invariant untouched.

### G1.4 The question-relay protocol (BLOCKED: QUESTION)
Instruction texts only (registered, audited):
- `ReportFormatImplementation` grammar gains:
  `STATUS: BLOCKED: QUESTION: <the single decision you need>` — used when the ONLY
  blocker is a decision the user must make.
- Main-side rule 5 (operating contract) gains one clause: on `BLOCKED: QUESTION`,
  ask the user with `ask_user`, then re-dispatch with the answer folded into the
  task. (This is a MAIN-PREFIX byte change → rides the single G4.1 epoch.)
- Executor system text (`SubagentGeneralSystem`) mentions the form once.
  (SUBAGENT-PREFIX byte change → same epoch; the session-long chain re-seeds once.)
Tests: parse `BLOCKED: QUESTION:` as ReportStatusBlocked (grammar unchanged for
machines) + instructions audit passes + tailcoherence still green.

### G1.5 Read-only MCP tools for the main model
- mcpclient: surface each tool's `readOnlyHint` annotation (already in the MCP tools
  list response) on the forwarding tool; `Registry` records a `readOnly` name set.
- `workspaceMutation` (rolesplit.go): `mcp__` returns true UNLESS the registry marks
  that tool read-only. Unknown/absent annotation ⇒ mutation (fail closed, as today).
- Tests: annotated read-only MCP call passes the role gate main-scope; unannotated
  is still refused; gatepolicy inventory comment updated.

**Gate**: full suite + instructions registry audit + goldens UNCHANGED in this phase
except where G1.4 defers to the G4.1 epoch commit (implement G1.4 text last, inside
G4.1).

---

## §4 Phase G2 — Unbreakable wire (agent repo)

### G2.1 Mid-stream retry (cache-warm by construction)
provider.go `Chat` loop: when `chatOnce` fails with a STREAMED transport death
(`streamed && isNetworkError/io.ErrUnexpectedEOF/idle-timeout`), retry ONCE with the
same request after 2s. Rationale in-code: the accumulated partial is discarded, the
request bytes are identical, and the just-sent prefix is provider-cached — the retry
costs cache-hit input only. Never retry a streamed HTTP-status error (a 4xx/5xx body
already landed) — that stays the current behavior.
Harness event: `provider/stream-retry` (telemetered, visible in /errors).
Tests: died-stream-then-success (one retry, one response); died-twice (error
surfaces); status-error streams never retry.

### G2.2 Turn-level recovery before task abort
turnloop.go:394-400: on `Chat` error after the client's own retries, do ONE in-loop
recovery attempt: re-send the SAME request (bytes unchanged — cache-warm) after a
3s backoff with a status line ("provider hiccup — retrying the turn"). Second
failure → today's path (friendly error, task aborts, history intact).
Subagent loop (subagent.go:350): same single recovery, result-status preserving.
Fault rows: transient-then-ok recovers invisibly; hard-down still aborts cleanly
with the friendly text (no infinite loop — bounded by ONE).

### G2.3 429 disambiguation (consumes B7)
friendly.go: on HTTP 429 from the gateway, call `FetchUsage` (5s bounded, quota-
exempt endpoint):
- a window with `current_spent_usd >= budget_usd` AND `credits.extra_remaining <= 0`
  → "Plan budget exhausted (resets HH:MM). Top up or wait; the task is paused." — NO
  auto-retry.
- otherwise → honor `Retry-After` (default 60s): status countdown, ONE auto-resume
  of the turn (composes with G2.2's machinery).
- usage fetch itself failing → generic 429 text (today's behavior).
Tests: fake gateway returning each shape; no-retry-on-budget; single-resume-on-rpm.

### G2.4 staticcheck zero
- DELETE dead: `(*Application).setModel` (runtime_build.go:236 — `/model` leftover),
  `combineBenchmarkStats` (delegationbench).
- The ~80 U1000 `*Text` vars in instructions/: for each, verdict = registered-with-
  side-effect (keep, add `//lint:ignore U1000 registered via init side effect` or
  restructure into the `_ = register(...)` form the registry prefers) vs genuinely
  orphaned (DELETE — likely several survived the consolidation).
- ST1005: fix the four non-model-facing ones (mcpclient, paths.go); the two
  registry.go MODEL-FACING messages keep their punctuation with a lint ignore and a
  comment saying the period is part of the model-visible instruction.
- Add `staticcheck.conf` + wire staticcheck into the repo's test gate doc.

### G2.5 Rename `testWriterOrStdout` → `degradedGuardSink` (it returns stderr).

### G2.6 Session-start catalog preflight
After the models list loads (actions.go), verify the three configured role models
exist in the catalog. Any missing → ONE friendly status line naming it
("execution model deepseek-v4-pro is not available on this gateway — falling back
to <best-catalog-match>") + advisor-assisted substitution through the EXISTING
`applyModelSwitch` (before first request, so RolesPinned is untouched).
This is the CLI-side guard for B1/B2-class gateway drift.
Tests: missing-executor substitutes and notices; full catalog is silent.

**Gate**: full suite; staticcheck exits 0; gauntlet forbiddenStrings green.

---

## §5 Phase G3 — Gateway fixes (repo commits + one prod-DB checklist)

> The gateway runs on **elest.io**; the model catalog is managed directly in the
> live PostgreSQL DB. NO seed/migration work. Nothing here blocks G0–G2; the
> CLI-side preflight (G2.6) shields against catalog drift regardless.

### G3.1 Prod-DB checklist (B1/B2 — SQL run once against the live Postgres, owner)
Verification + correction only; no schema changes:
```sql
-- 1. What the CLI can actually see and what it believes about windows:
SELECT id, name, target_model, status, muhiyacode_visible,
       context_window, max_output_tokens
FROM models WHERE status = 'active' ORDER BY name;

-- 2. Rows added by hand AFTER migration 021 default to hidden — expose the trio:
UPDATE models SET muhiyacode_visible = true
WHERE name IN ('minimax-m3', 'deepseek-v4-pro', 'deepseek-v4-flash');

-- 3. DeepSeek rows must report the migration-007 windows, or the CLI computes
--    context pressure against 64k while the model has 1M:
UPDATE models SET context_window = 1000000, max_output_tokens = 384000
WHERE name LIKE 'deepseek%' AND context_window < 1000000;
```
Why it matters CLI-side: a hidden row vanishes from `/v1/models`, so the CLI's
catalog lookup misses → ContextLimit falls back to the 128k profile default
(actions.go:185) → compaction fires ~8× too early → needless cache invalidations.
Visibility is therefore a CACHE fix, not cosmetics.

### G3.2 Distinct exhaustion semantics (fixes B5, pairs with G2.3)
- Budget-with-no-credits → keep 429 for wire compatibility BUT type
  `insufficient_quota` (OpenAI-compatible) and message prefix "Budget exhausted".
- Suspended user → 403 `permission_error`.
- True RPM/TPM → unchanged 429 `rate_limit_error`; ADD `Retry-After` on the agent
  path too (agent.go:81-84 parity with handler.go:661-666).
- Tests: limiter_test matrix for the three shapes.

### G3.3 OpenRouter affinity hardening (B4 — low urgency, M3/V4 are direct)
- `setOpenRouterHeaders`: ALSO set body `user` = derived stable identity when
  provider==openrouter (the documented sticky key), keeping `X-Session-Id`.
- Doc note in GATEWAY_AUDIT: X-Session-Id honoring is unverified; the `user` field
  is the documented mechanism. LIVE VERIFY = cost-gated, owner approval required.

### G3.4 Timeout headroom + CORS (B6/B8)
- `httpClient.Timeout` 15min → **30min**, env-overridable
  (`UPSTREAM_TOTAL_TIMEOUT`); `streamIdleTimeout` stays 120s (idle IS pathology;
  the CLI recovers via G2.1).
- Shutdown drain 60s → 120s (elest.io redeploys stop cutting mid-turn streams).
- CORS allowed-headers += `X-Muhiya-Session`, `X-Client-App`.

**Gate (gateway)**: `go test ./...` there; G3.1 checklist output pasted back for
the owner's records (SELECT before/after).

---

## §6 Phase G4 — The single cache epoch + full validation (agent repo)

### G4.1 One epoch commit
All prefix-byte changes from this plan land in ONE commit: G1.4's two text
additions (main operating-contract clause + executor system mention). Mechanics
(the recorded procedure): regenerate `instructions_dump.golden` +
`prefix_bytes.golden` (`-update`), `prefix_bytes_wire.golden`
(`-update-prefix-golden`), re-baseline `promptBaselineChars`, update
`specs/010-ultimate-consolidation/wiring-inventory-data.md`, note the epoch in the
contract doc. Everything else in this plan is tail/harness-only and must show
GOLDENS UNCHANGED in its own commit.

### G4.2 Validation matrix
- Full suite ×14 packages + gauntlet + fault injection (new rows: subagent ladder
  idle-terminates; stream-retry recovers; window-pressure wraps with STATUS;
  steering forwarded once).
- tailcoherence additions: the new governor rider (G0.1) and interjection prefix
  (G1.3) join the scanned literal set; forbidden vocabulary unchanged.
- Vacuity check (the lesson from this session, twice): every NEW guard test gets a
  one-commit sabotage verification — flip the guarded behavior locally, watch the
  test fail, revert. Record "sabotage-verified" in each test's comment.
- staticcheck exit 0. Build via scripts/build.ps1; swap vendored npm exe; doctor.

### G4.3 Memory + docs
Update `muhiyacode-native-agent-plan.md` (execution record), `docs/agent-design.md`
(ladders, window pressure, question relay, stream retry), CHANGELOG.

---

## §7 Sequencing, risks, balance

**Order**: G0 → G1 → G2 → G4 (agent repo, one PR-sized commit per phase);
G3 anytime in parallel (independent repo; G2.6 shields the CLI until deployed).

| Risk | Mitigation |
|------|------------|
| Ladder + retry loops compound into a runaway | Ladder extends ONLY on fresh progress; retries are bounded at ONE per layer; fault rows prove termination |
| G1.1 middle-cut confuses digest parsing somewhere | `TruncateMiddle` used ONLY at the two report/digest sites; grep-verified no other consumer parses offsets |
| Epoch regressions | Single epoch commit, goldens regenerated together, budget ratchet re-baselined once |
| Gateway/CLI timeout inversion (CLI outlives gateway) | Explicit pairing: CLI 14min/110s INSIDE gateway 15min(→30min)/120s; comment cross-references |
| Prod-DB checklist applied against wrong rows | G3.1 is SELECT-first: the before/after output is pasted back; UPDATEs are name-scoped and idempotent |
| U1000 deletions remove a reflection-consumed var | Per-var verdict against the registry's actual reader before any deletion; suite + dump golden catch a vanished text |

**Balance ledger (what deliberately does NOT change):**
- Serial one-subagent-at-a-time; model-driven dispatch; the role split.
- Liveness terminators (failure streak, repeat limiter, all-failed detector).
- The 2200 parent-report bound (becomes status-preserving, not bigger).
- `streamIdleTimeout` 120s gateway-side (silence IS failure; recovery is the CLI's job).
- Free-plan pricing/budgets (business decision — the CLI gets honest messaging, not bypasses).
- MiniMax M3 operational window already equals its documented 1M; DeepSeek windows
  come from the live catalog (fixed at the SOURCE by the G3.1 prod-DB checklist,
  not padded CLI-side).
