package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestDeterministicOutcomeFinalRequiresMutationAndVerification(t *testing.T) {
	mutation := toolOutcome{Call: contract.NewToolCall("edit", "edit_file", `{"path":"ui.css"}`), Output: "Applied edit"}
	verification := toolOutcome{Call: contract.NewToolCall("check", "run_shell", `{"command":"go test ./..."}`), Output: "ok"}
	final, ok := deterministicOutcomeFinal([]toolOutcome{mutation, verification}, "", map[string]bool{"ui.css": true})
	if !ok || !strings.Contains(final, "ui.css") || !strings.Contains(final, "Verification passed") {
		t.Fatalf("final=%q complete=%t", final, ok)
	}
	if _, ok := deterministicOutcomeFinal([]toolOutcome{mutation}, "", map[string]bool{"ui.css": true}); ok {
		t.Fatal("mutation without verification finalized without another request")
	}
	verification.Failed = true
	if _, ok := deterministicOutcomeFinal([]toolOutcome{mutation, verification}, "", map[string]bool{"ui.css": true}); ok {
		t.Fatal("failed verification produced a success final")
	}
}

func TestDeterministicOutcomeFinalReusesAdequateAssistantFinal(t *testing.T) {
	mutation := toolOutcome{Call: contract.NewToolCall("edit", "edit_file", `{"path":"ui.css"}`)}
	verification := toolOutcome{Call: contract.NewToolCall("check", "run_shell", `{"command":"go test ./..."}`)}
	want := "Updated the button and the focused test passes."
	if got, ok := deterministicOutcomeFinal([]toolOutcome{mutation, verification}, want, nil); !ok || got != want {
		t.Fatalf("got=%q complete=%t", got, ok)
	}
}
