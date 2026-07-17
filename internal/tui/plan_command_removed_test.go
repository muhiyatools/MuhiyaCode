package tui

import "testing"

// TestPlanCommandRemoved pins P1: /plan is gone from the command palette (planning
// is an internal agent flow now — the classifier handles "create a plan…" and
// "execute the plan in X.md" from natural language).
func TestPlanCommandRemoved(t *testing.T) {
	for _, c := range commands {
		if c.name == "/plan" {
			t.Fatal("/plan must be removed from the command palette")
		}
	}
}
