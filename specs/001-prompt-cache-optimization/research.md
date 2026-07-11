# Phase 0 Research: Prompt Cache Optimization

**Feature**: `001-prompt-cache-optimization` | **Date**: 2026-07-11
**Inputs**: [spec.md](spec.md), Constitution v1.0.0 (Principle VII mandate), deep code audits of
the DeepSeek Reasonix reference (`C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2`)
and the MuhiyaCode Go codebase.

**Method**: Two independent, evidence-based code audits (file:line citations throughout). Part A
explains why the reference achieves ~99% cache hits. Part B maps MuhiyaCode's current lifecycle
and its cache-busting defects. Part C is the adopt/adapt/reject decision matrix required by
FR-009 and Constitution Principle VII. Part D records the design decisions with rationale and
alternatives. Part E drafts the provider-limitations register required by FR-011.

---

## Part A — Why DeepSeek Reasonix reaches ~99% cache hits

Reasonix's request lifecycle: boot composes the **entire cacheable prefix once** (static base
system prompt + policies + a *persisted* environment snapshot + memory folded once + a skills
index; bodies load on demand) and stores it as `Messages[0]`. Tool schemas are canonicalized
once at registration and always emitted **alphabetically sorted**. History is **strictly
append-only**; every request re-sends the previous request's exact bytes plus the new tail.
The prefix mutates at exactly one deliberate point — compaction near ~80% of the context
window — and dynamic per-turn context (language, plan mode, memory updates, job notices) rides
the tail of the newest user message, never the prefix.

Cache invariants enforced by the reference (all verified in code):

| # | Invariant | Key evidence (reference repo) |
|---|-----------|-------------------------------|
| A1 | Request body built from ordered structs; deterministic JSON | `internal/provider/openai/openai.go:797-898` |
| A2 | System prompt is static consts + folded-once blocks; nothing dynamic ever enters it | `internal/config/config.go:1516-1538`, `internal/boot/boot.go:258-326` |
| A3 | Environment probes persisted per fingerprint; transient flaps merge against last-good snapshot | `internal/environment/snapshot.go:16-142` |
| A4 | Tool schemas sorted alphabetically, canonicalized exactly once, hashed for diagnostics | `internal/tool/tool.go:174-315` |
| A5 | MCP tool schemas pinned from a disk cache for the whole session; live handshake drift deferred to next session; placeholders forward to real tools | `internal/plugin/lazy.go:1-172`, `internal/plugin/cache.go:77-100` |
| A6 | History strictly append-only between compactions | `internal/agent/session.go:58-72` |
| A7 | Compaction is the single rare cache-reset: nothing below 0.5 pressure; free snips 0.6–0.8; summarize ≥0.8; force 0.9; fixed 16K-token tail; anti-thrash latch pauses compaction if two consecutive turns compact | `internal/agent/compact.go:19-147,266-278` |
| A8 | Stale tool results snipped/pruned (re-derivable) without summarizer calls; pairing preserved | `internal/agent/prune.go` |
| A9 | `reasoning_content` never re-uploaded, except the empty key DeepSeek requires on thinking-mode `tool_calls` turns | `agent.go:1164-1167`, `openai.go:473-477` |
| A10 | Retries/stream reconnects replay the identical marshaled bytes | `openai.go:366-434` |
| A11 | Session resume is a byte-for-byte round trip (fast-path normalization returns the same backing array) | `internal/provider/provider.go:132-205` |
| A12 | Planner/executor/subagents each run in their own session — no cross-model prefix pollution | `internal/agent/coordinator.go:91-94` |
| A13 | Dual-shape usage parsing (`prompt_cache_hit_tokens`/`prompt_cache_miss_tokens` and `prompt_tokens_details.cached_tokens`); per-turn + session hit rates displayed; misses attributed to `system`/`tools`/`log_rewrite` via per-turn prefix hashing | `openai.go:767-793,919-931`, `internal/agent/cache_shape.go`, `internal/cli/chat_tui.go:2695-2712` |
| A14 | Release-gating mock-endpoint test derives cache hits from byte-identical common prefix with the previous request; CI fails below 90% tail-average across 8 scenarios | `internal/agent/cachehit_e2e_test.go:59-477`, `.github/workflows/ci.yml:56` |
| A15 | Live A/B benchmark: seed 20 fat tool results → idle past TTL → resume pruned-vs-control against the real API; plus a comprehension arm proving pruning doesn't cause hallucination | `benchmarks/context-maintenance-e2e/main.go:171-238` |

