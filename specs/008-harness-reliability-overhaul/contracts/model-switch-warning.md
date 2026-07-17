# Contract: Mid-Session Model-Switch Warning

Feature 008. Governs the TUI warning gate in front of the model-switch apply path.
Evidence: [research.md](../research.md) R2 (verified flow map).

## 1. Trigger matrix

| Situation | Warning? |
|---|---|
| Different model selected (main or subagent role) AND session has ≥1 completed provider request (`Engine.UsageAggregate().Requests ≥ 1`) | **YES** |
| Same model re-selected (ID equal for that role) | no — silent no-op close |
| Zero completed requests in the session (fresh session / pre-first-turn) | no — applies immediately |
| "Refresh from gateway" branch | no — refresh never switches |
| Model set via CLI config path (non-interactive) | no modal (out of interactive scope; unchanged behavior) |

## 2. Modal semantics

| ID | Requirement |
|---|---|
| MS-1 | The warning renders BEFORE any dispatch: no settings mutation, no engine call, no invalidation event may occur until proceed is chosen |
| MS-2 | Content MUST name: the role (main/subagent), current → selected model IDs, that the provider cache restarts cold on the new model (full context re-read at the uncached rate), and that pricing may differ |
| MS-3 | Exactly two choices: proceed ("Switch model") and "Cancel"; **Cancel is the default-selected (Recommended) choice**; Esc behaves as Cancel |
| MS-4 | Cancel/Esc: modal closes with zero side effects (settings, session, cache state, invalidation ledger all untouched) |
| MS-5 | Proceed: dispatches the UNCHANGED apply path (SetModel → engine SwitchModel), so the existing `model-switch` invalidation event remains the single record of the switch — the modal adds no duplicate event |
| MS-6 | The engine's existing mid-task refusal ("cannot switch models while a task is running") is unchanged; if it fires after proceed, its error surfaces via the existing notice path |
| MS-7 | Warning copy is static text plus the interpolated role/IDs — no cost estimates are fabricated (pricing differences are stated qualitatively; Principle VI) |

## 3. Acceptance (maps to spec)

- 100% of mid-session different-model selections show the warning first;
  100% of cancels leave state byte-identical; fresh-session and same-model
  selections show zero warnings (SC-004, US2 scenarios 1–5).
- Unit tests drive the TUI modal machinery for: trigger cases, default selection
  on Cancel, Esc-cancel purity, proceed dispatch equivalence with today's path.
