package workspace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type CheckpointMetadata interface {
	AddCheckpoint(context.Context, string, string, string) error
	LatestCheckpoint(context.Context, string) (id, description string, ok bool, err error)
}

type CheckpointStore struct {
	SessionID  string
	SessionDir string
	Workspace  string
	Metadata   CheckpointMetadata
}

type checkpointFile struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	CreatedAt   string         `json:"createdAt"`
	Files       []snapshotFile `json:"files"`
}

type snapshotFile struct {
	Path          string `json:"path"`
	Existed       bool   `json:"existed"`
	Content       string `json:"content,omitempty"` // legacy v1 text snapshots
	ContentBase64 string `json:"contentBase64,omitempty"`
	Mode          uint32 `json:"mode,omitempty"`
}

func (s *CheckpointStore) Create(ctx context.Context, description string, files []string) (string, error) {
	if s == nil || s.Metadata == nil || s.SessionDir == "" || s.Workspace == "" {
		return "", nil
	}
	id, err := checkpointID()
	if err != nil {
		return "", err
	}
	checkpoint := checkpointFile{ID: id, Description: description, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	seen := make(map[string]bool)
	for _, file := range files {
		canonical, err := CanonicalPath(file)
		if err != nil || seen[normalizePath(canonical)] {
			continue
		}
		seen[normalizePath(canonical)] = true
		inside, err := IsInsideReal(s.Workspace, canonical)
		if err != nil || !inside {
			continue
		}
		data, readErr := os.ReadFile(canonical)
		if os.IsNotExist(readErr) {
			checkpoint.Files = append(checkpoint.Files, snapshotFile{Path: canonical})
			continue
		}
		if readErr != nil {
			return "", readErr
		}
		if int64(len(data)) > MaxSnapshotBytes {
			continue
		}
		info, statErr := os.Stat(canonical)
		if statErr != nil {
			return "", statErr
		}
		checkpoint.Files = append(checkpoint.Files, snapshotFile{Path: canonical, Existed: true, ContentBase64: base64.StdEncoding.EncodeToString(data), Mode: uint32(info.Mode().Perm())})
	}
	dir := filepath.Join(s.SessionDir, "checkpoints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeAtomic(filepath.Join(dir, id+".json"), payload, 0o600); err != nil {
		return "", err
	}
	if err := s.Metadata.AddCheckpoint(ctx, id, s.SessionID, description); err != nil {
		return "", err
	}
	return id, nil
}

func (s *CheckpointStore) RestoreLatest(ctx context.Context) (string, error) {
	if s == nil || s.Metadata == nil {
		return "No checkpoint available.", nil
	}
	id, _, ok, err := s.Metadata.LatestCheckpoint(ctx, s.SessionID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "No checkpoint available.", nil
	}
	payload, err := os.ReadFile(filepath.Join(s.SessionDir, "checkpoints", id+".json"))
	if err != nil {
		return "", err
	}
	var checkpoint checkpointFile
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		return "", fmt.Errorf("decode checkpoint %s: %w", id, err)
	}
	for _, snapshot := range checkpoint.Files {
		inside, err := IsInsideReal(s.Workspace, snapshot.Path)
		if err != nil || !inside {
			continue
		}
		if snapshot.Existed {
			if err := os.MkdirAll(filepath.Dir(snapshot.Path), 0o755); err != nil {
				return "", err
			}
			content := []byte(snapshot.Content)
			if snapshot.ContentBase64 != "" {
				decoded, decodeErr := base64.StdEncoding.DecodeString(snapshot.ContentBase64)
				if decodeErr != nil {
					return "", fmt.Errorf("decode checkpoint content for %s: %w", snapshot.Path, decodeErr)
				}
				content = decoded
			}
			mode := os.FileMode(snapshot.Mode)
			if mode == 0 {
				mode = 0o644
			}
			if err := writeAtomic(snapshot.Path, content, mode); err != nil {
				return "", err
			}
		} else if err := os.Remove(snapshot.Path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return fmt.Sprintf("Restored checkpoint %s: %s", checkpoint.ID, checkpoint.Description), nil
}

func (w *Workspace) checkpoint(ctx context.Context, description string, files []string) error {
	if w.options.Checkpoints == nil {
		return nil
	}
	_, err := w.options.Checkpoints.Create(ctx, description, files)
	return err
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".muhiya-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceAtomic(name, path)
}

func checkpointID() (string, error) {
	value := make([]byte, 6)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
