# CACHE AFFINITY OVERHAUL PLAN

**For:** Opus 4.8, fresh session, this repo (`F:\MuhiyaCode Agent Go`) + the gateway repo (`F:\MuhiyaWorkspace\MuhiyaWorkspace`).
**Authored:** 2026-07-21, from a live-log forensic audit of session `12:42` (six requests, gateway dashboard) plus a read-only audit of both repos and OpenRouter's provider-routing docs.
**Goal:** the highest prompt-cache hit rate achievable on OpenRouter + MiniMax M3, honest measurement of what that ceiling is, and zero new ways for the agent to fail.

---

## Part 0 — The audit. Read this before executing anything.

### 0.1 The evidence, decoded

The user's gateway log for one task (timestamps 12:42:13 → 12:42:46, same session, same virtual key, nothing changed between requests):

| # | Time | Model | In / Out | Cache Read | Latency | Verdict |
|---|------|-------|----------|-----------|---------|---------|
| 0 | 12:42:13 | deepseek-v4-flash | 484 / 80 | 384 | 1.4s | task advisor, own stream — fine |
| 1 | 12:42:15 | minimax-m3 | 41,104 / 1,062 | **128** | **12.6s** | COLD — full prefill |
| 2 | 12:42:28 | minimax-m3 | 42,899 / 300 | 41,103 | 3.3s | HIT — read = request 1's input |
| 3 | 12:42:32 | minimax-m3 | 43,507 / 282 | 42,898 | 6.8s | HIT — read = request 2's input |
| 4 | 12:42:39 | minimax-m3 | 43,866 / 626 | 43,506 | 6.0s | HIT — read = request 3's input |
| 5 | 12:42:46 | minimax-m3 | 45,039 / 772 | **114** | **11.7s** | COLD — full prefill, **7 seconds after a hit** |

Three facts jump out:

1. **The hits are perfect.** Each hit's cache read equals the *previous request's entire input* (41,103 ≈ 41,104; 42,898 ≈ 42,899; 43,506 ≈ 43,507). That is a textbook prefix-cache "zipper": every request extends the last one byte-for-byte and the cache reads all of it. **The client's byte discipline is not the problem. When the request lands on the right machine, the hit rate is ~99.9%.**
2. **The misses are placement, not bytes.** Request 5 missed *seven seconds* after request 4 hit, on input that is request 4's input plus a tail. No TTL expires in 7 seconds; no client byte changed (the zipper proves it). The request landed on a machine that had never seen this conversation.
3. **Latency corroborates independently.** Misses took 11.7–12.6s (full 41–45k prefill); hits took 3.3–6.8s. The billing numbers and the wall clock tell the same story.

Cost of placement failure, from this log: a cold 45k request ≈ $0.0144 vs ≈ $0.0031 warm — **4.6× per placement miss**. This session: 2 placement-class misses in 5 main requests → effective read ratio 59% instead of ~97%.

### 0.2 Root cause, confirmed against OpenRouter's live catalog

`minimax/minimax-m3` on OpenRouter is served by **nine upstream providers** (live endpoint listing, 2026-07-21): Novita (1M ctx), Venice (524k), GMICloud (1M), Minimax (524k), AtlasCloud (524k), Together (524k), Parasail (1M), DeepInfra (524k), Morph (256k, 2× price). Each keeps its **own** prompt cache. Without a routing preference OpenRouter load-balances among them per request. Two consecutive requests on different upstreams = a full re-read of the conversation at input price, with byte-identical requests and **no trace anywhere on our side**.

The user's dashboard R:114 / R:128 on cold requests: tiny reads on a machine that never saw the session — sub-block or cross-request boilerplate matches. Do not chase these values; they are noise on top of a placement miss (open question logged in T5.4).

### 0.3 What was already shipped on 2026-07-21 (this session — verify, don't redo)

