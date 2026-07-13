# Code Review Findings: Prompt Cache Optimization (feature 001)

**Reviewer**: Claude (full-feature audit) | **Date**: 2026-07-11
**Reviewed build**: branch `001-prompt-cache-optimization`, HEAD `04c8a2b` (behavior commit `695c3ce`)
**Audience**: GPT-5.6 Sol — each finding carries a REQUIRED FIX and ACCEPTANCE criteria. Work the
findings in ID order; F1–F5 are the payload, F6 makes the proof honest, F7–F10 are latent bugs,
F11 is cleanup.

---

## Executive summary

The implementation is well built where it was pointed: measurement plumbing (T004–T012),
MCP pinning, probe snapshot, date removal, maintenance gating, retry/resume determinism are
compliant with the contracts and unit-tested. The live target still failed — improved
**96.5602%** vs baseline **96.5699%** (a 0.01pp regression) — for two independent reasons the
sign-off conflates:

1. **A real, previously-unknown cache-buster survived: the landing request flips
   `tool_choice` to `"none"` for exactly one request** (engine.go:554-557). Tool definitions
   are rendered into the prompt by the provider/gateway, so that single request re-renders the
   prompt (observed: `prompt_tokens` *shrinks* at seq 37/44/55/71/81 in improved run-01 while
   history is provably append-only), collapses cache reads by 2.5–4.7K tokens, and the stream
   reverts on the next request. Cost: **~0.9–1.2pp in every run, in both arms** — the
   improvement commit never touched it, which is why baseline ≈ improved.
2. **The success metric was never implemented as specified.** SC-001's wording — "99–100% of
   *previously transmitted* stable context tokens served as cache reads" — is a
   prefix-stability rate that excludes each request's brand-new tail from the denominator. The
   harness instead gates on raw `Σread/Σprompt`, whose mathematical ceiling on the current
   4KB-fixture workload is **~97.65%** (new-tail 2.01pp + cold start 0.15pp + 64-token block
   quantization 0.18pp). **99% raw is unreachable on this fixture no matter what the client
   does.** Computed correctly, prefix-stability is **already 98.56%** and reaches
   **~99.4–99.5% once F1 is fixed** — which is the 99–100% the spec (and Reasonix comparisons)
   actually mean.

The reason F1 shipped undetected is systemic and must be fixed with it: **PrefixShape hashes
only {system, tools array, rewrite-version int, model}** — not `tool_choice`, not the actual
outbound bytes — so the busts produced `prefix_changed=false`; the attribution algorithm then
**defaults every unexplained miss to `provider`** (the contract's "miss > new-tail" bound was
never implemented), making `unattributed_misses = 0` vacuously true; and the offline
CacheHitGuard **averages soft ratios and explicitly `continue`s past shrinking requests**, so
the exact failure signature is excluded from the gate.

Why the user sees 94–96% on their gateway and never 97+: same F1 busts + the same raw-metric
tail math on real workloads (bigger uncached tails → lower raw ceiling). After F1–F5, expect
**prefix-stability 99–100%** wherever the gateway/provider caches; the *raw* rate will follow
workload shape (fat contexts ≈ 98–99%+, chatty small turns less) — see F4 for both numbers and
the gateway-side check in F10 that may recover more.

---

## Evidence base

- Three independent audits: (A) code trace of every history-mutation path, (B) contract W1–W12
  / task T015–T033 compliance + test-gap analysis, (C) benchmark harness review + full-token
  recomputation of all six committed runs (numbers reconcile exactly with
  `benchmarks/baseline/summary.md`).
- Direct verification of the F1 mechanism in `internal/orchestrator/engine.go:505-579` and
  `internal/gateway/provider.go:115-212`.

### The numbers (recomputed from committed runs; pooled = 6 runs)

