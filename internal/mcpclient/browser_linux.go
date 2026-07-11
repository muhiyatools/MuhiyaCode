//go:build linux

package mcpclient

import "os/exec"

func openBrowser(target string) bool { return exec.Command("xdg-open", target).Start() == nil }
