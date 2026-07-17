package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestHarnessEventRingBoundsTopicAndRedaction(t *testing.T) {
	settings := engineSettings()
	var mu sync.Mutex
	var topics, payloads []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "tel", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Redact: func(v string) string { return strings.ReplaceAll(v, "sk-secret", "····") },
		Persistence: Persistence{AddEvent: func(_ context.Context, topic, _, payload, _ string) error {
			mu.Lock()
			topics = append(topics, topic)
			payloads = append(payloads, payload)
			mu.Unlock()
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First event carries a secret; it must be redacted in the persisted payload.
	engine.recordHarnessEvent(context.Background(), contract.HarnessGate, "shell-readonly-block", "blocked: command uses key sk-secret here")
	// Overflow the ring: 1 + 105 = 106 recorded, cap is 100.
	for i := 0; i < 105; i++ {
		engine.recordHarnessEvent(context.Background(), contract.HarnessTool, "tool-failure", "generic failure")
	}

	events := engine.HarnessEvents()
	if len(events) != harnessEventRingCap {
		t.Fatalf("ring kept %d events, want %d (cap)", len(events), harnessEventRingCap)
	}
	for _, e := range events {
		if strings.Contains(e.Detail, "sk-secret") {
			t.Fatal("secret leaked into the in-memory ring")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(topics) != 106 {
		t.Fatalf("AddEvent called %d times, want 106 (every event persists)", len(topics))
	}
	for _, topic := range topics {
		if topic != "harness" {
			t.Fatalf("persist topic = %q, want \"harness\"", topic)
		}
	}
	if strings.Contains(payloads[0], "sk-secret") || !strings.Contains(payloads[0], "····") {
		t.Fatalf("first payload not redacted: %q", payloads[0])
	}
}

func TestHarnessEventsSinceCountsVisibleClasses(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "tel2", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	start := engine.harnessEventRingLen()
	engine.recordHarnessEvent(context.Background(), contract.HarnessGate, "g", "d")     // visible
	engine.recordHarnessEvent(context.Background(), contract.HarnessTool, "t", "d")     // visible
	engine.recordHarnessEvent(context.Background(), contract.HarnessRecovery, "r", "d") // NOT visible
	engine.recordHarnessEvent(context.Background(), contract.HarnessProvider, "p", "d") // NOT visible
	total, visible := engine.harnessEventsSince(start)
	if total != 4 || visible != 2 {
		t.Fatalf("since = (total %d, visible %d), want (4, 2)", total, visible)
	}
	// Robust to an out-of-range start (ring rollover): counts from the front.
	if total, _ := engine.harnessEventsSince(9999); total != 4 {
		t.Fatalf("out-of-range start must count from front, got total %d", total)
	}
}
