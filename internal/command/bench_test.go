package command

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestBenchCommand_NoArgs(t *testing.T) {
	cmd := newBenchCommand()
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error calling bench without args, got nil")
	}
	if !strings.Contains(err.Error(), "benchmark task is empty") {
		t.Fatalf("expected 'benchmark task is empty', got %v", err)
	}
}

func TestBenchExitCodes(t *testing.T) {
	cases := []struct {
		status contract.BenchmarkStatus
		want   int
	}{
		{contract.BenchmarkPass, 0},
		{contract.BenchmarkFail, 2},
		{contract.BenchmarkTimeout, 3},
		{contract.BenchmarkBlocked, 4},
		{contract.BenchmarkError, 1},
	}
	for _, tc := range cases {
		err := benchmarkExitError{status: tc.status}
		if got := ExitCode(err); got != tc.want {
			t.Errorf("ExitCode(%q) = %d; want %d", tc.status, got, tc.want)
		}
	}
}

func TestBenchmarkStatus(t *testing.T) {
	if got := benchmarkStatus(contract.TaskStats{StopCause: contract.StopCauseTimeout}, nil); got != contract.BenchmarkTimeout {
		t.Errorf("expected timeout, got %v", got)
	}
	if got := benchmarkStatus(contract.TaskStats{}, context.DeadlineExceeded); got != contract.BenchmarkTimeout {
		t.Errorf("expected timeout, got %v", got)
	}
	if got := benchmarkStatus(contract.TaskStats{}, context.Canceled); got != contract.BenchmarkBlocked {
		t.Errorf("expected blocked, got %v", got)
	}
}

func TestEmitBenchSummary(t *testing.T) {
	var buf bytes.Buffer
	result := benchmarkResult{
		Status: contract.BenchmarkPass,
	}
	result.Usage.CompletionTokens = 456
	if err := emitBenchmarkResult(&buf, "", result, contract.Secrets{}, nil); err != nil {
		t.Fatalf("emitBenchmarkResult() error = %v", err)
	}
	if !strings.Contains(buf.String(), `"status":"pass"`) {
		t.Errorf("expected JSON to contain status pass, got %s", buf.String())
	}
}

func TestBenchConfiguredModelMissing(t *testing.T) {
	options := benchOptions{
		model: "missing-model",
	}
	cmd := newBenchCommand()
	cmd.SetIn(strings.NewReader("dummy task"))
	err := runBench(cmd, t.TempDir(), options)
	if err == nil || !strings.Contains(err.Error(), "missing from the local catalog") {
		t.Fatalf("expected missing model error, got %v", err)
	}
}

func TestBenchConfiguredAdvisorMode(t *testing.T) {
	// We can test that the advisor override goes into the result config.
	options := benchOptions{
		advisorMode: "routed",
		timeoutSec:  1,
	}
	cmd := newBenchCommand()
	cmd.SetIn(strings.NewReader("dummy task"))
	
	// Because of missing dummy models, we will probably get a missing model error 
	// or another configuration error, but we can check if it tries to set it.
	_ = runBench(cmd, t.TempDir(), options)
	// Just executing to ensure no panics with the overrides
}
