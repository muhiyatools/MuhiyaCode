# Contract: Module Structure, Size Budget, Layering & Removal Ledger

Feature 010 (US4; FR-012..014). Evidence: [research.md](../research.md) R2–R3.

## 1. Size & responsibility

| ID | Requirement |
|---|---|
| MS-1 | No non-test `.go` file exceeds **800 lines**, enforced by a tree-walking test in the new leaf `internal/arch` package with an explicit (initially empty) allowlist map — every future entry needs a written reason |
| MS-2 | The splits are intra-package moves (zero import churn): engine.go → ~11 responsibility files; tui model/view/actions → update/keys/ingest/render_*/hittest/slash/modals/format; application.go → 5; mcpclient manager.go → 4; pipeline machinery → state machine / phase runners / plan bar (folds into the R1 lifecycle) |
| MS-3 | The legacy-restore/compat code lands in ONE file (`orchestrator/restore.go` + the state migration loader) so the UL-8 grep boundary is a file boundary |
| MS-4 | Misplaced missions move even when under budget: the read-only-shell classifier leaves `inspection.go`; splits are moves, not rewrites — `git log --follow` continuity and byte-identical function bodies except package-position edits |

## 2. Layering

| ID | Requirement |
|---|---|
| MS-5 | The dependency graph is acyclic with layering contract → {gateway, workspace, state} → orchestrator → {tui, command}; `command` is the composition root (may import all); `mcpclient → state` is the one documented allowed lateral edge |
| MS-6 | The `state → gateway` lateral edge is erased by moving `IsMiniMaxM3Name` to `contract` (wire behavior byte-identical; gateway keeps a private alias) |
| MS-7 | An import-allow-map test in `internal/arch` (stdlib parser only) fails on any edge not in the map and on any cycle, with a message naming the offending package pair |

## 3. Dead code & duplicate missions

| ID | Requirement |
|---|---|
| MS-8 | `deadcode` (both invocations) reports zero findings at completion; the 6 verified truly-dead functions are removed; the 28 test-only survivors are each resolved WITH their tests (migrate or remove together) — headline: the orphaned `TranscriptWindow` type and the SSE batch-decode island (tests migrate to the streaming path) |
| MS-9 | Write-only state is removed or wired: `AgentEvent.CallID/Turns/ToolCalls` (no consumer), `buildinfo.Commit/Date` (wire into `--version` or remove with their ldflags lines), `Settings.UI.Density` (wire a rendering effect or remove field + config key). `AgentEvent.Handoff` is KEPT with its benchmark consumer recorded in the wiring inventory |
| MS-10 | The duplicate-mission clusters merge to single survivors: token formatter (one foundation helper), char-truncation primitive (`truncateEllipsis` + `digest` wrapper; byte-based `truncateLine` replaced rune-safe), display-width engine (uniseg family only — restoring width.go's stated invariant; go-runewidth import removed), SSE decode (streaming only), restore notices (one builder over the unified lifecycle) |
| MS-11 | Every removal has an `RL-###` Removal Ledger row (data-model §6): zero-reference proof (grep command + deadcode line) or a migration note; committed only with a green suite |

## Acceptance

- SC-004: deadcode zero; arch tests green (size + layering + cycle); no file >800.
- Ledger complete: every deletion traceable; `Handoff`-style false positives
  prevented by the ledger's benchmarks-inclusive grep scope rule.
- Full suite green at every merge point (never-broken rule).
