//go:build windows

package mcpclient

import (
	"os/exec"
	"syscall"
)

func openBrowser(target string) bool {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start() == nil
}
