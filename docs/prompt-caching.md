# Prompt caching

MuhiyaCode keeps the system prompt, tool schemas, and settled conversation history byte-stable
across a session. Per-turn details such as the date, task class, and its tool/turn budgets are
appended to the latest user message. MCP schemas are pinned at the session boundary; a changed MCP configuration
is applied at the next deliberate boundary and recorded as an invalidation event.

Use a concrete model such as `deepseek-v4-flash` instead of a router that may choose a different
backing model per request. Model changes intentionally start a new provider cache scope — which is
why the session's models are frozen once work begins (see "Session-stable models" below).

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
- Subagent runs, per capability class: `<sessionID>:sub:explore`, `:sub:general`, `:sub:review`
- Onboarding: `<sessionID>:sub:onboarding`
- Session advisor: `<sessionID>:sub:advisor` (one utility-model call on the session's first
  prompt; ledgers under the `:sub:advisor` label)
- Compaction: `<sessionID>:main` on the wire (same `ActiveModelID` as main — shares the routing
  pin) while its usage record ledgers under the `:aux` label — wire pins and ledger pins are not
  1:1 for this stream
- There is no `:aux` wire pin: task classification is a local heuristic and sends no request

There is no `:sub:plan` pin: v1.1.0 removed the planning pipeline and its dedicated agent kind.
Planning is now a section of the main model's cached prefix, not a delegated run. The three
capability classes above are the complete set.

The value is header-only; it is never serialized into the JSON request body. Two consecutive
requests of the same stream carry an identical header, and the per-kind suffixes keep each
kind's prefix-cache identity independent.

`run_subagent` accepts an optional `role` ("auth-flow-mapper") that names a run in the transcript,
but the pin, the per-kind system message, and the context record's `Kind` all stay keyed on the
fixed capability class. A free-form name is display and handoff only, so it can never fragment the
provider cache into one namespace per invented role.

## Session-stable models (v1.1.0)

Three roles carry a session: MAIN plans, analyzes, and instructs (default MiniMax M3); EXECUTION
makes every workspace change from inside subagents (default DeepSeek V4 Pro); UTILITY serves cheap
auxiliary calls such as the advisor and onboarding (DeepSeek V4 Flash, resolved from the catalog by
name with a fallback to the configured subagent model). There is no `/model` command and no model
name in the TUI chrome — the user does not manage models.

The roles are **frozen for the session** once work begins. A mid-session switch would pay twice: it
cold-starts the main prefix under a new provider cache scope, and it fails the context linker's
stream-identity check (`model-changed`), collapsing the execution chain to a digest-seeded start.
Freezing is what lets both the main prefix and the session-long execution chain hold for a whole
session rather than only until the next model decision.

A session advisor enforces that mechanically. It runs at most once, on the session's first prompt,
before the first main request — the only moment when a switch is free because nothing is cached
yet. It runs on the utility model, is bounded to a small JSON answer, and its expected outcome is
to keep the configured pairing. An unavailable, malformed, or unknown-id answer keeps the
configured models and records a recovery event. Every later task in the session is a hard no.

Genuinely different large work arriving mid-session gets a one-line advisory that `/new` would give
it a clean start; nothing switches underneath a warm session. To take manual control,
`muhiyacode config set model <id>` and `config set subagentModel <id>` pin the roles (the advisor
proposes, it never overrides an explicit choice), and `config set advisor off` disables it
entirely. A 24h TTL refresh keeps the gateway model catalog current so the advisor and the
resolvers are choosing from real entries.

## The cached prefix (v1.1.0 epoch)

The stable prefix is the system prompt plus the serialized tool definitions, composed once per
session. The v1.1.0 prompt sections are: OPERATING CONTRACT, CONTEXT AND EDIT DISCIPLINE, CACHE
DISCIPLINE, PLANNING, TOOLS AND RECOVERY, DELEGATION, COMMUNICATION, SAFETY, ENVIRONMENT.

Relative to v1.0.6 this epoch removed the planning-pipeline prose and its tools (`update_plan`,
`exit_plan_mode`, `read_plan`) and added a plan/execute contract, a `tasks.md` checklist
convention, the PLANNING section, and the field-test fixes below. It still came out smaller: the
system prompt went 5777 → 5246 chars and the tool JSON 20317 → 18578 bytes. A compile-time ratchet
caps the prompt at 5700 chars and a wire golden pins the exact prefix bytes, so every addition has
to be paid for by tightening something else.

The ratchet moved once inside this release, 5440 → 5700, tracking a +257-char growth in the
composed prompt (5428 → 5685 as measured with web + subagents available; the wire golden composes
a few less without web). The reasoning is recorded in `prompt_budget_test.go` rather than in the
commit alone, because a ratchet that rises without a stated reason is not a ratchet. Those chars
buy exactly two things, both of which remove *recurring per-task* waste at a *one-time* prefix
cost:

- **Trust the report (contract rule 5).** The field test exposed a contradiction — the contract
  said "run the checks yourself" while DELEGATION said "treat its report as ground truth" — and
  the model resolved it by re-reading, on the main stream, every file an agent had just verified.
  A one-time prompt cost that deletes a per-task re-verification loop is the cheapest trade
  available at this size. `CACHE DISCIPLINE` now names agent reports as current truth alongside
  past reads, searches, and edit diffs, so re-checking a verified report is cache waste by
  definition.
- **The executor-ready standard (PLANNING step 3).** A `tasks.md` item must be runnable by the
  cheaper executor without further design decisions; an under-specified item costs far more in
  executor turns than the sentence costs in prefix.

Paid for in part by deleting the old rule 5 (it duplicated DELEGATION), the agent-allowance
clauses, and handoff detail that was stated twice.

Two things deliberately stay **out** of the prefix:

- **`tasks.md`** — the model's multi-step checklist lives at the workspace root and is written with
  ordinary file tools. It changes constantly; injecting it would invalidate the prefix every turn.
  The harness instead observes writes to that path in the shared dispatch gate (so main-loop and
  subagent edits both count), re-parses it, and feeds the to-do panel. The model reads it on
  demand like any other file.
- **Per-turn dynamics** — date, task class, and its tool/turn budgets still ride the newest user
  message. The brief carries no subagent count: delegation scale is the model's judgment, so there
  is no allowance to render, spend down, or keep consistent with an enforced cap.

Resumed sessions pay one attributed cold start the first time they run on v1.1.0, then the new
prefix is byte-stable.

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

### The session-long execution chain

CL-1 originally required a relatedness predicate before continuing a chain across task
boundaries. v1.1.0 drops that bar for the **execution** class only: a `general` dispatch continues
the session's most recent linkable `general` record across tasks unconditionally, and the decision
is recorded with reason `session-chain` instead of `eligible`.

The justification is the plan/execute split. The main model no longer changes files, so `general`
makes every workspace change in the session — the workspace is its shared subject by construction,
and the session-frozen executor model keeps stream identity intact for the whole session. That is
what lets one warm sub-agent context survive across prompts rather than being rebuilt per task.

`explore` and `review` keep the relatedness requirement. Research context is topic-specific, and
carrying an unrelated investigation forward pollutes the reasoning instead of saving tokens.

Every other CL-1 criterion is unchanged and still evaluated in order for a cross-task chain: kind
pair, terminal shape, model identity, pin derivation, staleness against end-of-run fingerprints,
window fit, and provider support. Any failure falls back to a digest-seeded start with the failing
criterion as the reason, exactly as before. The distinct `session-chain` reason exists so the
ledger and bench JSON can measure cross-task reuse rather than assume it.

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
