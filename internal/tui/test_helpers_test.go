package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// Shared deterministic test infrastructure (T009). testRuntime (the fake
// runtime/provider) lives in tui_test.go; this file adds the frame-normalization
// and event/page fixtures the story tests build on. All fixtures are seed-free and
// deterministic so frame comparisons are stable across runs.

// normalizeFrame strips ANSI styling and trailing whitespace from every row so two
// frames can be compared for structural equality independent of color codes.
func normalizeFrame(frame string) string {
	rows := strings.Split(ansi.Strip(frame), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return strings.Join(rows, "\n")
}

// eventFixture builds n deterministic alternating user/assistant events plus an
// occasional tool event, with fixed content and timestamps.
func eventFixture(n int) []contract.Event {
	base := time.Unix(0, 0).UTC()
	events := make([]contract.Event, 0, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			events = append(events, contract.Event{Role: "user", Content: fmt.Sprintf("request %d", i), CreatedAt: base})
		case 1:
			events = append(events, contract.Event{Role: "assistant", Content: fmt.Sprintf("reply %d", i), CreatedAt: base})
		default:
			events = append(events, contract.Event{Role: "tool", Type: "read_file", Content: fmt.Sprintf("read %d lines", i), CreatedAt: base})
		}
	}
	return events
}

// TestEventFixtureLoadsDeterministically (T009) proves the fixture loads into the
// transcript and that the same seed-free input renders an identical normalized
// frame twice — the property the story frame-comparison tests rely on.
func TestEventFixtureLoadsDeterministically(t *testing.T) {
	build := func() (frame, transcript string) {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Recent: eventFixture(12)})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(*Model)
		m.refreshViewport(true)
		return normalizeFrame(m.View().Content), m.transcriptContent
	}
	frameA, transcriptA := build()
	frameB, _ := build()
	if frameA != frameB {
		t.Fatal("identical fixtures produced different normalized frames")
	}
	// The first event may be scrolled above the fold, so assert on the full
	// transcript content rather than only the visible frame.
	if !strings.Contains(ansi.Strip(transcriptA), "request 0") {
		t.Fatal("fixture events did not load into the transcript")
	}
}

// todoSteps builds checklist steps from a status sequence with generated titles.
func todoSteps(spec ...contract.PlanStatus) []contract.PlanStep {
	out := make([]contract.PlanStep, len(spec))
	for i, st := range spec {
		out[i] = contract.PlanStep{Title: fmt.Sprintf("step %d", i+1), Status: st}
	}
	return out
}
