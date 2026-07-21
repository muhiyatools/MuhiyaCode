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
