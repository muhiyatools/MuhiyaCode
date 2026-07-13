//go:build windows

package main

// metrics_windows.go collects process CPU seconds, working-set memory, and
// total machine RAM on Windows. Process times come from GetProcessTimes and
// working set from GetProcessMemoryInfo (psapi). Total RAM comes from
// GlobalMemoryStatusEx. golang.org/x/sys/windows is already an (indirect)
// go.mod dependency, so no new dependency is introduced.

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters mirrors the Win32 PROCESS_MEMORY_COUNTERS struct. It is
// declared locally because the typed wrapper is not exported by x/sys/windows.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX struct for GlobalMemoryStatusEx.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// processCPUSeconds returns cumulative kernel+user CPU time for this process.
func processCPUSeconds() (float64, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("GetProcessTimes: %w", err)
	}
	// Filetime values are in 100-nanosecond ticks.
	ticks := filetimeTicks(kernel) + filetimeTicks(user)
	return float64(ticks) / 1e7, nil
}

func filetimeTicks(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// workingSetBytes returns the process working-set size (the OS-visible resident
// footprint on Windows) in bytes.
func workingSetBytes() (uint64, error) {
	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	ret, _, callErr := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if ret == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	return uint64(counters.WorkingSetSize), nil
}

// rssBytes: Windows has no distinct RSS metric, so the working set (the resident
// footprint the OS reports) is the faithful equivalent. rssNote records this.
func rssBytes() (uint64, string, error) {
	ws, err := workingSetBytes()
	if err != nil {
		return 0, "", err
	}
	return ws, "windows working-set (no distinct RSS metric)", nil
}

// machineRAMBytes returns total physical RAM via GlobalMemoryStatusEx.
func machineRAMBytes() (uint64, string) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return 0, "n/a (GlobalMemoryStatusEx failed)"
	}
	return status.TotalPhys, ""
}
