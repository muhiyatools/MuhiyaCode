package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestVerifyWorkspaceUsesTypedToolFailure(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := VerifyWorkspace(context.Background(), workspace, func(context.Context, string) contract.ToolResult {
		return contract.AdaptToolResult(
			"tool failed: command exited with code 1.\nexit code: 1\ncompile failed",
			errors.New("command exited with code 1"),
		)
	})

	if result.Result != "failure" {
		t.Fatalf("non-zero shell output must fail verification: %+v", result)
	}
	if !strings.Contains(result.OutputTruncated, "compile failed") {
		t.Fatalf("verification must retain short diagnostic output: %+v", result)
	}
}

func TestVerifyWorkspaceRetainsExecutionError(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := VerifyWorkspace(context.Background(), workspace, func(context.Context, string) contract.ToolResult {
		return contract.AdaptToolResult("", errors.New("shell unavailable"))
	})

	if result.Result != "failure" || !strings.Contains(result.OutputTruncated, "shell unavailable") {
		t.Fatalf("execution errors must be recorded as verification evidence: %+v", result)
	}
}
