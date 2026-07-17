# Data Model — Enforced Orchestration Pipeline & MiniMax (feature 009)

**Date**: 2026-07-14. From [spec.md](spec.md) Key Entities + [research.md](research.md)
decisions R1–R9. Only entities this feature creates or changes are modeled;
untouched primitives (contract.Plan, PlanPhase, Knowledge, Assessment, Callbacks)
are referenced as-is with their verified shapes.

## 1. PipelinePhase (new — the state machine, `pipeline.go`)

The task's progression. Distinct from `contract.PlanPhase` (which it drives): the
pipeline is the ORCHESTRATOR, plan-phase is the underlying lifecycle it manipulates.

| Phase | Enters when | Drives PlanPhase | Gate to leave |
|---|---|---|---|
| `direct` | NeedsPlan == false | None (untouched) | — (no pipeline) |
| `research` | NeedsPlan == true, at task start | Drafting (read-only) | ≥1 research subagent completed OR research degraded (recorded) |
| `plan` | research done | Drafting | plan artifact written (`update_plan` + WritePlan) and `exit_plan_mode` called |
| `approve` | plan written | Ready → Pending | user approves (→ implement) / steers (→ research/plan) / cancels (→ Pending, task ends) |
| `implement` | user approved | Executing | all plan steps completed (large/epic: via subagents; standard: main-loop allowed) |
| `validate` | implementation done | Executing | review pass + plan acceptance checks confirmed |
| `done` | validation passed | Finished | terminal |

**Persistence**: rides the EXISTING `plan_state.json` sidecar (PlanStateSnapshot,
types.go:328) plus one added field `PipelinePhase string` (nullable/omitempty —
backward-compatible; absent = legacy/direct). Resumable: a restart in `approve`
reloads the pending plan and re-presents approval; in `implement` resumes execution.

**Invariants**:
- No main-loop mutating tool succeeds before `implement` (gatedExecute, R2).
- `done` is unreachable while `hasIncompletePlan()` is true (anti-premature-finish,
  R5); a premature finalize stamps `Interrupted`, never `Finished`.
- `direct` tasks never touch any of this (fast path, R4).

## 2. PlanNeedVerdict (new — `classify.go`)

| Field | Definition |
|---|---|
| `NeedsPlan` | bool — true for Class ∈ {standard, large, epic} that is not a pure no-workspace question |
| `Depth` | `light` (standard: research+plan+approval; implementation may stay main-loop) or `full` (large/epic: implementation + validation via subagents) |
| `Reason` | reused `Assessment.Reason` (already computed) — surfaced for visibility (FR-001) |

Pure function of the existing `Assessment`; no new classifier. Effort selects
per-phase subagent counts via the unchanged `classAgents` map.

## 3. PlanArtifact (changed — the Markdown plan)

Extends the existing `plan.md` (WritePlan + planMarkdown). The pipeline requires it
to be **execution-grade** (US2/FR-007/FR-008):

| Element | Source / rule |
|---|---|
| Findings section | research subagents' banked reports (knowledge.go AddReport), cited by scope |
| Steps | `contract.Plan.Steps` (≤12, each `{Title, Status}`) — each step names its target scope, cites a finding, and carries an observable acceptance check (in the Title/Note text) |
| Verification | commands/checks listed for the validate phase |
| Risks | noted in the plan Note |
| Versioning | a new pipeline run on an existing plan supersedes it (SetPlanPhase Superseded, existing) — never silent overwrite (FR-009) |

No new file or format — the existing plan persistence carries it; the pipeline
enforces the content bar via the plan phase's exit condition and the validate gate.

## 4. HandoffContract (new — subagent launch/return shape, `subagent.go`)

| Direction | Fields |
|---|---|
| **In** (per subagent, appended to its system prompt) | `Role` (research-scope / implement-step / review), `Scope` (the specific files/step), `Context` (bounded `Knowledge.Briefing` extract — never transcript), `Deliverable`, `OutputFormat` (what the report must contain) |
| **Out** (structured summary) | the existing subagent report string (subagent.go:134) banked as a digested `KnowledgeFact` (digest 500 / full 4000) — bounded by construction; consumed by the next phase via `Briefing`, never re-read wholesale |

**Rules**: context is scope-relevant only (FR-010); cross-phase input is the
predecessor's structured summary (FR-011); a failed/unusable subagent result is
re-scoped once or absorbed into direct work (bounded recovery, FR-005/US3-4) —
never silent loss of a plan part.

## 5. AgentRoute (verified — no schema change, made observable)

`stream → provider/model` binding, already present per request (main =
ActiveModelID, sub = SubagentModelID; usage records carry `Model`, 008). Feature 009
surfaces it (phase + role + model per running subagent, FR-012) and relies on it for
per-provider cache affinity in mixed sessions (FR-019).

## 6. MiniMaxProviderProfile (new — gateway + client)

**Gateway** (`db` model/provider rows + `proxy`):

| Field | Value / rule |
|---|---|
| Base URL | `https://api.minimax.io/v1` (OpenAI-compatible); Bearer auth |
| Models | M3 (1M ctx), M2.7, M2.7-highspeed, M2.5, M2.1, M2 (204.8k) |
| Pricing | **tiered for M3** (≤512k: 0.30/1.20/0.06; >512k: 0.60/2.40/0.12 per M in/out/cached); M2.x flat; cost math branches on prompt size |
| Caching | passive ≥512 tokens; read `prompt_tokens_details.cached_tokens`; miss = prompt − cached (derived); recorded like DeepSeek |
| Reasoning | normalize to `reasoning_split` (documented), never raw client passthrough |
| Tool calls | standard OpenAI shapes (no legacy fields) |

**Client** (`internal/gateway/model.go` capability profile, 008-style):

| Field | Value |
|---|---|
| ContextWindow / OutputLimit | documented (204.8k–1M) — replaces stale 128k/16k |
| ReasoningReplayPolicy | **`preserve`** (MiniMax needs `reasoning_details` in replayed history — M1) vs DeepSeek's `strip` — the one genuinely per-family branch |
| ToolCallRescue | on (family already flagged) |
| Beta / JSON | as documented |

## 7. Relationships

```text
Assessment ──▶ PlanNeedVerdict ──(NeedsPlan?)──▶ PipelinePhase(direct | research…done)
PipelinePhase ──drives──▶ contract.PlanPhase (drafting/ready/pending/executing/finished)
research/implement/validate phases ──launch──▶ subagents (HandoffContract in)
subagent reports ──▶ Knowledge (banked, digested) ──Briefing──▶ next phase (HandoffContract out)
plan phase ──writes──▶ PlanArtifact (plan.md) ──approve gate──▶ implement
AgentRoute ──(stream→provider)──▶ {DeepSeek | MiniMax} profiles ──▶ per-model usage/cost ledger
```

## 8. Explicitly unchanged (compat boundaries)

`~/.muhiya` layout (plan_state.json gains one nullable field); effort→allowance
numbers; classAgents caps; the four subagent kinds/tools; denial texts; wire
protocol client↔gateway; the removed token ceiling stays removed; DeepSeek replay
policy unchanged (only MiniMax adds `preserve`); existing user model settings never
altered by the fresh-install default.
