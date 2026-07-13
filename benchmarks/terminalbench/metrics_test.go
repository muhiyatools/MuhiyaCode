package main

import (
	"runtime"
	"testing"
)

// TestProcessCPUSecondsPlausible verifies the CPU collector returns a
// non-negative, non-decreasing value across a burst of work.
func TestProcessCPUSecondsPlausible(t *testing.T) {
	first, err := processCPUSeconds()
	if err != nil {
		t.Fatalf("processCPUSeconds: %v", err)
	}
	if first < 0 {
		t.Fatalf("cpu seconds negative: %v", first)
	}
	// Burn a little CPU so the second reading is not below the first.
	sink := 0
	for i := 0; i < 5_000_000; i++ {
		sink += i % 7
	}
	_ = sink
	second, err := processCPUSeconds()
	if err != nil {
		t.Fatalf("processCPUSeconds second: %v", err)
	}
	if second < first {
		t.Fatalf("cpu seconds decreased: %v -> %v", first, second)
	}
}

// TestWorkingSetAndRSSPlausible verifies the memory collectors return positive
// byte counts.
func TestWorkingSetAndRSSPlausible(t *testing.T) {
	ws, err := workingSetBytes()
	if err != nil {
		t.Fatalf("workingSetBytes: %v", err)
	}
	if ws == 0 {
		t.Fatalf("working set is zero")
	}
	rss, note, err := rssBytes()
	if err != nil {
		t.Fatalf("rssBytes: %v", err)
	}
	if rss == 0 {
		t.Fatalf("rss is zero")
	}
	if note == "" {
		t.Fatalf("rss note should describe the metric semantics")
	}
	// A process resident footprint below 1 MiB or above 64 GiB is implausible.
	const oneMiB = 1 << 20
	const sixtyFourGiB = uint64(64) << 30
	if ws < oneMiB || ws > sixtyFourGiB {
		t.Fatalf("working set implausible: %d bytes", ws)
	}
}

// TestMachineMetadataPopulated verifies CPU count is populated and, where the
// platform supports it, total RAM is a plausible value. On platforms without a
// portable total-RAM syscall, the note must honestly say so.
func TestMachineMetadataPopulated(t *testing.T) {
	if runtime.NumCPU() <= 0 {
		t.Fatalf("cpu count not populated")
	}
	ram, note := machineRAMBytes()
	if ram == 0 {
		if note == "" {
			t.Fatalf("unavailable RAM must carry an honest note")
		}
		return
	}
	const oneGiB = uint64(1) << 30
	const sixteenTiB = uint64(16) << 40
	if ram < oneGiB/2 || ram > sixteenTiB {
		t.Fatalf("total RAM implausible: %d bytes", ram)
	}
}

// TestGoDiagnosticsAvailable verifies the Go heap/allocation diagnostics the
// contract §1 requires are readable.
func TestGoDiagnosticsAvailable(t *testing.T) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	if mem.HeapAlloc == 0 || mem.TotalAlloc == 0 {
		t.Fatalf("go heap diagnostics unavailable: heap=%d total=%d", mem.HeapAlloc, mem.TotalAlloc)
	}
}
