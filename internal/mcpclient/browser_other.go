//go:build !windows && !darwin && !linux

package mcpclient

func openBrowser(string) bool { return false }
