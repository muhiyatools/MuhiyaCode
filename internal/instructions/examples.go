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

// DelegationTaskExampleBody is the worked example in the run_subagent tool
// description showing the caller how to phrase a delegated task.
const DelegationTaskExampleBody = `find every caller of ApplyDiscount across services/, check which pass a nil tax table, and report file:line for each`

// ExampleDelegationTask is the registered form of DelegationTaskExampleBody.
var ExampleDelegationTask = RegisterExample(Example{ID: "example.delegation-task", Body: DelegationTaskExampleBody})
