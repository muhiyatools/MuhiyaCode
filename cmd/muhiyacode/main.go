package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/muhiya/muhiyacode/internal/buildinfo"
	"github.com/muhiya/muhiyacode/internal/command"
)

func main() {
	// T052/D11: last-resort panic backstop. The TUI already contains task-goroutine
	// panics (tui/crash.go) and Bubble Tea recovers Update panics; this catches
	// anything else (startup, teardown) so a crash saves a report and exits
	// cleanly instead of dumping a raw stack over the user's terminal.
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("muhiyacode-crash-%d.log", os.Getpid()))
			_ = os.WriteFile(path, []byte(fmt.Sprintf("MuhiyaCode %s (commit %s, built %s)\npanic: %v\n\n%s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, r, debug.Stack())), 0o644)
			fmt.Fprintf(os.Stderr, "MuhiyaCode crashed. A crash report was saved to %s\n", path)
			os.Exit(1)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command.Execute(ctx, os.Args[1:]); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}
