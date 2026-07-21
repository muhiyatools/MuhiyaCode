//go:build windows

package workspace

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

func prepareCommand(cmd *exec.Cmd) {
	// HideWindow stops console children from flashing a window; a new process
	// group keeps our own Ctrl+C from propagating into the child and lets the
	// group be signalled independently.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// jobs maps a supervised command to the Job Object its process is attached to.
// Keying on the *exec.Cmd (not the PID, which Windows recycles) keeps the entry
// unambiguous for the lifetime of the call.
var (
	jobsMu sync.Mutex
	jobs   = map[*exec.Cmd]windows.Handle{}
)

// superviseProcess attaches the started process to a Job Object. Processes it
// later spawns inherit the job, so a grandchild that outlives the wrapper shell
// — a server launched via Start-Process, for instance, which reparents away
// from our PID and thereby escapes "taskkill /t" — stays in the job and can
// still be terminated as a unit. The job is created WITHOUT kill-on-close so the
// normal path (releaseProcess) can detach without killing an intentionally
// backgrounded process; only killProcessTree terminates it.
func superviseProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return // degrade to taskkill in killProcessTree
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	jobsMu.Lock()
	jobs[cmd] = job
	jobsMu.Unlock()
}

// killProcessTree terminates the entire descendant tree. When a Job Object is
// attached, terminating it kills every process still in the job at once —
// including a reparented grandchild. Otherwise it falls back to walking the live
// PID tree with taskkill.
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	jobsMu.Lock()
	job, ok := jobs[cmd]
	if ok {
		delete(jobs, cmd)
	}
	jobsMu.Unlock()
	if ok {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return
	}
	kill := exec.Command("taskkill.exe", "/pid", strconv.Itoa(cmd.Process.Pid), "/t", "/f")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

// releaseProcess detaches our bookkeeping without killing anything: it closes
// the job handle so a process the command intentionally left running keeps
// running (the job dissolves once empty). It is a no-op if killProcessTree
// already consumed the entry.
func releaseProcess(cmd *exec.Cmd) {
	jobsMu.Lock()
	job, ok := jobs[cmd]
	if ok {
		delete(jobs, cmd)
	}
	jobsMu.Unlock()
	if ok {
		_ = windows.CloseHandle(job)
	}
}
