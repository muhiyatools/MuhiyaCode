package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const projectContextSidecar = "project_context.json"

// WriteProjectContext atomically persists the typed per-session project-context
// snapshot with user-only permissions (project-context contract §3). It must be
// written before the session's first provider request so a resume can restore
// the exact boot bytes rather than recompiling from mutable workspace state.
func (s *Sessions) WriteProjectContext(sessionID string, snapshot contract.ProjectContextSnapshot) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if snapshot.Version == 0 {
		snapshot.Version = contract.ProjectContextVersion
	}
	return writeJSON(filepath.Join(dir, projectContextSidecar), snapshot, true)
}

// ReadProjectContext loads the project-context sidecar. The bool is false when
// no valid, workspace-matching snapshot exists — a missing, corrupt, or
// foreign-workspace sidecar all yield the deterministic bootstrap path. A
// corrupt or mismatched file is backed up and never silently reused for another
// workspace (project-context contract §3, data-model §5.2).
func (s *Sessions) ReadProjectContext(sessionID, expectedWorkspaceKey string) (contract.ProjectContextSnapshot, bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return contract.ProjectContextSnapshot{}, false, err
	}
	file := filepath.Join(dir, projectContextSidecar)
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return contract.ProjectContextSnapshot{}, false, nil
	}
	if err != nil {
		return contract.ProjectContextSnapshot{}, false, err
	}
	var snapshot contract.ProjectContextSnapshot
	if json.Unmarshal(data, &snapshot) != nil || snapshot.Version == 0 {
		backupSidecar(file, "corrupt")
		return contract.ProjectContextSnapshot{}, false, nil
	}
	if snapshot.Version != contract.ProjectContextVersion {
		// An older schema (e.g. 005's ledger memory): discard so the boot block is
		// recomputed once for the current file-based memory format.
		backupSidecar(file, "stale-version")
		return contract.ProjectContextSnapshot{}, false, nil
	}
	if expectedWorkspaceKey != "" && snapshot.WorkspaceKey != expectedWorkspaceKey {
		backupSidecar(file, "workspace-mismatch")
		return contract.ProjectContextSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

func backupSidecar(file, reason string) {
	_ = os.Rename(file, fmt.Sprintf("%s.%s-%d.bak", file, reason, time.Now().UnixMilli()))
}