Documented tradeoffs the reference accepts knowingly: prefix grows unbounded between
compactions (context headroom traded for cache warmth); compaction is an unavoidable hard
reset; MCP schemas and environment data can be one session stale; thinking-mode tool loops pay
a `reasoning_content`-key replay ceiling; snip/prune is a partial mid-history reset accepted as
cheaper than summarization.

## Part B — MuhiyaCode current state

Lifecycle: `Engine.Run` (internal/orchestrator/engine.go:214) classifies the prompt, appends one
user message (`prompt + "\n\n" + brief`, plus optional goal/plan blocks), composes the system
prompt (engine.go:304-310) and tool set (engine.go:475-480) per task, then loops:
`History.BuildRequest` (history.go:90,213) → `gateway.OpenAICompatible.Chat` (provider.go:61)
→ SSE accumulation (sse.go) → append assistant/tool messages. Persistence: SQLite events +
`transcript.jsonl` + model-facing `history.json` (rewritten on each mutation); resume reloads
`history.json` and rebuilds engine + prompt + registry fresh.

**Already correct (keep and protect with tests):**

| # | Strength | Evidence |
|---|----------|----------|
| B1 | Deterministic request envelope: `map[string]any` body marshals with sorted keys; messages/tools are ordered slices/structs; no map-iteration nondeterminism reaches the wire | gateway/provider.go:105-124, contract/types.go:102-155 |
| B2 | System prompt session-stable in-process; no `time.Now` per turn; `HasSubagents` never flips | orchestrator/prompt.go:20-68, engine.go:301-310 |
| B3 | All per-turn dynamic content rides the newest user message (task brief, goal, plan-mode, steering, governor notices) | engine.go:291-298,340-348,406-465; classify.go:136; goal.go:66-76; plan.go:23-28 |
| B4 | Effort is a body param + `X-Muhiya-Effort` header, never a message | provider.go:118-137, engine.go:371 |
| B5 | History append-only under normal conditions; rewrites pressure-gated | engine.go:284,359; history.go |
| B6 | `MarkSuperseded` is deferred (records IDs; bytes rewritten only inside pressure-gated TrimAged) | history.go:96-139, engine.go:561-563 |
| B7 | Read dedup blocks covered re-reads with a tiny append-only notice; mtime+size fingerprints revoke coverage on external change | engine.go:544-551, inspection.go:108-150,317-335 |
| B8 | Retries remarshal the unchanged input → byte-identical resend | provider.go:69-92,124-127 |
| B9 | Subagents build isolated `[system,user]` streams with filtered tool subsets | subagent.go:104-131 |
| B10 | Resume restores the model-facing array byte-identically (`history.json` struct round trip) | application.go:419-433, state/session.go:105-129 |
| B11 | Workspace tool order fixed; static schemas; `run_subagent` enum sorted | workspace/registry.go:23-42, engine.go:873-879 |

**Cache-busting defects (ranked):**

| # | Defect | Severity | Evidence |
|---|--------|----------|----------|
| G1 | MCP tools register asynchronously after the 900 ms refresh deadline; servers finishing later inject schemas into the tools array on the next turn — tools appear between turn 1 and turn 2+ | HIGH (any MCP session) | mcpclient/manager.go:70-97,136-160; application.go:97,485-486; engine.go:475-480 |
| G2 | MCP tool order is goroutine-timing dependent (per-tool `onTool` → `registry.Add`; the sorted `manager.Tools()` is never used for registration), and `refreshMCP` (`RemovePrefix("mcp__")` + re-Refresh) runs on Add/Remove/SetEnabled/Authorize **and Test** | HIGH | manager.go:148-158,213-225; application.go:297-338,609-622; registry.go:65-76 |
| G3 | System prompt embeds `date:` captured via `time.Now()` at every `buildRuntime` — cross-day resume/restart changes prompt bytes while history replays identically | MEDIUM | application.go:514-517; prompt.go:63-64 |
| G4 | Pressure rewrites are aggressive and trickle: fold at 0.55×limit per task start; `TrimAged` every turn above 0.65 (batches ≥4); compact at 0.80–0.90; silent window drop at `contextLimit-12000`. Pressure is computed from a `len/4` estimate, not provider-reported tokens | MEDIUM | engine.go:26-27,284-286,359-361,747-750; history.go:80,104-235 |
| G5 | `web_search` availability probed per build (1.5 s timeout, in-process cache only) — flaps change both the tools array and the system prompt `HasWeb` line across builds | LOW | application.go:471-473,538-560; prompt.go:28-34 |
| G6 | `/model` switch leaves a stale model name in the system prompt (prompt fields captured once at build) | LOW (correctness nit; model switch changes cache namespace anyway) | application.go:514-517, engine.go:371 |

