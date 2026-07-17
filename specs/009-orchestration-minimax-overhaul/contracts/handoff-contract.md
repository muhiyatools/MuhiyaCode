# Contract: Sub-agent Handoff & Plan Artifact

Feature 009 (US2, US3; FR-007..011). How work and results move between the main
agent and sub-agents. Built on the knowledge store + plan-step model
([research.md](../research.md) R5); shapes in [data-model.md](../data-model.md) §3–4.

## 1. Launch contract (in)

| ID | Requirement |
|---|---|
| HO-1 | Every sub-agent launch MUST carry, in its system-prompt composition: **Role** (research-scope / implement-step / review), **Scope** (the specific files/step), **Context** (only the scope-relevant `Knowledge.Briefing` extract — NEVER the parent transcript), **Deliverable**, and **OutputFormat** (exactly what the report must contain) |
| HO-2 | Context passed MUST be bounded (the existing Briefing cap) and scope-relevant; a sub-agent MUST NOT receive unrelated phases' raw output |
| HO-3 | The `run_subagent` `task` field guidance (008 DG-4) is extended per phase with the required output format, so the caller specifies the deliverable shape |

## 2. Return contract (out)

| ID | Requirement |
|---|---|
| HO-4 | Sub-agent reports MUST bank as digested `KnowledgeFact`s (existing AddReport: digest ≤500 / full ≤4000) — bounded by construction; the next consumer receives only the relevant `Briefing` extract, never the full report inline (FR-011) |
| HO-5 | A consumer MUST NOT wholesale re-explore a scope a predecessor covered (measured by the duplicate-read audit; DG-14 carried forward) |
| HO-6 | Implementation sub-agents MUST report: changes made, validation performed, problems, and remaining concerns (US1/step 3) — the OutputFormat for the implement role |
| HO-7 | A failed or unusable sub-agent result MUST be absorbed with bounded recovery: the part is re-scoped once or completed in direct work — never a silent loss of a plan part (FR-005, US3-4) |

## 3. Plan artifact (execution-grade)

| ID | Requirement |
|---|---|
| HO-8 | The plan MUST be a durable workspace Markdown artifact (existing `plan.md` via WritePlan/planMarkdown) containing: research findings with exact references, ordered steps each naming its target scope + a cited finding + an observable acceptance check, verification commands, and risks (FR-007) |
| HO-9 | Plan quality MUST be execution-grade for a cheaper model: on the benchmark, implementation sub-agents following ONLY the plan's steps pass the task's full expected-outcome checklist (FR-008, SC-003) |
| HO-10 | A new pipeline run MUST version/supersede a pre-existing plan (existing Superseded phase), never silently overwrite unrelated content (FR-009) |
| HO-11 | The plan artifact and phase progress MUST persist and resume across session restarts (existing plan persistence + the new PipelinePhase field) |

## 4. Division & dependency awareness

| ID | Requirement |
|---|---|
| HO-12 | The implement phase MUST divide the plan into logically separated tasks by the plan's steps; steps the plan marks independent MAY run as parallel sub-agents (within the effort allowance), dependent steps run in order (Reasonix conductor pattern, R9) |
| HO-13 | The main agent MUST collect all sub-agent outputs, verify every plan step is completed, and resolve missing/conflicting work before the validate phase — resistant to skipped requirements and duplicated work (US1/step 4) |

## 5. Acceptance (maps to spec)

- SC-004: 100% of sub-agent prompts contain role + deliverable + output format +
  scoped context; 0 wholesale re-reads of predecessor-covered scopes (duplicate-read
  counter not above baseline).
- SC-003: plan-only execution passes 8/8; main conversation ≤50% of baseline.
- Unit tests: handoff composition (all five in-fields present), report banking +
  bounded Briefing consumption, failed-subagent bounded recovery, plan
  supersede-not-overwrite, resume mid-pipeline.
