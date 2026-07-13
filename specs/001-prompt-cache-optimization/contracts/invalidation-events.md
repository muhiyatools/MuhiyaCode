# Contract: Invalidation Events & Miss Attribution

**Feature**: `001-prompt-cache-optimization` | Consumers: `internal/orchestrator` (ledger,
shape), `internal/state` (persistence), `internal/tui` (annotations), cache-guard tests,
`benchmarks/cachebench` (SC-007 audit).

## Event taxonomy

| `cause` | Fired by | `trigger` values | Notes |
|---|---|---|---|
| `fold` | Consolidated maintenance pass folding completed-task payloads | `pressure` | Requires pressure ≥ 0.60 |
| `trim` | Maintenance pass rewriting aged/superseded tool payloads | `pressure` | Same pass as fold when both apply — one event per pass with combined scope |
| `compact` | Structured summary compaction | `pressure` | Includes the summary-message insertion |
| `window-drop` | Oldest units dropped from the assembled request | `pressure`, `boundary` | `boundary` identifies estimator bootstrap below the pressure floor |
| `toolset-change` | Pinned MCP surface change (user add/remove/enable/disable/authorize; first-ever handshake of an uncached server) | `user-action`, `boundary` | Applied only at task boundaries |
| `model-switch` | `/model` | `user-action` | Prompt model fields refresh in the same boundary |
| `prompt-rebuild` | Any R1 recomposition that changes bytes (config change, template version change) | `config-change` | Must name the changed input |
| `user-compact` | `/compact` | `user-action` | |
| `probe-change` | Persisted ProbeSnapshot definitive change | `config-change` | Applies at session build or task boundary |

## Recording rules

1. **Exactly one event per rewrite.** Any change to previously transmitted R1/R2/R3 bytes MUST
   be preceded by exactly one recorded event covering it (a consolidated maintenance pass is
   one event with combined scope). No event ⇒ the rewrite is a defect; tests fail on any
   PrefixShape diff without a matching event.
2. **Events precede transmission.** The event is recorded before the first request that
   transmits the changed bytes; `request_seq` links to that request.
3. **Pressure floor.** `trigger=pressure` events require recorded `pressure ≥ 0.60`
   (provider-token based when available). Below the floor, automatic rewrites are prohibited.
4. **Boundary application.** `toolset-change`, `model-switch`, and `probe-change` apply at
   task boundaries — never between a tool call and its result, never mid-turn.
5. **Persistence.** Events persist with the session (additive artifact per
   [data-model.md](../data-model.md) §4) and survive resume.
6. **Anti-thrash.** Two consecutive pressure-triggered passes latch automatic maintenance off
   until pressure drops below the floor or a compaction completes; the latch state change is
   itself visible in diagnostics.

## Attribution algorithm

Executed when a response's usage arrives (see [cache-metrics.md](cache-metrics.md)):

```text
if usage cache fields are null            → attribution = n/a
else if seq == 1 or first request after
        process start/resume              → attribution = cold-start
else if PrefixShape(cur) != PrefixShape(prev)
                                          → attribution = agent
                                            change_reasons = CompareShape reasons,
                                            cross-linked to the InvalidationEvent(s)
else if prompt shrank without an event OR cache read regressed by more than two 64-token
        blocks while messages grew OR miss > new_tail + two blocks
                                          → attribution = agent-suspect
else                                      → attribution = provider
                                            (identical bytes and miss inside the bounded
                                             new-tail/quantization tolerance)
```

Notes:

- `cold-start` is definitionally excluded from `steady_state_hit_rate` (SC-001).
- `agent` attributions without a matching ledger event are contract violations (tests assert
  zero).
- `agent-suspect` is unattributed until a matching client invalidation is identified; it can
  never be used to satisfy SC-007. `provider` attributions feed the limitations register in `docs/prompt-caching.md` with
  observed evidence (FR-011).
- The active tail (R4) of every request is expected uncached input; attribution concerns the
  R1–R3 prefix only. Benchmarks compute expected-new-tail tokens per turn to bound how much
  miss is "normal" before flagging.

## User-facing surfacing

- The usage line annotates prefix changes: `cache prefix changed: tools (mcp add supabase)`.
- `/context` lists the most recent events (cause, scope, when).
- `cachebench` reports `unattributed_misses` (MUST be 0 for SC-007) and a per-cause event
  histogram per run.
