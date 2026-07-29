//go:build !windows

package workspace

import (
	"errors"
	"os/exec"
	"syscall"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func prepareCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// superviseProcess is a no-op on Unix: Setpgid already places the child in its
// own process group and killProcessTree signals the whole group, so a
// backgrounded child in that group is reached without extra bookkeeping.
func superviseProcess(cmd *exec.Cmd) {}

// releaseProcess is a no-op on Unix; nothing is held between calls.
func releaseProcess(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Negative PID targets the whole process group created by Setpgid, so a child
	// backgrounded by the shell is killed along with it.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

func recoverableProcessTree(process contract.BackgroundProcess) bool {
	err := syscall.Kill(-process.PID, syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func stopRecoveredProcessTree(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
