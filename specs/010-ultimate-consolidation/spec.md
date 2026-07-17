# Feature Specification: Ultimate Consolidation — One Coherent, Provably Stable Agent

**Feature Branch**: `010-ultimate-consolidation`

**Created**: 2026-07-15

**Status**: Draft

**Input**: User description: "Full structural rework of MuhiyaCode: reorganize and
clean every module, refactor the agent system onto one strong structure, keep it
wired to all current features, remove unused features and dead code, merge anything
that duplicates a mission, and make the agent one fully connected system that is
completely stable — no errors caused by bad prompt engineering or a badly
instructed agent. It must just work."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One lifecycle, one truth (Priority: P1)

Today the agent tracks its working state in two overlapping systems that must be
manually synchronized (the original planning flags and the newer orchestration
phases). Every recent live failure — dead approval clicks, unexitable planning,
gates blocking unrelated work — traced to those two systems disagreeing. After
this rework there is exactly ONE task-lifecycle state machine with one owner, one
persisted shape, and one set of legal transitions. A disagreement between "what
mode am I in" and "what phase am I in" becomes impossible by construction, because
there is only one answer. Sessions saved by older versions still restore correctly
through a clearly bounded compatibility layer — the only place the legacy shape
survives.

**Why this priority**: The dual state machine is the proven root cause of the
worst live failures. Removing the seam removes the whole class of bug, not
instances of it.

**Independent Test**: Drive a scripted task through every lifecycle state
(including interrupt/restart at each one) and observe a single authoritative state
value everywhere it is displayed, persisted, gated on, or restored; grep-level
verification shows the legacy flags exist only inside the compatibility layer.

**Acceptance Scenarios**:

1. **Given** any task in any lifecycle state, **When** the state is read by the
   UI, the gates, persistence, or resume, **Then** all consumers report the same
   single state with no reconciliation logic between two sources.
2. **Given** a session sidecar written by any earlier release, **When** the
   session resumes, **Then** the state restores correctly through the
   compatibility layer and continues under the unified lifecycle.
3. **Given** a restart at every lifecycle state (planning, awaiting approval,
   implementing, validating), **When** the user returns, **Then** the task resumes
   from exactly that state with its plan, depth, and progress intact.
4. **Given** any user action that ends or discards a plan, **When** it executes,
   **Then** every gate, block, and affordance tied to that plan is released
   atomically — no stale gating can survive its plan.

---

### User Story 2 - Stability is a testable property, not a hope (Priority: P2)

