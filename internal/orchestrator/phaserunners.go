package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// preparePipelinePhase returns the current phase's instruction prelude.
// Feature 013 (model-driven dispatch, the user's standing directive): the
// harness NEVER launches subagents on its own — every phase instructs the main
// model, which chooses between direct work and run_subagent delegation. Phase
// progression stays harness-enforced (lifecycle gates, update_plan advancement,
// the review-verdict hook); only the launching moved to the model.
func (e *Engine) preparePipelinePhase(ctx context.Context, userPrompt string, budget Budget, profile EffortProfile) (string, error) {
	_, _, _ = userPrompt, budget, profile
	l := e.Lifecycle()
	if !l.Orchestrated() {
		return "", nil
	}
	switch l.State {
	case contract.LifecycleResearch:
		// Research is no longer a harness fan-out phase: transition straight to
		// planning and hand the model the investigate-then-plan instruction. Its
		// own explore dispatches (if it chooses any) bank findings as before.
		if err := e.MarkPipelineResearchCompleted(ctx); err != nil {
			return "", err
		}
		return e.planningPrelude(), nil
	case contract.LifecyclePlanning:
		return e.planningPrelude(), nil
	case contract.LifecycleApproval:
		return instructions.PipelineApprovalGateBody, nil
	case contract.LifecycleImplementing:
		if l.Depth == PipelineDepthFull {
			return e.runPipelineImplementation(ctx)
		}
		return instructions.PipelineLightImplementationBody, nil
	case contract.LifecycleValidating:
		if l.Depth == PipelineDepthFull && !l.Validated {
			return e.runPipelineValidation(ctx, budget.Class)
		}
		return instructions.PipelineValidationGateBody, nil
	case contract.LifecycleFinished:
		return instructions.PipelineCompleteBody, nil
	default:
		return "", nil
	}
}

// planningPrelude composes the planning instruction with whatever findings the
// model's own explore dispatches have banked (empty on a fresh task — the
// model investigates its own way first).
func (e *Engine) planningPrelude() string {
	briefing := ""
	if e.knowledge != nil {
		briefing = e.knowledge.Briefing(4000)
	}
	return fmt.Sprintf(instructions.PipelinePlanningInputTmpl, briefing)
}

// runPipelineImplementation (feature 013): no harness fan-out — the model
// executes the approved plan its own way (delegate step groups via
// run_subagent, or implement directly). update_plan progression advances the
// phase (maybeAdvancePipelineAfterPlanUpdate); an already-complete plan falls
// straight through to validation.
func (e *Engine) runPipelineImplementation(ctx context.Context) (string, error) {
	if !e.hasIncompletePlan() {
		if err := e.MarkPipelineStepsComplete(ctx, true); err != nil {
			return "", err
		}
		return e.runPipelineValidation(ctx, ClassLarge)
	}
	return instructions.PipelineImplementDelegateBody, nil
}

// pipelineAgentSucceeded reports whether a parent-visible subagent output is a
// usable report (feeds the review-verdict hook and resume paths).
func pipelineAgentSucceeded(output string, err error) bool {
	if err != nil || strings.TrimSpace(output) == "" {
		return false
	}
	lower := strings.ToLower(output)
	return !strings.Contains(lower, "[status: failed]") && !strings.Contains(lower, "[status: cancelled]") && !strings.Contains(lower, "returned no report")
}

func pipelineValidationPassed(output string, err error) bool {
	if !pipelineAgentSucceeded(output, err) {
		return false
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "verdict: pass") && !strings.Contains(lower, "verdict: fail")
}

func (e *Engine) writeCurrentPlan(ctx context.Context) error {
	plan := e.CurrentPlan()
	if e.persistence.WritePlan != nil {
		if err := e.persistence.WritePlan(ctx, e.executionPlanMarkdown(plan)); err != nil {
			return err
		}
	}
	if e.callbacks.PlanUpdate != nil {
		e.callbacks.PlanUpdate(plan)
	}
	return nil
}

