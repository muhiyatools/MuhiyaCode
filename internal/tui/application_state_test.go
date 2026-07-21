package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// stateModel builds an 80x24 model ready for state-cue assertions.
func stateModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(*Model)
}

// frameText returns the ANSI-stripped rendered frame, so cue assertions test the
// visible text, not escape codes.
func frameText(m *Model) string { return ansi.Strip(m.View().Content) }

// TestFirstRunState (v1.1.0) — an empty session renders an empty transcript.
// The welcome cue is deliberately gone: the composer, header, and mode-line
// hints orient a new user without a greeting to scroll past every session.
func TestFirstRunState(t *testing.T) {
	m := stateModel(t)
	m.refreshViewport(true)
	if strings.Contains(frameText(m), "Welcome to MuhiyaCode") {
		t.Fatal("the removed welcome cue is back")
	}
	if strings.TrimSpace(m.transcriptContent) != "" {
		t.Fatalf("empty session rendered transcript content: %q", m.transcriptContent)
	}
}

// TestBusyPrestreamState (US6 T071/T074) — a busy task with no tokens yet shows an
// explicit working cue (spinner + status), never a blank screen.
func TestBusyPrestreamState(t *testing.T) {
	m := stateModel(t)
	m.busy = true
	m.status = "Thinking…"
	m.started = time.Now()
	m.refreshViewport(true)
	if !strings.Contains(frameText(m), "Thinking") {
		t.Fatal("busy-prestream state did not show a working cue")
	}
}

// TestToolRunningState (US6 T071/T074) — a running tool is marked as running.
func TestToolRunningState(t *testing.T) {
	m := stateModel(t)
	m.busy = true
	m.items = append(m.items, item{kind: "tool", tool: &toolView{name: "read_file", target: "x.go", state: "running", started: time.Now()}})
	m.refreshViewport(true)
	text := frameText(m)
	if !strings.Contains(text, "Read") || !strings.Contains(text, "running") {
		t.Fatalf("tool-running state cue missing: %q", firstLines(text, 12))
	}
}

// The subagent-running state retired with the subagent system: there are no
// agent chips because there are no agents. The running-tool cue above is the
// surviving in-flight indicator.

// TestErrorState (US6 T071/T074) — an error surfaces on the notice line.
func TestErrorState(t *testing.T) {
	m := stateModel(t)
	m.warn("build failed unexpectedly")
	m.refreshViewport(true)
	if !strings.Contains(frameText(m), "build failed unexpectedly") {
		t.Fatal("error state cue missing")
	}
}

// TestTooSmallState (US6 T071/T074) — below the render floor shows a clear message.
func TestTooSmallState(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m = updated.(*Model)
	if !strings.Contains(frameText(m), "too small") {
		t.Fatal("too-small state cue missing")
	}
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}
