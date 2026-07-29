package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// v3 adds independent per-model cache lineages.
const SessionRuntimeVersion = 3

// SessionRuntimeConfig is the durable, session-scoped contract. Global settings
// supply only the default for a newly created session (the model comes from the
// configured provider; the permission mode defaults to Normal). Once a session
// exists, its runtime record is the single authority for the engine, workspace
// guard, MCP manager, sandbox bypass, TUI indicator, and resume (UMI-06).
type SessionRuntimeConfig struct {
	Version        int                              `json:"version"`
	ModelID        string                           `json:"modelId"`
	CacheEpoch     uint64                           `json:"cacheEpoch"`
	ModelLineages  map[string]contract.ModelLineage `json:"modelLineages,omitempty"`
	PermissionMode contract.PermissionMode          `json:"permissionMode,omitempty"`
}

func (s *Sessions) ReadRuntimeConfig(sessionID string) (SessionRuntimeConfig, bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return SessionRuntimeConfig{}, false, err
	}
	path := filepath.Join(dir, "runtime.json")
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SessionRuntimeConfig{}, false, nil
	}
	if err != nil {
		return SessionRuntimeConfig{}, false, err
	}
	var config SessionRuntimeConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return SessionRuntimeConfig{}, false, fmt.Errorf("decode session runtime: %w", err)
	}
	// v1 records predate PermissionMode and the version bump. A v1 record is
	// still valid: it carried ModelID + CacheEpoch only, and the session's
	// permission authority defaulted to Normal (the safe global default). Migrate
	// it forward by defaulting PermissionMode rather than rejecting it.
	if config.Version == 0 {
		config.Version = 1
	}
	if config.Version > SessionRuntimeVersion {
		return SessionRuntimeConfig{}, false, fmt.Errorf("session runtime version %d is newer than supported %d", config.Version, SessionRuntimeVersion)
	}
	if strings.TrimSpace(config.ModelID) == "" {
		return SessionRuntimeConfig{}, false, fmt.Errorf("invalid session runtime configuration")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = contract.PermissionNormal
	}
	if config.ModelLineages == nil {
		config.ModelLineages = map[string]contract.ModelLineage{
			config.ModelID: {CacheEpoch: config.CacheEpoch},
		}
	}
	if lineage, ok := config.ModelLineages[config.ModelID]; ok {
		config.CacheEpoch = lineage.CacheEpoch
	} else {
		config.ModelLineages[config.ModelID] = contract.ModelLineage{CacheEpoch: config.CacheEpoch}
	}
	return config, true, nil
}

func (s *Sessions) WriteRuntimeConfig(sessionID string, config SessionRuntimeConfig) error {
	config.Version = SessionRuntimeVersion
	config.ModelID = strings.TrimSpace(config.ModelID)
	if config.ModelID == "" || len(config.ModelID) > 256 {
		return fmt.Errorf("session model id is invalid")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = contract.PermissionNormal
	}
	if config.ModelLineages == nil {
		config.ModelLineages = make(map[string]contract.ModelLineage)
	}
	lineage := config.ModelLineages[config.ModelID]
	lineage.CacheEpoch = config.CacheEpoch
	config.ModelLineages[config.ModelID] = lineage
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "runtime.json"), config, false)
}