| Piece | Where | State |
|---|---|---|
| Capture OpenRouter's per-chunk `provider` field | `internal/gateway/sse.go` (`StreamResult.Upstream`) | shipped + tested |
| Upstream on every usage record | `internal/contract/cache.go` (`UsageRecord.Upstream`), `internal/orchestrator/usage.go` | shipped + tested |
| Flip notice ("Upstream changed: X → Y…") + harness event | `internal/orchestrator/cacheresilience.go` (`emitUpstreamNoticeIfPending`), `turnloop.go` | shipped + tested |
| Flip retires the warm-model ledger | `internal/orchestrator/usage.go` (`noteUpstreamFlip` → `warmPrefix = nil`) | shipped + tested |
| Pin: `provider: {order: [...], allow_fallbacks: true}` on requests 2+ | `internal/gateway/provider.go` (chatOnce), `internal/gateway/upstreampin.go`, `internal/orchestrator/turnloop.go` (`PinUpstream: e.upstreamPin()`) | shipped + tested |
| Slug normalization (display name → slug candidates) | `internal/gateway/upstreampin.go` (`upstreamOrderCandidates`) | shipped + tested |
| Rejection latch: a 4xx on a pinned request retries unpinned, latches off | `internal/gateway/provider.go` Chat retry loop | shipped + tested |
| Docs | `docs/prompt-caching.md` § "Upstream affinity (OpenRouter)" | shipped |

Key wire facts (verified against OpenRouter docs 2026-07-21, do not re-litigate):
- `provider.order` takes **slugs** (`"novita"`, `"deepinfra"`, `"atlas-cloud"`); examples in docs are lowercase; an unmatched entry is **silently skipped** (that silence is why `upstreamOrderCandidates` sends every plausible spelling).
- `allow_fallbacks` defaults true; with `order` it means "try these first, then anyone" — the reliability-preserving form. Never send `provider.only`.
- The response's `provider` field format is **undocumented** — hence candidates, not a single guess.
- The gateway (`proxy/handler.go:1047-1114`) passes unknown body fields through verbatim (deliberate: `handler.go:778-785`) and forwards SSE lines unmodified (`handler.go:1209`) — so both the outbound pin and the inbound `provider` chunks flow with **zero gateway changes**.

### 0.4 Session / context-memory architecture — audited, verdict: append-only discipline is intact

The full request each turn = `[system prompt] + [history window] + [tool JSON]`, assembled in `History.BuildRequestWithMetadata` (`internal/orchestrator/history.go:177`) and `turnloop.go:371`. Everything that can mutate transmitted bytes, with when it fires:

| Mutation | Trigger | Prefix effect | Guarded by |
|---|---|---|---|
| Append (user, assistant, tool, steering, review nudge, task brief) | every turn | none — append-only | — |
| `FoldCompletedTasks` | task boundary only | rewrites earlier turns | `rewriteVersion++`, invalidation event |
| `TrimAged` / `Maintain` | maintenance boundary, pressure ≥ 60% | rewrites tool results | min-yield floor (5%), invalidation event, anti-thrash latch |
| `CompactTo` | user `/compact` or force band ≥ 90% | rewrites everything | invalidation event, warm-ledger retirement |
| Window drop | `BuildRequest` over limit | drops oldest units | invalidation event ("request window dropped…") |
| Truncated-call marker | output-cap hit | rewrites ONE call's args | recorded; TB03 |
| Model/upstream switch | task boundary / provider flip | cold start | advisor gates; flip notice; `warmPrefix` retirement |

Per-turn guard: `NewWirePrefixShape` hashes system/tools/settled-history on the **normalized wire bytes**; any settled-byte change is recorded as `ChangeReasons` on the usage record with `Attribution: agent`. **The 12:42 session's misses cannot have been agent-side without appearing in that ledger** — and the zipper reads on neighbors prove the bytes chained correctly. At 45k/1M tokens (4.5% pressure) none of the rewrite triggers can fire. The session/context system is not the cause; placement is.

Project memory (MUHIYA.md / MEMORY.md probes, one-shot tail updates), checklist writes, and steering are all appends — cache-neutral by design and confirmed so in `prompt_stability_test.go` / `meta_test.go` / the wire golden.

