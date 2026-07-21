package tui

import (
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

// TestTodoPanelGating covers the panel's whole gating table: the busy checklist
// and the two retire cases — no steps at all, and every step completed (A3
// T031/T034). The panel is driven purely by m.plan's step statuses; there is no
// lifecycle predicate and, since 013, no visibility toggle behind it.
func TestTodoPanelGating(t *testing.T) {
	m := todoModel(t)
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanInProgress)}
	m.busy = true
	if got := m.renderTodos(); got == "" || !strings.Contains(got, m.glyphs.todoActive) {
		t.Fatalf("busy panel should show the checklist:\n%s", got)
	}

	// No steps at all → nothing to show, busy or idle.
	m.plan = contract.Plan{}
	for _, busy := range []bool{true, false} {
		m.busy = busy
		if got := m.renderTodos(); got != "" {
			t.Fatalf("empty plan (busy=%v) should render nothing, got:\n%s", busy, got)
		}
	}

	// Every step completed → the checklist has retired itself, busy or idle.
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanCompleted)}
	for _, busy := range []bool{true, false} {
		m.busy = busy
		if got := m.renderTodos(); got != "" {
			t.Fatalf("fully completed plan (busy=%v) should retire the panel, got:\n%s", busy, got)
		}
	}
}

// TestTodoPanelIdleSummaryLine proves that idle with open items collapses the
// panel to a single counted summary line rather than the whole checklist, and
// that the count covers exactly the not-completed steps (A3 T032).
func TestTodoPanelIdleSummaryLine(t *testing.T) {
	m := todoModel(t)
	m.busy = false
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanPending, contract.PlanPending)}
	got := m.renderTodos()
	if !strings.Contains(got, "to-dos remaining") {
		t.Fatalf("idle plan with open items should show the summary line, got:\n%s", got)
	}
	if !strings.Contains(got, "2 to-dos") {
		t.Errorf("summary line should count 2 remaining (1 of 3 done), got:\n%s", got)
	}
	// It is a SUMMARY, not the checklist: exactly one rendered line, and no
	// per-step titles.
	if lines := strings.Split(strings.Trim(got, "\n"), "\n"); len(lines) != 1 {
		t.Errorf("idle summary should be one line, got %d:\n%s", len(lines), got)
	}
	if strings.Contains(got, "step 1") || strings.Contains(got, "step 2") {
		t.Errorf("idle summary must not render individual steps:\n%s", got)
	}
}

// TestTodoPanelStaysVisibleWhileActive (013 FR-025/FR-026): the checklist is
// simply there for the whole active life of a task. Ctrl+T is gone, and no key
// may hide the panel — the replacement for the old toggle test.
func TestTodoPanelStaysVisibleWhileActive(t *testing.T) {
	m := todoModel(t)
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanInProgress, contract.PlanPending)}
	m.busy = true
	if m.renderTodos() == "" {
		t.Fatal("panel should be visible while the agent is active")
	}
	// Ctrl+T is no longer handled: it falls through to the composer, and the
	// panel is unaffected.
	m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.renderTodos() == "" {
		t.Fatal("ctrl+t must no longer be able to hide the to-do panel")
	}
	// Repeated renders across the task's life keep showing it.
	for i := 0; i < 3; i++ {
		if m.renderTodos() == "" {
			t.Fatalf("panel disappeared on render %d while still active", i)
		}
	}
}
