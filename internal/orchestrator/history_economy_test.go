package orchestrator

import (
	"strings"
	"testing"
)

func TestTaskControlNoticesReplaceAndDeduplicate(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.UpsertTaskControl("governor.converge", "Two steps remain; finish essential work.")
	history.UpsertTaskControl("governor.converge", "Two steps remain; finish essential work.")
	history.UpsertTaskControl("failure.recover", "Change approach once or report the blocker.")

	messages := history.All()
	controlCount := 0
	for _, message := range messages {
		if strings.HasPrefix(message.Content, TaskControlPrefix) {
			controlCount++
			if !strings.Contains(message.Content, "failure.recover") {
				t.Fatalf("stale task control survived replacement: %q", message.Content)
			}
		}
	}
	if controlCount != 1 {
		t.Fatalf("task control messages=%d, want one current checkpoint: %+v", controlCount, messages)
	}
}

func TestIdenticalTaskControlDoesNotRewriteHistory(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	if changed := history.UpsertTaskControl("governor.final", "Finish factually without tools."); !changed {
		t.Fatal("first control checkpoint was not recorded")
	}
	version := history.RewriteVersion()
	if changed := history.UpsertTaskControl("governor.final", "Finish factually without tools."); changed {
		t.Fatal("identical control checkpoint rewrote history")
	}
	if history.RewriteVersion() != version {
		t.Fatal("deduplicated control checkpoint changed rewrite version")
	}
}
