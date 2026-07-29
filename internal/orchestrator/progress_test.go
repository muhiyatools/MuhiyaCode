package orchestrator

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestProgressEvidenceCountsReadOnlyWorkAndDeduplicatesRepeats(t *testing.T) {
	tracker := newProgressEvidenceTracker()
	read := toolOutcome{
		Call:   contract.NewToolCall("read-1", "read_file", `{"path":"main.go"}`),
		Status: contract.ToolOutcomeSucceeded, Output: "package main",
	}
	if !tracker.Observe([]toolOutcome{read}) || tracker.Revision() != 1 {
		t.Fatalf("first read was not evidence: revision=%d", tracker.Revision())
	}
	read.Call = contract.NewToolCall("read-2", "read_file", `{"path":"main.go"}`)
	if tracker.Observe([]toolOutcome{read}) || tracker.Revision() != 1 {
		t.Fatalf("identical read bought duplicate progress: revision=%d", tracker.Revision())
	}
	read.Output = "package main\nfunc main() {}"
	if !tracker.Observe([]toolOutcome{read}) || tracker.Revision() != 2 {
		t.Fatalf("changed observation was not new evidence: revision=%d", tracker.Revision())
	}
}
