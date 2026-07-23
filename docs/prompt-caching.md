# Prompt caching

MuhiyaCode keeps the system prompt, tool schemas, and settled conversation history byte-stable
across a session. Per-turn details such as the date, task class, and its tool/turn budgets are
appended to the latest user message. MCP schemas are pinned at the session boundary; a changed MCP configuration
is applied at the next deliberate boundary and recorded as an invalidation event.

Use a concrete model such as `deepseek-v4-flash` instead of a router that may choose a different
backing model per request. Model changes intentionally start a new provider cache scope — which is
why the model for a task is frozen once that task begins (see "Task-stable model" below).

## Session routing pin (C1)

Every chat request carries an `X-Muhiya-Session` HTTP header so the gateway can pin model routing
for the whole session. Without it, a router can flip the upstream model between requests, and
DeepSeek's cache is per-model, so a single flip silently wipes the entire cached namespace — no
client-side byte-stability guard can see it.

The header is derived once from the session ID and never changes within a session. Each stream
gets a distinct suffix so interleaved traffic on different models does not thrash the pin.
The live wire pins (code is the source of truth):

- Main loop: `<sessionID>:main`
- Onboarding: `<sessionID>:sub:onboarding`
- Task advisor: `<sessionID>:sub:advisor` (one utility-model call at the start of every task;
  ledgers under the `:sub:advisor` label)
- Compaction: `<sessionID>:main` on the wire (same `ActiveModelID` as main — shares the routing
  pin) while its usage record ledgers under the `:aux` label — wire pins and ledger pins are not
  1:1 for this stream
- There is no `:aux` wire pin: task classification is a local heuristic and sends no request

The per-capability-class subagent pins (`:sub:explore`, `:sub:general`, `:sub:review`) are gone
with the subagent system itself, and so is the plan-kind pin that preceded them. There is no
dispatched work left to pin separately from the one session doing everything on `:main`; the two
remaining `:sub:` pins are both auxiliary calls — onboarding and the task advisor — never a
delegated run.

The value is header-only; it is never serialized into the JSON request body. Two consecutive
requests of the same stream carry an identical header, and the per-stream suffixes keep each
stream's prefix-cache identity independent.

## Upstream affinity (OpenRouter)

The session pin above governs routing *inside the gateway*. It has no authority over what
OpenRouter does next, and that is a second, independent place the cache can die.

A model slug on OpenRouter is served by several upstream providers, and **each one keeps its own
prompt cache**. Nothing in the OpenAI request format expresses a preference, so OpenRouter is free
to re-route between turns — and the next request re-reads the whole conversation as uncached input
while every byte we sent is identical to the turn before. No client-side stability guard can see
it, because nothing on our side changed. Observed live: a 30,592-token prompt returning a 114-token
cache read in a session otherwise running at 83%.

The session therefore learns its upstream and asks to go back to it:

1. Request 1 carries no preference — nothing is known yet.
2. OpenRouter names the upstream that served it (`provider`, in every chunk). The SSE accumulator
   reads it into `StreamResult.Upstream`; it is recorded on the usage record.
3. Every later request carries `provider: {order: [<upstream>], allow_fallbacks: true}`.

Three properties are deliberate:

- **Learned, not configured.** The set of upstreams behind a slug changes without notice, so a
  hardcoded list would rot and a wrong name is worse than none. Whoever served us first is by
  definition both reachable and holding our prefix.
- **A preference, never a restriction.** `allow_fallbacks` stays true and `provider.only` is never
  sent. A hard pin converts an upstream outage into a failed task — a total loss traded against one
  cold prefix. When a fallback does happen the session adopts the new upstream rather than asking
  forever for a machine that is not answering.
- **It cannot fail a task.** A pinned request refused with a non-retryable status is immediately
  retried without the pin, and the rejection latches off for the process. A 4xx is terminal in the
  retry loop, so without that branch a route that did not forward the field would turn every task
  into a hard failure.

An upstream *change* is reported once, with both sides named, and retires the warm-model ledger —
the advisor must not price a switch against a cache an upstream flip already destroyed.

The pin survives a restart. It is persisted on the prefix-shape sidecar and restored in
`NewEngine`, because resume is the single most expensive request in a session — the conversation
is at its largest — so landing it on a machine that never saw the conversation is the worst
placement miss available. A flip re-arms the sidecar, so a resume pins to where the session ended
rather than where it began. Warmth is deliberately *not* restored: a pin is a preference a router
may ignore, but warmth is an assertion about what a provider still holds, and that cannot be known
after a restart.

