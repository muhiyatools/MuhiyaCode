# Prompt caching

MuhiyaCode keeps the system prompt, tool schemas, and settled conversation history byte-stable
across a session. Per-turn details such as the date, task class, and budgets are appended to the
latest user message. MCP schemas are pinned at the session boundary; a changed MCP configuration
is applied at the next deliberate boundary and recorded as an invalidation event.

Use a concrete model such as `deepseek-v4-flash` instead of a router that may choose a different
backing model per request. Model changes intentionally start a new provider cache scope.

## Session routing pin (C1)

Every chat request carries an `X-Muhiya-Session` HTTP header so the gateway can pin model routing
for the whole session. Without it, a router can flip the upstream model between requests, and
DeepSeek's cache is per-model, so a single flip silently wipes the entire cached namespace — no
client-side byte-stability guard can see it.

The header is derived once from the session ID and never changes within a session. Each stream
gets a distinct suffix so interleaved traffic on different models does not thrash the pin.
The live wire pins (feature 011 D4 per-kind pins; corrected here by feature 012 R-D14 — code is
the source of truth):

- Main loop: `<sessionID>:main`
- Subagent runs, per kind: `<sessionID>:sub:explore`, `:sub:plan`, `:sub:review`, `:sub:general`
- Onboarding: `<sessionID>:sub:onboarding`
- Compaction: `<sessionID>:main` on the wire (same `ActiveModelID` as main — shares the routing
  pin) while its usage record ledgers under the `:aux` label — wire pins and ledger pins are not
  1:1 for this stream
- There is no `:aux` wire pin: task classification is a local heuristic and sends no request

The value is header-only; it is never serialized into the JSON request body. Two consecutive
requests of the same stream carry an identical header, and the per-kind suffixes keep each
kind's prefix-cache identity independent.

## Subagent context linking (feature 012)

A continuation subagent replays its predecessor's stored transcript verbatim on the
predecessor's exact pin and appends one user message, so the provider serves the shared prefix
from cache (DeepSeek: token-0 identity in 64-token blocks; MiniMax: passive cache over
tool-list → system → messages with a 512-token floor). The subagent system message is
per-kind-per-session stable — the per-run handoff rides the first user message — so even fresh
dispatches of a kind share the cached system+tools prefix. Continuation records live under
`~/.muhiya/sessions/<id>/agents/` (compact JSON: re-indenting raw `reasoning_details` would
change replayed bytes). Every dispatch's link decision, reason, and provider-verified cache
share appear in the task summary and bench records; `contextLinking=off` restores pre-012
dispatch behavior exactly.

## Maintenance scheduling and the anti-thrash latch (C4)

History rewrites (folding completed-task context, trimming aged tool payloads) only run above a
0.60 usable-context pressure floor and are consolidated into one boundary-scheduled maintenance
pass per task. Each pass records exactly one `fold` or `trim` invalidation event with its scope.

An anti-thrash latch prevents oscillating workloads (pressure flapping 0.59 ↔ 0.61) from paying
repeated full-prefix resets. After two maintenance passes the latch engages and pauses automatic
rewrites. The latch does **not** reset on transient pressure dips below 0.60 — it only clears on a
genuine compaction or session restart. This stops an oscillating workload from refilling the reset
budget every time pressure dips.

A minimum-yield gate skips low-yield folds: if a maintenance pass produced no fold and no folded
tokens and pressure is below the 0.80 hard-fold threshold, the pass is skipped and does not
consume a latch slot. When pressure is firmly above 0.80, folding proceeds even with low yield so
the context does not fill up. The `/context` report surfaces the latch state as
"Automatic maintenance: paused by anti-thrash latch".

## Reading the metrics

`/context` reports prompt and output totals, cache-read and uncached input, session, raw
steady-state, and prefix-stability rates, per-stream request counts, unavailable usage records,
recent invalidations, and context pressure.
The activity line shows a compact tag such as `cache 99% (12.3k read / 128 new)`. `unavailable`
means the endpoint did not provide trustworthy cache figures; it is never treated as zero.

### Task-lifecycle usage and rate honesty (C6/C7)

The live activity line and the end-of-task summary describe the **whole current task**, not the
last request. After every recorded request the engine emits the task-cumulative usage —
`Σ(usage)` over every request the task made across the main, subagent, and aux (onboarding,
compaction) streams, measured from a baseline snapshot taken at task start. The live cache tag and
the summary's `cache %` therefore always agree by construction; a task that delegates to a
subagent shows the subagent's tokens folded into the same figure rather than a main-loop-only
number that snaps back at the end.

Rate arithmetic — the session and steady-state rates in `/context` **and** the per-task delta the
summary divides — draws only on records that reported **both** cache operands (the paired sums). A
provider payload that reports a cache read with an underivable miss (or vice versa) still
contributes to the displayed `Cache read / uncached` totals, but never to a rate denominator, so a
one-sided record can never fabricate part of a hit rate. Reads and misses without a partner are
counted for display and excluded from every percentage.

The raw steady-state rate is `Σread/(Σread+Σmiss)` after cold start and is the cost KPI. Prefix
stability is `Σread/Σ(prompt-new_tail)` and is the SC-001 measure because it excludes each
request's genuinely new tail. The first request is excluded from both. A prefix change is
accompanied by a cause such as tools, model, or compaction. A miss is `provider` only when the
shape is stable and it stays inside the new-tail plus two-cache-block tolerance; larger
unexplained shrink or read regression is `agent-suspect` and fails the benchmark gate.

