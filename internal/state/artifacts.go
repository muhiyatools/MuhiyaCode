package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ArtifactSettings struct {
	Version         int   `json:"version"`
	PerSessionQuota int64 `json:"per_session_quota"`
}

func ArtifactRoot(stateRoot, sessionID string) string {
	return filepath.Join(stateRoot, "sessions", sessionID, "artifacts")
}

func LoadArtifactSettings(path string) ArtifactSettings {
	settings := ArtifactSettings{Version: 1, PerSessionQuota: 256 << 20}
	body, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(body, &settings) != nil || settings.PerSessionQuota <= 0 {
		return ArtifactSettings{Version: 1, PerSessionQuota: 256 << 20}
	}
	return settings
}