```text
run   reqs  Σprompt    Σread      Σmiss   raw%    steady%  ceiling%  prefixStab%  shrinkBusts
bas-1  94  1,617,618  1,562,240  55,378  96.577  96.747   97.744    98.618        4
bas-2  86  1,409,135  1,356,928  52,207  96.295  96.454   97.536    98.530        5
bas-3  97  1,688,600  1,627,392  61,208  96.375  96.508   97.640    98.519        7
imp-1 100  2,043,187  1,976,192  66,995  96.721  96.831   97.901    98.637        5
imp-2  87  1,343,560  1,292,672  50,888  96.212  96.379   97.469    98.501        5
imp-3  90  1,698,022  1,635,840  62,182  96.338  96.470   97.581    98.554        5

Pooled miss decomposition (pp of Σprompt = 9,800,122):
  cold start 0.149 | new tail (unavoidable) 2.008 | shrink busts (CLIENT BUG) 0.905
  | growth residual (quantization + growth-side busts) 0.498
Raw-rate ceiling with a perfect client on this fixture: ~97.65% mean → 99% raw UNREACHABLE.
SC-001 prefix-stability metric: 98.56% today → ~99.4–99.5% with F1 fixed.
```

Decisive against "provider eviction": max inter-request gap in all six runs is 7.8s (no TTL
plausibly applies), and on the shrink events the client *sent fewer prompt tokens* — a
provider cannot shrink what you send.

---

## Findings

### F1 — CRITICAL: landing requests flip `tool_choice`, re-rendering the prompt for one request

**Where**: `internal/orchestrator/engine.go:554-557` (`toolChoice := "auto"; if isFinal {
toolChoice = "none" }`), sent at `engine.go:561`; body field emitted at
`internal/gateway/provider.go:126-128`.

**What happens**: tool schemas are rendered into the provider-side prompt. With
`tool_choice:"none"` the rendered prompt for that one request loses/changes the tool block:
`prompt_tokens` shrinks (seq 37: 13221→13003 despite ~300–500 tokens of appended history),
cache read collapses to the divergence point, and the next request (back to `"auto"`) restores
the original rendering (seq 38 read 13056 > seq 37's whole prompt 13003 — proof of a
one-request fork). Happens on the landing request of every turn-capped task — in this
benchmark, the long editing turns; in real gateway sessions, frequently.

**Required fix**: `tool_choice` (like `tools`) is part of the rendered prefix on this
provider family — treat it as **R2: session-constant**. Remove the flip; keep `"auto"` on the
landing request. Landing is already enforced behaviorally by the appended governor message
(engine.go:508-510); additionally hard-enforce client-side: if the landing response contains
tool calls, discard them and re-prompt once / synthesize the final answer path — never via a
per-request parameter change. Update `contracts/wire-request.md` W6 to say explicitly:
*"`tools` and `tool_choice` alter the provider-rendered prompt and are R2 — session-constant
absent a recorded toolset-change event."*

**Acceptance**: in a fresh benchmark run, zero requests with `prompt_tokens` lower than the
previous request; zero read-collapses >1 block at task boundaries; per-run shrinkBusts = 0;
prefix-stability ≥ 99.4%.

### F2 — CRITICAL: PrefixShape cannot see the wire — hash the outbound bytes

**Where**: `internal/orchestrator/prefixshape.go:19-46` ({SystemHash, ToolsHash,
RewriteVersion int, ModelID}); computed pre-serialization at `engine.go:546`; gateway mutates
messages afterwards (`replayMessages`, provider.go:199-212) and `tool_choice` is not hashed at
all.

**What happens**: the settled conversation (R3) is represented by one integer; anything that
changes bytes without bumping it — F1's parameter flip, `replayMessages`' conditional
reasoning keys (F7), any future bug — is invisible, so `prefix_changed=false` and misses
default to `provider`.