## Provider limitations and observed evidence

The original live 24-turn evidence used three baseline and three pre-fix improved runs on
`deepseek-v4-flash` at `low` effort. Their raw rates were about 96.57%, but the evidence was
superseded after review found a one-request `tool_choice="none"` landing fork and an incorrect
raw-rate acceptance denominator. Those artifacts remain historical evidence and must not be
used as post-fix sign-off. A fresh live run is required for the final numeric claim.

- Cold starts cannot read a session cache and are excluded from steady state.
- Cache TTL, eviction, and capacity are provider-controlled. Stable bytes may still miss.
- DeepSeek's 64-token cache blocks leave a small uncached trailing block.
- Raw hit rate has a workload-dependent ceiling: cold start, each request's new-tail share, and
  64-token block quantization all remain in its denominator. A 40–60K-token stable-context
  scenario is included to give raw rate enough headroom for a ≥99% validation.
- Caches are scoped by model and account; routers can fragment them.
- OpenAI-compatible endpoints generally provide no explicit cache-retention control.
- Reporting varies: fields may be absent, nested, zero, or malformed. Missing data stays unavailable.
- Thinking-mode tool-call replay requires an empty `reasoning_content` key, while reasoning text is never replayed; this replay form is independent of live effort changes.
- `tools` and `tool_choice` both affect rendered prompt shape and remain session-constant.
- Gateway templates should render tools at a fixed position immediately after the system
  message. If the gateway forwards tools verbatim, the upstream provider template controls
  placement; verify this before attributing residual plateaus to eviction.

## Feature 002: Reasonix-aligned mechanisms

The 002 overhaul adopted several Reasonix cache/context mechanisms. All preserve the prime
invariant: the stable prefix (system prompt + tools + settled history) is composed once and
mutated only at explicit, event-recorded boundaries; every per-turn dynamic rides the newest
user turn.

- **Cache-discipline prompt section** — a compile-time, byte-stable `CACHE DISCIPLINE` block in
  the system prompt tells the model not to re-read unchanged files, re-run searches, or repeat
  failed calls, and to keep tool arguments minimal and stable. One-time upgrade break, then it
  rides the cache forever (`internal/orchestrator/prompt.go`).
- **Maintenance ladder** — soft advisory at 0.50 (zero mutation), reclamation (fold/trim) in the
  0.60–0.80 band via an estimate-before-mutate gate (a pass below a 5%-of-window yield floor is
  skipped entirely — no mutation, no event, no latch), prune-before-compact at ≥0.80 (skip the
  paid summarization if reclamation clears the trigger), force at 0.90.
- **Estimate-before-mutate invariant** — a maintenance pass that changes settled bytes is always
  paired with an invalidation event; a below-floor pass leaves history byte-identical. This
  closes the class of bug where a rewrite without a recorded event fails the next request's
  prefix-shape guard.
- **Content-aware reclamation geometry** — trimmed tool results keep a head/tail sized by the
  producing tool's kind (read-only front-loaded, side-effecting balanced), with a 1024-byte
  floor and error-result pinning. Originals are archived to `pruned.jsonl` before any rewrite.
- **Calibrated token estimation** — the pressure estimator calibrates tokens/char from real
  provider usage (clamped 0.05–2, 0.25 fallback, +4/message +8/tool-call framing, reasoning
  excluded) instead of a fixed guess.
- **Digest-accumulating compaction** — repeated compaction appends a new digest and preserves
  prior digests byte-identical (no lossy re-summarization); the summarizer is bounded to 90s
  with one retry and a mechanical fallback so compaction always frees context and never loops.
- **Session routing pin** — every request carries `X-Muhiya-Session` with a per-stream suffix
  (`:main` for the loop + compaction, `:sub` for subagents + onboarding) so the gateway keeps a
  session pinned to one model; DeepSeek's cache is per-model, so a route flip would wipe it.

## Feature 005: project context and memory (cache-neutral)

The terminal/memory feature adds cross-session project orientation without touching the cached
prefix. Project context is two root-local files — `MUHIYA.md` (user instructions) and `MEMORY.md`
(agent-managed memory). On startup both are composed **once** into a byte-stable `## PROJECT
CONTEXT` boot block and persisted as the session's `RenderedBootContext`; on resume that exact
block is reused verbatim rather than recompiled from live state, so the `SystemHash` never drifts
mid-session. Mid-session edits to either file are delivered as one-shot `<memory-update>` /
`<project-instructions-update>` tail blocks on the newest user message — a deliberate, attributable
boundary gated on the file's content hash — never by rewriting the prefix. The model records memory
by editing `MEMORY.md` with its ordinary file tools (no answer-trailer, no side database), so the
write path is plain tool output that never mutates the cached prefix. All TUI mouse/hover/selection/
scroll work is presentation-only and cannot reach the request path. Net effect: zero new prefix-cache
invalidation surface. (Feature 006 replaced feature 005's SQLite ledger + `<project-memory>` trailer;
the one-time system-prompt change is an expected upgrade break, byte-stable thereafter.)
