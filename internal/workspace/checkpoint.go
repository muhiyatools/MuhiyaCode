package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type CheckpointMetadata interface {
	AddCheckpoint(context.Context, string, string, string) error
	LatestCheckpoint(context.Context, string) (id, description string, ok bool, err error)
}

type checkpointCatalog interface {
	Checkpoint(context.Context, string, string) (contract.CheckpointInfo, bool, error)
	ListCheckpoints(context.Context, string, int) ([]contract.CheckpointInfo, error)
	DeleteCheckpoint(context.Context, string, string) error
}

type CheckpointRetention struct {
	MaxCount int
	MaxAge   time.Duration
}

type CheckpointStore struct {
	SessionID  string
	SessionDir string
	Workspace  string
	Metadata   CheckpointMetadata
	Retention  CheckpointRetention
}

type checkpointFile struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	CreatedAt   string         `json:"createdAt"`
	Files       []snapshotFile `json:"files"`
}

type snapshotFile struct {
	Path           string `json:"path"`
	Existed        bool   `json:"existed"`
	Content        string `json:"content"`
	Mode           uint32 `json:"mode,omitempty"`
	ModTimeRFC3339 string `json:"modTime,omitempty"`
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
		data, fileInfo, readErr := safeReadTarget(s.Workspace, canonical)
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
		checkpoint.Files = append(checkpoint.Files, snapshotFile{
			Path: canonical, Existed: true, Content: string(data),
			Mode: uint32(fileInfo.Mode().Perm()), ModTimeRFC3339: fileInfo.ModTime().UTC().Format(time.RFC3339Nano),
		})
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
	result, _, err := s.RestoreCode(ctx, id)
	return result, err
}

func (s *CheckpointStore) List(ctx context.Context, limit int) ([]contract.CheckpointInfo, error) {
	catalog, ok := s.Metadata.(checkpointCatalog)
	if !ok {
		return nil, nil
	}
	return catalog.ListCheckpoints(ctx, s.SessionID, limit)
}

func (s *CheckpointStore) Info(ctx context.Context, id string) (contract.CheckpointInfo, error) {
	if !safeCheckpointID(id) {
		return contract.CheckpointInfo{}, fmt.Errorf("invalid checkpoint id %q", id)
	}
	return s.checkpointInfo(ctx, id)
}

func (s *CheckpointStore) Delete(ctx context.Context, id string) error {
	if !safeCheckpointID(id) {
		return fmt.Errorf("invalid checkpoint id %q", id)
	}
	catalog, ok := s.Metadata.(checkpointCatalog)
	if !ok {
		return fmt.Errorf("checkpoint catalog is unavailable")
	}
	path := filepath.Join(s.SessionDir, "checkpoints", id+".json")
	quarantine := path + ".deleting"
	if err := os.Rename(path, quarantine); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := catalog.DeleteCheckpoint(ctx, s.SessionID, id); err != nil {
		if _, statErr := os.Stat(quarantine); statErr == nil {
			_ = os.Rename(quarantine, path)
		}
		return err
	}
	if err := os.Remove(quarantine); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RestoreCode restores one selected code snapshot and returns its matching
// event cursor so the caller can fork or rewind conversation state coherently.
func (s *CheckpointStore) RestoreCode(ctx context.Context, id string) (string, int64, error) {
	if !safeCheckpointID(id) {
		return "", 0, fmt.Errorf("invalid checkpoint id %q", id)
	}
	info, err := s.checkpointInfo(ctx, id)
	if err != nil {
		return "", 0, err
	}
	payload, err := os.ReadFile(filepath.Join(s.SessionDir, "checkpoints", id+".json"))
	if err != nil {
		return "", 0, err
	}
	var checkpoint checkpointFile
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		return "", 0, fmt.Errorf("decode checkpoint %s: %w", id, err)
	}
	if checkpoint.ID != id {
		return "", 0, fmt.Errorf("checkpoint identity mismatch: requested %s, file contains %s", id, checkpoint.ID)
	}
	changes, err := s.restoreChanges(checkpoint)
	if err != nil {
		return "", 0, err
	}
	restorer := &Workspace{
		root:         s.Workspace,
		readLedger:   newPathLedger(),
		patchJournal: newPatchJournal(s, s.Workspace, nil),
	}
	if err := restorer.commitPatchChanges(changes); err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Restored checkpoint %s: %s", checkpoint.ID, checkpoint.Description), info.EventCursor, nil
}

func (s *CheckpointStore) restoreChanges(checkpoint checkpointFile) ([]patchChange, error) {
	changes := make([]patchChange, 0, len(checkpoint.Files))
	for _, snapshot := range checkpoint.Files {
		inside, err := IsInsideReal(s.Workspace, snapshot.Path)
		if err != nil || !inside {
			return nil, fmt.Errorf("checkpoint target is outside workspace: %s", snapshot.Path)
		}
		change, err := s.restoreChange(snapshot)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (s *CheckpointStore) restoreChange(snapshot snapshotFile) (patchChange, error) {
	before, existed, beforeMode, beforeTime, err := readPatchTarget(s.Workspace, snapshot.Path)
	if err != nil {
		return patchChange{}, err
	}
	afterMode := os.FileMode(snapshot.Mode)
	if afterMode == 0 {
		afterMode = 0o644
	}
	var afterTime time.Time
	if snapshot.ModTimeRFC3339 != "" {
		afterTime, err = time.Parse(time.RFC3339Nano, snapshot.ModTimeRFC3339)
		if err != nil {
			return patchChange{}, fmt.Errorf("decode checkpoint timestamp: %w", err)
		}
	}
	return patchChange{
		Path: snapshot.Path, Before: before, After: snapshot.Content,
		Existed: existed, Drop: !snapshot.Existed,
		BeforeMode: beforeMode, AfterMode: afterMode,
		BeforeModTime: beforeTime, AfterModTime: afterTime,
	}, nil
}

func (s *CheckpointStore) checkpointInfo(ctx context.Context, id string) (contract.CheckpointInfo, error) {
	catalog, ok := s.Metadata.(checkpointCatalog)
	if !ok {
		return contract.CheckpointInfo{ID: id, SessionID: s.SessionID}, nil
	}
	info, exists, err := catalog.Checkpoint(ctx, s.SessionID, id)
	if err != nil {
		return contract.CheckpointInfo{}, err
	}
	if !exists {
		return contract.CheckpointInfo{}, fmt.Errorf("checkpoint not found: %s", id)
	}
	return info, nil
}

func (s *CheckpointStore) Prune(ctx context.Context, now time.Time) error {
	catalog, ok := s.Metadata.(checkpointCatalog)
	if !ok || (s.Retention.MaxCount <= 0 && s.Retention.MaxAge <= 0) {
		return nil
	}
	checkpoints, err := catalog.ListCheckpoints(ctx, s.SessionID, 500)
	if err != nil {
		return err
	}
	for index, checkpoint := range checkpoints {
		overCount := s.Retention.MaxCount > 0 && index >= s.Retention.MaxCount
		tooOld := s.Retention.MaxAge > 0 && now.Sub(checkpoint.CreatedAt) > s.Retention.MaxAge
		if !overCount && !tooOld {
			continue
		}
		path := filepath.Join(s.SessionDir, "checkpoints", checkpoint.ID+".json")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := catalog.DeleteCheckpoint(ctx, s.SessionID, checkpoint.ID); err != nil {
			return err
		}
	}
	return nil
}

func safeCheckpointID(id string) bool {
	if len(id) != 12 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
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