**Required fix**: compute the shape from what is actually sent. After the request body is
final (post-`replayMessages`), hash: (a) the serialized system message, (b) serialized
`tools` **plus `tool_choice` and any other render-affecting params**, (c) the serialized
settled messages (everything except the current turn's fresh tail) as `HistoryHash`, (d)
model ID. Keep RewriteVersion as a diagnostic label, not the detector. Also maintain
last-sent length + rolling hash and assert each request is a byte-prefix-superset of the
previous send absent a ledger event (this is invariant W1 made executable).

**Acceptance**: a test that mutates one settled message byte (or flips tool_choice) without an
event → `CompareShape` returns a reason and the guard fails. Re-running the improved arm's
request stream through the new shape flags seq 37/44/55/71/81 as `agent`.

### F3 — CRITICAL: attribution defaults to `provider`; the contract's bound is unimplemented

**Where**: `engine.go:1112-1125` (unconditional `default: provider`), mirrored at
`engine.go:1156-1157`; `benchmarks/cachebench/main.go:326-347` counts `provider` as
"attributed", so `unattributed_misses` is structurally 0. `data-model.md:28` and
`contracts/invalidation-events.md:63-65` require a "miss > expected new-tail" bound that no
code implements.

**Required fix**: implement the bound and two sanity detectors — (1) prompt-shrink:
`prompt_tokens(N) < prompt_tokens(N−1)` with no window/compact/fold/trim event ⇒ label
`agent-suspect`, never `provider`; (2) read-regression: `cache_read(N) <
floor(prompt(N−1)/64)·64 − 2 blocks` while messages grew and no event ⇒ `agent-suspect`;
(3) `provider` only when the shape is unchanged AND miss ≤ new-tail-estimate + tolerance;
anything else above the bound counts as unattributed in cachebench.

**Acceptance**: replaying the shipped improved run-01 records through the new logic yields
unattributed/agent-suspect > 0 (it must fail on the old data); after F1, a fresh run yields 0.

### F4 — HIGH: implement the SC-001 metric (prefix-stability); stop gating on a number with a 97.6% ceiling

**Where**: `internal/contract/cache.go:100-107` (steady-state = raw rate excluding only
cold-start), `contracts/cache-metrics.md:35` (mislabels it "the SC-001 acceptance figure"),
`benchmarks/cachebench/main.go:579-582`.

**Required fix**: add `prefix_stability_rate = Σ cache_read / Σ (prompt_tokens − new_tail)`
over requests n≥2, where `new_tail = max(0, prompt_n − prompt_{n−1})` (use the byte-accurate
tail when F2's request log lands). Report it per run alongside the existing raw rates
(keep raw as the cost KPI). Evaluate SC-001 on prefix-stability ≥ 0.99. Update
`cache-metrics.md`, `data-model.md` §2, and the spec's SC-001 note accordingly, and document
the raw-rate ceiling math in `docs/prompt-caching.md`'s limitations register (new-tail share,
64-token quantization, cold start).

**Acceptance**: comparison output shows both metrics; SC-001 judged on prefix-stability;
current fixture passes ≥0.99 after F1; the report explains raw% vs stability% in one line.

### F5 — HIGH: the CacheHitGuard is structurally unable to catch these bugs

**Where**: `internal/orchestrator/cachehit_guard_test.go` — soft tail-average ≥90%
(line ~60), `continue` on message-count shrink (lines ~80-81), no strict prefix assertion, no
bytes-vs-shape cross-check, 2–4-prompt toy scenarios, maintenance only ever force-injected
(`maintenance_test.go:25`).

**Required fix**: (1) assert for every consecutive request pair: previous serialized request
(system+tools+params+messages) is a byte-exact prefix of the current one UNLESS a ledger event
exists for that seq — shrink is a hard FAIL, not a skip; (2) recompute a content hash of the
settled prefix per request and assert it changes only with a ledger event (validates F2
independently); (3) add a realistic multi-task scenario replaying the cachebench workload
shape offline (reads, edits, re-reads, turn-capped tasks hitting the landing path, a
governor-notice mid-task, an /mcp add at a boundary) against the byte-accurate mock;
(4) keep the ≥90% average as a secondary signal only.

**Acceptance**: the new guard FAILS on commit `04c8a2b` (catching F1) and PASSES after the F1
fix. Guard runs in `go test ./...` as before.

### F6 — HIGH: benchmark honesty — SC-005 logic, aux leakage, fixture realism, artifact hygiene

**Where**: `benchmarks/cachebench/main.go:583` (SC-005 = variance-only → `comparison.md`
printed `SC-005 true` for a regression while `signoff.md` says FAIL — two committed artifacts
contradict); `contract/cache.go:92-104` (aux `n/a` records with cache fields leak into
steady-state); fixture ≈ 4KB with mean tail 380 tokens vs the plan's "realistic" requirement
and Reasonix's 20×12KB seeding; raw-usage JSONL is 99.3% `payload:null` lines with an fsync
per line (`main.go:438`, `provider.go:171-177`); steady-state excludes only seq 1 (not
resume/eviction cold points).

**Required fix**: (1) SC-005 = improvement clause (improved meets target, baseline doesn't)
AND variance ≤1pp — never true on a regression; (2) exclude `attribution=n/a` from all rate
denominators; (3) add a fat-context scenario (seed ~40–60K tokens of stable fixture content à
la Reasonix) so the raw metric has headroom ≥99%, keep the current scenario for tail-math
regression; (4) log raw usage only for lines that contain a non-null usage object and assert
exactly one per request; buffer writes; (5) regenerate `comparison.md`/`signoff.md` after
fixes so no committed artifacts contradict each other.

**Acceptance**: rerun produces internally consistent artifacts; fat scenario raw ≥99% and
prefix-stability ≥99.4% on the fixed build; `n/a` requests visibly excluded.

### F7 — MEDIUM (latent): `replayMessages` couples settled bytes to the live reasoning param

**Where**: `internal/gateway/provider.go:205-209` — empty `reasoning_content` key injected on
DeepSeek assistant tool-call turns only when `reasoning != ""`. `SetEffort`
(`engine.go:187-191`, reachable from the TUI) changes reasoning with no event; a tier mapping
to `""` (or a family change) would rewrite every settled assistant tool-call message at once,
undetected (W6 latent breach; today masked because `ReasoningForEffort` never returns `""`).

**Required fix**: freeze the decision per message at append time (store "needs empty
reasoning key" as message metadata when the assistant message is created) so replay is a pure
function of the message, not of live settings. F2's HistoryHash must cover the post-replay
bytes so any residual coupling is at least visible; record an invalidation event on any
effort/profile change that would alter replay output.

**Acceptance**: unit test — flip effort mid-session; outbound settled bytes are unchanged (or
a recorded event exists); shape stays stable.

### F8 — MEDIUM (latent): aux/subagent usage pollutes the shared seq stream and rates

**Where**: `engine.go:1138-1143` (`recordAuxUsage`), `subagent.go:143`
(`recordIsolatedUsage`) — same `requestSeq` counter and `usageRecords` stream as the main
loop; at effort ≥ medium, classifier/summarizer/subagent requests (different prefixes) would
inject foreign cold-start/miss records into the session's SC-001 figures.

**Required fix**: per-stream usage records (main vs aux vs per-subagent) or a `stream` field
on UsageRecord + rate computation over `stream=main` only (F6's `n/a` exclusion covers part;
subagent isolated records need their own bucket). Keep aggregates per stream in `/context`.

**Acceptance**: a medium-effort session with one subagent shows main-session prefix-stability
unaffected by the subagent's cold start; totals still reconcile.

### F9 — MEDIUM (latent): estimate-driven window-drop below the 0.60 floor errors the task

**Where**: `history.go:318-339` (window from `len/4` estimate, independent of pressure) +
`engine.go:535-544` (records `window-drop` with `trigger=pressure`) +
`invalidation.go:112-119` (validation rejects pressure events < 0.60 with an error that
propagates at engine.go:541-543).

**Required fix**: size the window from provider-reported tokens once available (same source
as maintenance pressure); if a drop legitimately occurs below the floor (estimator bootstrap),
record it with a distinct trigger (`boundary`/`bootstrap`) instead of failing the task.

**Acceptance**: unit test with inflated estimator + low reported pressure completes the task
and records one window-drop event.

### F10 — VERIFY IN GATEWAY (user action): tool-block render position

The intra-task read plateaus (e.g., improved run-01 seq 13–14 stuck at 5120 → full recovery at
seq 15) and the ~3–5K-from-tail divergence geometry suggest the gateway renders the tool
definition block at a position tied to the **latest user message / end of history** rather
than a fixed position after the system message. Mid-task user-role appends (governor notices,
steering) would then move the block and re-render everything after it. **Action (user's Go
gateway)**: render tools at a fixed position immediately after the system message, byte-stable
across requests of a session, and keep the rendering independent of `tool_choice` where the
upstream API allows. This is worth several tenths of a pp on top of F1 and likely more on real
sessions. If the gateway forwards `tools` verbatim to DeepSeek, this finding is informational
(DeepSeek's own template applies) — F1 remains the client-side fix either way.

### F11 — LOW: cleanup and record-keeping

1. `signoff.md:14` "residual misses are provider-attributed … PASS within client control" is
   wrong (they are F1 busts); `signoff.md:21` "requires a provider/cache-policy change" is
   incomplete (raw-metric tail math + F1). Rewrite after fixes with the F4 metrics.
2. `research.md:128-130` ("provider-attributed rather than an unexplained implementation
   defect") is superseded by F1 — update Part C/E accordingly (per T043's standing rule).
3. `tasks.md` T039/T040 reference `usage_integrity_test.go`/`usage_resume_test.go`; the
   functionality lives in `command/application_test.go:202` and
   `orchestrator/usage_record_test.go:10` — fix the references or rename the tests.
4. `TestEngineAttributesToolShapeChangeToAgent` (`usage_record_test.go:78`) creates an
   agent-change-without-event state and asserts nothing about the ledger — extend it to
   assert the guard/contract behavior (post-F2 it should require an event).
5. `restart_determinism_test.go` doesn't exercise the disk `ToolSurfaceSnapshot` or
   `ProbeSnapshot` reload paths it exists to cover (T031/W9) — add both.
6. Update `docs/prompt-caching.md`: add the raw-vs-stability metric explanation, the
   `tool_choice` invariant, and the gateway render-position guidance (F10).

---

## What was verified as correct (no action)

R1 purity (no date/time; `prompt_stability_test` enforces), R2 MCP pinning end-to-end
(toolcache fingerprint with sorted env keys, pin-before-first-request, disk-only handshake
updates, read-only `/mcp` untouched, boundary-applied changes with events), probe snapshot
last-good merge, date-in-brief (settled once, byte-stable), maintenance floor/latch/single-
pass consolidation (unit level), usage parsing precedence + null/zero honesty + no clamping,
usage.jsonl + invalidations.jsonl append-only persistence and resume-time aggregate rebuild,
display honesty ("unavailable" ≠ 0), retry byte-identity, marshal determinism, model-switch
events, subagent isolation (own R1/R2/shape). Baseline/summary artifacts reconcile exactly
with independent recomputation — no fabricated numbers anywhere.

## Expected outcomes after F1–F6

- Offline: new guard red on `04c8a2b`, green after F1; shape/attribution tests catch synthetic
  mutations.
- This fixture: prefix-stability ≈ 99.4–99.5% (SC-001 PASS as specified); raw ≈ 97.4–97.65%
  (at its ceiling — document, don't chase); fat-context scenario raw ≥ 99%.
- User's gateway sessions: expect ~97–99% raw depending on tail share once F1 (+F10 if
  applicable) lands, with prefix-stability at 99–100% — the honest "Reasonix-class" claim.