**Usage-accounting gaps:** `sse.go:147-171` folds `prompt_tokens_details.cached_tokens` OR
`prompt_cache_hit_tokens` (whichever larger) into a single `CachedTokens`, assigned only when
> 0; `prompt_cache_miss_tokens` is ignored; no per-request hit rate; cache-read vs uncached vs
output not distinguished in display; SQLite events have no usage columns and `sessionUsage` is
in-memory only — cumulative usage resets to zero on resume; context-pressure triggers run off
the `len/4` estimate rather than actual `prompt_tokens`.

## Part C — Adopt / Adapt / Reject matrix (FR-009, Constitution VII)

| Reference practice | Decision | Rationale / MuhiyaCode mapping |
|---|---|---|
| A1 Ordered-struct body marshaling | **Adapt** | Map body already deterministic in Go (sorted keys). Keep it (Principle VIII: smallest change); add golden byte-stability tests instead of restructuring (D10). |
| A2 Nothing dynamic in system prompt | **Adopt** | Remove `date:` from prompt; deliver the date in the per-task brief that already rides the user turn (D2). Config-stable fields (workspace, OS, shell) stay. |
| A3 Persisted probe snapshot + last-good merge | **Adopt** | Persist the `web_search` probe per `baseURL+apiKey` fingerprint under the state dir; merge transient failures against last-good (D3). |
| A4 Alphabetical tool sort + canonicalize-once | **Adapt** | Workspace + built-in order is already fixed and deterministic; full alphabetical reorder would churn for no gain. Only the MCP block becomes sorted (by namespaced name) and canonicalized once (D1). |
| A5 MCP schema pinning via disk cache + forwarding placeholders | **Adopt** | Core fix for G1/G2. Schema cache persisted per server fingerprint; session presents the pinned surface from turn 1; live handshakes refresh the cache for the *next* session; execution forwards to live tools (D1). |
| A6 Strictly append-only history | **Adapt** | Already append-only normally; the change is scheduling and floors for the rewrite paths (D4), not removal of fold/trim/compact capabilities (Principle II — context control preserves quality). |
| A7 Single rare compaction, tiered thresholds, anti-thrash latch | **Adapt** | Consolidate fold+trim into boundary-scheduled maintenance passes with raised floors and a thrash latch; keep MuhiyaCode's structured summary compaction (D4). |
| A8 Free snip/prune of stale tool results | **Keep (exists)** | `TrimAged`/supersede already implement this; only the trigger cadence changes (D4). |
| A9 `reasoning_content` strip + required-key exception | **Adopt** | Verify MuhiyaCode never re-uploads reasoning; conform to the DeepSeek empty-key requirement on thinking-mode tool_calls turns (D9). |
| A10 Byte-identical retry replay | **Keep (exists)** | B8 verified; add regression test. |
| A11 Byte-identical resume round trip | **Adapt** | `history.json` round trip already byte-stable (B10); the resume busts come from prompt/tool inputs (G3/G5/G1) — fixed by D1–D3; add restart determinism test (SC-002). |
| A12 Per-model isolated sessions | **Keep (exists)** | B9; `/model` switch stays a user feature, recorded as an invalidation event (D6, D11). |
| A13 Dual-shape usage parsing + hit/miss display + miss attribution | **Adopt** | Extend `Usage` with distinct read/miss fields and unavailable semantics; add PrefixShape hashing and reason labels (D5, D6). |
| A14 Mock-endpoint cache-hit guard test (release gating) | **Adopt** | Offline, deterministic, measures exactly what we control (byte-stable prefixes). Threshold per SC-001/SC-005 (D7). |
| A15 Live A/B benchmark harness | **Adapt** | Build a scripted realistic-workload harness with JSON metrics output for the before/after protocol (FR-010, D8). TTL-idle arm optional. |
| Cache-impact PR governance (CI metadata gates) | **Reject (defer)** | Repo has no git/CI yet. Constitution gates + the guard tests cover the risk. Revisit when CI exists. |
| Memory folded into prefix once per session | **Reject (n/a)** | MuhiyaCode does not inject memory into the main prompt per turn; knowledge briefing is embedded once per subagent run. No change needed. |
| Skills index in prefix, bodies on demand | **Reject (n/a)** | No evidence of skill bodies entering the prompt; verify during implementation, no design change. |

