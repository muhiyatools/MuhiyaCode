package orchestrator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// harnessEventRingCap bounds the in-memory harness-event ring. 100 is generous
// for a single session's friction and keeps the buffer trivially cheap.
const harnessEventRingCap = 100

// recordHarnessEvent records one harness-caused friction event (T010/T011). It
// (a) appends to the bounded in-memory ring so the TUI can surface friction with
// no DB read, and (b) persists via the AddEvent hook (topic "harness") for
// durable telemetry. Detail is redacted before either path sees it. Safe when no
// persistence hook is wired (tests) — the ring still records.
func (e *Engine) recordHarnessEvent(ctx context.Context, class contract.HarnessEventClass, code, detail string) {
	redacted := e.redact(detail)
	event := contract.HarnessEvent{At: time.Now().UTC(), Class: class, Code: code, Detail: contract.Digest(redacted, 200)}

	e.taskMu.Lock()
	e.harnessEvents = append(e.harnessEvents, event)
	if len(e.harnessEvents) > harnessEventRingCap {
		// Copy into a right-sized slice so the backing array cannot grow unbounded
		// across a long session.
		trimmed := make([]contract.HarnessEvent, harnessEventRingCap)
		copy(trimmed, e.harnessEvents[len(e.harnessEvents)-harnessEventRingCap:])
		e.harnessEvents = trimmed
	}
	e.taskMu.Unlock()

	if e.persistence.AddEvent != nil {
		if raw, err := json.Marshal(map[string]any{"class": string(class), "code": code, "detail": redacted}); err == nil {
			_ = e.persistence.AddEvent(ctx, "harness", code, string(raw), "")
		}
	}
}

// HarnessEvents returns a copy of the session's harness-event ring, oldest first.
// Read-only and lock-guarded — safe to call from the TUI Update goroutine.
func (e *Engine) HarnessEvents() []contract.HarnessEvent {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return append([]contract.HarnessEvent(nil), e.harnessEvents...)
}

// harnessEventRingLen snapshots the ring length — the task loop captures this at
// task start so the summary can mark only the friction THIS task produced (T013).
func (e *Engine) harnessEventRingLen() int {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return len(e.harnessEvents)
}

// harnessEventsSince returns the events recorded since a captured ring length,
// and how many of them are user-visible friction classes (gate/tool/ui) worth a
// summary marker (T013). Robust to ring trimming: a start beyond the current
// length (the ring rolled over) counts from the buffer front.
func (e *Engine) harnessEventsSince(start int) (total, visible int) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	from := start
	if from < 0 || from > len(e.harnessEvents) {
		from = 0
	}
	for _, event := range e.harnessEvents[from:] {
		total++
		switch event.Class {
		case contract.HarnessGate, contract.HarnessTool, contract.HarnessUI:
			visible++
		}
	}
	return total, visible
}
