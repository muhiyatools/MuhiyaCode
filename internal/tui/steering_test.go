package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSteeringNotLostInBusyEngineGap pins D1: when the UI is busy but the engine
// has no live task to steer (the startup / completion window where m.busy and
// e.cancel disagree), a submitted message must be restored to the composer, never
// silently dropped.
func TestSteeringNotLostInBusyEngineGap(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	m.ctx = context.Background()
	m.busy = true // UI thinks a task is in flight; the engine (no Run) returns false.
	before := len(m.items)
	m.submit("dont drop me")
	if m.input.Value() != "dont drop me" {
		t.Fatalf("steering text lost in the busy/engine gap: composer = %q, want restored", m.input.Value())
	}
	if len(m.items) != before {
		t.Fatalf("a restored (not queued) message must not add a transcript row: +%d", len(m.items)-before)
	}
}

// The delegate-row status test (D4) and the subagent shell-streaming test (D2)
// both retired with the subagent system: there is no Delegate row and no agent
// card. The shell output they were about now streams to the ordinary run_shell
// tool row, which tui_test.go's tool-lifecycle coverage already exercises.
//
// TestSteeringNotLostInBusyEngineGap above survives untouched: the busy/engine
// gap it guards is a property of the main loop, which is now the only loop.