## Part D — Decisions

### D1: Deterministic, session-pinned MCP tool surface
**Decision**: Introduce a persisted MCP schema cache keyed by server spec fingerprint (with
sorted env keys). At session build, register the cached tool surface immediately — before the
first request — as forwarding entries; sort the MCP block by namespaced tool name; canonicalize
each schema once at registration. Live handshake results update the disk cache (next session
sees them) but never mutate the live registry surface. A cache-miss server (never seen before)
contributes a one-time connect stub or joins on explicit user action. Read-only `/mcp` actions
(list, test) MUST NOT touch the registry; `add`/`remove`/`enable`/`disable`/`authorize` are
legitimate toolset changes recorded as invalidation events, applied at a task boundary.
**Rationale**: Eliminates G1 and G2, the highest-frequency full-prefix busts. Matches reference
practice A5 with the same freshness-deferred-one-session tradeoff the constitution's reference
mandate anticipates.
**Alternatives considered**: (a) Block session start until all servers connect — unbounded
startup latency, violates the isolation requirement that broken servers can't stall the agent;
(b) sort-only without pinning — fixes ordering but late-connecting servers still rewrite the
array mid-session; (c) freeze tools at first request without a disk cache — turn 1 in every
new process would present zero MCP tools, degrading quality (Principle II).

### D2: Remove the date from the system prompt; carry it in the task brief
**Decision**: Delete `date:` from the prompt template. Add the current date to the per-task
brief appended to the user message (already dynamic, already uncached).
**Rationale**: Fixes G3 with zero quality loss — the model still receives the date every task,
now in the position designed for dynamic content (Principle IV). Also improves cross-session
prefix sharing (same workspace prompt bytes are day-independent).
**Alternatives**: (a) Persist a session-creation date — stale after midnight and still
fragments cross-session sharing; (b) keep date and accept cross-day busts — fails FR-001/SC-002.

### D3: Persist the web-search probe snapshot
**Decision**: Persist the probe result per `baseURL+apiKey` fingerprint in the state dir.
On rebuild, a definitive probe result (supported / unsupported) wins; a transient failure
(timeout, network) reuses the last-good snapshot. Changes to the snapshot are legitimate
toolset-change invalidation events.
**Rationale**: Fixes G5; identical to reference practice A3 (persisted environment snapshot,
last-good merge).
**Alternatives**: (a) Probe once and never refresh — gateway upgrades would never surface;
(b) drop `web_search` gating entirely — breaks gateways without search support.

### D4: Consolidate history rewrites into rare, boundary-scheduled maintenance
**Decision**: Keep fold, trim/supersede, compaction, and window-drop capabilities, but:
no rewrite of previously transmitted bytes below a pressure floor of 0.60× usable context;
run fold+trim together as one consolidated maintenance pass at a task boundary (or a turn
boundary when forced), never as a per-turn trickle; add an anti-thrash latch (if two
consecutive maintenance passes fire, pause automatic rewrites until pressure genuinely
recedes or compaction resolves it); compute pressure from the provider-reported
`prompt_tokens` of the latest response (exact) with the estimator only as bootstrap fallback;
record every pass as an invalidation event with cause and scope (D6). Exact threshold values
are tuned against the benchmark (D8) within these bounds: floor ≥0.60, compact ≥0.80,
force ≥0.90.
**Rationale**: G4 is the dominant in-session bust for long sessions: fold at 0.55 per task and
per-turn trims above 0.65 rewrite the middle of history repeatedly — each one a partial cache
reset. The reference proves the shape: do nothing early, snip freely in a band, summarize
rarely, latch against thrash (A7). Using exact provider token counts stops estimate-driven
premature rewrites (Principle VI applied to internal decisions).
**Alternatives**: (a) Pure append-only until compaction (reference-pure) — rejected: MuhiyaCode
targets smaller context windows too, and folding completed tasks preserves quality per its
token-efficiency design; (b) keep current thresholds and only fix MCP/date — leaves the
highest-cost long-session busts in place, fails SC-001 for 20+ turn sessions under pressure.