`provider.order` takes lowercase **slugs** (`novita`, `deepinfra`, `atlas-cloud`) while the
response reports **display names** (`Novita`, `DeepInfra`, `AtlasCloud`), and an entry matching no
slug is skipped *silently*. Passing the reported name verbatim would therefore pin nothing and
report no error. `upstreamOrderCandidates` sends every plausible spelling — base slug first, then
the camel-hyphenated form, then the value verbatim — because unmatched entries cost nothing and a
silent no-op costs a full prefill.

One consequence worth knowing: a model slug behind a router is not one window. The upstreams
serving MiniMax M3 range from 256k to 1M context while the catalog carries the largest, so a
conversation past `routedWindowFloorTokens` (500k) can fail outright if a fallback puts it on a
smaller peer. That is an advisory, not a gate — one notice per compaction cycle suggesting
`/compact`, because the request may well succeed and refusing work over a resolvable risk would be
worse.

A direct provider connection sends no `provider` field at all. The value stays empty, no preference
is ever sent, and the whole mechanism is silently inert. Its absence across a session is itself the
answer to "is a routing layer even in this path?".

## Task-stable model

There is one model, not three roles: it plans, edits, and verifies, plus a **utility model**
resolved on the fly for cheap auxiliary calls (the task advisor's own call, onboarding) — whichever
catalog entry's id or name contains "flash", or failing that the smallest-window entry, or failing
that the active model itself. There is no `/model` command and no model name in the TUI chrome —
the user does not manage models.

The model is **frozen for a task**, not a session. A mid-task switch would cold-start the main
prefix under a new provider cache scope for no reason a task boundary would not also serve, so
nothing switches once a task's first request has gone out. Between tasks the model is free to
move — caching is no longer a reason to hold it still for the rest of the session, because there is
no execution chain left to break by moving it.

A **task advisor** enforces that on a schedule, not a one-time gate. It runs at the start of every
task, before that task's first main request — the only moment a switch is free because nothing for
this task is cached yet. It runs on the utility model, is bounded to a small JSON answer (200 max
tokens, 8-second timeout), and its expected outcome is to keep the current model. Two hard gates
back it up and it cannot override either:

- **FIT** — the inherited conversation must fit the candidate's context window with roughly 30%
  growth headroom plus its output budget, or the assembler would silently drop the oldest messages.
- **COST** — a model this session has never used holds no cache for this conversation and re-reads
  all of it as uncached input on its first request. That is affordable under `coldStartCapTokens`
  (25,000 tokens) regardless of warmth, and affordable at any size when the candidate is already
  **warm** — this session has already sent it the conversation at its current revision. Otherwise
  the proposal is declined and the current model, whose cache already holds the prefix, keeps the
  task.

Warmth is a per-session ledger (`Engine.warmPrefix`: model id → history revision), written only
when a main-stream request completes, so the advisor's own call and other auxiliary traffic never
count as warming a model. A compaction, a pressure trim, or a completed-task fold rewrites earlier
messages in place and retires every model's recorded warmth with it, since their cached prefixes
now describe a conversation that no longer exists. A resumed session starts with an empty warm
ledger and treats every model as cold — the conservative choice for a resume. The advisor's prompt
is shown the cold-start figure and the warm-model list under a `SWITCH COST` heading, and the same
list is what the `/context` card's "Warm:" row reads from (see *Reading the metrics* below).

An unavailable, malformed, or unknown-id advisor answer keeps the current model and records a
recovery event, same as a FIT or COST decline. Genuinely different large work arriving mid-session
that shares nothing with what the session has already done gets a separate, one-line advisory
instead — that `/new` would give it a clean start — because a different model bolted onto a
conversation still full of unrelated history saves nothing; the model choice and the conversation's
cache are different problems. To take manual control, `muhiyacode config set model <id>` pins the
model (pinning disables the advisor outright), and `config set advisor off` disables it directly. A
24h TTL refresh keeps the gateway model catalog current so the advisor and the utility-model
resolution are choosing from real entries.

## The cached prefix (the unified-session epoch)

The stable prefix is the system prompt plus the serialized tool definitions, composed once per
session. The current prompt sections are: OPERATING CONTRACT, CONTEXT AND EDIT DISCIPLINE, CACHE
DISCIPLINE, PLANNING, TOOLS AND RECOVERY, MODEL, COMMUNICATION, SAFETY, ENVIRONMENT. `DELEGATION`
is gone; `MODEL` takes its slot — a short, session-invariant section telling the model that its
model may change between tasks, that it must never narrate or request a switch, and that every
earlier turn is its own regardless of which model produced it.

Relative to v1.1.0 this epoch removed the subagent system entirely — dispatch, the plan/execute
role gate, and context linking between delegated runs — so the one remaining session got back
everything the execution agent used to be told alone: the `edit_file`/`multi_edit` contract, the
`write_file` permission rule, the chunked-write rule, and the preserve-user-work clauses, now
folded into `CONTEXT AND EDIT DISCIPLINE` for the single model that both reads and edits. The
system prompt grew accordingly — +708 characters, to 6,101 against a 6,120 regression ceiling —
but the fixed prefix it rides beside did not: prompt plus tool JSON fell from 24,393 to 21,908
bytes (-10.2%), because deleting `run_subagent`'s schema (its description plus the task/role/skills
property texts) cut the tool JSON by 3,214 bytes, more than paying for the prompt's growth. The
reasoning is recorded in `prompt_budget_test.go` rather than in the commit alone, because a ratchet
that rises without a stated reason is not a ratchet.

