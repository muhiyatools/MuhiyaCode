# Phase 0 Research — Ultimate Consolidation (feature 010)

**Date**: 2026-07-15. Inputs: [spec.md](spec.md), constitution v1.0.0, and five
parallel substrate inventories (lifecycle, module structure, dead code,
instruction system, fault/wiring) with file:line evidence throughout. All
Phase-0 unknowns (R1–R5) are resolved; no NEEDS CLARIFICATION remains.

**Headline findings**:
1. The two lifecycle machines' own field comments EACH claim to be "single
   truth" (engine.go:145-150) — the contradiction is written into the code. The
   predicate analysis proves the two mutation gates guard the SAME state set,
   so the unification is a collapse, not an invention.
2. Every proposed module split is an intra-package move (zero import churn),
   and the package layering already holds acyclic — the restructure is
   mechanical, not architectural surgery.
3. Verified dead weight exists but is bounded: 6 truly-dead functions, 28
   test-only survivors (including an entire orphaned `TranscriptWindow` type),
   3 write-only event fields, 2 injected-but-unread ldflags vars, and 7
   duplicate-mission clusters (three width engines; two token formatters with
   byte-identical bodies).
4. The instruction audit found real incoherence: the `general` subagent's
   schema advertises tools its executor silently drops; the shell allowlist is
   disclosed only AFTER rejection; the plan-step example is hand-duplicated in
   4 places and has already drifted.
5. No existing fault test asserts the three-outcome invariant through a shared
   helper — stability is currently per-scenario luck, not a property.

---

## R1. Unified lifecycle — one state machine, 11 states

**Decision**: Replace `planMode` + `pendingPlan` + `planPhase` (9-state
`contract.PlanPhase`) + `pipeline PipelineState` (7-state
`contract.PipelinePhase`) with ONE `LifecycleState` enum owned by a new
`Lifecycle` type in `internal/orchestrator/lifecycle.go` (under the existing
`modeMu`, alongside goal state for the plan⇄goal exclusion invariant):

| Unified state | Merges (pipeline / plan) | Read-only | Proceed hint | Terminal |
|---|---|---|---|---|
| `direct` | direct / none | no | no | no |
| `research` | research / drafting | yes | no | no |
| `planning` | plan / drafting | yes | no | no |
| `awaiting-approval` | approve / ready | yes | yes | no |
| `pending` | — / pending | no | yes | no |
| `implementing` | implement / executing | no | no | no |
| `validating` | validate / executing | no | no | no |
| `interrupted` | — / interrupted | no | yes | no |
| `finished` | done / finished | no | no | yes |
| `superseded` | — / superseded | no | no | yes |
| `discarded` | — / discarded | no | no | yes |

Consumers bind to PREDICATES, not raw states: `IsReadOnly()` =
{research, planning, awaiting-approval} — which is ALSO `BlocksMutation()`,
collapsing the two mutation gates (engine.go:1814-1841) into one;
`InvitesProceed()` = {pending, interrupted}; `IsApprovalPause()`;
`IsPipelineResumable()`; `IsTerminal()`; `IsActive()`. The single legal-edge
table merges `PipelineState.Transition` with the SetPlanMode/SetPlanPhase/
DiscardPlan rules; the terminal-sticky guard becomes one rule (no edge leaves
a terminal state except into a fresh planning/research). `DrivenPlanPhase`,
`persistedPipelinePhase`, `restoredPipelineState`, and the two-truth
reconciliation in `stampPlanCompletionPhase` are all deleted.

