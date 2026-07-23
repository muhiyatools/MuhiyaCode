package tui

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// H-2: the permission toggles must not panic when a runtime was built without
// Settings (they deref m.runtime.Settings).
func TestPermissionTogglesNilSettingsAreSafe(t *testing.T) {
	m := &Model{} // runtime.Settings and runtime.Engine both nil
	if cmd := m.cyclePermission(); cmd != nil {
		t.Fatal("cyclePermission with nil Settings must no-op, not act")
	}
	// setPermission with nil Engine and nil Settings must not panic on the write.
	_ = m.setPermission(contract.PermissionAutoAccept)
}
