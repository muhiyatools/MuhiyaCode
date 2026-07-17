package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) executeOne(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition) (string, error) {
	name := call.ToolName()
	switch name {
	case "update_plan":
		return e.updatePlan(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "ask_user":
		return e.askUser(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "propose_changes":
		return e.proposeChanges(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "save_memory":
		// Feature 008 US5 (T029): first-class durable-memory save. The
		// definition, the approval-gate parity, and the append helper live in
		// memory.go.
		return e.saveMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "recall_memory":
		// Experience Overhaul B3: read one topic file from the per-project store.
		// Read-only, so no approval gate; the store dir is engine-managed.
		return e.recallMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "edit_memory":
		// Memory Parity N2: update, delete, or consolidate one saved entry. The
		// definition, matcher, approval-gate parity, and rewrite live in memory.go.
		return e.editMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "run_subagent":
		return e.runSubagentTool(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "exit_plan_mode":
		// T017/REV B2a: only meaningful in a read-only lifecycle. A stray
		// exit_plan_mode call outside plan mode is a harmless no-op — it must NOT
		// end the task or set a phantom pending plan.
		// 009/010: the orchestrated planning phase runs the content bar and
		// advances planning→approval. Before the unification the legacy planMode
		// flag and the pipeline phase could desync, stranding exit_plan_mode as a
		// no-op while the phase gate blocked every mutation; the single lifecycle
		// state makes that stall unrepresentable.
		l := e.Lifecycle()
		if l.State == contract.LifecyclePlanning && l.Orchestrated() {
			if err := e.pipelinePlanContentBar(ctx); err != nil {
				return "", err
			}
			// Belt and suspenders: the bar guarantees ≥1 step, so the plan is
			// written by definition — stamp it before the gated transition so a
			// missed MarkPipelinePlanWritten can never re-stall the exit.
			e.MarkPipelinePlanWritten(ctx)
			if err := e.transitionLifecycle(ctx, contract.LifecycleApproval); err != nil {
				return "", err
			}
			return "", contract.ErrPlanModeExited
		}
		if !e.PlanMode() {
			return "not in plan mode — continue with the task", nil
		}
		return "", contract.ErrPlanModeExited // P2: a soft signal that the task should finalize
	default:
		allowed := make(map[string]bool)
		for _, definition := range definitions {
			allowed[definition.Function.Name] = true
		}
		return e.registry.Execute(ctx, name, json.RawMessage(call.ArgumentsJSON()), allowed)
	}
}

// repairJSONStringEscapes doubles invalid backslash escapes inside JSON
// string values (\s, \d, \w, Windows paths… — regex/path text the model wrote
// verbatim, a live update_plan failure: "invalid character 's' in string
// escape code"). Applied ONLY after Unmarshal fails; valid JSON is untouched.
func repairJSONStringEscapes(raw []byte) []byte {
	out := make([]byte, 0, len(raw)+8)
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			out = append(out, c)
			continue
		}
		if c == '\\' {
			if i+1 < len(raw) {
				next := raw[i+1]
				switch next {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
					out = append(out, c, next)
				default:
					// Invalid JSON escape: keep the author's literal backslash.
					out = append(out, '\\', '\\', next)
				}
				i++
				continue
			}
			out = append(out, '\\', '\\')
			continue
		}
		if c == '"' {
			inString = false
		}
		out = append(out, c)
	}
	return out
}

// decodeToolArgs unmarshals tool arguments with a one-shot invalid-escape
// rescue so a regex like \d+ inside a string argument does not hard-fail the
// whole tool call.
func decodeToolArgs(raw json.RawMessage, v any) error {
	err := json.Unmarshal(raw, v)
	if err == nil {
		return nil
	}
	if repaired := repairJSONStringEscapes(raw); json.Unmarshal(repaired, v) == nil {
		return nil
	}
	return err
}

const (
	// planStepsGuideMax is the guided ideal (the tool description says "≤12");
	// planStepsHardMax is the accept-with-note ceiling. Beyond it the gate rejects
	// once, then accepts (D5/T032) — it never loops or destroys steps.
	planStepsGuideMax = 12
	planStepsHardMax  = 24
)

func (e *Engine) updatePlan(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Steps []contract.PlanStep `json:"steps"`
		Note  *string             `json:"note"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	// D5 (T032): the step cap is BOUNDED, not a hard wall. An empty plan is the
	// one hard stop (a plan needs steps). ≤12 is the guided ideal. 13–24 is
	// accepted with a soft telemetry note. >24 is rejected ONCE with the mechanical
	// fix, then accepted on a second attempt — the model's steps are never
	// destroyed, and the gate can never loop (policy clause c). This replaced the
	// old hard "1-12 steps" wall that produced the live rejection pain.
	if len(input.Steps) == 0 {
		return "", errors.New("plan requires at least one step — call update_plan with the ordered implementation steps")
	}
	if len(input.Steps) > planStepsHardMax {
		e.taskMu.Lock()
		already := e.taskOversizedPlanRejected
		e.taskOversizedPlanRejected = true
		e.taskMu.Unlock()
		if !already {
			e.recordHarnessEvent(ctx, contract.HarnessGate, "plan-steps-exceeded", fmt.Sprintf("%d steps", len(input.Steps)))
			return "", fmt.Errorf("plan has %d steps — that is too granular. Merge related edits into phase-sized steps (aim for ≤%d) and move per-file detail into the note's Verification:/Risks: sections, then call update_plan again", len(input.Steps), planStepsGuideMax)
		}
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "plan-steps-waived", fmt.Sprintf("accepted %d steps after one guidance round", len(input.Steps)))
	} else if len(input.Steps) > planStepsGuideMax {
		e.recordHarnessEvent(ctx, contract.HarnessGate, "plan-steps-soft-exceeded", fmt.Sprintf("%d steps (>%d ideal)", len(input.Steps), planStepsGuideMax))
	}
	sawActive := false
	for i := range input.Steps {
		input.Steps[i].Title = strings.TrimSpace(input.Steps[i].Title)
		if input.Steps[i].Title == "" {
			return "", errors.New("plan step title is empty")
		}
		if input.Steps[i].Status == contract.PlanInProgress {
			if sawActive {
				input.Steps[i].Status = contract.PlanPending
			}
			sawActive = true
		}
		if input.Steps[i].Status != contract.PlanPending && input.Steps[i].Status != contract.PlanInProgress && input.Steps[i].Status != contract.PlanCompleted {
			return "", fmt.Errorf("invalid plan status %q", input.Steps[i].Status)
		}
	}
	// Models commonly refine only the step list after the first update. Treat an
	// omitted note as a partial update, not as an instruction to erase the
	// already-approved Verification/Risks contract. An explicit empty note still
	// clears it when the caller genuinely intends that.
	note := e.CurrentPlan().Note
	if input.Note != nil {
		note = contract.TruncateEllipsis(*input.Note, 4000)
	}
	plan := contract.Plan{Steps: input.Steps, Note: note, UpdatedAt: time.Now().UTC()}
	e.mu.Lock()
	e.plan = plan
	e.mu.Unlock()
	// 004 US2 / 010: lifecycle transitions driven by plan edits. In a read-only
	// state the model is still drafting — the state itself needs no change. Out of
	// the read-only window, the first step that starts or completes means a saved
	// plan (pending/interrupted) is being executed: flip it to implementing — the
	// phrasing-independent backstop (T8) for a go-ahead the continuation matchers
	// (T7) did not catch.
	if !e.PlanMode() && e.LifecycleState().InvitesProceed() {
		if planHasProgressingStep(input.Steps) {
			e.SetLifecycleState(contract.LifecycleImplementing)
		}
	}
	if e.persistence.WritePlan != nil {
		if err := e.persistence.WritePlan(ctx, e.executionPlanMarkdown(plan)); err != nil {
			return "", err
		}
	}
	if l := e.Lifecycle(); l.State == contract.LifecyclePlanning && l.Orchestrated() {
		e.MarkPipelinePlanWritten(ctx)
	}
	if e.callbacks.PlanUpdate != nil {
		e.callbacks.PlanUpdate(plan)
	}
	completed := 0
	for _, step := range plan.Steps {
		if step.Status == contract.PlanCompleted {
			completed++
		}
	}
	advance, err := e.maybeAdvancePipelineAfterPlanUpdate(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("To-dos updated: %d/%d done.%s", completed, len(plan.Steps), advance), nil
}

func (e *Engine) askUser(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Questions []contract.Question `json:"questions"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Questions) == 0 || len(input.Questions) > 3 || e.callbacks.Ask == nil {
		return "", errors.New("ask_user requires 1-3 questions and an interactive client")
	}
	// Each question must carry at least two choices (the tool schema requires it,
	// but the payload is model-controlled and smaller models sometimes omit them).
	// Reject a choiceless question with a tool error the model can act on, rather
	// than passing it to the modal where indexing an empty choice slice would panic.
	for i, q := range input.Questions {
		if len(q.Choices) < 2 {
			return "", fmt.Errorf("ask_user question %d (%q) must include at least 2 choices", i+1, q.Question)
		}
	}
	answers, err := e.callbacks.Ask(ctx, input.Questions)
	if err != nil {
		return "", err
	}
	return encodeAnswers(answers), nil
}

func (e *Engine) proposeChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.callbacks.Ask == nil {
		// 004 US3 (T036): label the non-interactive verdict honestly — no human
		// reviewed this proposal, so the model must not read it as human approval.
		return `{"verdict":"auto_approved","note":"auto-approved by non-interactive policy — no human reviewed this proposal; proceed minimally and verify each change yourself"}`, nil
	}
	var input struct {
		Summary        string `json:"summary"`
		EstimatedSteps int    `json:"estimatedSteps"`
		Files          []any  `json:"files"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	answers, err := e.callbacks.Ask(ctx, []contract.Question{{Question: fmt.Sprintf("Approve this change plan? (%d files, ~%d steps)\n%s", len(input.Files), input.EstimatedSteps, input.Summary), Choices: []contract.QuestionChoice{{Label: "Proceed", Description: "Apply and verify the plan", Recommended: true}, {Label: "Proceed carefully", Description: "Minimize each edit and stop on surprises"}, {Label: "Stop", Description: "Do not edit"}}}})
	if err != nil {
		return "", err
	}
	index := 0
	if len(answers) > 0 {
		index = answers[0].Index
	}
	if index == 2 {
		return `{"verdict":"rejected","instruction":"Do not edit; summarize the plan and wait."}`, nil
	}
	if index == 1 {
		return `{"verdict":"approved_with_caution","instruction":"Make the smallest changes and verify each file."}`, nil
	}
	return `{"verdict":"approved","instruction":"Execute and verify the plan."}`, nil
}

func (e *Engine) hasIncompletePlan() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, step := range e.plan.Steps {
		if step.Status != contract.PlanCompleted {
			return true
		}
	}
	return false
}

// appendCompletionDisclosure (004 US1, T014/FR-017) appends an honest note when
// the model finalizes a plan that is executing (or resumed-interrupted) with
// steps still open, so the final answer never implies work it did not do. A
// completed plan, a pre-execution plan, or a non-plan task is untouched — the
// note is driven entirely by recorded step state, not the model's self-report
// (research B3: the launch model tends to over-claim completion).
func (e *Engine) appendCompletionDisclosure(content string) string {
	if !disclosesIncomplete(e.LifecycleState()) {
		return content
	}
	plan := e.CurrentPlan()
	done, total := planStepProgress(plan)
	if total == 0 || done >= total {
		return content
	}
	var open []string
	for _, step := range plan.Steps {
		if step.Status != contract.PlanCompleted {
			open = append(open, step.Title)
			if len(open) == 3 {
				break
			}
		}
	}
	return content + fmt.Sprintf("\n\n— %d of %d to-dos incomplete: %s", total-done, total, strings.Join(open, "; "))
}

func (e *Engine) appendPipelineAttribution(content string) string {
	l := e.Lifecycle()
	if !(l.State == contract.LifecycleFinished && l.Orchestrated()) || strings.Contains(strings.ToLower(content), "phase contributions:") {
		return content
	}
	return content + "\n\nPhase contributions: research supplied scoped evidence; planning converted it into approved, verifiable steps; implementation executed those steps; validation independently checked the result."
}