### 0.5 Why it will never be "exactly like Claude Code" — say this honestly

Claude Code hits ~constantly because Anthropic caching is **explicit and contractual**: `cache_control` breakpoints, provider-guaranteed 5min/1h TTL, one provider, no router, cache writes billed to make the guarantee real. MuhiyaCode on OpenRouter+MiniMax has **implicit, best-effort** caching behind a third-party router choosing among nine independent caches. The client-side ceiling (byte-identical prefixes) is already achieved — §0.1 proves 99.9% on hits. What remains is *placement*, which only routing control improves. The realistic targets:

- **Pinned via OpenRouter:** placement-hit ≥ 90% of eligible requests (the pin is a preference; OpenRouter may still divert on load).
- **Direct MiniMax (no router):** likely ≥ 95%+, single provider infra — but unknown replica-level affinity; must be measured, not assumed (Phase 3).
- **Structurally unfixable remainder:** first request of any task after cache TTL, and genuine upstream outages. These are honest cold starts and the C6 notice already names them.

---

## Part 1 — Execution rules (identical discipline to prior plans)

1. **One cache epoch.** Any prefix-byte change (there should be at most the /context card text, which is NOT prefix) batches into one commit with goldens regenerated once: `go test ./internal/instructions/ -run TestDump -update` and `go test ./internal/orchestrator/ -run TestWiring_PrefixBytesGolden -update-prefix-golden`. Tasks below are designed to touch **zero prefix bytes**; if you find yourself editing `internal/instructions/*`, stop and re-read the task.
2. **Never use `Set-Content`/`Out-File` on source files** — em-dash mojibake. Edit/Write tools only.
3. Gate every phase: `gofmt -l internal cmd` clean, `go vet ./...` clean, `staticcheck ./...` exit 0 (config at repo root ignores ST1005), `go test ./... -count=1` all ok. The arch test caps files at 800 lines — split, never allowlist.
4. Gateway repo work (Phase 4) is Go + SQL migrations; its own build/test conventions apply (`go build ./...`, migrations numbered sequentially under `db/migrations/`).
5. Constitution VI: no fabricated numbers. Anything unmeasured renders "unavailable".

---

## Part 2 — Phase 1: prove the pin live (no code, ~10 minutes, DO THIS FIRST)

The pin + candidates shipped today have **never run against real OpenRouter**. Every later phase branches on this evidence.

- **T1.1** Have the user run one 4+ turn task (the `ttt.html` "press N for new round" prompt is ideal). Then read the session's `usage.jsonl` (under the muhiya state dir, session folder): confirm `upstream` is non-empty on main records. If empty on ALL records → the gateway is not forwarding `provider` chunks after all → jump to T4.1 (gateway capture) and re-verify; the pin cannot work without the signal.
- **T1.2** In the same ledger, check requests 2+: did any record a *different* upstream than request 1? If yes and a flip notice appeared → pin sent but not honored → check T1.3. If upstream is stable across 2+ → **pin works**; skip T1.3.
- **T1.3** (only if pin ignored) The user checks OpenRouter's own activity page (openrouter.ai/activity), which names the provider per generation, against the gateway dashboard timestamps. If OpenRouter shows the pin's target *available* but still routed elsewhere, capture one outbound body from the gateway dashboard "Details" (or add a temporary gateway log of `bodyMap["provider"]`) and verify the candidates array survived pass-through. Fix accordingly (most likely: response field format is something unanticipated — extend `upstreamOrderCandidates`, test in `upstream_pin_test.go`).
- **T1.4** Record in the response to the user: placement-hit ratio of the run (hits = records where `cache_read ≥ 50%` of input, excluding request 1).

**Acceptance:** a written yes/no: "pin observed live: sent on 2+, upstream stable, hit ratio N/M".

## Part 3 — Phase 2: agent-repo hardening (the pin survives real usage)

