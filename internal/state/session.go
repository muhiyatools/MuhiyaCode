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

func (s *Sessions) Transcript(sessionID string) ([]map[string]any, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(dir, "transcript.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var value map[string]any
		if json.Unmarshal(scanner.Bytes(), &value) == nil {
			result = append(result, value)
		}
	}
	return result, scanner.Err()
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
