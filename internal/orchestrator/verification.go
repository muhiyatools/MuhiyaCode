package orchestrator

import (
	"context"
	"os"
	"path/filepath"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func DiscoverTestCommand(workspace string) string {
	if _, err := os.Stat(filepath.Join(workspace, "go.mod")); err == nil {
		return "go test ./..."
	}
	if _, err := os.Stat(filepath.Join(workspace, "package.json")); err == nil {
		return "npm test"
	}
	if _, err := os.Stat(filepath.Join(workspace, "Cargo.toml")); err == nil {
		return "cargo test"
	}
	if _, err := os.Stat(filepath.Join(workspace, "pyproject.toml")); err == nil {
		return "pytest"
	}
	return ""
}

func VerifyWorkspace(ctx context.Context, workspace string, runCmd func(context.Context, string) (string, error)) contract.VerificationResult {
	cmd := DiscoverTestCommand(workspace)
	if cmd == "" {
		return contract.VerificationResult{Ran: false}
	}
	out, err := runCmd(ctx, cmd)
	res := "success"
	if err != nil {
		res = "failure"
	}
	truncated := ""
	if len(out) > 500 {
		truncated = out[len(out)-500:]
	}
	return contract.VerificationResult{
		Ran:             true,
		Command:         cmd,
		Result:          res,
		OutputTruncated: truncated,
	}
}