A developer (or the harness's own test suite) can inject every known chaos
condition — malformed tool arguments, exhausted budgets at every phase,
interrupts and restarts at every state, oversized plans, invalid search patterns,
blocked capabilities, empty results — and the agent NEVER enters an unrecoverable
state. Every failure path does exactly one of three things: succeeds after bounded
guidance, degrades with a visibly recorded reason, or lands at a user decision
point. There is no fourth outcome: no error loops, no silent drops, no stalls.

**Why this priority**: "10000% stable" is only real if stability is enforced by
an automated suite that proves the recovery invariant for every failure class —
including ones introduced by future changes.

**Independent Test**: Run the fault-injection suite; it must pass 100% with the
recovery invariant asserted on every scenario, and a mutation of any gate that
removes its bounded-recovery behavior must fail the suite.

**Acceptance Scenarios**:

1. **Given** any injected fault from the chaos catalog, **When** the task runs,
   **Then** it ends in success, recorded degradation, or a user decision — never
   a repeating error, a hang, or a silently lost result.
2. **Given** a rule the harness enforces (format, budget, phase, capability),
   **When** the model violates it repeatedly, **Then** guidance is bounded and the
   flow proceeds past the gate with the relaxation recorded — a gate can never
   reject forever.
3. **Given** a sub-agent that exhausts its allowance mid-work, **When** it wraps
   up, **Then** its partial findings are always returned and banked — work is
   never destroyed by a limit.

---

### User Story 3 - The agent is instructed by one coherent voice (Priority: P3)

Everything the model is told — its standing instructions, each tool's
description, phase guidance, gate and denial messages — reads as one internally
consistent instruction system with a single review surface. Every rule the
harness enforces is stated where the model sees it BEFORE it can violate it, with
a worked example of the expected shape. Every gate message lists all problems at
once, names the exact expected format, is aware of the model's remaining budget,
and points to an alternative the model can actually afford. No instruction
contradicts another; no instruction references a capability the agent lacks.

**Why this priority**: The user's live sessions failed repeatedly because the
model discovered rules only by being rejected, was told to use tools it could not
afford, and received advice written for a different project type. Instruction
coherence is what "the agent knows what it can do" means.

**Independent Test**: An instruction-system audit walks every model-facing text
as one document and finds zero contradictions, zero references to unavailable
capabilities, zero enforced-but-unstated rules, and a worked example for every
formatted deliverable; conformance is re-checked by automated tests that fail on
reintroduction.

**Acceptance Scenarios**:

1. **Given** any rule that a gate enforces, **When** the model first reaches the
   context where the rule applies, **Then** the rule and a worked example are
   already present in its instructions for that context.
2. **Given** any gate or denial message, **When** it renders, **Then** it lists
   every unmet requirement at once, shows the exact expected shape, reflects the
   real remaining budget, and names an affordable next action.
3. **Given** the full set of model-facing texts, **When** audited pairwise,
   **Then** no two texts disagree about a capability, a limit, a format, or a
   permission.

---

### User Story 4 - Clean structure with nothing dead and nothing duplicated (Priority: P4)

The codebase is reorganized into cohesive modules with single responsibilities
and enforced size boundaries; the dependency graph is a clean acyclic layering
with no back-edges. Automated analysis proves there are zero unreachable
declarations, zero write-only fields, and zero unused features; any two
mechanisms serving the same mission are merged into one. Every removal is either
provably unreferenced or carries a migration note.

**Why this priority**: Structure is what keeps the first three stories true over
time — oversized mixed-responsibility files and duplicate mechanisms are where
the seams and dead weight came from.

**Independent Test**: Automated dead-code analysis reports zero findings; a
dependency check shows no cycles or layering violations; no source file exceeds
the agreed size budget; a duplicate-mission review finds no two units serving the
same purpose.

**Acceptance Scenarios**:

1. **Given** the reworked tree, **When** dead-code analysis runs, **Then** it
   reports zero unreachable declarations and zero write-only state.
2. **Given** the package graph, **When** layering is checked, **Then** the
   foundation → services → orchestration → interface ordering holds with no
   cycles and no back-edges.
3. **Given** any behavior that existed twice, **When** the rework completes,
   **Then** exactly one implementation remains and all callers use it.

---

### User Story 5 - Everything advertised is wired, accurate, and observable (Priority: P5)

Every feature, tool, view, keybinding, setting, and event either demonstrably
works end-to-end or is removed. Tool descriptions match real behavior and real
limits; every UI affordance functions; every emitted event has a consumer; every
configuration setting has an observable effect. A user can trust that anything
the product shows or documents actually does what it says.

**Why this priority**: A "fully connected" agent means zero ghost features — the
wiring audit converts that from a slogan into a checklist with two outcomes:
wired or removed.

**Independent Test**: A wiring inventory enumerates every advertised
capability and marks each as verified-working (with the exercising test or
manual check) or removed; no third category remains.

**Acceptance Scenarios**:

1. **Given** any tool the model can call, **When** its description is compared to
   its real behavior and limits, **Then** they match exactly.
2. **Given** any UI affordance (view switch, modal choice, clickable row,
   keybinding, slash command), **When** exercised, **Then** it performs its
   advertised action.
3. **Given** any setting or emitted event, **When** traced, **Then** it has a
   real consumer/effect — or it no longer exists.

---

### Edge Cases

- A session saved by an older release (legacy sidecar shapes, mid-task states)
  resumes correctly through the compatibility layer; nothing older is silently
  dropped.
- The rework lands while users have in-flight plans: an interrupted old-format
  task must restore into the unified lifecycle without losing the plan.
- The full existing regression suite must stay green at every merge point during
  the rework, not only at the end — the tree is never broken mid-flight.
- Static model-facing text changes land as ONE recorded upgrade epoch for the
  release, so provider-side caching takes exactly one cold restart.
- Provider wire behavior for existing deployments must remain byte-identical
  where pinned; the rework must be invisible on the wire.
- Removal mistakes: any feature removed as "unused" that a user actually relies
  on must be recoverable — removals are itemized and reviewable before release.
- Fault-injection scenarios that reveal new unrecoverable states during the
  rework become permanent suite entries, not one-off fixes.

## Requirements *(mandatory)*

### Functional Requirements

**Unified lifecycle (US1)**

- **FR-001**: The system MUST represent a task's working state in exactly one
  lifecycle state machine with one owner, one persisted shape, and one set of
  legal transitions; all consumers (display, gating, persistence, resume) MUST
  read that single source.
- **FR-002**: The legacy state shape MUST survive only inside a bounded
  compatibility layer that migrates older saved sessions into the unified
  lifecycle on load; no runtime logic outside that layer may reference it.
- **FR-003**: Every lifecycle state MUST be restorable: interrupt/restart at any
  state resumes the task at that state with plan content, depth, and progress
  intact.
- **FR-004**: Ending, discarding, superseding, or parking a plan MUST atomically
  release every gate and affordance tied to it — stale gating outliving its plan
  must be structurally impossible.

**Provable stability (US2)**

- **FR-005**: The system MUST include a fault-injection suite covering, at
  minimum: malformed tool arguments, budget exhaustion at every phase,
  interrupt/restart at every lifecycle state, oversized plans, invalid search
  patterns, blocked capabilities, and empty/failed sub-agent results.
- **FR-006**: Every failure path MUST resolve to one of exactly three outcomes —
  success after bounded guidance, degradation with a recorded reason, or a user
  decision point — and the suite MUST assert this invariant for every scenario.
- **FR-007**: No gate may reject indefinitely: repeated violation of any enforced
  rule MUST lead to bounded guidance and then a recorded relaxation or a user
  decision, never an unbounded loop.
- **FR-008**: Limits MUST shape work, never destroy it: any bounded activity that
  ends at its limit MUST still return and preserve its partial results.

**Instruction coherence (US3)**

- **FR-009**: All model-facing text MUST form a single audited instruction system
  with one review surface; an automated audit MUST verify zero contradictions and
  zero references to unavailable capabilities.
- **FR-010**: Every harness-enforced rule MUST be stated, with a worked example,
  in the instructions the model sees before the rule can be violated.
- **FR-011**: Every gate/denial message MUST list all unmet requirements at once,
  show the exact expected shape, reflect real remaining budget, and name an
  affordable alternative action.

**Clean structure (US4)**

- **FR-012**: Modules MUST have single responsibilities within enforced size
  budgets; the dependency graph MUST be acyclic with foundation → services →
  orchestration → interface layering and no back-edges.
- **FR-013**: The tree MUST contain zero unreachable declarations, zero
  write-only state, and zero unused features, verified by automated analysis;
  each removal MUST be provably unreferenced or carry a migration note.
- **FR-014**: Any two mechanisms serving the same mission MUST be merged into
  one, with all call sites moved to the survivor.

**Full wiring (US5)**

- **FR-015**: Every advertised capability (tools, views, keybindings, settings,
  events, commands) MUST be verified working end-to-end or removed; the wiring
  inventory MUST show no third category.
- **FR-016**: Tool descriptions MUST match real behavior and real limits exactly.

**Preservation (all stories)**

- **FR-017**: The complete pre-existing regression suite MUST remain green
  throughout the rework; pinned behaviors (byte-stable prompt prefix discipline,
  the always-pause approval gate, per-user cache isolation, honest usage
  accounting, provider wire compatibility) MUST be preserved byte-for-byte where
  captured.
- **FR-018**: All static model-facing text changes MUST land as one recorded
  upgrade epoch for the release.
- **FR-019**: Provider conformance captures and steady-state cache baselines MUST
  match before/after within the agreed tolerance (cache within 1 point; wire
  captures byte-identical for existing deployments).

### Key Entities

- **Task Lifecycle**: the single state machine — states, legal transitions,
  owner, persisted shape, and the compatibility migration from legacy shapes.
- **Instruction System**: the audited set of all model-facing texts with their
  placement (standing instructions, tool descriptions, phase guidance, gate and
  denial messages) and their consistency rules.
- **Fault Catalog**: the enumerated chaos scenarios and, for each, its required
  outcome class (guided success / recorded degradation / user decision).
- **Wiring Inventory**: the enumerated advertised capabilities, each marked
  verified-working (with its exercising check) or removed.
- **Removal Ledger**: every deleted feature/mechanism with its zero-reference
  proof or migration note.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Exactly one lifecycle state machine remains; automated search
  proves the legacy state flags appear nowhere outside the compatibility layer.
- **SC-002**: The fault-injection suite passes 100% of its catalog with the
  three-outcome recovery invariant asserted on every scenario; zero unrecoverable
  states, error loops, silent drops, or stalls.
- **SC-003**: The instruction-system audit reports zero contradictions, zero
  unavailable-capability references, and a worked example present for every
  formatted deliverable.
- **SC-004**: Automated dead-code analysis reports zero unreachable declarations;
  no source file exceeds the agreed size budget; the dependency graph has zero
  cycles and zero layering back-edges.
- **SC-005**: The wiring inventory shows 100% of advertised capabilities either
  verified-working or removed, with zero unresolved entries.
- **SC-006**: The complete pre-existing test suite passes on the reworked build;
  the delegation benchmark completes with plan-only execution passing its full
  checklist (8/8); the provider conformance capture is byte-identical for
  existing deployments; steady-state cache hit rate regresses by no more than 1
  point.
- **SC-007**: A fresh live end-to-end session (research → plan → approve →
  implement → validate) completes without a single harness-caused error message.
- **SC-008**: All improvement claims are backed by before/after runs on pinned
  conditions per the constitution's measurement standards, with simulated
  figures labeled as such.

## Out of Scope

- The gateway repository (except re-verifying client wire compatibility against
  existing captures).
- Any new user-facing features; this rework changes structure, coherence, and
  reliability — not the feature set (removals of unused features are in scope).
- Live MiniMax traffic (simulated verification only, per standing directive);
  DeepSeek remains the live benchmark provider within existing budget windows.
- Changes to the client↔gateway wire protocol or the `~/.muhiya` storage
  contract (beyond the additive compatibility migration).
- Re-introducing any removed hard ceilings (token or turn) as failure modes.

## Assumptions

- The existing regression suite plus the pinned captures (conformance, denial
  texts, prompt-budget, prefix stability) constitute the behavioral contract; a
  change that keeps them green and honors the FRs is a valid rework step.
- "Agreed size budget" for source files defaults to a per-file ceiling agreed in
  planning (working default: no file above roughly one-quarter of the current
  largest file's size) and is enforced by an automated check thereafter.
- The rework ships as one release (one prompt epoch, one migration), developed
  behind the always-green-suite rule rather than a long-lived divergent branch.
- The fault catalog seeds from every live failure documented in features 008–009
  (all currently pinned by regression tests) and grows with any new incident.
- Dead-code verification uses standard automated static analysis for the
  language plus the wiring inventory for user-facing features; tooling choice is
  a planning decision.
