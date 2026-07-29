package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	ExitSuccess       = 0
	ExitGeneralError  = 1
	ExitIncomplete    = 3
	ExitFailed        = 4
	ExitIndeterminate = 5
	ExitCancelled     = 130
)

type TaskExitError struct {
	Status contract.TaskStatus
	Reason string
}

func (e *TaskExitError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("task %s", e.Status)
	}
	return fmt.Sprintf("task %s: %s", e.Status, e.Reason)
}

func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if errors.Is(err, context.Canceled) {
		return ExitCancelled
	}
	var taskErr *TaskExitError
	if !errors.As(err, &taskErr) {
		return ExitGeneralError
	}
	switch taskErr.Status {
	case contract.TaskStatusIncomplete:
		return ExitIncomplete
	case contract.TaskStatusFailed:
		return ExitFailed
	case contract.TaskStatusIndeterminate:
		return ExitIndeterminate
	case contract.TaskStatusCancelled:
		return ExitCancelled
	default:
		return ExitGeneralError
	}
}
