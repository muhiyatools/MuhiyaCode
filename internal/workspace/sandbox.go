package workspace

import (
	"errors"
	"fmt"
)

var ErrSandboxUnavailable = errors.New("workspace: enforced sandbox unavailable")

type SandboxCapability struct {
	Backend       string
	Enforced      bool
	NetworkDenied bool
	Reason        string
}

type SandboxRequest struct {
	Arguments []string
	CWD       string
}

// SandboxBackend lives with the process-launch client. Implementations wrap an
// argv before exec; a backend must report Enforced only when the OS applies the
// filesystem and network policy.
type SandboxBackend interface {
	Capability() SandboxCapability
	Wrap(SandboxRequest) ([]string, error)
}

type SandboxSupport struct {
	OS      string
	Backend string
	Status  string
}

func SandboxSupportMatrix() []SandboxSupport {
	return []SandboxSupport{
		{OS: "linux", Backend: "bubblewrap", Status: "enforced when bwrap is installed"},
		{OS: "darwin", Backend: "sandbox-exec", Status: "enforced when sandbox-exec is installed"},
		{OS: "windows", Backend: "container", Status: "enforced with configured Docker/Podman image; native backend unavailable"},
	}
}

type unavailableSandbox struct {
	capability SandboxCapability
}

func (s unavailableSandbox) Capability() SandboxCapability { return s.capability }

func (s unavailableSandbox) Wrap(SandboxRequest) ([]string, error) {
	return nil, fmt.Errorf("%w: %s", ErrSandboxUnavailable, s.capability.Reason)
}

func sandboxCommand(backend SandboxBackend, request SandboxRequest, allowUnsandboxed, bypass bool) ([]string, SandboxCapability, error) {
	if backend == nil || bypass {
		return request.Arguments, SandboxCapability{Backend: "host", Reason: "explicit unrestricted execution"}, nil
	}
	capability := backend.Capability()
	if capability.Enforced {
		arguments, err := backend.Wrap(request)
		return arguments, capability, err
	}
	if !allowUnsandboxed {
		return nil, capability, fmt.Errorf("%w: %s", ErrSandboxUnavailable, capability.Reason)
	}
	return request.Arguments, capability, nil
}
