package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func todoModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	_ = m.Init()
	return mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
}

// todoSteps builds a plan from a status sequence with generated titles.
func todoSteps(spec ...contract.PlanStatus) []contract.PlanStep {
	out := make([]contract.PlanStep, len(spec))
	for i, st := range spec {
		out[i] = contract.PlanStep{Title: fmt.Sprintf("step %d", i+1), Status: st}
	}
	return out
}

// TestTodoRowsCollapse pins the collapse algorithm (A3 T030): under budget shows
// every step; over budget rolls up completed steps (when ≥2) and summarizes the
// remainder as "… N more", always within the row budget.
func TestTodoRowsCollapse(t *testing.T) {
	m := todoModel(t)
	P, I, D := contract.PlanPending, contract.PlanInProgress, contract.PlanCompleted
	cases := []struct {
		name     string
		steps    []contract.PlanStep
		wantRows int
		wantHas  []string
		wantNot  []string
	}{
		{"under budget all shown", todoSteps(D, I, P), 3, nil, []string{"done", "more"}},
		{"exactly five", todoSteps(D, D, I, P, P), 5, nil, []string{"more"}},
		{"rollup and more", todoSteps(D, D, D, I, P, P, P, P), 5, []string{"3 done", "2 more"}, nil},
		{"no rollup when done is one", todoSteps(D, I, P, P, P, P), 5, []string{"2 more"}, []string{"done"}},
		{"all completed roll up", todoSteps(D, D, D, D, D, I), 2, []string{"5 done"}, []string{"more"}},
		{"no completed steps", todoSteps(I, P, P, P, P, P, P, P), 5, []string{"4 more"}, []string{"done"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := m.todoRows(tc.steps)
			if len(rows) != tc.wantRows {
				t.Fatalf("got %d rows, want %d:\n%s", len(rows), tc.wantRows, strings.Join(rows, "\n"))
			}
			joined := strings.Join(rows, "\n")
			for _, h := range tc.wantHas {
				if !strings.Contains(joined, h) {
					t.Errorf("rows missing %q:\n%s", h, joined)
				}
			}
			for _, n := range tc.wantNot {
				if strings.Contains(joined, n) {
					t.Errorf("rows unexpectedly contain %q:\n%s", n, joined)
				}
			}
		})
	}
}

// TestTodoRowGlyphsAndAsciiFallback proves each status renders with its state glyph
// and that the ASCII glyph table is honored (A3 T030).
func TestTodoRowGlyphsAndAsciiFallback(t *testing.T) {
	m := todoModel(t)
	if r := m.todoRow(contract.PlanStep{Title: "x", Status: contract.PlanInProgress}); !strings.Contains(r, m.glyphs.todoActive) {
		t.Errorf("in-progress row missing the active glyph: %q", r)
	}
	if r := m.todoRow(contract.PlanStep{Title: "x", Status: contract.PlanCompleted}); !strings.Contains(r, m.glyphs.todoDone) {
		t.Errorf("completed row missing the done glyph: %q", r)
	}
	// ASCII fallback: swap the glyph table and confirm the bracket markers appear.
	m.glyphs = newGlyphs(true)
	if r := m.todoRow(contract.PlanStep{Title: "x", Status: contract.PlanPending}); !strings.Contains(r, "[ ]") {
		t.Errorf("ascii pending row should use [ ]: %q", r)
	}
}

// TestTodoRowRTLTitleStaysValid proves an Arabic step title passes through the RTL
// shaper without corrupting the row (A3 T030/INV-10).
func TestTodoRowRTLTitleStaysValid(t *testing.T) {
	m := todoModel(t)
	row := m.todoRow(contract.PlanStep{Title: "مرحبا بالعالم", Status: contract.PlanInProgress})
	if row == "" || !utf8.ValidString(row) {
		t.Fatalf("RTL title produced an invalid row: %q", row)
	}
}

// TestTodoPanelGating covers the busy checklist, the Ctrl+T hide, and the idle
// non-resumable case where the panel stays empty (A3 T031/T034).
func TestTodoPanelGating(t *testing.T) {
	m := todoModel(t)
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanInProgress)}
	m.busy = true
	if got := m.renderTodos(); got == "" || !strings.Contains(got, m.glyphs.todoActive) {
		t.Fatalf("busy panel should show the checklist:\n%s", got)
	}
	m.todoVisible = false
	if got := m.renderTodos(); got != "" {
		t.Fatalf("Ctrl+T-hidden panel should render nothing, got:\n%s", got)
	}
	m.todoVisible = true
	m.busy = false
	if got := m.renderTodos(); got != "" {
		t.Fatalf("idle non-resumable plan should render nothing, got:\n%s", got)
	}
}

// TestTodoPanelTerminalRetires proves a terminal (discarded) plan retires the panel
// even while busy with steps (A3 T034).
func TestTodoPanelTerminalRetires(t *testing.T) {
	rt := testRuntime(t)
	rt.Engine.DiscardPlan() // → discarded, a terminal lifecycle state
	m := NewModel(Options{Runtime: rt, Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m.busy = true
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanInProgress)}
	if got := m.renderTodos(); got != "" {
		t.Fatalf("a terminal plan must retire the panel, got:\n%s", got)
	}
}

// TestTodoPanelIdleResumeLine proves a pending (resumable) plan shows the compact
// idle resume line with the remaining count (A3 T032).
func TestTodoPanelIdleResumeLine(t *testing.T) {
	rt := testRuntime(t)
	rt.Engine.SetLifecycleState(contract.LifecyclePlanning) // → planning (read-only)
	rt.Engine.DeferPlan(context.Background())               // → pending (invites "proceed")
	m := NewModel(Options{Runtime: rt, Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m.busy = false
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanPending, contract.PlanPending)}
	got := m.renderTodos()
	if !strings.Contains(got, "to-dos remaining") || !strings.Contains(got, "proceed") {
		t.Fatalf("pending plan should show the idle resume line, got:\n%s", got)
	}
	if !strings.Contains(got, "2 to-dos") {
		t.Errorf("resume line should count 2 remaining (1 of 3 done), got:\n%s", got)
	}
}

// TestCtrlTTogglesTodoPanel drives the real keybinding to prove Ctrl+T flips the
// panel visibility (A3 T033, the wiring-inventory Verified-by for ctrl+t).
func TestCtrlTTogglesTodoPanel(t *testing.T) {
	m := todoModel(t)
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanInProgress, contract.PlanPending)}
	m.busy = true
	if m.renderTodos() == "" {
		t.Fatal("panel should be visible by default")
	}
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("ctrl+t should be a pure UI toggle with no command")
	}
	if m.todoVisible {
		t.Fatal("ctrl+t did not clear todoVisible")
	}
	if m.renderTodos() != "" {
		t.Fatal("panel should be hidden after ctrl+t")
	}
	// A second press restores it.
	m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if !m.todoVisible || m.renderTodos() == "" {
		t.Fatal("second ctrl+t did not restore the panel")
	}
}
