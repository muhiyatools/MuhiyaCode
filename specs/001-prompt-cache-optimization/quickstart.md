# Quickstart: Validating Prompt Cache Optimization

**Feature**: `001-prompt-cache-optimization` | **Date**: 2026-07-11
Validation scenarios proving the feature end-to-end. References:
[contracts/wire-request.md](contracts/wire-request.md),
[contracts/cache-metrics.md](contracts/cache-metrics.md),
[contracts/invalidation-events.md](contracts/invalidation-events.md),
[data-model.md](data-model.md).

## Prerequisites

- Go 1.25+ on PATH; repository builds: `go build -trimpath ./cmd/muhiyacode`
- For live runs (V4–V6): a MuhiyaLLM gateway API key and a **pinned concrete model**
  (e.g., `deepseek-v4-pro`) — never `muhiya-ai-router` (documented cache-fragmenting
  limitation). Same model, effort, and workspace fixture for every arm.
- Recommended before implementation starts: `git init` + an initial commit, so the baseline
  build is reproducible from a tagged tree (the repo is not yet under version control).

## V0 — Existing quality gates (FR-012 / SC-006, run constantly)

```sh
go mod verify
go vet ./...
go test ./... -count=1
```

Expected: 100% pass, including the pre-existing prompt-ceiling and stability gates. Any
failure blocks (Constitution I).

## V1 — Offline determinism & prefix stability (SC-002, W7–W9)

```sh
go test ./internal/orchestrator/ -run 'PromptStability|RestartDeterminism' -count=1 -v
go test ./internal/gateway/ -run 'MarshalDeterminism|RetryReplay' -count=1 -v
```

Expected:

- System prompt bytes identical across double-build (golden).
- Full request bytes identical across build → persist → reload → rebuild (restart
  determinism; covers date, probe snapshot, MCP pinning fixes).
- Retry path re-marshals byte-identically.

## V2 — Mock-endpoint cache guard (SC-001 proxy, offline)

```sh
go test ./internal/orchestrator/ -run 'CacheHitGuard' -count=1 -v
```

The mock endpoint computes hit tokens as the byte-identical common prefix with the previous
request (measures client stability only). Expected: every scenario (plain dialogue, tool
loops, MCP-pinned surface, restart replay, steering, goal continuation, pressure maintenance)
reports tail-average ≥ 90% guard threshold; scenarios without maintenance events report a
strict prefix-of relationship between consecutive requests (contract W1); zero PrefixShape
diffs without matching InvalidationEvents.

## V3 — Metrics honesty (SC-004, offline)

```sh
go test ./internal/gateway/ -run 'UsageParsing' -count=1 -v
```

Expected: DeepSeek top-level and OpenAI nested shapes parse to the exact reported values;
absent fields yield nulls that display as "unavailable" (never zero-as-fact); derived misses
flagged `miss_derived`; malformed usage never fails a request.

## V4 — Baseline capture (FR-010; run BEFORE behavior changes land)

Order of work: W1 (measurement) merges first; capture baseline with W1's accounting on the
otherwise-unmodified agent; only then merge W2–W5. If W1's own capture is disputed, the
harness's raw provider-payload log is the ground truth for both arms.

```sh
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label baseline \
  -out "specs/001-prompt-cache-optimization/benchmarks/baseline"
```

Expected artifacts: per-run JSON (per-request read/miss/output verbatim, both hit rates,
latency, derived cost + price source, `unattributed_misses`). Baseline is expected to show the
documented defects (e.g., hit-rate collapse on MCP sessions, cross-day resume, long-session
maintenance thrash).

## V5 — Post-implementation run + comparison (SC-001, SC-003, SC-005, SC-007)

```sh
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label improved \
  -out "specs/001-prompt-cache-optimization/benchmarks/improved"
go run ./benchmarks/cachebench -compare \
  "specs/001-prompt-cache-optimization/benchmarks/baseline" \
  "specs/001-prompt-cache-optimization/benchmarks/improved"
```

Acceptance (identical model/effort/workspace across arms):

- `steady_state_hit_rate ≥ 0.99` on every improved run for cache-eligible requests (SC-001);
  baseline below it.
- Steady-state uncached input per turn ≈ the turn's genuinely new tail (SC-003).
- Run-to-run variance ≤ 1 percentage point (SC-005).
- `unattributed_misses = 0` (SC-007); provider-caused misses appear with evidence in the
  limitations register.
- Comparison report states total-cost delta and is stored with the artifacts.

## V6 — Live UX spot-checks (US1/US2 acceptance scenarios)

1. Start a session with an MCP server configured (e.g., a stdio server with slow startup).
   Send 3 turns. Expected: tools identical from turn 1 (pinned surface); `/context` shows
   steady-state hit rate ≥ 99% from request 2 on; no `toolset-change` events.
2. Run `/mcp` list and test actions mid-session. Expected: zero invalidation events, no
   hit-rate dip.
3. Resume yesterday's session (or fake the date boundary). Expected: byte-identical rebuild —
   no `prompt-rebuild` event, and any miss attributes to `provider`/`cold-start`, not `agent`.
4. Trigger `/compact`. Expected: exactly one `user-compact` event; next request re-stabilizes
   (shape stable), `/context` annotates the cause.
5. Point at an endpoint without cache reporting. Expected: full functionality; cache metrics
   render "unavailable" (FR-013).
6. Edit a file outside the agent, then ask the agent about it. Expected: fresh content is
   read (coverage revoked) — correctness beats caching (FR-012).

## V7 — Documentation gates (FR-009, FR-011; Constitution workflow)

- `docs/prompt-caching.md` exists: cache guide + limitations register populated with benchmark
  evidence (cold start, TTL/eviction, 64-token block granularity, per-model scoping, router
  fragmentation, reporting variance, thinking-mode replay).
- README / docs/agent-design.md / docs/architecture.md updated where they describe changed
  behavior (e.g., the "within a task" prefix-stability invariant strengthens to
  session-scoped-with-attributable-events).
- research.md Part C matrix reflects any implementation-time deviations.

## Troubleshooting

- **Misses despite stable shape** → provider TTL/eviction (attribution `provider`); verify via
  the harness's identical-bytes assertion, then record in the register — not a code defect.
- **Guard test fails after a change** → read the PrefixShape reasons in the failure output;
  the region named (`system`/`tools`/`rewrite`) points at the offending diff.
- **Baseline vs improved dispute** → both arms' raw provider payload logs are canonical; never
  compare against estimated figures (Constitution VI).
