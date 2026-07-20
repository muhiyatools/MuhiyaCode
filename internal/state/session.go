package state

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type Sessions struct {
	DB      *DB
	Secrets contract.Secrets
	mu      sync.Mutex
}

func (s *Sessions) GetOrCreate(ctx context.Context, workspace, title string) (contract.Session, error) {
	if session, ok, err := s.DB.LatestSession(ctx, workspace); err != nil {
		return contract.Session{}, err
	} else if ok {
		return session, nil
	}
	return s.New(ctx, workspace, title)
}

func (s *Sessions) New(ctx context.Context, workspace, title string) (contract.Session, error) {
	session, err := s.DB.CreateSession(ctx, workspace, title)
	if err != nil {
		return contract.Session{}, err
	}
	dir, err := SessionDir(s.DB.paths, session.ID)
	if err != nil {
		return contract.Session{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return contract.Session{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "transcript.jsonl"), nil, false); err != nil {
		return contract.Session{}, err
	}
	return session, nil
}

func (s *Sessions) AppendTranscript(sessionID string, value map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	value = redactObject(value, s.Secrets).(map[string]any)
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, "transcript.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Sessions) AppendUsage(sessionID string, record contract.UsageRecord) error {
	return s.appendJSONLine(sessionID, "usage.jsonl", record)
}

func (s *Sessions) UsageRecords(sessionID string) ([]contract.UsageRecord, error) {
	return readSessionJSONLines[contract.UsageRecord](s.DB.paths, sessionID, "usage.jsonl")
}

func (s *Sessions) AppendInvalidation(sessionID string, event contract.InvalidationEvent) error {
	return s.appendJSONLine(sessionID, "invalidations.jsonl", event)
}

// AppendPruned (T041) archives an original tool result before reclamation
// shortens it, so a pruned/folded result stays recoverable for debugging.
func (s *Sessions) AppendPruned(sessionID string, record contract.PrunedRecord) error {
	return s.appendJSONLine(sessionID, "pruned.jsonl", record)
}

func (s *Sessions) InvalidationEvents(sessionID string) ([]contract.InvalidationEvent, error) {
	return readSessionJSONLines[contract.InvalidationEvent](s.DB.paths, sessionID, "invalidations.jsonl")
}

func (s *Sessions) appendJSONLine(sessionID, name string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func readSessionJSONLines[T any](paths Paths, sessionID, name string) ([]T, error) {
	dir, err := SessionDir(paths, sessionID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []T
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return nil, fmt.Errorf("decode %s line %d: %w", name, line, err)
		}
		result = append(result, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return result, nil
}

// WritePrefixShape persists the session-stable prefix shape (Ultimate Polish C3),
// so a resumed session can compare its would-be first request against the last one
// and attribute a skills/tools/model change. Same atomic-write path as plan_state.
func (s *Sessions) WritePrefixShape(sessionID string, snapshot contract.PrefixShapeSnapshot) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "prefix_shape.json"), snapshot, false)
}

// ReadPrefixShape loads the prefix-shape sidecar. The bool is false when none
// exists (a fresh session); a malformed file is treated as absent so it never
// blocks resume.
func (s *Sessions) ReadPrefixShape(sessionID string) (contract.PrefixShapeSnapshot, bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return contract.PrefixShapeSnapshot{}, false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "prefix_shape.json"))
	if os.IsNotExist(err) {
		return contract.PrefixShapeSnapshot{}, false, nil
	}
	if err != nil {
		return contract.PrefixShapeSnapshot{}, false, err
	}
	var snapshot contract.PrefixShapeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return contract.PrefixShapeSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

var sessionJSONName = regexp.MustCompile(`^(history|knowledge|inspection)\.json$`)

func (s *Sessions) ReadJSON(sessionID, name string, fallback any, output any) error {
	if !sessionJSONName.MatchString(name) {
		return fmt.Errorf("invalid session JSON file %q", name)
	}
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	file := filepath.Join(dir, name)
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		encoded, _ := json.Marshal(fallback)
		return json.Unmarshal(encoded, output)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, output); err != nil {
		backup := file + ".corrupt.bak"
		_ = os.Rename(file, backup)
		encoded, _ := json.Marshal(fallback)
		return json.Unmarshal(encoded, output)
	}
	return nil
}

func (s *Sessions) WriteJSON(sessionID, name string, value any) error {
	if !sessionJSONName.MatchString(name) {
		return fmt.Errorf("invalid session JSON file %q", name)
	}
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, name), value, false)
}

// Agent context records (feature 012 R-D3, data-model.md SubagentContextRecord)
// live as one JSON file per completed subagent run under agents/ inside the
// session dir. Stored verbatim (byte-identical replay requires it) with the
// same on-disk protection as history.json; bounded by maxAgentRecords with
// oldest-completed-first eviction so a long session cannot grow unbounded.
var agentRecordName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

const maxAgentRecords = 32

func (s *Sessions) WriteAgentRecord(sessionID, runID string, value any) error {
	if !agentRecordName.MatchString(runID) {
		return fmt.Errorf("invalid agent record id %q", runID)
	}
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}
	// COMPACT encoding, never writeJSON's indented form: the transcript's raw
	// ReasoningDetails bytes are replayed verbatim on continuation, and
	// re-indenting a json.RawMessage would change the replayed wire bytes and
	// break provider prefix identity (research R-F6; caught by
	// TestAgentRecordRoundTripPreservesBytes).
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode agent record %s: %w", runID, err)
	}
	if err := writeFileAtomic(filepath.Join(agentsDir, runID+".json"), payload, false); err != nil {
		return err
	}
	pruneAgentRecords(agentsDir)
	return nil
}

// ReadAgentRecords returns the raw bytes of every persisted agent record for
// the session (unordered; each record carries its own completion timestamp).
// A missing agents/ dir is an empty result, never an error.
func (s *Sessions) ReadAgentRecords(sessionID string) ([][]byte, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "agents"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records [][]byte
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, "agents", entry.Name()))
		if readErr != nil {
			continue
		}
		records = append(records, data)
	}
	return records, nil
}

// pruneAgentRecords enforces the per-session record cap by removing the
// oldest-modified files beyond maxAgentRecords. Best-effort: eviction failures
// never surface (the cap is hygiene, not correctness).
func pruneAgentRecords(agentsDir string) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil || len(entries) <= maxAgentRecords {
		return
	}
	type aged struct {
		name string
		mod  int64
	}
	var files []aged
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, aged{name: entry.Name(), mod: info.ModTime().UnixNano()})
	}
	if len(files) <= maxAgentRecords {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod < files[j].mod })
	for _, file := range files[:len(files)-maxAgentRecords] {
		_ = os.Remove(filepath.Join(agentsDir, file.name))
	}
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)\s*[:=]\s*[^\s,}]+`),
}

func Redact(input string, secrets contract.Secrets, extra ...string) string {
	output := input
	for _, value := range append([]string{secrets.ProviderAPIKey}, extra...) {
		if len(value) >= 8 {
			output = strings.ReplaceAll(output, value, "[REDACTED_SECRET]")
		}
	}
	for _, pattern := range secretPatterns {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}
	return output
}

func redactObject(value any, secrets contract.Secrets) any {
	switch typed := value.(type) {
	case string:
		return Redact(typed, secrets)
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = redactObject(typed[i], secrets)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			result[key] = redactObject(entry, secrets)
		}
		return result
	default:
		return value
	}
}
