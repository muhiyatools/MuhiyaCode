# Phase 0 Research — DeepSeek API Alignment (feature 007)

**Date**: 2026-07-14. Inputs: [spec.md](spec.md), [audit-baseline.md](audit-baseline.md),
constitution v1.0.0, five parallel research passes (client trace C1–C5, gateway trace
G1–G6+G3b, live DeepSeek docs W1–W4, README references RM1–RM3, Reasonix reference
X1–X5 + inventory I1–I20). All facts below are cited to those IDs; full evidence in
§Appendix. Every plan unknown is resolved — no NEEDS CLARIFICATION remains.

---

## R1. Keep-alive & timeout posture (D4) — mostly already correct; two real gaps

**Decision**: Keep both idle watchdogs as-is (they are already keep-alive-aware). Fix
exactly two gaps: (a) raise the gateway upstream `ResponseHeaderTimeout` from 90s to
**630s** (10-minute documented pre-inference window + 30s margin) on the chat-proxy
transport; (b) restructure the client's 10-minute whole-attempt lifetime into a
**first-byte deadline of 10 minutes** plus the existing 90s rolling idle timeout, so a
request that queues 9 minutes and then streams a long answer is not killed mid-answer.

**Rationale (verified facts)**:
- Client: the 90s idle timer resets on EVERY scanned line — `: keep-alive` comments,
  empty lines, anything (C1); the SSE consumer silently skips non-`data:` lines without
  counting them malformed (C2). So streaming keep-alives already sustain the client.
- Gateway: the 120s watchdog likewise resets on every upstream line, all four relay
  loops (G1); the MuhiyaCode path (OpenAI→OpenAI) forwards keep-alive lines to the
  client verbatim (G2), which in turn resets the client's timer. The chain is sound
  once streaming has begun.
- The genuine break: gateway `ResponseHeaderTimeout` 90s (G3) — a queued streaming
  request whose response headers arrive later than 90s dies as a 502, well inside the
  provider's documented 10-minute window. Whether DeepSeek delays headers is not
  determinable from code (G3b) — the audit's keep-alive simulation (spec assumption)
  must exercise both early-header and late-header cases.
- Client secondary: the 10-minute `RequestLifetime` covers the whole attempt including
  the stream (C3); with a multi-minute queue plus a long completion this cap is
  reachable. Reasonix uses first-token-oriented timeouts for the same reason: 120s
  `ResponseHeaderTimeout` "models can think for a while before the first token" plus a
  120s idle watchdog and no whole-stream cap (X2).
- The one-shot CLI path overrides lifetime to 45s (C3, root.go:435) — audit will flag
  it as intentionally short for non-interactive use, not a defect.

**Alternatives considered**: Making the client tolerate 10 minutes of *silence*
(raising idle to 600s) — rejected: keep-alives already reset the timer, so silence
beyond 90s genuinely indicates a dead path; raising it would only slow failure
detection. Removing the gateway header timeout entirely — rejected: a hung upstream
would then consume a slot for the full 15-minute client cap.

## R2. Stable user identifier (D1) — adopt `user_id`, gateway-authoritative

**Decision**: Gateway injects `user_id` into every DeepSeek-bound JSON body:
`"mu-" + hex(HMAC-SHA256(server_secret, user.UUID))[:40]` (43 chars, alphabet
`[a-z0-9-]` ⊂ documented `[a-zA-Z0-9\-_]`, ≪512 limit). Derived from the authenticated
`User.ID` (not the virtual key), so key rotation preserves it (spec FR-010). Any
client-sent `user_id`/`user` value is deleted before injection (FR-011). The HMAC
secret comes from gateway configuration (same posture as the existing provider-key
encryption secret); identical derivation on every instance keeps the value stable
across restarts and horizontal scale. Non-DeepSeek OpenAI-compatible upstreams receive
the same value in the standard `user` field; Anthropic-dialect upstreams receive it in
`metadata.user_id`; providers with no documented field get nothing (Principle IX
graceful degradation).

**Rationale**: DeepSeek documents `user_id` for content-safety identity distinction,
**"KVCache isolation for privacy management"**, and scheduling isolation, with
per-`user_id` concurrency on expanded quotas (W4). The privacy framing matters: today
all Muhiya users share one anonymous account-wide cache namespace, so one user's
prefix timing is observable to another — DeepSeek itself positions per-user isolation
as the privacy-correct posture. HMAC (not plain SHA-256 of the UUID) prevents anyone
who learns a user UUID from confirming the upstream identifier. Reasonix sends no user
field at all (X1) — a justified deviation: Reasonix is a single-user direct client;
the gateway is multi-tenant.

