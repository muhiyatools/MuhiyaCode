package workspace

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type mockTrustPort struct {
	trusted bool
}

func (m *mockTrustPort) IsTrusted(ctx context.Context, path string) (bool, error) {
	return m.trusted, nil
}

func (m *mockTrustPort) Trust(ctx context.Context, path string) error {
	m.trusted = true
	return nil
}

func TestGuard_AutoAcceptTrust(t *testing.T) {
	port := &mockTrustPort{trusted: false}
	guard, _ := NewGuard("test", GuardOptions{Trust: port})
	guard.SetMode(contract.PermissionAutoAccept)

	err := guard.ensureTrusted(context.Background(), ActionWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !port.trusted {
		t.Errorf("expected port to be trusted in auto-accept mode")
	}
}

func TestGuard_PreTrust(t *testing.T) {
	port := &mockTrustPort{trusted: false}
	guard, _ := NewGuard("test", GuardOptions{Trust: port})
	
	err := guard.PreTrust(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !port.trusted {
		t.Errorf("expected port to be trusted after PreTrust")
	}
}