- **T2.1 Persist the pin across resume.** `lastUpstream` is in-memory; a restart forgets it, request 1 of the resume goes unpinned and may land anywhere, silently cold-starting a cache that was still warm (MiniMax TTLs are unpublished; short-gap resumes are common — the user restarts constantly). Persist upstream alongside the prefix shape: add `Upstream string \`json:"upstream,omitempty"\`` to `contract.PrefixShapeSnapshot` (`internal/contract/cache.go`), write it in the engine's shape persistence path (`internal/orchestrator/prefixshape.go` / where `WritePrefixShape` is called), restore into `e.lastUpstream` in `NewEngine` from `InitialPrefixShape` (`internal/orchestrator/engine.go`). The restored pin is a *preference*, so a stale name is harmless (candidates are skipped if gone). Tests: resume restores pin → first request carries it; fresh session carries none. **Do NOT restore `warmPrefix`** — warmth stays conservative on resume (existing design, keep it).
- **T2.2 Pin the aux streams that replay session-sized context.** Compaction (`internal/orchestrator/maintenance.go:76`) sends a digest request on ActiveModelID — small, but pin it anyway (same one-line `PinUpstream: e.upstreamPin()`); the advisor call stays unpinned (tiny, different model). Test: compaction request carries the pin.
- **T2.3 Surface it in `/context`.** Add `Upstream string` to `ContextReport` (`internal/orchestrator/contextreport.go`), fill from `e.upstreamPin()`; render under the Model group in `internal/tui/format.go`: `Served by: <name> (pinned)` — omit the line entirely when empty (direct connections must stay clean). Update `context_panel_test.go` / `actions_test.go` / the 80×24 fit test. **Not prefix bytes — no epoch.**
- **T2.4 Bench visibility.** `internal/command/benchjson.go`: per-request upstream list + flip count on the bench summary (from `stats` / usage records), so measurement runs (Phase 5) are machine-readable. Test in `internal/command`.
- **T2.5 Window-safety guard (the 9-upstream context trap).** Only 3 of 9 upstreams serve ≥1M context; Morph serves 256k at 2× price. Our catalog row says 1,000,000, so a 600k conversation routed to a 524k upstream would 400 mid-task. In `historyFitsModel` / the advisor fit gate (`internal/orchestrator/switchcost.go`), nothing to change client-side — but add to T4.3 (gateway: exclude sub-window endpoints via `provider.ignore` or document the operator setting on OpenRouter) and meanwhile: when `inUseContextTokens() > 500_000` on a minimax model, emit a one-time notice advising `/compact` (cheap, honest). Keep it one sentence, Tail-class, no prefix change.

**Acceptance:** full gauntlet green; `/context` shows the upstream on an OpenRouter session and nothing on a DeepSeek-direct session.

## Part 4 — Phase 3: the placement decision (measure, then commit)

- **T3.1 A/B protocol (cost-gated — ask the user before spending).** Same scripted task (the N-shortcut edit) run 3× via pinned OpenRouter and 3× via **direct MiniMax** (the gateway already seeds `https://api.minimax.io/v1`; the user must activate the row + key on Elest.io). Metrics per run, from `usage.jsonl` + gateway dashboard: placement-hit ratio, mean latency, total cost, flips.
- **T3.2 Decision matrix.** Direct ≥ 95% placement AND OpenRouter-pinned < 90% → recommend the user re-point `minimax-m3` to the direct row (keep OpenRouter row as manual fallback). Pinned OpenRouter ≥ 90% → stay, revisit only if the dashboard shows drift. Both < 80% → MiniMax-class caching is placement-broken for us; T3.3 applies.
- **T3.3 (conditional) Teach the advisor the truth.** If placement is structurally poor, the advisor's SWITCH COST is optimistic for minimax (it assumes warm = cheap). Add a per-family placement discount to `switchEconomics` fed from the session's own observed flip rate (records exist per T2.4). Data-driven only — do not hardcode a penalty without T3.1's numbers.

**Acceptance:** a one-page RESULTS.md (scratch, not committed) with the six runs' numbers and the decision, delivered in chat.

## Part 5 — Phase 4: gateway repo (visibility + guardrails at the source)

