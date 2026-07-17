package state

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	if err := s.WritePlan(session.ID, "No task has been planned yet."); err != nil {
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

func (s *Sessions) WritePlan(sessionID, content string) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	body := []byte(strings.TrimSpace(content) + "\n")
	if err := writeFileAtomic(filepath.Join(dir, "plan.md"), body, false); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "tasks.md"), body, false)
}

func (s *Sessions) ReadPlan(sessionID string) (string, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan.md"))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// WriteGoal (G4) persists the active-goal sidecar as goal.json next to the
// session files, following the WritePlan atomic-write pattern. Only active
// goals are written; callers clear the sidecar when a goal completes, is
// blocked, or is cleared so a finished objective never resurrects on resume.
func (s *Sessions) WriteGoal(sessionID string, snapshot contract.GoalSnapshot) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "goal.json"), snapshot, false)
}

// ReadGoal (G4) loads the goal sidecar. The bool is false when no sidecar
// exists (no goal was active when the session last closed); a malformed file
// is treated as absent so a corrupt sidecar never blocks session resume.
func (s *Sessions) ReadGoal(sessionID string) (contract.GoalSnapshot, bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return contract.GoalSnapshot{}, false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "goal.json"))
	if os.IsNotExist(err) {
		return contract.GoalSnapshot{}, false, nil
	}
	if err != nil {
		return contract.GoalSnapshot{}, false, err
	}
	var snapshot contract.GoalSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return contract.GoalSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

// ClearGoal (G4) removes the goal sidecar. A missing file is not an error so
// callers can invoke it unconditionally on every goal-clearing transition.
func (s *Sessions) ClearGoal(sessionID string) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "goal.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WritePlanState (P2) persists the plan-mode and pending-plan flags to the
// plan_state.json sidecar next to the session files, mirroring the goal
// sidecar's atomic-write pattern. The plan CONTENT persists via plan.md/
// tasks.md (WritePlan); this carries only the two flags the proceed flow needs.
func (s *Sessions) WritePlanState(sessionID string, snapshot contract.PlanStateSnapshot) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "plan_state.json"), snapshot, false)
}

// ReadPlanState (P2) loads the plan-state sidecar. The bool is false when no
// sidecar exists (no plan mode was active and no plan was pending when the
// session last closed); a malformed file is treated as absent so a corrupt
// sidecar never blocks session resume.
func (s *Sessions) ReadPlanState(sessionID string) (contract.PlanStateSnapshot, bool, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return contract.PlanStateSnapshot{}, false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan_state.json"))
	if os.IsNotExist(err) {
		return contract.PlanStateSnapshot{}, false, nil
	}
	if err != nil {
		return contract.PlanStateSnapshot{}, false, err
	}
	var snapshot contract.PlanStateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return contract.PlanStateSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

// ClearPlanState (P2) removes the plan-state sidecar when both flags are false
// (plan mode off and no pending plan). A missing file is not an error so
// callers can invoke it unconditionally when the state goes fully idle.
func (s *Sessions) ClearPlanState(sessionID string) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "plan_state.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
