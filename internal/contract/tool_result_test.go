package contract

import (
	"context"
	"errors"
	"testing"
)

func TestAdaptToolResultPreservesExecutionState(t *testing.T) {
	// Cases: success / rejected before start / cancellation after start / failure after start.
	cases := []struct {
		name   string
		err    error
		status ToolOutcomeStatus
		state  ToolExecutionState
	}{
		{name: "success", status: ToolOutcomeSucceeded, state: ToolExecutionCompleted},
		{name: "rejected", err: ToolNotStarted(errors.New("denied")), status: ToolOutcomeRejected, state: ToolExecutionNotStarted},
		{name: "cancelled", err: context.Canceled, status: ToolOutcomeCancelled, state: ToolExecutionIndeterminate},
		{name: "failed", err: errors.New("remote failed"), status: ToolOutcomeFailed, state: ToolExecutionIndeterminate},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := AdaptToolResult("diagnostic", testCase.err)
			if result.Status != testCase.status || result.State != testCase.state {
				t.Fatalf("result = %+v, want status=%s state=%s", result, testCase.status, testCase.state)
			}
			if result.Output != "diagnostic" {
				t.Fatalf("diagnostic output was lost: %+v", result)
			}
		})
	}
}
