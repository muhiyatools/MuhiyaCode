package command

import (
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/state"
)

func TestWebProbeSnapshotFresh(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		snapshot state.ProbeSnapshot
		want     bool
	}{
		{name: "recent supported", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeSupported, CheckedAt: now.Add(-time.Hour)}, want: true},
		{name: "recent unsupported", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeUnsupported, CheckedAt: now.Add(-time.Hour)}, want: true},
		{name: "unknown", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeUnknown, CheckedAt: now.Add(-time.Hour)}},
		{name: "unrecognized", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeSupport("future-value"), CheckedAt: now.Add(-time.Hour)}},
		{name: "expired", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeSupported, CheckedAt: now.Add(-webSearchProbeTTL - time.Second)}},
		{name: "future", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeSupported, CheckedAt: now.Add(time.Second)}},
		{name: "missing time", snapshot: state.ProbeSnapshot{WebSearch: state.ProbeSupported}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := webProbeSnapshotFresh(test.snapshot, now); got != test.want {
				t.Fatalf("fresh=%v, want %v", got, test.want)
			}
		})
	}
}