### D5: Honest, persistent, distinguishable usage accounting
**Decision**: Extend the usage model to carry distinct nullable fields: cache-read tokens,
cache-miss (uncached input) tokens, output tokens, total prompt tokens — populated exactly
from provider payloads (`prompt_cache_hit_tokens`/`prompt_cache_miss_tokens`, or
`prompt_tokens_details.cached_tokens` with miss derived as `prompt − cached`), never
fabricated; absent fields stay null and display as "unavailable" (never zero-as-fact).
Persist one usage record per request to `sessions/<id>/usage.jsonl` (append-only, additive to
the v1 bundle format); rebuild session aggregates from it on resume. Surface per-request and
cumulative session hit rate plus read/uncached/output splits in `/context` and the activity
line.
**Rationale**: FR-007/SC-004; fixes all five accounting gaps (miss ignored, no rate, collapsed
fields, resume reset, no persistence). JSONL in the session dir follows the existing
transcript pattern and preserves the `~/.muhiya` compatibility boundary (additive file; v1
readers ignore it).
**Alternatives**: (a) New SQLite usage table — heavier migration surface on the compatibility
boundary for no added benefit at this scale; (b) in-memory only with TaskComplete callbacks —
fails resume continuity and honest before/after measurement.

### D6: Prefix-shape hashing and invalidation-event ledger
**Decision**: Compute a per-request PrefixShape (hashes of: system prompt bytes, canonical
tools bytes, history rewrite-version, model ID). Compare with the prior request; on any change
record an InvalidationEvent naming the cause (`prompt-rebuild`, `toolset-change`,
`fold`, `trim`, `compact`, `window-drop`, `model-switch`, `user-compact`) and scope. When
provider usage shows misses while the shape is unchanged, classify the miss as provider-side
(TTL/eviction/cold start). Events persist with the session and annotate the usage display
("prefix changed: tools").
**Rationale**: FR-005/FR-008/SC-007 — every miss becomes attributable; mirrors reference A13's
`PrefixShape/CompareShape` self-diagnosis.
**Alternatives**: Full byte-diff storage of every request — precise but heavy and unnecessary;
hashes + rewrite versions identify the region without storing payloads.

### D7: Offline mock-endpoint cache guard as the regression gate
**Decision**: Add a deterministic e2e test suite with a mock OpenAI-compatible endpoint that
computes "cache hit tokens" as the byte-identical common prefix with the previous request.
Scenarios: plain dialogue, tool loops, mixed sizes, MCP-pinned surface, restart/resume replay,
steering, goal continuation, pressure maintenance. The suite fails if steady-state tail-average
hit rate drops below target (≥90% guard threshold; SC-001 evaluated at 99% on eligible-token
accounting in the live benchmark). Wire into the standard `go test` run per the constitution's
prefix-stability gate.
**Rationale**: Measures exactly what the client controls (byte stability) with zero network
dependence; the reference proves the technique gates regressions effectively (A14).
**Alternatives**: Only live benchmarks — nondeterministic (provider eviction), slow, unusable
as a per-change gate.

