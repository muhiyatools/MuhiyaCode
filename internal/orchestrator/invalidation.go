package orchestrator

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type InvalidationPersistence func(context.Context, contract.InvalidationEvent) error

// InvalidationLedger is the in-memory view of the append-only session ledger.
type InvalidationLedger struct {
	recordMu sync.Mutex
	mu       sync.RWMutex
	events   []contract.InvalidationEvent
	persist  InvalidationPersistence
}

func NewInvalidationLedger(existing []contract.InvalidationEvent, persist InvalidationPersistence) *InvalidationLedger {
	return &InvalidationLedger{events: cloneInvalidationEvents(existing), persist: persist}
}

func (l *InvalidationLedger) Record(ctx context.Context, event contract.InvalidationEvent) error {
	event = cloneInvalidationEvent(event)
	if err := validateInvalidation(event); err != nil {
		return err
	}

	// Serialize the persistence/publication transaction. Without this separate
	// lock, concurrent callers can append A,B on disk but publish B,A in memory.
	// The event lock remains available to readers while persistence is in flight.
	l.recordMu.Lock()
	defer l.recordMu.Unlock()

	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	l.mu.RLock()
	if count := len(l.events); count > 0 && event.RequestSeq < l.events[count-1].RequestSeq {
		previous := l.events[count-1].RequestSeq
		l.mu.RUnlock()
		return fmt.Errorf("invalidation request sequence moved backwards: %d after %d", event.RequestSeq, previous)
	}
	l.mu.RUnlock()
	if l.persist != nil {
		// Give persistence its own copy so a callback retaining or mutating a
		// pointer-valued field cannot mutate the published ledger.
		if err := l.persist(ctx, cloneInvalidationEvent(event)); err != nil {
			return err
		}
	}
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
	return nil
}

func (l *InvalidationLedger) Events() []contract.InvalidationEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneInvalidationEvents(l.events)
}

func (l *InvalidationLedger) Recent(limit int) []contract.InvalidationEvent {
	if limit <= 0 {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	start := max(0, len(l.events)-limit)
	return cloneInvalidationEvents(l.events[start:])
}

func (l *InvalidationLedger) EventsForRequest(seq int) []contract.InvalidationEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var result []contract.InvalidationEvent
	for _, event := range l.events {
		if event.RequestSeq == seq {
			result = append(result, cloneInvalidationEvent(event))
		}
	}
	return result
}

func validateInvalidation(event contract.InvalidationEvent) error {
	if !validInvalidationCause(event.Cause) {
		return fmt.Errorf("unknown invalidation cause %q", event.Cause)
	}
	if !validInvalidationTrigger(event.Trigger) {
		return fmt.Errorf("unknown invalidation trigger %q", event.Trigger)
	}
	if !triggerAllowedForCause(event.Cause, event.Trigger) {
		return fmt.Errorf("trigger %q is not valid for invalidation cause %q", event.Trigger, event.Cause)
	}
	if strings.TrimSpace(event.Scope) == "" {
		return fmt.Errorf("invalidation event requires a non-empty scope")
	}
	if event.RequestSeq <= 0 {
		return fmt.Errorf("invalidation event requires positive request sequence")
	}
	if event.Pressure != nil {
		if math.IsNaN(*event.Pressure) || math.IsInf(*event.Pressure, 0) || *event.Pressure < 0 {
			return fmt.Errorf("invalidation event has invalid pressure %v", *event.Pressure)
		}
	}
	if event.Trigger == contract.InvalidationPressure {
		if event.Pressure == nil {
			return fmt.Errorf("pressure invalidation requires pressure")
		}
		if *event.Pressure < 0.60 {
			return fmt.Errorf("pressure invalidation below 0.60 floor: %.3f", *event.Pressure)
		}
	}
	return nil
}

func validInvalidationCause(cause contract.InvalidationCause) bool {
	switch cause {
	case contract.InvalidationFold,
		contract.InvalidationTrim,
		contract.InvalidationCompact,
		contract.InvalidationWindowDrop,
		contract.InvalidationToolsetChange,
		contract.InvalidationModelSwitch,
		contract.InvalidationPromptRebuild,
		contract.InvalidationUserCompact,
		contract.InvalidationProbeChange:
		return true
	default:
		return false
	}
}

func validInvalidationTrigger(trigger contract.InvalidationTrigger) bool {
	switch trigger {
	case contract.InvalidationPressure,
		contract.InvalidationUserAction,
		contract.InvalidationConfigChange,
		contract.InvalidationBoundary:
		return true
	default:
		return false
	}
}

func triggerAllowedForCause(cause contract.InvalidationCause, trigger contract.InvalidationTrigger) bool {
	switch cause {
	case contract.InvalidationFold, contract.InvalidationTrim, contract.InvalidationCompact, contract.InvalidationWindowDrop:
		return trigger == contract.InvalidationPressure
	case contract.InvalidationToolsetChange:
		return trigger == contract.InvalidationUserAction || trigger == contract.InvalidationBoundary
	case contract.InvalidationModelSwitch, contract.InvalidationUserCompact:
		return trigger == contract.InvalidationUserAction
	case contract.InvalidationPromptRebuild, contract.InvalidationProbeChange:
		return trigger == contract.InvalidationConfigChange
	default:
		return false
	}
}

func cloneInvalidationEvents(events []contract.InvalidationEvent) []contract.InvalidationEvent {
	if events == nil {
		return nil
	}
	result := make([]contract.InvalidationEvent, len(events))
	for index, event := range events {
		result[index] = cloneInvalidationEvent(event)
	}
	return result
}

func cloneInvalidationEvent(event contract.InvalidationEvent) contract.InvalidationEvent {
	if event.Pressure != nil {
		pressure := *event.Pressure
		event.Pressure = &pressure
	}
	return event
}
