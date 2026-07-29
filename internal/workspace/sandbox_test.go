package workspace

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

type sandboxFixture struct {
	capability SandboxCapability
	wrap       func(SandboxRequest) ([]string, error)
}

func (s sandboxFixture) Capability() SandboxCapability { return s.capability }

func (s sandboxFixture) Wrap(request SandboxRequest) ([]string, error) {
	if s.wrap == nil {
		return request.Arguments, nil
	}
	return s.wrap(request)
}

func TestSandboxCommandFailsClosedWithoutFallback(t *testing.T) {
	backend := unavailableSandbox{capability: SandboxCapability{
		Backend: "fixture",
		Reason:  "not installed",
	}}
	_, capability, err := sandboxCommand(backend, SandboxRequest{
		Arguments: []string{"tool"},
		CWD:       t.TempDir(),
	}, false, false)
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("expected ErrSandboxUnavailable, got %v", err)
	}
	if capability.Backend != "fixture" || capability.Enforced {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}

func TestSandboxCommandFallbackAndExplicitBypassAreReported(t *testing.T) {
	request := SandboxRequest{Arguments: []string{"tool"}, CWD: t.TempDir()}
	backend := unavailableSandbox{capability: SandboxCapability{Backend: "fixture", Reason: "missing"}}

	arguments, capability, err := sandboxCommand(backend, request, true, false)
	if err != nil || arguments[0] != "tool" || capability.Backend != "fixture" {
		t.Fatalf("unexpected approved fallback: args=%v capability=%+v err=%v", arguments, capability, err)
	}

	arguments, capability, err = sandboxCommand(backend, request, false, true)
	if err != nil || arguments[0] != "tool" || capability.Backend != "host" || capability.Enforced {
		t.Fatalf("unexpected unrestricted bypass: args=%v capability=%+v err=%v", arguments, capability, err)
	}
}

func TestShellRunnerRecordsEnforcedSandboxCapability(t *testing.T) {
	command := "printf sandbox"
	if runtime.GOOS == "windows" {
		command = "Write-Output sandbox"
	}
	backend := sandboxFixture{capability: SandboxCapability{
		Backend:       "fixture",
		Enforced:      true,
		NetworkDenied: true,
	}}
	runner := &ShellRunner{Sandbox: backend}
	result, err := runner.Run(context.Background(), t.TempDir(), command, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sandbox != backend.capability {
		t.Fatalf("sandbox result = %+v, want %+v", result.Sandbox, backend.capability)
	}
}

func TestPlatformSandboxNeverOverstatesCapability(t *testing.T) {
	backend := newPlatformSandbox(t.TempDir())
	capability := backend.Capability()
	if runtime.GOOS == "windows" && capability.Enforced {
		t.Fatalf("Windows host fallback must not claim enforcement: %+v", capability)
	}
	if capability.Enforced && capability.Backend == "" {
		t.Fatalf("enforced sandbox must name its backend: %+v", capability)
	}
}
