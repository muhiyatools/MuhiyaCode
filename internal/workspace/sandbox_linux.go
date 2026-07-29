//go:build linux

package workspace

import (
	"fmt"
	"os/exec"
)

type bubblewrapSandbox struct {
	executable string
	workspace  string
}

func newPlatformSandbox(workspace string) SandboxBackend {
	if container := configuredContainerSandbox(workspace); container != nil {
		return container
	}
	executable, err := exec.LookPath("bwrap")
	if err != nil {
		return unavailableSandbox{capability: SandboxCapability{Backend: "bubblewrap", Reason: "bubblewrap executable was not found"}}
	}
	return bubblewrapSandbox{executable: executable, workspace: workspace}
}

func (bubblewrapSandbox) Capability() SandboxCapability {
	return SandboxCapability{Backend: "bubblewrap", Enforced: true, NetworkDenied: true}
}

func (s bubblewrapSandbox) Wrap(request SandboxRequest) ([]string, error) {
	if !IsInside(s.workspace, request.CWD) {
		return nil, fmt.Errorf("sandbox cwd %s is outside workspace %s", request.CWD, s.workspace)
	}
	arguments := []string{
		s.executable,
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--share-user",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", s.workspace, s.workspace,
		"--chdir", request.CWD,
		"--",
	}
	return append(arguments, request.Arguments...), nil
}
