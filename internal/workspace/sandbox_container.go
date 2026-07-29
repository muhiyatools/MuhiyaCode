package workspace

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const sandboxImageEnvironment = "MUHIYA_SANDBOX_CONTAINER_IMAGE"

type containerSandbox struct {
	runtime     string
	image       string
	workspace   string
	runtimeName string
}

func configuredContainerSandbox(workspace string) SandboxBackend {
	image := strings.TrimSpace(os.Getenv(sandboxImageEnvironment))
	if image == "" {
		return nil
	}
	if err := validateSandboxImage(image); err != nil {
		return unavailableSandbox{capability: SandboxCapability{
			Backend: "container",
			Reason:  err.Error(),
		}}
	}
	runtimeName := strings.TrimSpace(os.Getenv("MUHIYA_SANDBOX_RUNTIME"))
	candidates := []string{runtimeName}
	if runtimeName == "" {
		candidates = []string{"docker", "podman"}
	}
	for _, candidate := range candidates {
		executable, err := exec.LookPath(candidate)
		if err == nil {
			return containerSandbox{
				runtime: executable, runtimeName: candidate,
				image: image, workspace: workspace,
			}
		}
	}
	return unavailableSandbox{capability: SandboxCapability{
		Backend: "container",
		Reason:  sandboxImageEnvironment + " is configured but docker/podman was not found",
	}}
}

func validateSandboxImage(image string) error {
	const marker = "@sha256:"
	index := strings.LastIndex(image, marker)
	if index <= 0 {
		return fmt.Errorf("%s must pin an immutable image digest (name@sha256:<64 hex characters>)", sandboxImageEnvironment)
	}
	digest := image[index+len(marker):]
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("%s contains an invalid SHA-256 image digest", sandboxImageEnvironment)
	}
	return nil
}

func (sandbox containerSandbox) Capability() SandboxCapability {
	return SandboxCapability{
		Backend:  sandbox.runtimeName + ":" + sandbox.image,
		Enforced: true, NetworkDenied: true,
	}
}

func (sandbox containerSandbox) Wrap(request SandboxRequest) ([]string, error) {
	if !IsInside(sandbox.workspace, request.CWD) {
		return nil, fmt.Errorf("sandbox cwd %s is outside workspace %s", request.CWD, sandbox.workspace)
	}
	relative, err := filepath.Rel(sandbox.workspace, request.CWD)
	if err != nil {
		return nil, err
	}
	containerCWD := "/workspace"
	if relative != "." {
		containerCWD += "/" + filepath.ToSlash(relative)
	}
	mountSource := filepath.ToSlash(sandbox.workspace)
	arguments := []string{
		sandbox.runtime, "run", "--rm", "--init",
		"--stop-timeout", "1",
		"--network", "none",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "256",
		"--memory", "4g",
		"--cpus", "4",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=256m",
		"--mount", "type=bind,src=" + mountSource + ",dst=/workspace,rw",
		"--workdir", containerCWD,
		sandbox.image,
	}
	return append(arguments, request.Arguments...), nil
}