The old two-way trade no longer applies the same way: a "trust the report" contract rule paid for
by deleting the `DELEGATION` prose it replaced assumed a second party's report to trust, and there
is none now. `OPERATING CONTRACT` rule 5 states verification directly instead: run the check that
proves a change works, then tick its item; never report a result you did not observe. `PLANNING`
step 3's executor-ready standard is unchanged in substance — an item should still name its files,
its change, and its check well enough to run without further design decisions — even though the
model writing the plan and the model carrying it out are now, always, the same one.

Two things deliberately stay **out** of the prefix:

- **`tasks.md`** — the model's multi-step checklist is written with ordinary file tools, usually at
  the workspace root or in the directory the work targets. It changes constantly; injecting it
  would invalidate the prefix every turn. The harness instead observes writes to that path in the
  dispatch gate, re-parses it, and feeds the to-do panel. The model reads it on demand like any
  other file.
- **Per-turn dynamics** — date, task class, and its tool/turn budgets still ride the newest user
  message.

Resumed sessions pay one attributed cold start the first time they run past this upgrade, then the
new prefix is byte-stable.

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

`/context` is an essentials card (013 FR-023): context in-use/free, session prompt and output
totals, cache-read and uncached input, the session hit rate, session cost, and a `Model` group. The
`Model` group always shows a `Running:` row for the model handling the current task, and — only
when more than one model is warm — a `Warm:` row listing every model this session has already sent
the conversation to, most recently used first (see *Task-stable model* above). Every token figure
is a complete, comma-grouped count — cached tokens included, never abbreviated. The diagnostics it
used to carry (per-model and per-pairing rows, the estimated category split, invalidation history,
pressure internals, API/active time, lines±, the steady-state rate) remain available to benchmark
tooling through `ContextReport` and the usage ledger; they are simply no longer rendered.

**The displayed session hit rate spans every stream** — main loop and auxiliary calls (013 FR-021,
`SessionUsageAggregate.AllStreamHitRate`). The main-stream-only `SessionHitRate` and the
steady-state rate keep their existing definitions as benchmark KPIs so historical comparisons stay
meaningful, but neither is shown to the user.

The activity line shows the task's complete token count plus a percentage-only tag such as
`cache 99%`. `unavailable` means the endpoint did not provide trustworthy cache figures; it is
never treated as zero.

### Task-lifecycle usage and rate honesty (C6/C7)

The live activity line and the end-of-task summary describe the **whole current task**, not the
last request. After every recorded request the engine emits the task-cumulative usage —
`Σ(usage)` over every request the task made across the main and aux (onboarding, the task advisor,
compaction) streams, measured from a baseline snapshot taken at task start. The live cache tag and
the summary's `cache %` therefore always agree by construction, whether the task ran on one model
throughout or the advisor moved it to a different one at the task boundary.

Rate arithmetic — every rate, including the all-stream rate shown in `/context` **and** the
per-task delta the summary divides — draws only on records that reported **both** cache operands
(the paired sums). A
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
  (`:main` for the loop + compaction, `:sub` for onboarding and the task advisor) so the gateway
  keeps a session pinned to one model; DeepSeek's cache is per-model, so a route flip would wipe it.

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
