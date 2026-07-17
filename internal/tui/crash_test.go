package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecoverTaskPanicSurvivesAndReports (T052/D11) proves a task-goroutine panic
// becomes a calm task error (the terminal survives) and writes a crash report
// naming the panic — instead of a raw stack dump crashing the process.
func TestRecoverTaskPanicSurvivesAndReports(t *testing.T) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("muhiyacode-crash-%d.log", os.Getpid()))
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	rm := recoverTaskPanic("boom in the widget")
	if rm.err == nil || !strings.Contains(rm.err.Error(), "stopped this task safely") {
		t.Fatalf("expected a calm task-error, got: %v", rm.err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("crash report was not written: %v", err)
	}
	if !strings.Contains(string(data), "panic: boom in the widget") {
		t.Fatalf("crash report does not name the panic:\n%s", data)
	}
	if !strings.Contains(string(data), "MuhiyaCode") {
		t.Fatalf("crash report missing build identity:\n%s", data)
	}
}
