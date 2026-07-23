package state

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// PrunedRecords (T041) reads back the archived originals for recovery. This
// is the read-half of a persisted write/read pair (feature 010 T038): the
// write half, AppendPruned, is wired in production (command/runtime_build.go);
// this read half has no production consumer today, so it lives here as the
// round-trip proof that what gets archived can be read back byte-correctly,
// rather than in session.go where it would be the write-only twin.
func (s *Sessions) PrunedRecords(sessionID string) ([]contract.PrunedRecord, error) {
	return readSessionJSONLines[contract.PrunedRecord](s.DB.paths, sessionID, "pruned.jsonl")
}

// Transcript reads back the durable per-session transcript.jsonl for
// round-trip verification (feature 010 T038: no production reader exists —
// the live TUI/orchestrator paths reconstruct history from the SQLite event
// log, not this file — so this stays test-only alongside PrunedRecords).
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

func TestLegacySettingsAndSecrets(t *testing.T) {
	paths := testPaths(t)
	legacy := `{"version":1,"provider":{"type":"openai-compatible","baseUrl":"https://example/v1","model":"deepseek-pro","activeModelId":"","subagentModelId":"","models":[]},"permissionMode":"normal","effort":"ultra","theme":"muhiya-dark","shell":{"preferred":"auto","timeoutMs":1000,"outputLimit":1000},"rtl":{"mode":"auto","align":"auto"},"ui":{"borderMode":"auto","density":"auto"}}`
	if err := os.WriteFile(paths.SettingsFile, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(paths)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Effort != contract.EffortMax || settings.Provider.ActiveModelID != "deepseek-pro" || len(settings.Provider.Models) != 1 {
		t.Fatalf("legacy settings not normalized: %+v", settings)
	}
	secrets := contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}
	if err := SaveSecrets(secrets, paths); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSecrets(paths)
	if err != nil || loaded != secrets {
		t.Fatalf("secrets=%+v err=%v", loaded, err)
	}
}

// TestPrunedArchiveRoundTrip (T041) verifies a reclaimed tool result archived to
// pruned.jsonl is recoverable verbatim.
func TestPrunedArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Trust(ctx, `F:\work`); err != nil {
		t.Fatal(err)
	}
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, `F:\work`, "test")
	if err != nil {
		t.Fatal(err)
	}
	record := contract.PrunedRecord{ToolCallID: "c1", ToolName: "read_file", Reason: "trim", OriginalBytes: 3000, ReducedToBytes: 500, OriginalContent: "the full original tool output"}
	if err := sessions.AppendPruned(session.ID, record); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.PrunedRecords(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OriginalContent != record.OriginalContent || got[0].ToolName != "read_file" || got[0].Reason != "trim" {
		t.Fatalf("pruned archive round-trip failed: %+v", got)
	}
}

func TestDatabaseCompatibilityAndSessions(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Trust(ctx, `F:\work`); err != nil {
		t.Fatal(err)
	}
	if trusted, _ := db.IsTrusted(ctx, `F:\work`); !trusted {
		t.Fatal("trust did not persist")
	}
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, `F:\work`, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(ctx, session.ID, "user", "message", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if err := sessions.AppendTranscript(session.ID, map[string]any{"role": "tool", "content": "Bearer abcdefghijklmnop"}); err != nil {
		t.Fatal(err)
	}
	lines, err := sessions.Transcript(session.ID)
	if err != nil || len(lines) != 1 || lines[0]["content"] == "Bearer abcdefghijklmnop" {
		t.Fatalf("transcript redaction failed: %#v %v", lines, err)
	}
	if _, err := os.Stat(filepath.Join(paths.SessionsDir, session.ID, "transcript.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicJSONCorruptionBackup(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.SecretsFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := LoadSecrets(paths)
	if err != nil || value.ProviderAPIKey != "" {
		t.Fatalf("fallback=%+v err=%v", value, err)
	}
	matches, _ := filepath.Glob(paths.SecretsFile + ".corrupt-*.bak")
	if len(matches) != 1 {
		raw, _ := json.Marshal(matches)
		t.Fatalf("missing corrupt backup: %s", raw)
	}
}

func TestMCPConfigAndSecretSeparation(t *testing.T) {
	paths := testPaths(t)
	server := MCPServer{Name: "filesystem", Transport: "stdio", Enabled: true, TimeoutMS: 30000, Command: "mcp-server", Args: []string{"."}}
	if err := UpsertMCPServer(server, paths); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPEnv(server.Name, map[string]string{"TOKEN": "secret"}, paths); err != nil {
		t.Fatal(err)
	}
	config, _ := LoadMCPConfig(paths)
	secrets, _ := LoadMCPSecrets(paths)
	if len(config.Servers) != 1 || secrets.Env[server.Name]["TOKEN"] != "secret" || config.Servers[0].Env["TOKEN"] != "" {
		t.Fatalf("config=%+v secrets=%+v", config, secrets)
	}
	removed, err := RemoveMCPServer(server.Name, paths)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	paths, err := EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
