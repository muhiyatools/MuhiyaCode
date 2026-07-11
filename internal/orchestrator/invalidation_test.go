package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestInvalidationLedgerPersistsBeforePublishing(t *testing.T) {
	var persisted []contract.InvalidationEvent
	ledger := NewInvalidationLedger(nil, func(_ context.Context, event contract.InvalidationEvent) error {
		persisted = append(persisted, event)
		return nil
	})
	pressure := 0.60
	event := contract.InvalidationEvent{Cause: contract.InvalidationFold, Trigger: contract.InvalidationPressure, Scope: "folded one task", Pressure: &pressure, RequestSeq: 2}
	if err := ledger.Record(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || len(ledger.Events()) != 1 || persisted[0].At.IsZero() {
		t.Fatalf("persisted=%+v events=%+v", persisted, ledger.Events())
	}
}

func TestInvalidationLedgerRejectsPressureBelowFloor(t *testing.T) {
	ledger := NewInvalidationLedger(nil, nil)
	pressure := 0.59
	err := ledger.Record(context.Background(), contract.InvalidationEvent{Cause: contract.InvalidationTrim, Trigger: contract.InvalidationPressure, Scope: "too early", Pressure: &pressure, RequestSeq: 1})
	if err == nil {
		t.Fatal("expected pressure-floor error")
	}
}
