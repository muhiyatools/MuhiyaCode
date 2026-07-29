//go:build windows

package workspace

func newPlatformSandbox(workspace string) SandboxBackend {
	if container := configuredContainerSandbox(workspace); container != nil {
		return container
	}
	return unavailableSandbox{capability: SandboxCapability{
		Backend: "windows-host",
		Reason:  "no Windows restricted-token/AppContainer backend is installed; execution requires an explicit approval or unrestricted mode",
	}}
}