### D8: Live before/after benchmark harness
**Decision**: Build a scripted-workload harness (standalone command under `benchmarks/`)
driving the real configured gateway: realistic multi-turn coding sessions (≥20 turns: reads,
searches, edits, tool loops, follow-ups), N≥3 repetitions, JSON output of per-request usage
(read/miss/output), session hit rate, wall-clock latency, and derived cost. Run once on the
unmodified baseline build, once post-implementation, identical config (model pinned, no
router), per the spec's before/after protocol; store results under
`specs/001-prompt-cache-optimization/benchmarks/`.
**Rationale**: FR-010/SC-005/SC-006; adapts reference A15 to a before/after design.
**Alternatives**: Synthetic single-shot prompts — fails the constitution's "realistic
multi-turn" definition; manual sessions — not reproducible.

### D9: Reasoning-content replay policy
**Decision**: Verify the assistant messages replayed to the provider exclude reasoning
content; where DeepSeek thinking mode requires the `reasoning_content` key on assistant
`tool_calls` turns, emit the minimal required form only on those turns.
**Rationale**: Re-sent reasoning is billable uncached input every turn (violates FR-004) and
the missing-key 400 would force history repairs (violates FR-001). Reference A9 documents both
edges.
**Alternatives**: Always include reasoning — pure waste; always omit — provider 400s in
thinking mode.

### D10: Keep the map-based body; lock determinism with golden tests
**Decision**: Keep `map[string]any` request marshaling (Go sorts map keys — deterministic
today) and add golden tests asserting byte-identical serialization for identical inputs,
including tools canonicalization and cross-restart request rebuilds.
**Rationale**: Principle VIII (improve, don't rewrite): the current envelope is already
deterministic (B1); tests convert an accident into a guarantee.
**Alternatives**: Port to ordered structs like the reference — cleaner but a wire-touching
rewrite with migration risk and no measurable cache gain.

### D11: Model-switch prompt refresh
**Decision**: On `/model`, rebuild the prompt context (model name/addendum) and record a
`model-switch` invalidation event. The switch already changes the provider cache namespace, so
the refresh has zero marginal cache cost and fixes the stale-name correctness nit (G6).
**Rationale**: Truthful prompt; free given the namespace change.
**Alternatives**: Remove the model name from the prompt — loses model-specific addenda that
target known tool-call failure modes (quality risk, Principle II).

### D12: Provider-limitations register location
**Decision**: Maintain the register as a section of a new `docs/prompt-caching.md` (user- and
maintainer-facing), seeded from Part E below and updated with benchmark evidence during
implementation.
**Rationale**: FR-011 requires durable documentation; README already points users at docs/ for
design details.
**Alternatives**: Keep only in specs/ — hidden from users who need the "why not 100%" answer.

## Part E — Provider limitations register (initial draft)

To be promoted to `docs/prompt-caching.md` with benchmark evidence (FR-011, SC-007):

1. **Cold start**: the first request of a session (or after provider cache expiry) cannot read
   cache; it necessarily writes. Steady-state targets exclude it by definition.
2. **Cache lifetime/eviction is provider-controlled and opaque**: identical bytes can still
   miss after idle periods or under provider capacity pressure. Client-side attribution: shape
   unchanged + miss reported → provider-caused (D6).
3. **Minimum cacheable granularity**: DeepSeek caches in fixed-size token blocks (64-token
   units); the trailing partial block of any prefix never reads as cached, so 100.0% is not
   reachable even with perfect stability — 99%+ is (documented basis for SC-001's 99–100%
   band).
4. **Per-model (and per-account) cache scoping**: switching models mid-session cold-starts by
   design; router-style virtual IDs (`muhiya-ai-router`) that re-select the underlying model
   per request fragment caching — users must pin a concrete model for the target hit rates
   (already noted in README; register makes it normative guidance).
5. **No explicit cache control on OpenAI-compatible endpoints**: caching is implicit; the
   client can only maximize byte stability, not command retention.
6. **Reporting variance across providers**: some endpoints omit cache fields entirely or use
   the nested `cached_tokens` shape without a miss figure; metrics must degrade to
   "unavailable" (FR-013) and hit-rate targets apply only where reporting exists.
7. **Thinking-mode replay requirement**: DeepSeek requires the `reasoning_content` key on
   replayed assistant tool-call turns in thinking mode; the fresh tail of each turn is always
   uncached input — chatty short turns therefore cap the achievable session-average rate.

## Resolution of Technical Context unknowns

No NEEDS CLARIFICATION markers remain. All Technical Context fields in [plan.md](plan.md) are
grounded in the audits above.
