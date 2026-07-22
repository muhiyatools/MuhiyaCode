package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const toolSurfaceSpecVersion = 1

type ToolSurfaceSnapshot struct {
	Fingerprint string                    `json:"fingerprint"`
	Server      string                    `json:"server"`
	Tools       []contract.ToolDefinition `json:"tools"`
	CapturedAt  time.Time                 `json:"captured_at"`
	SpecVersion int                       `json:"spec_version"`
}

type toolSurfaceFile struct {
	Version int                            `json:"version"`
	Entries map[string]ToolSurfaceSnapshot `json:"entries"`
}

type ToolSurfaceStore struct {
	mu      sync.Mutex
	file    string
	entries map[string]ToolSurfaceSnapshot
}

func NewToolSurfaceStore(paths Paths) (*ToolSurfaceStore, error) {
	file := filepath.Join(paths.CacheDir, "mcp-tool-surfaces.json")
	stored, err := readJSON(file, toolSurfaceFile{Version: 1, Entries: map[string]ToolSurfaceSnapshot{}})
	if err != nil {
		return nil, err
	}
	if stored.Version != 1 || stored.Entries == nil {
		stored = toolSurfaceFile{Version: 1, Entries: map[string]ToolSurfaceSnapshot{}}
	}
	return &ToolSurfaceStore{file: file, entries: stored.Entries}, nil
}

func (s *ToolSurfaceStore) Get(fingerprint string) (ToolSurfaceSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.entries[fingerprint]
	return cloneToolSurface(snapshot), ok && snapshot.SpecVersion == toolSurfaceSpecVersion
}

func (s *ToolSurfaceStore) Put(snapshot ToolSurfaceSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot.SpecVersion = toolSurfaceSpecVersion
	snapshot.Tools = cloneToolDefinitions(snapshot.Tools)
	s.entries[snapshot.Fingerprint] = snapshot
	return writeJSON(s.file, toolSurfaceFile{Version: 1, Entries: s.entries}, false)
}

func (s *ToolSurfaceStore) DeleteServer(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fingerprint, snapshot := range s.entries {
		if snapshot.Server == server {
			delete(s.entries, fingerprint)
		}
	}
	return writeJSON(s.file, toolSurfaceFile{Version: 1, Entries: s.entries}, false)
}

func MCPServerFingerprint(server MCPServer, secretEnv map[string]string, oauthSecret map[string]any) string {
	type envEntry struct{ Key, Value string }
	combined := make(map[string]string, len(server.Env)+len(secretEnv))
	for key, value := range server.Env {
		combined[key] = value
	}
	for key, value := range secretEnv {
		combined[key] = value
	}
	keys := make([]string, 0, len(combined))
	for key := range combined {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]envEntry, 0, len(keys))
	for _, key := range keys {
		env = append(env, envEntry{Key: key, Value: combined[key]})
	}
	payload, _ := json.Marshal(struct {
		Name      string
		Transport string
		Command   string
		URL       string
		CWD       string
		Args      []string
		Env       []envEntry
		OAuth     *MCPOAuth
		OAuthAuth map[string]any
	}{server.Name, server.Transport, server.Command, server.URL, server.CWD, append([]string(nil), server.Args...), env, server.OAuth, oauthSecret})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

type ProbeSupport string

const (
	ProbeSupported   ProbeSupport = "supported"
	ProbeUnsupported ProbeSupport = "unsupported"
)

type ProbeSnapshot struct {
	Fingerprint string       `json:"fingerprint"`
	WebSearch   ProbeSupport `json:"web_search"`
	CheckedAt   time.Time    `json:"checked_at"`
}

type probeSnapshotFile struct {
	Version int                      `json:"version"`
	Entries map[string]ProbeSnapshot `json:"entries"`
}

type ProbeStore struct {
	mu      sync.Mutex
	file    string
	entries map[string]ProbeSnapshot
}

func NewProbeStore(paths Paths) (*ProbeStore, error) {
	file := filepath.Join(paths.CacheDir, "probe-snapshots.json")
	stored, err := readJSON(file, probeSnapshotFile{Version: 1, Entries: map[string]ProbeSnapshot{}})
	if err != nil {
		return nil, err
	}
	if stored.Version != 1 || stored.Entries == nil {
		stored = probeSnapshotFile{Version: 1, Entries: map[string]ProbeSnapshot{}}
	}
	return &ProbeStore{file: file, entries: stored.Entries}, nil
}

func ProbeFingerprint(baseURL, apiKey string) string {
	sum := sha256.Sum256([]byte(baseURL + "\x00" + apiKey))
	return hex.EncodeToString(sum[:])
}

func (s *ProbeStore) Get(fingerprint string) (ProbeSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.entries[fingerprint]
	return snapshot, ok
}

func (s *ProbeStore) Put(snapshot ProbeSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[snapshot.Fingerprint] = snapshot
	return writeJSON(s.file, probeSnapshotFile{Version: 1, Entries: s.entries}, false)
}

func cloneToolSurface(snapshot ToolSurfaceSnapshot) ToolSurfaceSnapshot {
	snapshot.Tools = cloneToolDefinitions(snapshot.Tools)
	return snapshot
}

func cloneToolDefinitions(values []contract.ToolDefinition) []contract.ToolDefinition {
	payload, _ := json.Marshal(values)
	var result []contract.ToolDefinition
	_ = json.Unmarshal(payload, &result)
	return result
}
