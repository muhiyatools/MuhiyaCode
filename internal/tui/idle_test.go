package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestIdleStopsScheduling (US2 T026) proves that once a task ends and no notice is
// pending, the lifecycle tick stops rescheduling — an idle session issues no
// recurring application work.
func TestIdleStopsScheduling(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	// Enter a busy state → animation is needed.
	m.busy = true
	if !m.needsAnimation() {
		t.Fatal("busy state should need animation")
	}
	m.ensureTick()
	if !m.ticking {
		t.Fatal("tick did not start while busy")
	}
	// Task ends: a tick fires, sees no work, and stops.
	m.busy = false
	updated, _ = m.Update(tickMsg(time.Time{}))
	m = updated.(*Model)
	if m.ticking {
		t.Fatal("tick kept rescheduling after returning to idle")
	}
	if m.needsAnimation() {
		t.Fatal("idle session still reports needing animation")
	}
}

// TestExpiringNoticeDrivesThenStops (US2 T026) proves a transient notice keeps the
// tick alive until it expires, then rest resumes.
func TestExpiringNoticeDrivesThenStops(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.notify("saved") // sets an expiring notice
	if !m.needsAnimation() {
		t.Fatal("a pending expiring notice should need animation")
	}
	// Force the notice to have already expired, then tick: it clears and rest resumes.
	m.flash.expires = time.Now().Add(-time.Second)
	updated, _ = m.Update(tickMsg(time.Time{}))
	m = updated.(*Model)
	if m.flash.text != "" {
		t.Fatal("expired notice was not cleared")
	}
	if m.needsAnimation() {
		t.Fatal("rest not resumed after the notice expired")
	}
}
