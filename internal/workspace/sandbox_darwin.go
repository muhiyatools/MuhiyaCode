//go:build darwin

package workspace

import (
	"fmt"
	"os/exec"
	"strconv"
)

type macOSSandbox struct {
	executable string
	workspace  string
}

func newPlatformSandbox(workspace string) SandboxBackend {
	if container := configuredContainerSandbox(workspace); container != nil {
		return container
	}
	executable, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return unavailableSandbox{capability: SandboxCapability{Backend: "sandbox-exec", Reason: "sandbox-exec was not found"}}
	}
	return macOSSandbox{executable: executable, workspace: workspace}
}

func (macOSSandbox) Capability() SandboxCapability {
	return SandboxCapability{Backend: "sandbox-exec", Enforced: true, NetworkDenied: true}
}

func (s macOSSandbox) Wrap(request SandboxRequest) ([]string, error) {
	if !IsInside(s.workspace, request.CWD) {
		return nil, fmt.Errorf("sandbox cwd %s is outside workspace %s", request.CWD, s.workspace)
	}
	workspace := strconv.Quote(s.workspace)
	profile := `(version 1)(deny default)(allow process*)(allow file-read* (subpath "/usr") (subpath "/bin") (subpath "/System") (subpath "/Library") (subpath "/dev") (subpath "/private/tmp"))(allow file-read* file-write* (subpath ` + workspace + `))`
	arguments := []string{s.executable, "-p", profile, "--"}
	return append(arguments, request.Arguments...), nil
}
