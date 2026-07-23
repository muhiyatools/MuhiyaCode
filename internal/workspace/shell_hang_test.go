package workspace

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// backgroundHolderShell returns a (preferredShell, command, marker) that prints
// marker, then leaves a background child alive holding the inherited stdout pipe
// for far longer than the test tolerates. This is the shape that used to wedge
// run_shell: the wrapper shell exits immediately but Wait blocked until the
// detached child closed the pipe (minutes), so the tool call "ran" long after it
// should have returned.
func backgroundHolderShell() (preferred, command, marker string) {
	if runtime.GOOS == "windows" {
		// Start-Process -NoNewWindow makes the child inherit our stdout handle, then
		// the launching PowerShell exits — exactly the Start-Process ... http.server
		// case from the bug report, minus the network bind.
		return "powershell", "Start-Process -NoNewWindow -FilePath powershell -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 20'; Write-Output holder-started", "holder-started"
	}
	return "sh", "sleep 20 & echo holder-started", "holder-started"
}

// TestRunShellDoesNotHangOnBackgroundChild is the regression for the reported
// hang: a command whose spawned child keeps the output pipe open must still
// return promptly (bounded by the kill grace), not block until the child exits.
func TestRunShellDoesNotHangOnBackgroundChild(t *testing.T) {
	preferred, command, marker := backgroundHolderShell()
	runner := &ShellRunner{Preferred: preferred, Timeout: 2 * time.Minute, OutputLimit: 1 << 16}
	// Deliberately NOT t.TempDir(): the surviving background child inherits this as
	// its working directory, and t.TempDir()'s auto-cleanup would fail trying to
	// remove a directory a live process still holds. The shared temp root is never
	// removed, so the leaked child is harmless.
	start := time.Now()
	result, err := runner.Run(context.Background(), os.TempDir(), command, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// The background child lives 20s; the grace is 3s. Anything near 20s means Wait
	// blocked on the inherited pipe — the old bug. A generous 12s bound keeps CI
	// slack while still failing hard on the pre-fix behavior.
	if elapsed > 12*time.Second {
		t.Fatalf("Run blocked %v on a background child — the pipe wait is not bounded", elapsed)
	}
	if !result.BackgroundLeft {
		t.Fatalf("expected BackgroundLeft to flag the surviving child, got %+v", result)
	}
	if !strings.Contains(result.Output, marker) {
		t.Fatalf("expected the foreground output %q, got %q", marker, result.Output)
	}
	if result.TimedOut || result.Cancelled {
		t.Fatalf("a returning command must not be reported as timed out/cancelled: %+v", result)
	}
}

// longRunningShell returns a (preferredShell, command) that blocks ~30s so a
// cancel/timeout has something live to interrupt.
func longRunningShell() (preferred, command string) {
	if runtime.GOOS == "windows" {
		return "powershell", "Start-Sleep -Seconds 30"
	}
	return "sh", "sleep 30"
}

// TestRunShellCancelReturnsPromptly is the regression for the stuck-on-"Stopping"
// symptom: cancelling the parent context must terminate the process and return
// quickly (so the task goroutine can unwind and the UI can leave "Stopping…"),
// with the result marked Cancelled.
func TestRunShellCancelReturnsPromptly(t *testing.T) {
	preferred, command := longRunningShell()
	runner := &ShellRunner{Preferred: preferred, Timeout: 2 * time.Minute, OutputLimit: 1 << 16}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	result, err := runner.Run(ctx, t.TempDir(), command, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Cancelled {
		t.Fatalf("expected Cancelled after context cancel, got %+v", result)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("cancel took %v — the post-cancel wait is not bounded", elapsed)
	}
}

// TestRunShellTimeoutReturnsPromptly asserts a per-call timeout is honored and
// bounded: the process is killed and Run returns near the deadline, not later.
func TestRunShellTimeoutReturnsPromptly(t *testing.T) {
	preferred, command := longRunningShell()
	runner := &ShellRunner{Preferred: preferred, Timeout: 2 * time.Minute, OutputLimit: 1 << 16}
	start := time.Now()
	result, err := runner.Run(context.Background(), t.TempDir(), command, 500*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected TimedOut after a 500ms timeout, got %+v", result)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("timeout took %v — the post-timeout wait is not bounded", elapsed)
	}
}
