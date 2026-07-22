package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const taskEpochLedgerVersion = 1

type TaskEpochLedger struct {
	Version  int               `json:"version"`
	ActiveID string            `json:"activeId,omitempty"`
	Records  []json.RawMessage `json:"records"`
}

func (s *Sessions) WriteTaskEpochLedger(sessionID string, value any) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "task_epochs.json"), value, false)
}

func (s *Sessions) ReadTaskEpochLedger(sessionID string, output any) (bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "task_epochs.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &header) != nil || header.Version != taskEpochLedgerVersion {
		_ = os.Rename(filepath.Join(dir, "task_epochs.json"), filepath.Join(dir, "task_epochs.json.corrupt.bak"))
		return false, nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return false, fmt.Errorf("decode task epoch ledger: %w", err)
	}
	return true, nil
}
