package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

type ShellResult struct {
	Shell     string
	ExitCode  int
	Output    string
	TimedOut  bool
	Cancelled bool
	Truncated bool
	// BackgroundLeft is set when the command itself returned but a process it
	// spawned (a server, a detached job) kept the output pipe open past the kill
	// grace. The command did not fail — MuhiyaCode simply stopped waiting on the
	// inherited pipe so the agent stays responsive instead of hanging.
	BackgroundLeft bool
	Duration       time.Duration
}

func (s ShellResult) RawToolResult() RawToolResult {
	status := RawResultSuccess
	if s.TimedOut {
		status = RawResultTimeout
	} else if s.Cancelled {
		status = RawResultCancelled
	} else if s.ExitCode != 0 {
		status = RawResultFailure
	}
	return RawToolResult{
		Status:            status,
		Complete:          !s.Truncated,
		Content:           []byte(s.Output),
		SourceFingerprint: fmt.Sprintf("exit-%d", s.ExitCode),
		ExitCode:          &s.ExitCode,
		TimedOut:          s.TimedOut,
		Cancelled:         s.Cancelled,
		Encoding:          "utf-8",
	}
}

// shellKillGrace bounds how long Run waits for a process's inherited output
// pipes to close after the process exits or after a kill is issued. A detached
// grandchild (e.g. a server launched via Start-Process) can hold those pipes
// open forever; once the grace elapses Wait force-closes them and returns, so a
// shell command can never wedge the agent indefinitely.
const shellKillGrace = 3 * time.Second

// errShellAbandoned is returned internally when even the bounded post-kill wait
// elapses — the process tree was killed but something still held the pipe. It is
// treated like a timeout/cancel for reporting (never surfaced as a raw error).
var errShellAbandoned = errors.New("shell process abandoned after kill grace")

type ShellRunner struct {
	Preferred   string
	Timeout     time.Duration
	OutputLimit int
	OnOutput    func(string)
}

func ChooseShell(preferred string) (string, error) {
	if runtime.GOOS != "windows" {
		if preferred != "" && preferred != "auto" && preferred != "sh" {
			return "", fmt.Errorf("shell %q is not supported on %s", preferred, runtime.GOOS)
		}
		if _, err := exec.LookPath("sh"); err != nil {
			return "", fmt.Errorf("find sh: %w", err)
		}
		return "sh", nil
	}
	if preferred != "" && preferred != "auto" {
		name := preferred
		if preferred == "cmd" {
			name = "cmd.exe"
		}
		if _, err := exec.LookPath(name); err != nil {
			return "", fmt.Errorf("find %s: %w", preferred, err)
		}
		return preferred, nil
	}
	for _, candidate := range []string{"pwsh", "powershell", "cmd"} {
		name := candidate
		if candidate == "cmd" {
			name = "cmd.exe"
		}
		if _, err := exec.LookPath(name); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("no supported Windows shell found")
}

func (r *ShellRunner) Run(parent context.Context, cwd, command string, timeout time.Duration) (ShellResult, error) {
	if strings.TrimSpace(command) == "" {
		return ShellResult{}, errors.New("shell command is empty")
	}
	shell, err := ChooseShell(r.Preferred)
	if err != nil {
		return ShellResult{}, err
	}
	return r.runArgv(parent, cwd, shell, shellArgs(shell, command), timeout)
}

// runArgv executes an already-split argv without a shell, so a caller that
// builds its own arguments never composes a command string an interpreter
// could re-parse. label names the program for ShellResult.Shell.
func (r *ShellRunner) runArgv(parent context.Context, cwd, label string, args []string, timeout time.Duration) (ShellResult, error) {
	if len(args) == 0 {
		return ShellResult{}, errors.New("shell command is empty")
	}
	if timeout <= 0 {
		timeout = r.Timeout
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if timeout > MaxShellTimeout {
		timeout = MaxShellTimeout
	}
	limit := r.OutputLimit
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// CommandContext ties the process to ctx: on deadline or task cancellation Go
	// invokes cmd.Cancel (below) and then, after WaitDelay, force-closes the
	// inherited I/O pipes so Wait always returns. Without WaitDelay a detached
	// grandchild that keeps stdout open (a server started via Start-Process, a
	// backgrounded job) blocks Wait forever — the root cause of a shell call that
	// runs for minutes after it should have stopped.
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Cancel = func() error { killProcessTree(cmd); return nil }
	cmd.WaitDelay = shellKillGrace
	prepareCommand(cmd)
	collector := &boundedWriter{limit: limit, onOutput: r.OnOutput}
	cmd.Stdout = collector
	cmd.Stderr = collector
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return ShellResult{}, err
	}
	// Bind the process (and its descendants) to a supervised group/job so
	// killProcessTree can terminate the whole tree — even a grandchild that
	// outlived the wrapper shell. releaseProcess detaches without killing on the
	// normal path so an intentionally-backgrounded process is left running.
	superviseProcess(cmd)
	defer releaseProcess(cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
		// Bounded even when a daemon inherited the pipe: WaitDelay closes it.
	case <-ctx.Done():
		// Deadline hit or the task was cancelled. CommandContext already ran Cancel
		// (the tree kill) and WaitDelay bounds the unwind; wait for Wait to return,
		// but never past the grace — an orphaned grandchild holding the pipe must
		// not be able to wedge the agent (which would also freeze Stop, since the
		// task goroutine cannot unwind until this returns).
		select {
		case waitErr = <-done:
		case <-time.After(shellKillGrace + time.Second):
			killProcessTree(cmd) // best-effort second attempt
			waitErr = errShellAbandoned
		}
	}
	output, truncated := collector.Result()
	if truncated {
		output = strings.TrimRight(output, " \t\r\n") + "\n... output truncated ..."
	}
	result := ShellResult{Shell: label, ExitCode: 0, Output: output, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), Cancelled: errors.Is(parent.Err(), context.Canceled), Truncated: truncated, Duration: time.Since(started)}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		switch {
		case errors.Is(waitErr, exec.ErrWaitDelay):
			// The command returned, but a background process it started still held
			// the output pipe past the grace. Not a failure — report it so the model
			// knows a process was left running rather than treating it as an error.
			result.BackgroundLeft = true
		case errors.Is(waitErr, errShellAbandoned):
			// Timeout/cancel is already reflected in TimedOut/Cancelled below.
		default:
			var exit *exec.ExitError
			if !errors.As(waitErr, &exit) && ctx.Err() == nil {
				return result, waitErr
			}
		}
	}
	return result, nil
}

func shellArgs(shell, command string) []string {
	switch shell {
	case "cmd":
		return []string{"cmd.exe", "/d", "/s", "/c", command}
	case "pwsh", "powershell":
		return []string{shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	default:
		return []string{"sh", "-c", command}
	}
}

type boundedWriter struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
	onOutput  func(string)
}

func (w *boundedWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = true
		w.mu.Unlock()
		return len(value), nil
	}
	toWrite := value
	if len(toWrite) > remaining {
		toWrite = toWrite[:remaining]
		w.truncated = true
	}
	_, _ = io.Copy(&w.buffer, bytes.NewReader(toWrite))
	chunk := string(append([]byte(nil), toWrite...))
	w.mu.Unlock()
	if w.onOutput != nil && chunk != "" {
		w.onOutput(chunk)
	}
	return len(value), nil
}

func (w *boundedWriter) Result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String(), w.truncated
}
