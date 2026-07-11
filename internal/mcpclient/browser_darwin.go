//go:build darwin

package mcpclient

import "os/exec"

func openBrowser(target string) bool { return exec.Command("open", target).Start() == nil }
