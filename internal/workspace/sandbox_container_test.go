package workspace

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerSandboxWrapsWithDenyByDefaultControls(t *testing.T) {
	root := t.TempDir()
	sandbox := containerSandbox{
		runtime: "docker", runtimeName: "docker",
		image: "example/sandbox@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", workspace: root,
	}
	arguments, err := sandbox.Wrap(SandboxRequest{
		Arguments: []string{"sh", "-c", "go test ./..."},
		CWD:       filepath.Join(root, "subdir"),
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	for _, required := range []string{
		"--network none", "--read-only", "--cap-drop ALL",
		"no-new-privileges", "--pids-limit 256", "dst=/workspace,rw",
		"--workdir /workspace/subdir", "--stop-timeout 1",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("container sandbox omitted %q:\n%s", required, joined)
		}
	}
	if !sandbox.Capability().Enforced || !sandbox.Capability().NetworkDenied {
		t.Fatalf("container capability is incomplete: %+v", sandbox.Capability())
	}
}

func TestConfiguredContainerSandboxRejectsMutableOrInvalidImage(t *testing.T) {
	t.Setenv(sandboxImageEnvironment, "example/sandbox:latest")
	if capability := configuredContainerSandbox(t.TempDir()).Capability(); capability.Enforced || !strings.Contains(capability.Reason, "immutable image digest") {
		t.Fatalf("mutable image was not rejected: %+v", capability)
	}
	t.Setenv(sandboxImageEnvironment, "example/sandbox@sha256:abc")
	if capability := configuredContainerSandbox(t.TempDir()).Capability(); capability.Enforced || !strings.Contains(capability.Reason, "invalid SHA-256") {
		t.Fatalf("invalid digest was not rejected: %+v", capability)
	}
}

func TestContainerSandboxRejectsOutsideCWD(t *testing.T) {
	sandbox := containerSandbox{runtime: "docker", image: "image", workspace: t.TempDir()}
	if _, err := sandbox.Wrap(SandboxRequest{
		Arguments: []string{"sh"}, CWD: filepath.Dir(t.TempDir()),
	}); err == nil {
		t.Fatal("container sandbox accepted a cwd outside its mounted workspace")
	}
}