**Cache trade-off (Principle II disclosure)**: per-user KVCache isolation forfeits
cross-user sharing of the common system-prompt prefix. Within-user steady-state hit
rate (what SC-006 measures) is unaffected — a user's own sessions stay in their own
namespace. The before/after benchmark runs under a single user and must show no
steady-state regression; the cross-user sharing loss is accepted as the documented
privacy-correct behavior and recorded in the audit report.

**Alternatives considered**: Per-virtual-key identifier — rejected: key rotation would
shift the user's cache namespace and fragment concurrency attribution. Plain user UUID
— rejected: leaks internal identifiers upstream ("do not include user privacy
information", W4). Per-session identifier — rejected: defeats cross-session cache
reuse and contradicts "stable" (FR-010).

## R3. Model limits & names (D2, D9) — reconcile to 1M/384K; staged name migration

**Decision**: (a) Update gateway model rows to the documented limits — context
1,000,000 and max output 384K (metadata used for reporting; the gateway does not
enforce truncation, unchanged). (b) Client `deepseek` ModelProfile: raise
`ContextWindowTokens` to the documented 1M but keep the *operational* context budget
(the `contextLimit` setting, default 128k) as a deliberate cost/latency bound — being
below provider limits satisfies FR-004 ("MUST NOT exceed"); the budget rationale is
recorded in the audit report. Keep sending explicit `max_tokens` (byte-stable,
runaway-guard) validated ≤ the documented maximum; 16000 is valid. (c) Model names:
the chat endpoint documents only `deepseek-v4-flash` / `deepseek-v4-pro` as allowed
values (W4), while the gateway targets legacy `deepseek-chat` / `deepseek-reasoner`
names that demonstrably still work (live sessions succeed — undocumented aliasing).
Migrate `TargetModel` values to the documented names as an explicit, logged,
scheduled **cache-epoch event** (a model change wipes the per-model prefix cache
once); verify alias behavior live first so the audit records whether the legacy names
are rejected, aliased, or deprecated-but-working.

**Rationale**: Pricing page (W1): both models list 1M context / "MAXIMUM: 384K"
output; cache-hit vs cache-miss input pricing confirmed; **no off-peak/time-variable
pricing exists** (two extraction passes) — the spec's off-peak edge case resolves to
"confirmed absent; static gateway prices remain correct." Reasonix corroborates the
1M window: its config backfills 1,000,000 for DeepSeek and feeds it only to the
compaction ladder, never the wire (X3) — same shape as our (b).

**Alternatives considered**: Raising the client's operational budget to 1M — rejected
for now: compaction discipline is feature-001/002 tuned territory; a 8× budget jump is
a separate measured feature (Principle X). Immediate hard cut-over of model names —
rejected: unverified alias risk plus an unscheduled cache wipe for every live session
violates the spirit of Principle V.

## R4. Reasoning/thinking controls (D3) — sanitize unconditionally, explicit thinking

**Decision**: Gateway `ApplyThinkingOpenAI` becomes **unconditional** for DeepSeek
targets: it always deletes client-supplied `reasoning_effort`/`thinking` and always
re-emits the documented form — `thinking:{type:"enabled"}` + `reasoning_effort:
"high"|"max"` when an effort level resolves to reasoning-on, and `thinking:
{type:"disabled"}` when it resolves to off/absent. No raw client value can pass
through on any path (spec FR-003 "including when the client sends no explicit effort
setting at all"). The v4 docs make `thinking` default-**enabled** (W4/A-capture), so
explicit `disabled` is required to keep today's non-reasoning behavior deterministic
rather than server-default-dependent.

**Rationale**: Today the rewrite is gated on a resolved effort level; a third-party or
stale client body reaches DeepSeek verbatim with e.g. `reasoning_effort:"low"` — an
undocumented value (docs: `high`/`max` only). Explicit-on-every-request also follows
Reasonix's capability-switch pattern: per-provider request shape decided once, never
server-defaulted (X5), and its boot-time clamp of DeepSeek-isms per backend (X3).
`thinking`/`reasoning_effort` are top-level params — not part of the tokenized prompt
prefix — so toggling them cannot invalidate the KV cache.

**Alternatives considered**: Rejecting invalid efforts with a 400 — rejected: the
gateway's job is to make valid requests from trusted-intent clients, not to fail them
(Principle I: correctness of the user's session first).

## R5. Deprecated/unknown parameter policy (D7) — explicit strip-list

**Decision**: The gateway strips a small, named list from every DeepSeek-bound body:
`frequency_penalty`, `presence_penalty` (documented "no longer supported", W2/A),
plus the identity fields covered by R2 (`user`, `user_id` — replaced). Everything
else continues to pass through verbatim (lossless-map forwarding preserved). The
strip-list is a package-level constant with a test asserting the exact set — the
"documented strip" posture the spec's edge case demands.

**Rationale**: Smallest change (Principle VIII); mirrors Reasonix's
`reservedExtraBodyField` allow/strip discipline for param hygiene (X4). A full
allowlist would break unknown-but-valid future params and third-party clients.

## R6. Cache-miss accounting (D5) — new column, plumb the existing parse

**Decision**: Add nullable `cache_miss_tokens` to `request_logs` (new migration),
persisted from the already-parsed `OpenAIUsage.prompt_cache_miss_tokens` (G6 —
translator already declares the field; it is captured but dropped today). Surface it
in `/v1/usage` aggregates and the dashboard's cache-hit-rate computation (replacing
the derived `input − cache_read` approximation with provider-reported misses when
present). Estimated-usage rows (mid-stream disconnects, `UsageEstimated=true`) leave
the column NULL — never a fabricated miss count (Principle VI; spec FR-017).

**Alternatives considered**: Deriving miss as `input − cache_read` at query time —
rejected: hides provider-reported contradictions the client already flags
(baseline §B) and fails FR-016's "provider-reported" requirement.

## R7. Rate-limit semantics (D10) — conform; add Retry-After propagation

**Decision**: Client behavior already conforms (bounded retries, honors Retry-After,
byte-identical replay, no retry once streamed — C5). Gateway: propagate upstream
`Retry-After` on relayed ≥400 responses (today only Content-Type is set, G5) and add
`Retry-After` to the gateway's own 429s. Billing: verified NO duplicate-billed rows on
any traced path (failed upstream rows are logged with cost 0 and excluded from spend;
`finish()` is idempotent; outbox retries reuse the primary key; router failover bills
only the winning candidate) — G4. Audit verdict: conform, with the two additive
header fixes.

## R8. Client capability profile (US3) — extend the existing ModelProfile

**Decision**: Extend the client's per-family `ModelProfile` (internal/gateway/model.go)
into the capability profile the spec requires: documented parameter set, deprecated
list, context/output limits (R3 values), JSON-mode preconditions, and beta features
with adoption status (R9). The request builder consults it (it already consults the
profile for temperature/top_p/max_tokens); a debug surface (existing `/context` or
doctor output) can render it. One struct, one file, session-frozen at boot — the
Reasonix pattern of capability flags frozen at client construction so the wire shape
can never flip mid-session (X5).

**Alternatives considered**: A gateway `/v1/capabilities`-driven dynamic profile —
rejected for this feature: the endpoint exists (baseline §C) but making the client's
request shape depend on a network fetch adds a boot dependency and a mid-session
drift risk (violates X5's frozen-shape principle); revisit when capabilities actually
vary per deployment.

## R9. Beta features (D8) — all "not adopted", recorded

**Decision**: Capability profile records: **chat prefix completion — not adopted**
(agent output is free-form; no format-forcing need; `/beta` base not exposed by
gateway); **FIM — not adopted** (MuhiyaCode edits via tool calls, not raw insertion;
only `deepseek-v4-pro` supports it, W2); **strict tool schemas (beta) — not adopted
now, flagged re-evaluate** (would harden tool-call arguments but requires the `/beta`
base URL chain-wide and schema-strictness guarantees for all 20+ tools; the DSML
rescue layer (baseline §B) already handles malformed calls). Each decision satisfies
FR-015's "recorded with measurement" by citing the absence of a correctness
divergence and the unavailability of the beta base through the gateway.

## R10. Session-affinity durability (D6) — best-effort, made observable

**Decision**: Keep in-memory sticky pins (best-effort posture) but (a) log every
model-swap event for a known session (the FR-012 observability requirement), and
(b) mitigate the restart blast-radius the cheap way: client sessions pin the model
explicitly (MuhiyaCode already requests a fixed configured model — the router only
picks for `muhiya-ai-router` virtual requests), so MuhiyaCode traffic is unaffected
by restarts; only router-model users are exposed, and the swap log quantifies it.
Redis-backed pin persistence recorded as the documented future option if the swap
log shows real impact (measured decision, Principle X).

**Alternatives considered**: Immediate Redis persistence — rejected: Redis is
optional infrastructure today (rate limiting only, fail-open); making cache
stability depend on it adds an availability coupling without measured need.

## R11. README structure (US5) — synthesized 9-section skeleton, ≤125 lines

**Decision**: Adopt the RM3 skeleton, tuned to MuhiyaCode: centered HTML hero
(wordmark, one-line tagline, shields.io flat-square badges: release version /
Go build / license, terminal screenshot or demo GIF, `---`) → `## Install`
(single commented block: npm i -g muhiyacode + GitHub releases; supported platforms
stated honestly) → `## Quick start` (3 steps: install → `cd` project → `muhiyacode`,
first prompt, `/login`) → `## Features` (≤7 bullets: agentic coding, DeepSeek prefix-
cache-optimized, plan mode, goals, subagents, MCP, Arabic/RTL, memory) → `## Commands`
(one table: all 17 in-app slash commands + aliases from the registry, framed
explicitly as "typed inside MuhiyaCode") → `## Configuration` (pointer + 3-line
example) → `## Documentation` / `## Contributing` / community footer (one line each).
Budget: hero ≤15, install ≤14, quick start ≤10, features ≤10, commands ≤26 (17 rows +
frame), config ≤10, tail ≤10, headings/rules ≤15 → ~110 lines, inside the 125 cap.

**Rationale**: claude-code proves radical brevity works (73 lines, gif does the
selling; deep topics are one-line pointers) — RM1; opencode proves the centered hero
+ single install block + tables aesthetic (129 lines) — RM2. MuhiyaCode differs from
both in ONE deliberate way: a complete in-app command table (neither reference lists
slash commands; the user's explicit requirement is that commands shown are the
in-app ones) — RM3 skeleton already reserves that section.

## R12. Reasonix reference findings (constitution Principle VII gate)

Fresh repo pass (X1–X5) + feature-002 inventory carry-forward (I1–I20). Cache- and
wire-relevant conclusions for THIS feature:

| Finding | Source | Consequence here |
|---|---|---|
| No user/user_id field anywhere; headers only auth/content | X1 | Our R2 is a justified deviation (multi-tenant gateway vs single-user client) |
| Idle watchdog 120s reset by every line incl. comments; reconnect ≤3 only when nothing emitted; clean-FIN guard against half-turns | X2 | Confirms R1 posture; MuhiyaCode's no-retry-once-streamed already matches (I14/T045) |
| ResponseHeaderTimeout 120s "models can think before first token"; no whole-stream cap | X2 | Supports R1's first-byte-deadline restructure and gateway 630s header timeout |
| Context window is config metadata (1M for DeepSeek) feeding compaction only, never the wire | X3 | Matches R3(b): profile carries documented limits; operational budget separate |
| max_tokens omitted (omitempty) on OpenAI path | X3 | Deviation kept: MuhiyaCode sends explicit max_tokens (byte-stable runaway guard); top-level param, cache-neutral |
| Boot-time per-backend effort clamping; invalid values rejected at construction | X3 | Supports R4's never-pass-raw posture (gateway-side equivalent) |
| reservedExtraBodyField strip-list prevents param drift | X4 | Direct pattern for R5's strip-list |
| Capability flags frozen at construction; wire shape session-stable per provider | X5 | Direct pattern for R8's profile; R4's explicit thinking |
| Inventory I1–I20 (prompt byte-stability, append-only history, no-op maintenance skip, usage normalization, reasoning_content empty-key, etc.) | I1–I20 | All PRESENT/adopted in MuhiyaCode via features 001/002; this feature adds no history/prompt changes, so no new adoption rows — verdicts stand |

## R13. Duplicate billing & 429 relay — verified conform (see R7)

Absorbed into R7; G4/G5 facts. No design change beyond Retry-After propagation.

## R14. JSON mode — profile rules only

**Decision**: No MuhiyaCode feature uses `response_format` today (baseline §B). The
capability profile records the documented preconditions (prompt must contain "json" +
an example; size max_tokens against truncation; known empty-content issue) so any
future use inherits them (FR-014). No wire change.

---

## Appendix — research fact index

- **C1–C5** (client trace): idle-reset-on-every-line; comment/empty lines skipped
  silently; 10-min whole-attempt lifetime (45s CLI override); no ResponseHeaderTimeout
  (bare http.Client); byte-identical 429 replay, never after streaming.
- **G1–G6, G3b** (gateway trace): watchdog resets on every line, all four relays;
  same-dialect relays forward keep-alives verbatim, translating relays swallow them;
  ResponseHeaderTimeout 90s → 502 on late headers (upstream header timing unknowable
  from code); no duplicate-billed rows (failed rows cost-0 and spend-excluded,
  idempotent finish, PK-guarded outbox, failover bills winner only); 429 relayed
  verbatim minus Retry-After; request_logs has cache_read/write but no miss column.
- **W1–W4** (live docs 2026-07-14): both v4 models 1M context / 384K max output;
  cache-hit vs miss input pricing; **no off-peak pricing**; FIM = beta `/completions`,
  v4-pro only, prompt/suffix/echo/logprobs; no documented max_tokens default;
  `user_id` ≤512 chars `[a-zA-Z0-9\-_]`, "do not include user privacy information",
  used for content safety + **KVCache isolation (privacy)** + scheduling isolation.
- **RM1–RM3** (README refs): claude-code 73 lines (badges+gif hero, delegate-to-docs);
  opencode 129 lines (centered theme-aware hero, single install block, tables);
  synthesized ≤125-line skeleton adopted in R11.
- **X1–X5, I1–I20** (Reasonix reference, Principle VII): see R12 table.

Raw agent outputs preserved in the session workflow journal; wire maps in
[audit-baseline.md](audit-baseline.md).