**Persistence/compat**: keep `plan_state.json`; add ONE additive field
`state` (omitempty) + `depth`. A single migration loader in `internal/state`
is the ONLY reader of the legacy fields (satisfying SC-001's grep proof):
(1) `state` present → use it; (2) else `pipeline_phase` maps via the existing
restore table; (3) else derive from planMode/pendingPlan/phase exactly as
engine.go:351-370 does today, INCLUDING the two truthfulness corrections
(pending-but-complete → finished; executing-no-pipeline → interrupted/finished
by step state). Malformed-file-is-absent, terminal-written-not-deleted, and
idle-clear rules unchanged.

**Complete consumer inventory** (the rework's checklist — full file:line map
in the R1 report): writers = NewEngine restore, goal.go SetGoal/SetPlanMode,
plan.go setters ×5, pipeline.go transitions ×12, engine.go Run inline ×5;
readers grouped as gating (engine.go:1814-1841, subagent.go:118-171),
flow gates (exit_plan_mode, content bar, validation nudges), task-entry
routing (engine.go:742-760), TUI affordances (actions.go:205-231, 903-947 —
the dual-branch modal is the duplication to delete; view.go:329-391),
persistence (plan.go:129-177), headless one-shot, goals, finalize disclosures.

**Rationale**: every recent live incident was a new, unpredicted sync point
between the two machines; one enum makes disagreement unrepresentable (FR-001).
**Alternatives rejected**: invariant-checker over two types (detects desync
after the fact, cannot prevent it); computed getters over pipeline.Phase
(pending/interrupted/terminals have no pipeline representation); folding goals
in (orthogonal — only the exclusion edge is preserved).

## R2. Module structure — 800-line budget, intra-package splits, layering test

**Decision**: per-file budget **800 lines** (≈¼ of engine.go's 3,152), which
captures exactly the current offenders: engine.go 3152, model.go 1379,
view.go 1265, actions.go 1120, application.go 1043, delegationbench/main.go
1019, pipeline.go 965, manager.go 883. Split map (ALL intra-package — zero
import changes): engine.go → ~11 files (engine/restore/turnloop/dispatch/
gates/validate/toolhandlers/definitions/usage/maintenance/contextreport/
planrender); tui → update/keys/ingest/render_header/render_transcript/
render_tool/render_modal/hittest/slash/modals/format; application.go → 5;
manager.go → 4; pipeline.go → 3 (state machine / phase runners / plan bar).
The legacy restore lands in `orchestrator/restore.go` so SC-001's compat-layer
grep is structurally checkable. `inspection.go`'s read-only-shell classifier
(lines 524-756) moves out as a misplaced mission even though under budget.

**Layering verdict**: the intended contract → gateway/workspace/state →
orchestrator → tui/command layering HOLDS today, acyclic, zero true
back-edges. Two lateral edges: `state → gateway` (sole cause:
`gateway.IsMiniMaxM3Name` at config.go:201) — **erase it by moving the
12-line classifier to `contract`**; `mcpclient → state` — keep as a documented
allowed edge (a service persisting via the storage service).

**Enforcement**: two stdlib-only tests in a new leaf `internal/arch` package:
a tree-walking size-budget test (non-test .go files ≤800, empty allowlist
map with per-entry reasons) and an import-allow-map layering test with a
transitive cycle check (`command` = composition root, allowed `*`).
**Alternatives rejected**: golangci-lint/depguard (external config CI can
skip; supply-chain surface); statement-count budgets (harder to reason
about); shell-script `go list` checks (outside the `go test ./...` gate).

## R3. Dead code & duplicate missions — tool-verified ledgers

**Tooling decision**: `GOFLAGS=-mod=mod go run
golang.org/x/tools/cmd/deadcode@latest ./...` run TWICE — with and without
`-test` — treating the difference as the signal: 34 findings without,
6 with. The 6 that survive `-test` are safe deletes; the 28 in the gap are
test-only survivors that must be resolved WITH their tests. deadcode analyzes
funcs only — write-only fields and package vars come from targeted grep
(optionally staticcheck U1000 as a second pass). `go vet` exit 0 and
`gofmt -l` empty are the captured before-gates.

**Verified inventories** (full evidence in the R3 report):
- 6 truly-dead funcs: `Application.Paths`, `contract.CreditsToUSD`,
  `Manager.uniqueName`, `orchestrator.assembleRequest`, `state.CheckpointPath`
  (NOT the live checkpoint feature), `Workspace.Root`.
- 28 test-only survivors, headline clusters: the entire orphaned
  `tui.TranscriptWindow` type (17 methods, zero production callers — paging
  lives elsewhere) and the `gateway.ParseOpenAIStream`/`NewStreamAccumulator`
  batch-decode island (3 tests migrate to the streaming `ConsumeLine` path).
- Write-only fields: `AgentEvent.CallID/Turns/ToolCalls` (`.Handoff` is NOT
  dead — delegationbench reads it; the canonical false-positive trap).
- Injected-but-unread: `buildinfo.Commit`/`Date` (ldflags-set, zero readers —
  wire into `--version` or remove with their ldflags lines).
- 7 duplicate-mission clusters: token formatters (`humanTokens` ≡
  `formatTokens`, byte-identical → one foundation helper); five char-count
  truncators → one `truncateEllipsis` primitive + `digest` wrapper; THREE
  display-width engines (uniseg vs go-runewidth vs x/ansi — violating
  width.go's own stated invariant; survivor: the uniseg family); SSE batch vs
  streaming decode (survivor: streaming); dual restore-notice builders and the
  dual lifecycle itself (both gated on R1); two dead alignment helpers.
- Reconciliation with R5: `Settings.UI.Density` has config-layer references
  (default/validate/set) but NO renderer consumer — plumbed yet effect-free,
  a ghost per FR-015 (wire a density effect or remove field + key).
- Negative findings: no token-ceiling remnants; no dead slash commands; no
  dead settings fields besides Density's missing effect.

**Removal Ledger format**: `RL-###` rows with Symbol, Location, Class
(dead / test-only / write-only-field / injected-unread / duplicate-merged),
Proof (zero-reference command + deadcode line), Migration note (mandatory
when Class ≠ dead), Verification (green-suite command). Commit rule: proof
empty-output + deadcode-clean, OR migration note + green suite.

## R4. Instruction system — one audited artifact

**Decision**: a new foundation-layer `internal/instructions` package (imports
only `contract`) owning EVERY model-facing text as a named constant PLUS a
registered record: `Text{ID, Audience (MainStatic/MainDynamic/Subagent/Gate),
Cache (Prefix/Tail/Sidecar), Body, StatesRule, EnforcesRule, Example,
MentionsTools, AllowlistCtx}`. Composer functions (SystemPrompt, planBlock,
gate messages…) move there as pure functions; orchestrator/workspace/gateway
call them. The complete site inventory (7 files, 3 packages today — prompt
sections, 19 tool descriptions, subagent system/handoff/capability texts,
~25 pipeline preludes/notices, ~35 gate/denial/guard texts, workspace tool
errors, gateway per-family addenda) is enumerated in the R4 report §1A–1G.

**Audit suite** (internal/instructions/audit_test.go + dump_test.go):
(a) stated-in-advance — every EnforcesRule has a StatesRule text visible in
the reader's context, with Example when the rule governs a format;
(b) contradiction checks — canonical single copies referenced by ID (kills
the 4× plan-step example duplication and the "Risks" vs "Risks/unknowns"
drift by construction); (c) capability-reference — every tool named in a text
must be in that reader's real allowlist (wired from subagentSpecs);
(d) worked-example conformance — each example must PASS the very validator
its gate uses (e.g. `missingPlanStepRequirements(example) == nil`);
(e) golden instruction-system dump — every text rendered with fixed inputs
into one reviewable file, plus a SEPARATE Prefix-bytes golden whose single
update in the release PR IS the recorded epoch (FR-018), with a sentinel
assertion that Prefix-class bodies never contain dynamic markers.

**Verified incoherences the audit must force fixed** (ranked): the `general`
subagent schema advertises `run_subagent`/`exit_plan_mode`/`ask_user`/
`propose_changes` that its executor drops to "unknown tool"; the read-only
shell allowlist disclosed only on rejection (E2); the 4× duplicated plan-step
example + divergent report-format field names (D1/D4); `write_file`
permission mismatch between tool desc and system prompt (C1); no worked
example anywhere for the plan-note Verification:/Risks: body.

**Alternatives rejected**: per-package instructions files (loses the single
review surface — kept as fallback if a cycle risk emerges); embedded .md
catalogs (loses compile-time safety); pointer-registry over in-place strings
(cannot reference unexported cross-package consts).

## R5. Fault-injection suite & wiring inventory

**Decision (faults)**: `internal/orchestrator/faultinjection_test.go` — a
table-driven catalog where every row runs a full `Engine.Run` against the
scripted provider and passes through ONE shared
`assertRecoveryInvariant(t, engine, stats, runErr, wantOutcome)` helper:
exactly one of {success-after-guidance, recorded-degradation, user-decision}
(never a fourth outcome), plus liveness (turns < hard ceiling; no identical
denial text > 3×; no hard crash). New rows inherit the assertions for free —
the property FR-006 demands. A mutation-guard row proves the invariant bites
(flipping a bounded-recovery constant must fail the suite). Small addition:
`PipelineState.hasAnyDegradation()` (becomes `Lifecycle.HasAnyDegradation()`
post-R1).

**Coverage matrix** (full table in R5 report): COVERED — subagent wrap-up,
agent-cap denials, restart-per-state, blocked capabilities, all seven
pinned 008/009 incidents; PARTIAL — malformed args (main-loop `Run` path and
the required/enum/primitive schema branches untested), oversized plans (the
>12-step rejection has NO test); MISSING — per-phase budget exhaustion,
turn-governor/token-breaker as bounded outcomes, invalid regex through a full
Run, the FR-004b trailing-intent bounded retry. These become the seed rows.

**Decision (wiring)**: a checked-in inventory
(contracts/wiring-inventory.md): `Surface | Identifier | Advertised behavior |
Verified-by | Status(wired/removed)` — no third status — PLUS guard tests that
enumerate the live surface at runtime (Registry.Names, synthetic tools, the
command palette + runSlash cases, handleKey cases, Settings fields,
AgentEvent kinds, Callbacks members) and fail when the live surface and the
inventory diverge in either direction.

**Confirmed ghosts to resolve**: `Settings.UI.Density` (no renderer);
`AgentEvent.CallID/Turns/ToolCalls` (no consumer; `Handoff`'s consumer is the
benchmark — record it explicitly); grep's invalid-pattern hint path untested;
conditional surfaces (`web_search` probe-gated; `--simple`-mode "unavailable"
commands) get inventory rows naming WHICH interface verifies them.

**Alternatives rejected**: per-scenario bespoke assertions (the status quo —
a new fault can skip the invariant); gate unit tests only (miss cross-gate
interactions, which were the real incidents); random fuzzing as the core
suite (non-deterministic outcomes; may be added later as a separate lane).

---

## Appendix — decision index

| ID | Decision | Contract |
|---|---|---|
| R1 | 11-state `Lifecycle`, predicate-bound consumers, additive `state` sidecar field, single migration loader | [unified-lifecycle.md](contracts/unified-lifecycle.md) |
| R2 | 800-line budget; intra-package split map; `internal/arch` size+layering tests; `IsMiniMaxM3Name` → contract | [module-structure.md](contracts/module-structure.md) |
| R3 | deadcode ×2 tooling; 6+28+5 verified candidates; 7 merge clusters; RL-ledger format | [module-structure.md](contracts/module-structure.md) §ledger |
| R4 | `internal/instructions` Text registry; 5 audit checks; Prefix golden = the epoch | [instruction-system.md](contracts/instruction-system.md) |
| R5 | table-driven chaos suite + `assertRecoveryInvariant`; wiring inventory + guard tests | [fault-injection.md](contracts/fault-injection.md), [wiring-inventory.md](contracts/wiring-inventory.md) |

Full inventories (every reader/writer, every text site, every dead symbol
with its grep proof, the complete coverage matrix) are preserved in the five
Phase-0 agent reports; the contracts distill them into requirement IDs.
