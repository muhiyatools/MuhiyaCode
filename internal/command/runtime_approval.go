package command

import (
	"context"
	"errors"

	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

func (a *Application) confirmWorkspaceApproval(
	ctx context.Context,
	engine *orchestrator.Engine,
	request workspace.ApprovalRequest,
) (bool, error) {
	record, err := engine.BeginApproval(ctx, string(request.Action), approvalTarget(request), request.Reason)
	if err != nil {
		return false, err
	}
	approved, approvalErr := false, error(nil)
	if a.callbacks.Confirm != nil {
		approved, approvalErr = a.callbacks.Confirm(ctx, approvalMessage(request))
	}
	if err := engine.ResolveApproval(ctx, record, approved, approvalErr); err != nil {
		return false, errors.Join(approvalErr, err)
	}
	return approved, approvalErr
}

func approvalTarget(request workspace.ApprovalRequest) string {
	if request.Action == workspace.ActionShell {
		return request.Command
	}
	return request.Path
}
