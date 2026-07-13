//go:build !windows

package main

// metrics_unix.go collects process CPU seconds and RSS on unix-like systems via
// syscall.Getrusage(RUSAGE_SELF). Only the standard library is used. Total
// machine RAM has no portable stdlib syscall, so it is honestly reported as
// unavailable rather than guessed (contract §7: never fake a value).

import (
	"fmt"
	"runtime"
	"syscall"
)

// processCPUSeconds returns cumulative user+system CPU time for this process.
func processCPUSeconds() (float64, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, fmt.Errorf("getrusage: %w", err)
	}
	user := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6
	system := float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	return user + system, nil
}

// rssBytes returns the maximum resident set size. Getrusage reports Maxrss in
// kilobytes on Linux and in bytes on Darwin; normalize to bytes.
func rssBytes() (uint64, string, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, "", fmt.Errorf("getrusage: %w", err)
	}
	maxrss := uint64(usage.Maxrss)
	if runtime.GOOS == "darwin" {
		return maxrss, "getrusage maxrss (bytes, peak)", nil
	}
	return maxrss * 1024, "getrusage maxrss (KiB->bytes, peak)", nil
}

// workingSetBytes: unix has no WorkingSet concept; RSS is the faithful
// equivalent, so callers fall back to rssBytes.
func workingSetBytes() (uint64, error) {
	value, _, err := rssBytes()
	return value, err
}

// machineRAMBytes: no portable stdlib syscall exists, so report unavailable
// honestly instead of adding a dependency or guessing.
func machineRAMBytes() (uint64, string) {
	return 0, "n/a (no portable stdlib syscall for total RAM)"
}
