package instructions

import "fmt"

// Example is one worked example a Text can point to via its Example field.
// Registered separately from Text because several Texts legitimately
// reference the SAME worked example (the canonical-copy discipline, IS-5) —
// keeping examples in their own namespace makes "does this example exist
// exactly once" a direct registry lookup rather than a text-body diff.
type Example struct {
	ID   string
	Body string
}

var (
	examples     = map[string]Example{}
	exampleOrder []string
)

// RegisterExample adds an Example to the registry and returns it unchanged.
// Panics on an empty or duplicate ID, matching Register's discipline.
func RegisterExample(e Example) Example {
	if e.ID == "" {
		panic("instructions: Example registered with empty ID")
	}
	if _, exists := examples[e.ID]; exists {
		panic(fmt.Sprintf("instructions: duplicate Example ID %q", e.ID))
	}
	examples[e.ID] = e
	exampleOrder = append(exampleOrder, e.ID)
	return e
}

// PlanStepExampleBody is the ONE canonical worked plan-step title (IS-5):
// before feature 010 this exact shape was duplicated across four sites with
// one of them silently drifting to a different file/finding/check. Every
// site that shows the model a plan-step example now renders this literal
// string via ExamplePlanStep, so a future edit to the shape can only happen
// in one place and the drift class is structurally impossible.
const PlanStepExampleBody = `src/store/editorStore.ts: add undo/redo history stack (50 snapshots) [F2] — Verify: npx tsc --noEmit passes`

// ExamplePlanStep is the registered form of PlanStepExampleBody. It passes
// pipeline.go's missingPlanStepRequirements validator (a path target, an
// [F#] citation, and a Verify: acceptance check) — pinned by
// internal/instructions/audit_test.go's worked-example-passes-validator
// check (IS-7) and internal/orchestrator/instructions_wiring_test.go, which
// calls the real validator against this exact body.
var ExamplePlanStep = RegisterExample(Example{ID: "example.plan-step", Body: PlanStepExampleBody})

// PlanNoteExampleBody is the canonical worked plan-note body (IS-7's fifth
// incoherence fix): before feature 010 no production text anywhere showed a
// concrete plan note with literal "Verification:" and "Risks:" labels and
// content, only a description of the requirement. It passes pipeline.go's
// labeledPlanSection parser for both "verification" and "risks" sections —
// pinned by the same worked-example-passes-validator audit check.
const PlanNoteExampleBody = "Verification:\n- npx tsc --noEmit passes\n- go test ./internal/store -run TestUndoRedo\nRisks:\n- history stack adds bounded memory (50-snapshot cap); no unbounded growth"

// ExamplePlanNote is the registered form of PlanNoteExampleBody.
var ExamplePlanNote = RegisterExample(Example{ID: "example.plan-note", Body: PlanNoteExampleBody})

// DelegationTaskExampleBody is the worked example in the run_subagent tool
// description showing the caller how to phrase a delegated task.
const DelegationTaskExampleBody = `find every caller of ApplyDiscount across services/, check which pass a nil tax table, and report file:line for each`

// ExampleDelegationTask is the registered form of DelegationTaskExampleBody.
var ExampleDelegationTask = RegisterExample(Example{ID: "example.delegation-task", Body: DelegationTaskExampleBody})
