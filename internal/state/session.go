package state

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/secrecy"
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

func (s *Sessions) ForkAt(ctx context.Context, sourceID, title string, cursor int64, cutoff time.Time) (contract.Session, error) {
	session, err := s.DB.ForkSessionAt(ctx, sourceID, title, cursor, cutoff)
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
	sourceRuntime, ok, err := s.ReadRuntimeConfig(sourceID)
	if err != nil {
		return contract.Session{}, err
	}
	if ok {
		sourceRuntime.CacheEpoch = 0
		sourceRuntime.ModelLineages = map[string]contract.ModelLineage{
			sourceRuntime.ModelID: {CacheEpoch: 0},
		}
		if err := s.WriteRuntimeConfig(session.ID, sourceRuntime); err != nil {
			return contract.Session{}, err
		}
	}
	return session, nil
}

func (s *Sessions) Delete(ctx context.Context, id string) error {
	dir, err := SessionDir(s.DB.paths, id)
	if err != nil {
		return err
	}
	inside, err := filepath.Rel(s.DB.paths.SessionsDir, dir)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse to delete session directory outside session root")
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		if os.IsNotExist(statErr) {
			return s.DB.DeleteSession(ctx, id)
		}
		return statErr
	}
	quarantine := filepath.Join(s.DB.paths.SessionsDir, ".deleting-"+id+"-"+fmt.Sprint(time.Now().UnixNano()))
	if err := os.Rename(dir, quarantine); err != nil {
		return err
	}
	if err := s.DB.DeleteSession(ctx, id); err != nil {
		_ = os.Rename(quarantine, dir)
		return err
	}
	return os.RemoveAll(quarantine)
}

func (s *Sessions) Import(ctx context.Context, bundle SessionBundle, workspace string) (contract.Session, error) {
	session, err := s.DB.ImportSession(ctx, bundle, workspace)
	if err != nil {
		return contract.Session{}, err
	}
	dir, err := SessionDir(s.DB.paths, session.ID)
	if err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	if err == nil {
		err = writeFileAtomic(filepath.Join(dir, "transcript.jsonl"), nil, false)
	}
	if err != nil {
		_ = s.DB.DeleteSession(ctx, session.ID)
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

func (s *Sessions) WriteUsageRecords(sessionID string, records []contract.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var content []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			return err
		}
		content = append(content, line...)
		content = append(content, '\n')
	}
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "usage.jsonl"), content, false)
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
	var rawLines [][]byte
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		rawLines = append(rawLines, append([]byte(nil), scanner.Bytes()...))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	var result []T
	for i, raw := range rawLines {
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			// These files are written append-only with a trailing fsync, so a crash
			// mid-append can leave only the LAST line partial. Tolerate a malformed
			// trailing line (return everything parsed so far) rather than discarding
			// the whole ledger and hard-failing resume (F-2). A malformed line with
			// real data after it is genuine corruption and still surfaces.
			if allBlankLines(rawLines[i+1:]) {
				log.Printf("[state] %s line %d is truncated (likely a crash mid-append); resuming with the %d prior record(s)", name, i+1, len(result))
				break
			}
			return nil, fmt.Errorf("decode %s line %d: %w", name, i+1, err)
		}
		result = append(result, value)
	}
	return result, nil
}

// allBlankLines reports whether every line is whitespace-only, i.e. nothing but a
// partial trailing record follows the current position.
func allBlankLines(lines [][]byte) bool {
	for _, line := range lines {
		if len(strings.TrimSpace(string(line))) != 0 {
			return false
		}
	}
	return true
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

func Redact(input string, secrets contract.Secrets, extra ...string) string {
	return secrecy.Redact(input, append([]string{secrets.ProviderAPIKey}, extra...)...)
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
