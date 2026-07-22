package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const SchemaVersion = 1

type RetentionClass string

const (
	RetentionTask    RetentionClass = "task"
	RetentionSession RetentionClass = "session"
	RetentionDebug   RetentionClass = "debug"
)

type Metadata struct {
	Version           int            `json:"version"`
	Handle            string         `json:"handle"`
	ContentHash       string         `json:"content_hash"`
	SessionID         string         `json:"session_id"`
	WorkspaceID       string         `json:"workspace_id"`
	Source            string         `json:"source"`
	Status            string         `json:"status"`
	Complete          bool           `json:"complete"`
	SizeBytes         int64          `json:"size_bytes"`
	SourceFingerprint string         `json:"source_fingerprint,omitempty"`
	ReducerVersion    string         `json:"reducer_version,omitempty"`
	Retention         RetentionClass `json:"retention"`
	CreatedAt         time.Time      `json:"created_at"`
	ExpiresAt         *time.Time     `json:"expires_at,omitempty"`
}

type PutInput struct {
	SessionID, WorkspaceID, Source, Status, SourceFingerprint, ReducerVersion string
	Complete                                                                  bool
	Retention                                                                 RetentionClass
	ExpiresAt                                                                 *time.Time
	Content                                                                   []byte
}

func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func newMetadata(input PutInput) (Metadata, error) {
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.WorkspaceID) == "" {
		return Metadata{}, errors.New("artifact session and workspace ownership are required")
	}
	hash := Hash(input.Content)
	handleSeed := []byte(input.SessionID + "\x00" + input.WorkspaceID + "\x00" + hash)
	handle := "artifact_" + Hash(handleSeed)[:24]
	retention := input.Retention
	if retention == "" {
		retention = RetentionTask
	}
	return Metadata{Version: SchemaVersion, Handle: handle, ContentHash: hash, SessionID: input.SessionID, WorkspaceID: input.WorkspaceID, Source: input.Source, Status: input.Status, Complete: input.Complete, SizeBytes: int64(len(input.Content)), SourceFingerprint: input.SourceFingerprint, ReducerVersion: input.ReducerVersion, Retention: retention, CreatedAt: time.Now().UTC(), ExpiresAt: input.ExpiresAt}, nil
}
