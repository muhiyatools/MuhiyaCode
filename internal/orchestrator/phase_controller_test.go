package orchestrator

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPhaseControllerTransitionsFromObservedActions(t *testing.T) {
	controller := newPhaseController()
	if controller.phase != phaseExplore {
		t.Fatalf("initial phase=%s", controller.phase)
	}
	controller.Observe([]toolOutcome{{
		Call:   contract.NewToolCall("write", "write_file", `{"path":"x","content":"y"}`),
		Status: contract.ToolOutcomeSucceeded, Output: "wrote x",
	}})
	if controller.phase != phaseExecute || controller.EvidenceRevision() != 1 {
		t.Fatalf("after write: %+v", controller)
	}
	controller.Observe([]toolOutcome{{
		Call:   contract.NewToolCall("test", "run_shell", `{"command":"go test ./..."}`),
		Status: contract.ToolOutcomeSucceeded, Output: "ok",
	}})
	if controller.phase != phaseVerify {
		t.Fatalf("after check phase=%s", controller.phase)
	}
	controller.transition(phaseFinalize)
	controller.Observe([]toolOutcome{{
		Call:   contract.NewToolCall("late", "write_file", `{"path":"x","content":"z"}`),
		Status: contract.ToolOutcomeSucceeded, Output: "wrote x",
	}})
	if controller.phase != phaseFinalize {
		t.Fatalf("final phase accepted a late action: %+v", controller)
	}
}
