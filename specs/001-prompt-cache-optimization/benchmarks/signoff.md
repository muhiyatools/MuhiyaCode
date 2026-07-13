# Prompt-cache optimization sign-off

Review-fix sweep executed 2026-07-11. Offline implementation verification is complete. Live
SC-001/SC-005/SC-007 remain pending because the committed runs predate the corrected request,
metric, and attribution logic; this file does not reinterpret them as post-fix evidence.

## OVERHAUL_PLAN phase status

All seven phases of `OVERHAUL_PLAN.md` have been implemented and unit-tested:

- **Phase 1 (Cache)** — C1–C5: session routing pin, prefix-shape guard, subagent shape guard,
  anti-thrash fold latch, compaction-failure role fix. Code complete, tests pass.
- **Phase 2 (Goals)** — G1–G6: marker scanning on every assistant text, completed-goal removal
  from active state, plan⇄goal mutual exclusion, **G4 goal.json sidecar persistence** (restore
  active goals on resume), per-task auto-turn counter, replacement notice. Code complete.
- **Phase 3 (Plan mode)** — P1–P5: strengthened plan brief, **P2 exit_plan_mode + pendingPlan
  persistence + restart safety** (plan_state.json sidecar, Proceed now/later modal, bare
  "proceed" executes saved plan), subagent escape hatch closed, violation counter + read-only
  shell passthrough, busy-guarded bare toggle. Code complete.
- **Phase 4 (Harness)** — H1–H8: argument validation, failed-call dedup + storm breaker,
  subagentKind fail-closed, cancel-safe result pairing, **H5 token circuit breaker +
  distinct-failure terminator** (8 failures / 6 turns → force-finalize; terminated reason
  surfaced to TUI), shared dispatch gates, read-only shell exemption, dead-MCP error.
  Code complete.
- **Phase 5 (MCP)** — M1–M8: blocking refresh, live modal updates, in-place status map,
  state vocabulary cleanup, lazy connect, expired-token classification, refresh debounce,
  liveness detection. Code complete.
- **Phase 6 (TUI)** — T1–T3: persistent usage footer, thinking snippet redesign, unified
  content width. Code complete.
- **Phase 7 (Verification)** — V1–V4: see gate table below.

## Gate table

| Gate | Evidence | Result |
|---|---|---|
| V0 / SC-006 | Module, format/diff, vet, full tests; both arms completed 72/72 turns | PASS |
| V1 / SC-002 | Prompt, restart, marshal, retry determinism | PASS |
| V1 race | `scripts/race-tests.sh` / `.ps1` created for CGO-capable host; no GCC on this Windows env | PENDING CGO HOST |
| V2 / SC-001 proxy | Strict append-only wire guard, landing path, shape mutation tests | PASS |
| V3 / SC-004 | Usage parsing, integrity, display honesty, resume continuity | PASS |
| V3 walkthroughs | `v3-walkthroughs.md` checklist (plan/goal/MCP/footer/cancel flows) | PENDING LIVE TUI |
| V4 | Three baseline runs and raw usage; `docs/prompt-caching.md` updated with C1 + C4 | COMPLETE |
| V5 / SC-001 | Fresh prefix-stability run required | PENDING LIVE RUN |
| V5 / SC-003 | Offline byte-prefix guard and bounded attribution pass | PASS OFFLINE |
| V5 / SC-005 | Fresh baseline/improved comparison, including fat-context scenario, required | PENDING LIVE RUN |
| V5 / SC-007 | Historical zero invalid; fresh `agent-suspect` audit required | PENDING LIVE RUN |
| V6 | `v6-spotchecks.md` | PASS |
| V7 | User and maintainer documentation | PASS |

The historical raw rates remain useful workload evidence, not acceptance evidence. Final sign-off
requires three fresh runs per arm showing prefix stability ≥99%, no unexplained/agent-suspect
misses, and internally consistent generated artifacts. Gateway tool-block placement should also
be verified as described in `docs/prompt-caching.md`.

## Environment-blocked items

Three items cannot complete on this Windows development host and are deferred:

1. **V1 race tests** — `go test -race` requires CGO + a C compiler (gcc). This host has no GCC.
   Run `scripts/race-tests.sh` (Linux/macOS) or `scripts/race-tests.ps1` (Windows + MinGW) on a
   CGO-capable host or CI runner. The existing unit tests already exercise the concurrency
   surface; `-race` only adds the data-race detector.
2. **V3 manual walkthroughs** — require a live gateway API key and an interactive terminal.
   The `v3-walkthroughs.md` checklist documents the exact steps and expected outcomes.
3. **V5 live benchmark** — requires a live gateway API key for fresh before/after runs.
   The benchmark harness (`benchmarks/cachebench`) and comparison format are ready.
