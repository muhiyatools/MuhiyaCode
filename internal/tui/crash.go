package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/muhiya/muhiyacode/internal/buildinfo"
	"github.com/muhiya/muhiyacode/internal/secrecy"
)

// recoverTaskPanic converts a panic from the engine task goroutine into a task
// error so the TUI survives instead of the whole terminal crashing with a raw
// stack (Stability Overhaul T052, defect D11). It writes a best-effort crash
// report and returns a resultMsg carrying a calm, actionable message.
func recoverTaskPanic(r any) resultMsg {
	path := writeCrashLog(r, debug.Stack())
	message := "MuhiyaCode hit an internal error and stopped this task safely — your session is still open."
	if path != "" {
		message += " A crash report was saved to " + path
	}
	return resultMsg{err: errors.New(message)}
}

// writeCrashLog saves the panic value, stack, and build identity to a timestamp-
// free per-process file (so it is stable within a run) under the OS temp dir.
// Best-effort: returns "" on any failure — a crash report must never itself panic.
func writeCrashLog(r any, stack []byte) string {
	name := filepath.Join(os.TempDir(), fmt.Sprintf("muhiyacode-crash-%d.log", os.Getpid()))
	content := fmt.Sprintf("MuhiyaCode %s (commit %s, built %s)\npanic: %v\n\n%s\n",
		buildinfo.Version, buildinfo.Commit, buildinfo.Date, r, stack)
	content = secrecy.Redact(content)
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		return ""
	}
	return name
}
