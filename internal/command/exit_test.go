package command

import (
	"context"
	"errors"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestExitCodeMapsTaskTerminalStates(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, ExitSuccess},
		{errors.New("boom"), ExitGeneralError},
		{context.Canceled, ExitCancelled},
		{&TaskExitError{Status: contract.TaskStatusIncomplete}, ExitIncomplete},
		{&TaskExitError{Status: contract.TaskStatusFailed}, ExitFailed},
		{&TaskExitError{Status: contract.TaskStatusIndeterminate}, ExitIndeterminate},
		{&TaskExitError{Status: contract.TaskStatusCancelled}, ExitCancelled},
	}
	for _, test := range cases {
		if got := ExitCode(test.err); got != test.want {
			t.Errorf("ExitCode(%v) = %d, want %d", test.err, got, test.want)
		}
	}
}
