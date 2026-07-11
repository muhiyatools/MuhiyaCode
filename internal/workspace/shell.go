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
	Duration  time.Duration
}

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
	args := shellArgs(shell, command)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	prepareCommand(cmd)
	collector := &boundedWriter{limit: limit, onOutput: r.OnOutput}
	cmd.Stdout = collector
	cmd.Stderr = collector
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return ShellResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		killProcessTree(cmd)
		waitErr = <-done
	}
	output, truncated := collector.Result()
	if truncated {
		output = strings.TrimRight(output, " \t\r\n") + "\n... output truncated ..."
	}
	result := ShellResult{Shell: shell, ExitCode: 0, Output: output, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), Cancelled: errors.Is(parent.Err(), context.Canceled), Truncated: truncated, Duration: time.Since(started)}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) && ctx.Err() == nil {
			return result, waitErr
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
	if w.onOutput != nil {
		w.onOutput(string(value))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(value), nil
	}
	toWrite := value
	if len(toWrite) > remaining {
		toWrite = toWrite[:remaining]
		w.truncated = true
	}
	_, _ = io.Copy(&w.buffer, bytes.NewReader(toWrite))
	return len(value), nil
}

func (w *boundedWriter) Result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String(), w.truncated
}
