package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestFormatHarnessEventsEmpty(t *testing.T) {
	if got := formatHarnessEvents(nil); !strings.Contains(got, "clean run") {
		t.Fatalf("empty ring should read as a clean run, got: %q", got)
	}
}

func TestTaskSummaryHarnessMarker(t *testing.T) {
	colors := newPalette("muhiya-dark")
	base := contract.TaskStats{Usage: contract.Usage{TotalTokens: 100}}
	base.HarnessEvents = 2
	if got := taskSummaryLine(base, colors); !strings.Contains(got, "⚠ 2 harness") {
		t.Fatalf("a task that hit friction should mark it: %q", got)
	}
	base.HarnessEvents = 0
	if got := taskSummaryLine(base, colors); strings.Contains(got, "harness") {
		t.Fatalf("a clean task must add no harness noise: %q", got)
	}
}

func TestFormatHarnessEventsContent(t *testing.T) {
	at := time.Date(2026, 7, 16, 13, 45, 1, 0, time.Local)
	events := []contract.HarnessEvent{
		{At: at, Class: contract.HarnessGate, Code: "shell-readonly-block", Detail: "dir /s /b 2>nul"},
		{At: at, Class: contract.HarnessTool, Code: "tool-failure:read_file", Detail: "file not found"},
		{At: at, Class: contract.HarnessGate, Code: "repeat-limiter:grep", Detail: "same grep 4x"},
	}
	got := formatHarnessEvents(events)
	for _, want := range []string{"3 event(s)", "gate 2", "tool 1", "shell-readonly-block", "tool-failure:read_file", "repeat-limiter:grep"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted /errors panel missing %q:\n%s", want, got)
		}
	}
}