- **T4.1 Capture upstream per request.** Migration `NNN_upstream_provider.sql`: `ALTER TABLE request_logs ADD COLUMN upstream_provider VARCHAR(100)`. In `proxy/handler.go` streaming path, parse the `provider` field from chunks (the code currently declares only id/object/created/model/choices/usage in `OpenAIChunk` — `translator.go:188` — and **silently discards** it); store on the log row. Non-streaming path: same from the response body. Dashboard (`static/app.js:1308` region): new column. This turns the user's screenshot workflow into the exact correlation view that diagnosed this whole issue.
- **T4.2 Fix the lying comment.** `proxy/handler.go:1032-1035` claims `X-Session-Id` "pins the same underlying provider across a conversation" — no such OpenRouter feature is documented and nothing implements it. Rewrite the comment to what it is (a trace header), or delete the header. A comment that claims a guarantee that does not exist will misdirect the next audit exactly as it tried to misdirect this one.
- **T4.3 (optional, operator-level) Endpoint hygiene for minimax-m3.** Either in the gateway body (add `provider.ignore: ["morph"]` — 2× price, 256k window, never the right answer for this workload) or via OpenRouter account-level provider preferences. Document whichever in the gateway README.
- **T4.4 (optional) Belt-and-braces default pin.** If T1 showed old/unpinned clients still matter: gateway injects `provider.order` for openrouter+minimax when the client sent none, keyed on its own `upstream_provider` history for that session id. Skip if all clients are current — the client-side pin is the right home.

**Acceptance:** dashboard shows an upstream per row; the user can see placement flips without asking anyone.

## Part 6 — Phase 5: verification + regression armor (agent repo)

- **T5.1 Zipper regression test.** Offline: 4-turn scripted session; assert every request's messages extend the previous request's byte-for-byte (the §0.1 zipper as a permanent test, over the wire-normalized messages — `engine_test.go` has the pattern; generalize to 4 turns with tool calls + steering + review nudge; assert zero `ChangeReasons` on every record).
- **T5.2 Flip-storm resilience.** Scripted provider alternates upstreams A/B/A/B: assert one notice per change (no spam), warmth retired each time, pins follow (A, then B, then A…), and the task still completes. Extends `upstream_test.go`.
- **T5.3 Rejection-latch longevity.** Already tested for one provider instance; add: latch survives `UpdateConfig` (config reload must not re-enable a rejected pin — check `snapshot()`/`UpdateConfig` interplay in `provider.go`).
- **T5.4 Open-question log.** The recurring R:114/R:128 on cold requests: after T4.1 lands, check whether the value correlates with upstream identity. If one upstream always reports ~114 on cold, it is that provider's accounting floor — document in docs/prompt-caching.md § limitations and stop wondering. No code.
- **T5.5 `-race` stays a CI gate** (no CGO on this host). The new taskMu fields (`lastUpstream`, `pendingUpstreamNotice`) follow the existing lock pattern; CI will verify.

**Acceptance:** full gauntlet + the new tests green; docs updated where noted; memory updated (see Part 7).

---

## Part 7 — Bookkeeping

- Update `C:\Users\mydwa\.claude\projects\F--MuhiyaCode-Agent-Go\memory\muhiyacode-unified-session.md` (execution status of this plan) and the MEMORY.md index line.
- The 23,397-output-token question from the Broadside Duel session is **explicitly out of scope** here (it is an output-discipline issue, not caching) — but T1.1's run doubles as its probe: count `write_file` rows on ttt.html in that session's transcript and report.

## Success criteria (the whole plan, in three numbers)

1. **Placement-hit ratio ≥ 90%** of eligible main requests (eligible = request 2+ of a task, same task) over a 3-session sample — measured from `usage.jsonl`, not estimated.
2. **Zero unexplained misses:** every sub-50% read request in the ledger carries either attribution (cold-start/agent/compaction) or an upstream change on the same or prior record.
3. **Zero new failure modes:** the pin path provably cannot fail a task (latch test), and the full suite + staticcheck stay green on both repos.
