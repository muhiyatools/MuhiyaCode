package workspace

import (
	"context"
	"os"
	"path/filepath"
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
		return "powershell", "$s = New-Object System.Diagnostics.ProcessStartInfo 'powershell', '-NoProfile -Command Start-Sleep -Seconds 20'; $s.UseShellExecute = $false; [System.Diagnostics.Process]::Start($s) | Out-Null; Write-Output holder-started", "holder-started"
	}
	return "sh", "sleep 20 & echo holder-started", "holder-started"
}

// TestRunShellDoesNotHangOnBackgroundChild is the regression for the reported
// hang: a command whose spawned child keeps the output pipe open must still
// return promptly (bounded by the kill grace), not block until the child exits.
func TestRunShellDoesNotHangOnBackgroundChild(t *testing.T) {
	preferred, command, marker := backgroundHolderShell()
	registry := filepath.Join(t.TempDir(), "processes.json")
	manager := newProcessManager(registry)
	runner := &ShellRunner{Preferred: preferred, Timeout: 2 * time.Minute, OutputLimit: 1 << 16, Processes: manager}
	defer manager.Close()
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
	if result.BackgroundProcessID == "" {
		t.Fatalf("background process was not assigned an owned ID: %+v", result)
	}
	if listed := manager.List(); len(listed) != 1 || listed[0].ID != result.BackgroundProcessID {
		t.Fatalf("owned process list = %+v", listed)
	}
	if payload, readErr := os.ReadFile(registry); readErr != nil || !strings.Contains(string(payload), result.BackgroundProcessID) {
		t.Fatalf("durable process registry missing ownership: payload=%q err=%v", payload, readErr)
	}
	recovered := newProcessManager(registry)
	if listed := recovered.List(); len(listed) != 1 || listed[0].ID != result.BackgroundProcessID {
		t.Fatalf("durably recovered process list = %+v", listed)
	}
	if err := recovered.Stop(result.BackgroundProcessID); err != nil {
		t.Fatalf("stop owned process: %v", err)
	}
	if listed := recovered.List(); len(listed) != 0 {
		t.Fatalf("stopped process remained listed: %+v", listed)
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