func (e *Engine) runPipelineValidation(ctx context.Context, class TaskClass) (string, error) {
	l := e.Lifecycle()
	if l.State == contract.LifecycleFinished || l.Validated {
		return instructions.PipelineValidationAlreadyDoneBody, nil
	}
	if l.State != contract.LifecycleValidating {
		return "", nil
	}
	plan := e.CurrentPlan()
	// Feature 011 T018: the validation review is GATED, no longer unconditional.
	// Full-depth pipelines only exist for corroborated Large/Epic work (D2), so
	// this site's gate decides mode-off, tier, and ceiling — never a size skip.
	// Risk terms in the plan raise focused to deep; explicit user requests run
	// through the ungated run_subagent path and never depend on this site.
	samples := []string{pipelineValidationBrief(plan)}
	for _, step := range plan.Steps {
		samples = append(samples, step.Title)
	}
	decision := e.decideValidationReview(class, samples)
	e.setTaskReviewDecision(decision)
	if decision.Tier == ReviewTierSkip {
		if err := e.ConfirmPipelineValidation(ctx, "review gating off — automatic validation review skipped"); err != nil {
			return "", err
		}
		return instructions.PipelineValidationGateBody, nil
	}
	// Feature 013 (model-driven dispatch): the harness composes the review task
	// but the MODEL launches it. The run_subagent review-report hook
	// (observeOrchestratedSubagent) confirms validation on VERDICT: PASS; until
	// then completion stays blocked and this instruction re-renders.
	task := fmt.Sprintf(instructions.PipelineValidateTaskTmpl, pipelineValidationBrief(plan))
	if decision.Tier == ReviewTierFocused {
		// T022/D6: bound the focused tier to the changed surface + direct
		// dependents (cap 15) and require the machine-readable Coverage line.
		task += instructions.PipelineValidateFocusedScopeBody
	}
	return fmt.Sprintf(instructions.PipelineValidateInstructTmpl, task), nil
}

// observeOrchestratedSubagent (feature 013) watches MODEL-launched dispatches
// for the pipeline outcomes the harness used to own by dispatching itself: a
// review report during validating confirms validation on VERDICT: PASS, and a
// failed implementer during implementing opens the read gate for diagnosis
// (RG-4). Phase enforcement stays with the harness; only launching moved.
func (e *Engine) observeOrchestratedSubagent(ctx context.Context, kind string, result subagentResult) {
	l := e.Lifecycle()
	if !l.Orchestrated() {
		return
	}
	usable := result.Status == "done" || strings.HasPrefix(result.Status, "partial")
	switch {
	case l.State == contract.LifecycleValidating && kind == "review":
		e.updateTaskReviewOutcome(result.Report)
		lower := strings.ToLower(result.Report)
		if usable && strings.Contains(lower, "verdict: pass") && !strings.Contains(lower, "verdict: fail") {
			_ = e.ConfirmPipelineValidation(ctx, "model-launched review subagent")
		}
	case l.State == contract.LifecycleImplementing && kind == "general" && !usable:
		e.markImplementFailureDiagnosis(ctx)
	}
}

func pipelineValidationBrief(plan contract.Plan) string {
	var lines []string
	lines = append(lines, "Approved implementation steps:")
	for _, step := range plan.Steps {
		lines = append(lines, "- "+step.Title)
	}
	verification := labeledPlanSection(plan.Note, "verification", "risks")
	if verification == "" {
		verification = "Run the observable acceptance check named by every implementation step."
	}
	lines = append(lines, "", "Verification:", verification)
	return strings.Join(lines, "\n")
}

func (e *Engine) maybeAdvancePipelineAfterPlanUpdate(ctx context.Context) (string, error) {
	l := e.Lifecycle()
	if l.State != contract.LifecycleImplementing && l.State != contract.LifecycleValidating {
		return "", nil
	}
	complete := !e.hasIncompletePlan()
	if l.State == contract.LifecycleImplementing {
		if err := e.MarkPipelineStepsComplete(ctx, complete); err != nil {
			return "", err
		}
	}
	if !complete {
		return "", nil
	}
	l = e.Lifecycle()
	if l.State == contract.LifecycleFinished {
		return " Pipeline validation already passed; pipeline done.", nil
	}
	if l.Depth == PipelineDepthFull {
		// No task budget in scope here; a full-depth pipeline is Large/Epic-scale
		// by construction (NeedsPlan), so Large is the faithful class stand-in.
		validation, err := e.runPipelineValidation(ctx, ClassLarge)
		if err != nil {
			return "", err
		}
		return " " + validation, nil
	}
	return " All implementation steps are complete; run the verification command(s) to satisfy the validation gate.", nil
}
