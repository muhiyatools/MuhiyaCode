package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrRetryablePersistence = errors.New("retryable artifact persistence failure")

type Store struct {
	root       string
	quotaBytes int64
}

func NewStore(root string, quotaBytes int64) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact root is required")
	}
	if quotaBytes <= 0 {
		quotaBytes = 256 << 20
	}
	store := &Store{root: root, quotaBytes: quotaBytes}
	for _, path := range []string{store.blobDir(), store.metadataDir()} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (store *Store) Put(input PutInput) (Metadata, error) {
	metadata, err := newMetadata(input)
	if err != nil {
		return Metadata{}, err
	}
	used, err := store.usedBytes()
	if err != nil {
		return Metadata{}, err
	}
	increment := metadata.SizeBytes
	blobPath := store.blobPath(metadata.ContentHash)
	if _, statErr := os.Stat(blobPath); statErr == nil {
		increment = 0
	}
	if used+increment > store.quotaBytes {
		return Metadata{}, fmt.Errorf("artifact quota exceeded: %d + %d > %d", used, metadata.SizeBytes, store.quotaBytes)
	}
	if _, err := os.Stat(blobPath); errors.Is(err, fs.ErrNotExist) {
		if err := atomicWrite(blobPath, input.Content, 0o600); err != nil {
			return Metadata{}, fmt.Errorf("%w: write blob: %v", ErrRetryablePersistence, err)
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return Metadata{}, err
	}
	if existing, existingErr := store.Metadata(metadata.Handle); existingErr == nil && existing.ContentHash == metadata.ContentHash {
		return existing, nil
	}
	if err := atomicWrite(store.metadataPath(metadata.Handle), encoded, 0o600); err != nil {
		return Metadata{}, fmt.Errorf("%w: write metadata: %v", ErrRetryablePersistence, err)
	}
	return metadata, nil
}

func (store *Store) Fetch(handle, sessionID, workspaceID string, offset, limit int64) ([]byte, Metadata, error) {
	metadata, err := store.Metadata(handle)
	if err != nil {
		return nil, Metadata{}, err
	}
	if metadata.SessionID != sessionID || metadata.WorkspaceID != workspaceID {
		return nil, Metadata{}, errors.New("artifact ownership mismatch")
	}
	if metadata.ExpiresAt != nil && !metadata.ExpiresAt.After(time.Now()) {
		return nil, Metadata{}, errors.New("artifact handle expired")
	}
	content, err := os.ReadFile(store.blobPath(metadata.ContentHash))
	if err != nil {
		return nil, Metadata{}, err
	}
	if Hash(content) != metadata.ContentHash {
		return nil, Metadata{}, errors.New("artifact content hash mismatch")
	}
	if containsSecret(content) {
		return nil, Metadata{}, errors.New("artifact contains secret-like material and cannot be fetched into model context")
	}
	if offset < 0 || limit <= 0 || offset > int64(len(content)) {
		return nil, Metadata{}, errors.New("artifact range is invalid")
	}
	end := min(int64(len(content)), offset+limit)
	return append([]byte(nil), content[offset:end]...), metadata, nil
}

func (store *Store) Metadata(handle string) (Metadata, error) {
	if !regexp.MustCompile(`^artifact_[a-f0-9]{24}$`).MatchString(handle) {
		return Metadata{}, errors.New("invalid artifact handle")
	}
	body, err := os.ReadFile(store.metadataPath(handle))
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return Metadata{}, err
	}
	if metadata.Version <= 0 || metadata.Handle != handle {
		return Metadata{}, errors.New("invalid artifact metadata")
	}
	return metadata, nil
}

func (store *Store) RecoverOrphans() error {
	referenced := map[string]bool{}
	entries, err := os.ReadDir(store.metadataDir())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		metadata, readErr := store.Metadata(strings.TrimSuffix(entry.Name(), ".json"))
		if readErr == nil {
			referenced[metadata.ContentHash] = true
		}
	}
	blobs, err := os.ReadDir(store.blobDir())
	if err != nil {
		return err
	}
	for _, blob := range blobs {
		if !referenced[blob.Name()] {
			_ = os.Remove(filepath.Join(store.blobDir(), blob.Name()))
		}
	}
	return nil
}

func (store *Store) usedBytes() (int64, error) {
	var total int64
	err := filepath.WalkDir(store.blobDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return err
	})
	return total, err
}

func (store *Store) blobDir() string             { return filepath.Join(store.root, "blobs") }
func (store *Store) metadataDir() string         { return filepath.Join(store.root, "metadata") }
func (store *Store) blobPath(hash string) string { return filepath.Join(store.blobDir(), hash) }
func (store *Store) metadataPath(handle string) string {
	return filepath.Join(store.metadataDir(), handle+".json")
}

func atomicWrite(path string, content []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{12,}|api[_-]?key\s*[:=]\s*\S+|authorization:\s*bearer\s+\S+)`),
}

func containsSecret(content []byte) bool {
	for _, pattern := range secretPatterns {
		if pattern.Match(bytes.ToLower(content)) {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
