package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

type pipelineAgentOutcome struct {
	Input  subagentInput
	Output string
	Err    error
}

func (e *Engine) preparePipelinePhase(ctx context.Context, userPrompt string, budget Budget, profile EffortProfile) (string, error) {
	l := e.Lifecycle()
	if !l.Orchestrated() {
		return "", nil
	}
	switch l.State {
	case contract.LifecycleResearch:
		return e.runPipelineResearch(ctx, userPrompt, budget, profile)
	case contract.LifecyclePlanning:
		briefing := ""
		if e.knowledge != nil {
			briefing = e.knowledge.Briefing(4000)
		}
		return fmt.Sprintf(instructions.PipelinePlanningInputTmpl, briefing), nil
	case contract.LifecycleApproval:
		return instructions.PipelineApprovalGateBody, nil
	case contract.LifecycleImplementing:
		if l.Depth == PipelineDepthFull {
			return e.runPipelineImplementation(ctx, budget, profile)
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

func (e *Engine) runPipelineResearch(ctx context.Context, userPrompt string, budget Budget, profile EffortProfile) (string, error) {
	// Dynamic fan-out (act-like-Claude-Code): do not research an empty workspace.
	// A from-scratch build in a greenfield folder has no existing code to inspect,
	// so parallel explore agents would burn tokens mapping nothing. Skip straight
	// to planning and record why, instead of dispatching a fixed 2–3-agent fan-out.
	if !e.workspaceHasCodeToResearch() {
		reason := "greenfield workspace — no existing code to research; planning proceeds on the task description directly"
		if err := e.RecordPipelineDegradation(ctx, contract.LifecycleResearch, reason); err != nil {
			return "", err
		}
		if err := e.transitionLifecycle(ctx, contract.LifecyclePlanning); err != nil {
			return "", err
		}
		return "[research phase skipped]\nGreenfield workspace — no existing code to map; proceeding directly to planning.", nil
	}
	scopes, explicitDirs := pipelineResearchScopes(userPrompt)
	count := min(max(1, budget.MaxAgentRuns), len(scopes))
	if !explicitDirs {
		// The generic lenses re-read the same tree from different angles; scale
		// their number to the codebase size so a small project gets a single pass
		// rather than three. User-named disjoint directories (explicitDirs) keep
		// their per-directory fan-out untouched.
		count = min(count, e.dynamicResearchLensCount())
	}
	inputs := make([]subagentInput, 0, count)
	for i := 0; i < count; i++ {
		inputs = append(inputs, subagentInput{
			Agent: "explore",
			Title: scopes[i].title,
			Task:  fmt.Sprintf(instructions.PipelineResearchScopeTmpl, i+1, count, scopes[i].focus, userPrompt),
		})
	}
	outcomes := e.runPipelineAgentBatch(ctx, inputs, profile.ParallelAgents)
	succeeded := 0
	for _, outcome := range outcomes {
		if pipelineAgentSucceeded(outcome.Output, outcome.Err) {
			succeeded++
		}
	}
	if succeeded == 0 {
		reason := "all research subagents failed or returned unusable reports; continuing with bounded read-only main-agent investigation"
		if err := e.RecordPipelineDegradation(ctx, contract.LifecycleResearch, reason); err != nil {
			return "", err
		}
		if err := e.transitionLifecycle(ctx, contract.LifecyclePlanning); err != nil {
			return "", err
		}
	} else if err := e.MarkPipelineResearchCompleted(ctx); err != nil {
		return "", err
	}
	// Feature 012 R-D11 (fixes verified gap R-F15): in the normal single-Run
	// flow this research→planning transition happens MID-Run, and the planning
	// prelude (which carries the findings briefing) only fires when a Run
	// STARTS in planning — so before this fix the planner never received the
	// findings and read files itself. Deliver the briefing here, once, on the
	// same turn that announces research completion.
	status := fmt.Sprintf("[research phase complete]\n%d/%d research scopes produced usable reports before planning.", succeeded, len(inputs))
	if e.knowledge != nil {
		if briefing := e.knowledge.Briefing(4000); strings.TrimSpace(briefing) != "" {
			return status + "\n" + fmt.Sprintf(instructions.PipelinePlanningInputTmpl, briefing), nil
		}
	}
	return status, nil
}

type pipelineResearchScope struct{ title, focus string }

var explicitDirectoryScopeRE = regexp.MustCompile(`(?i)(?:^|[\s,(])([a-z0-9_.-]+[/\\])`)

// pipelineResearchScopes returns the research scopes for a prompt and whether
// they are explicit user-named directories (true) or the generic fallback lenses
// (false). The caller scales the generic lenses to codebase size but leaves
// disjoint directory scopes fanning out per directory.
func pipelineResearchScopes(userPrompt string) ([]pipelineResearchScope, bool) {
	seen := make(map[string]bool)
	var explicit []pipelineResearchScope
	for _, match := range explicitDirectoryScopeRE.FindAllStringSubmatch(userPrompt, 12) {
		if len(match) < 2 {
			continue
		}
		path := strings.ReplaceAll(match[1], "\\", "/")
		key := strings.ToLower(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		explicit = append(explicit, pipelineResearchScope{
			title: "Inspect " + path,
			focus: "Inspect only the independent scope " + path + ". Start listing at exactly " + path + ", never at workspace root. Read each relevant file once, identify exact change targets and acceptance checks, and do not re-explore sibling scopes.",
		})
	}
	if len(explicit) >= 2 {
		return explicit, true
	}
	// Generic fallback lenses are capped at THREE: they are not disjoint file
	// scopes, so every extra lens re-reads largely the same code (a live run
	// spent ~915k tokens fanning five near-identical full-repo reads over a
	// 14-file project). Three lenses cover what an execution-grade plan needs
	// — structure, change surface, acceptance; risks/UX observations belong in
	// each report. User-named directory scopes above still fan out per
	// directory (those ARE disjoint), bounded by the effort allowance.
	return []pipelineResearchScope{
		{"Architecture and execution path", "Map the relevant packages, entry points, data flow, and existing primitives. Cite exact files and symbols. Note structural risks you encounter; do not exhaustively read files outside the task's blast radius."},
		{"Implementation surface", "Find every file/function likely to change, current invariants, and compatibility constraints. Cite exact references. Skip files the task clearly does not touch."},
		{"Tests and acceptance", "Inspect existing tests and fixtures; derive concrete per-step acceptance checks and missing regression coverage. Include verification commands the plan should carry."},
	}, false
}

func (e *Engine) runPipelineAgentBatch(ctx context.Context, inputs []subagentInput, parallel bool) []pipelineAgentOutcome {
	outcomes := make([]pipelineAgentOutcome, len(inputs))
	run := func(index int) {
		// Harness-driven launches call the typed core directly — the JSON
		// boundary belongs only where the model's real tool call enters.
		output, runErr := e.runSubagentInput(ctx, inputs[index])
		outcomes[index] = pipelineAgentOutcome{Input: inputs[index], Output: output, Err: runErr}
	}
	if parallel && len(inputs) > 1 {
		var wg sync.WaitGroup
		for i := range inputs {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				run(index)
			}(i)
		}
		wg.Wait()
		return outcomes
	}
	for i := range inputs {
		run(i)
	}
	return outcomes
}

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

type pipelineStepGroup struct {
	Indexes     []int
	Independent bool
}

func groupPipelineSteps(plan contract.Plan, limit int) []pipelineStepGroup {
	var pending []int
	for i, step := range plan.Steps {
		if step.Status != contract.PlanCompleted {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	count := min(max(1, limit), len(pending))
	groups := make([]pipelineStepGroup, count)
	for i, index := range pending {
		group := i
		if group >= count {
			group = count - 1
		}
		groups[group].Indexes = append(groups[group].Indexes, index)
	}
	for i := range groups {
		groups[i].Independent = true
		for _, index := range groups[i].Indexes {
			title := strings.ToLower(plan.Steps[index].Title)
			if !strings.Contains(title, "[parallel]") && !strings.Contains(title, "independent") {
				groups[i].Independent = false
			}
			if strings.Contains(title, "depends on") || strings.Contains(title, " after ") || strings.Contains(title, "[serial]") {
				groups[i].Independent = false
			}
		}
	}
	return groups
}

// pipelineStepsAreDependent reports whether the pending plan steps carry a
// coherence dependency that makes fragmenting them across isolated blind
// subagents worse than one agent doing them in a single context: an explicit
// ordering marker ([serial] / "depends on" / " after "), or two pending steps
// that touch the same file. Independent, distinct-file steps return false and
// keep their per-group fan-out.
func pipelineStepsAreDependent(plan contract.Plan) bool {
	targetSeen := make(map[string]bool)
	for _, step := range plan.Steps {
		if step.Status == contract.PlanCompleted {
			continue
		}
		lower := strings.ToLower(step.Title)
		if strings.Contains(lower, "[serial]") || strings.Contains(lower, "depends on") || strings.Contains(lower, " after ") {
			return true
		}
		for target := range planStepTargets(step.Title) {
			if targetSeen[target] {
				return true
			}
			targetSeen[target] = true
		}
	}
	return false
}

// mergePipelineGroups folds every group's steps into one non-independent group —
// the single coherent implementation pass for dependent work.
func mergePipelineGroups(groups []pipelineStepGroup) pipelineStepGroup {
	merged := pipelineStepGroup{Independent: false}
	for _, group := range groups {
		merged.Indexes = append(merged.Indexes, group.Indexes...)
	}
	return merged
}

// pipelineStepPendingCount sums the steps across groups (the pending total).
func pipelineStepPendingCount(groups []pipelineStepGroup) int {
	n := 0
	for _, group := range groups {
		n += len(group.Indexes)
	}
	return n
}

func pipelineGroupsDisjoint(plan contract.Plan, groups []pipelineStepGroup) bool {
	seenTargets := make(map[string]bool)
	for _, group := range groups {
		if !group.Independent {
			return false
		}
		for _, index := range group.Indexes {
			targets := planStepTargets(plan.Steps[index].Title)
			if len(targets) == 0 {
				return false
			}
			for target := range targets {
				if seenTargets[target] {
					return false
				}
				seenTargets[target] = true
			}
		}
	}
	return true
}

func planStepTargets(title string) map[string]bool {
	targets := make(map[string]bool)
	for _, match := range pathRE.FindAllString(title, -1) {
		target := strings.ToLower(strings.Trim(match, " \t\r\n\"'`()[]"))
		if target != "" {
			targets[target] = true
		}
	}
	return targets
}

func (e *Engine) runPipelineImplementation(ctx context.Context, budget Budget, profile EffortProfile) (string, error) {
	plan := e.CurrentPlan()
	groups := groupPipelineSteps(plan, budget.MaxAgentRuns)
	if len(groups) == 0 {
		if err := e.MarkPipelineStepsComplete(ctx, true); err != nil {
			return "", err
		}
		return e.runPipelineValidation(ctx, budget.Class)
	}
	// Dynamic fan-out: a small set of DEPENDENT steps (explicit ordering, or a
	// shared file) loses coherence when split across isolated blind subagents that
	// cannot see each other's edits — collapse them into one agent that does the
	// whole sequence in a single context. Independent steps keep their per-group
	// fan-out; a large dependent plan keeps its bounded sequential chunking.
	if len(groups) > 1 && pipelineStepPendingCount(groups) <= implementationCoherentMax && pipelineStepsAreDependent(plan) {
		groups = []pipelineStepGroup{mergePipelineGroups(groups)}
	}
	inputs := make([]subagentInput, len(groups))
	parallel := profile.ParallelAgents && pipelineGroupsDisjoint(plan, groups)
	// Feature 012 PH-2: the phase handoff carries the plan Note's Verification/
	// Risks digest (bounded) beside the step titles, closing the step-titles-only
	// gap — the implementer sees what "done" must satisfy without re-deriving it.
	noteDigest := ""
	if note := strings.TrimSpace(plan.Note); note != "" {
		noteDigest = "\nPlan verification notes (digest): " + contract.Digest(note, 600)
	}
	for i, group := range groups {
		var steps []string
		for _, index := range group.Indexes {
			steps = append(steps, fmt.Sprintf("%d. %s", index+1, plan.Steps[index].Title))
		}
		inputs[i] = subagentInput{
			Agent: "general",
			Title: fmt.Sprintf("Implement plan part %d", i+1),
			Task:  fmt.Sprintf(instructions.PipelineImplementTaskTmpl, strings.Join(steps, "\n")) + noteDigest,
		}
	}
	outcomes := e.runPipelineAgentBatch(ctx, inputs, parallel)
	failed := 0
	for i, outcome := range outcomes {
		if pipelineAgentSucceeded(outcome.Output, outcome.Err) {
			e.completePipelineStepGroup(groups[i])
		} else {
			failed++
		}
	}
	if failed > 0 {
		// Feature 012 RG-4: a failed implementation dispatch opens the read gate
		// for the phase's remainder — the orchestrator diagnoses freely.
		e.markImplementFailureDiagnosis(ctx)
	}
	if err := e.writeCurrentPlan(ctx); err != nil {
		return "", err
	}
	complete := !e.hasIncompletePlan()
	if err := e.MarkPipelineStepsComplete(ctx, complete); err != nil {
		return "", err
	}
	if !complete {
		reason := fmt.Sprintf("%d implementation group(s) failed or were unusable; their steps remain open for one bounded direct recovery", failed)
		if err := e.RecordPipelineDegradation(ctx, contract.LifecycleImplementing, reason); err != nil {
			return "", err
		}
		return instructions.PipelineImplementationRecoveryBody, nil
	}
	validation, err := e.runPipelineValidation(ctx, budget.Class)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(instructions.PipelineImplementationCompleteTmpl, len(groups), validation), nil
}

func (e *Engine) completePipelineStepGroup(group pipelineStepGroup) {
	e.mu.Lock()
	for _, index := range group.Indexes {
		if index >= 0 && index < len(e.plan.Steps) {
			e.plan.Steps[index].Status = contract.PlanCompleted
		}
	}
	e.mu.Unlock()
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
	input := subagentInput{
		Agent:        "review",
		Title:        fmt.Sprintf("Validate approved implementation (%s review)", decision.Tier),
		Task:         fmt.Sprintf(instructions.PipelineValidateTaskTmpl, pipelineValidationBrief(plan)),
		TokenCeiling: decision.AbsoluteCapTokens,
	}
	if decision.Tier == ReviewTierFocused {
		// T022/D6: bound the focused tier to the changed surface + direct
		// dependents (cap 15) and require the machine-readable Coverage line.
		input.Task += instructions.PipelineValidateFocusedScopeBody
	}
	outcome := e.runPipelineAgentBatch(ctx, []subagentInput{input}, false)[0]
	e.updateTaskReviewOutcome(outcome.Output)
	if !pipelineValidationPassed(outcome.Output, outcome.Err) {
		e.taskMu.Lock()
		used, limit := e.taskPhaseAgentRuns[contract.LifecycleValidating], e.taskAgentCap
		e.taskMu.Unlock()
		if used < limit {
			e.recordPipelineEvent(ctx, "subagent_recovery", map[string]any{"phase": pipelineLabel(contract.LifecycleValidating), "strategy": "rescope_once"})
			input.Title = "Re-scoped validation"
			input.Task += instructions.PipelineValidateRecoveryAppendBody
			outcome = e.runPipelineAgentBatch(ctx, []subagentInput{input}, false)[0]
			e.updateTaskReviewOutcome(outcome.Output)
		}
	}
	if !pipelineValidationPassed(outcome.Output, outcome.Err) {
		reason := "validation subagent failed or returned an unusable report; main-agent verification is required"
		if err := e.RecordPipelineDegradation(ctx, contract.LifecycleValidating, reason); err != nil {
			return "", err
		}
		return instructions.PipelineValidationDegradedBody, nil
	}
	if err := e.ConfirmPipelineValidation(ctx, "completed review subagent pass"); err != nil {
		return "", err
	}
	return instructions.PipelineValidationCompleteBody, nil
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
