package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// pipelinePlanContentBar is a NON-BLOCKING quality bar. It NEVER rejects a
// non-empty plan: exit_plan_mode must succeed on the FIRST call — no guidance
// loop, no "fix ALL of the following then call exit_plan_mode again" round-trip
// (the live friction the user hit repeatedly). The execution-grade step shape
// (exact target, [F#] where findings exist, an observable Verify check) is stated
// PROACTIVELY in the update_plan tool description and the pipeline prelude, so the
// model is guided up front; anything still short of it is RECORDED as a plan-phase
// degradation — visible in the session — and the plan is accepted. The human
// approval pause, where the user reads the whole plan before a single edit is
// made, is the real quality gate. The one hard stop is structural: a plan with
// zero steps cannot be approved, so it is refused with a mechanical fix.
func (e *Engine) pipelinePlanContentBar(ctx context.Context) error {
	if e.LifecycleState() != contract.LifecyclePlanning {
		return nil
	}
	plan := e.CurrentPlan()
	if len(plan.Steps) == 0 {
		// Structural minimum — never waived: an empty plan cannot be approved
		// (the plan→approve gate requires written steps), so accepting here would
		// only move the dead end one call later. The fix is always mechanical.
		return errors.New("pipeline plan is incomplete: call update_plan with the ordered implementation steps first, then call exit_plan_mode again")
	}
	// Grounding is only meaningful when there is existing code to research and
	// cite. When NO research findings are banked — a greenfield/new-project task
	// with nothing to inspect, or research that legitimately produced none, or an
	// exhausted subagent allowance — record the research degradation so the plan
	// proceeds on direct investigation instead of demanding citations to findings
	// that cannot exist. This is what makes "build X from scratch" work: a
	// brand-new project has no [F#] evidence to ground on.
	if e.knowledge == nil || len(e.knowledge.ResearchFindings(1)) == 0 {
		if !e.Lifecycle().IsDegraded(contract.LifecycleResearch) {
			_ = e.RecordPipelineDegradation(ctx, contract.LifecycleResearch, "no research findings banked (greenfield/new project, or research produced none); the plan proceeds on direct investigation")
		}
	}
	gaps := e.planQualityGaps(plan)
	if len(gaps) == 0 {
		return nil
	}
	// Non-blocking acceptance: note what fell short of the execution-grade shape
	// and accept on this first call. Recorded once per planning phase so a
	// re-plan (KeepPlanning) does not stack duplicate notes.
	if !e.Lifecycle().IsDegraded(contract.LifecyclePlanning) {
		listed := gaps
		if len(listed) > 8 {
			listed = listed[:8]
		}
		reason := fmt.Sprintf("plan accepted with %d quality gap(s) noted: %s", len(gaps), strings.Join(listed, "; "))
		_ = e.RecordPipelineDegradation(ctx, contract.LifecyclePlanning, reason)
	}
	return nil
}

// planQualityGaps lists every unmet execution-grade requirement, first gap
// first. Pure inspection — the strike/waiver policy lives in the caller.
func (e *Engine) planQualityGaps(plan contract.Plan) []string {
	var gaps []string
	l := e.Lifecycle()
	findings := 0
	if e.knowledge != nil {
		findings = len(e.knowledge.ResearchFindings(12))
	}
	if findings == 0 && !l.IsDegraded(contract.LifecycleResearch) {
		gaps = append(gaps, instructions.GatePlanGapNoResearchBody)
	}
	// [F#] citations reference banked research findings; a step can only carry one
	// when findings exist. For a greenfield project (no findings) the citation is
	// meaningless, so it is not required — the step's target and Verify check are
	// what make it execution-grade.
	requireCitation := findings > 0
	for i, step := range plan.Steps {
		if missing := missingPlanStepRequirements(step.Title, requireCitation); len(missing) > 0 {
			gaps = append(gaps, fmt.Sprintf(instructions.GatePlanGapStepMissingTmpl, i+1, strings.Join(missing, ", ")))
		}
	}
	if labeledPlanSection(plan.Note, "verification", "risks") == "" {
		gaps = append(gaps, instructions.GatePlanGapNoVerificationBody)
	}
	if labeledPlanSection(plan.Note, "risks") == "" {
		gaps = append(gaps, instructions.GatePlanGapNoRisksBody)
	}
	return gaps
}

func missingPlanStepRequirements(title string, requireCitation bool) []string {
	lower := strings.ToLower(title)
	var missing []string
	if !pathRE.MatchString(title) && !strings.Contains(lower, "function") && !strings.Contains(lower, "package ") && !strings.Contains(lower, "symbol ") {
		missing = append(missing, "an exact file/function target")
	}
	if requireCitation && !findingCitationRE.MatchString(title) {
		missing = append(missing, "a [F#] finding citation")
	}
	if !acceptanceLabelRE.MatchString(title) {
		missing = append(missing, "an observable Acceptance:/Verify: check")
	}
	return missing
}
