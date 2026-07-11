package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

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
	if err := db.AddEvent(ctx, session.ID, "user", "message", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := sessions.AppendTranscript(session.ID, map[string]any{"role": "tool", "content": "Bearer abcdefghijklmnop"}); err != nil {
		t.Fatal(err)
	}
	lines, err := sessions.Transcript(session.ID)
	if err != nil || len(lines) != 1 || lines[0]["content"] == "Bearer abcdefghijklmnop" {
		t.Fatalf("transcript redaction failed: %#v %v", lines, err)
	}
	if _, err := os.Stat(filepath.Join(paths.SessionsDir, session.ID, "plan.md")); err != nil {
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
