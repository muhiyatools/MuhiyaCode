package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestRepairSessionHistoryRebuildsCorruptProjection(t *testing.T) {
	ctx := context.Background()
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &state.Sessions{DB: db}
	workspace := t.TempDir()
	session, err := sessions.New(ctx, workspace, "repair")
	if err != nil {
		t.Fatal(err)
	}
	baseline := orchestrator.HistorySnapshot{
		Version:  1,
		Messages: []contract.Message{{Role: contract.RoleUser, Content: "before"}},
	}
	payload, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := contract.ProjectionCheckpoint{
		Name: "history", Version: 1, Payload: payload,
		Checksum: contract.ProjectionChecksum(payload),
	}
	events := []contract.ExecutionEvent{
		{
			Version: 1, SessionID: session.ID, TaskID: "task",
			Kind: contract.ExecutionProjectionSaved, Checkpoint: &checkpoint,
		},
		{
			Version: 1, SessionID: session.ID, TaskID: "task",
			Kind: contract.ExecutionMessageRecorded,
			Message: &contract.MessageRecord{
				Kind: "message", Message: contract.Message{Role: contract.RoleAssistant, Content: "after"},
			},
		},
	}
	for _, event := range events {
		if _, err := db.AppendExecutionEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	sessionDir, err := state.SessionDir(paths, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "history.json"), []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	request := sessionRepairRequest{paths: paths, workspace: workspace}
	if err := repairSessionHistory(ctx, &output, request); err != nil {
		t.Fatal(err)
	}
	var repaired orchestrator.HistorySnapshot
	reopened, err := state.Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repairedSessions := &state.Sessions{DB: reopened}
	if err := repairedSessions.ReadJSON(session.ID, "history.json", nil, &repaired); err != nil {
		t.Fatal(err)
	}
	if len(repaired.Messages) != 2 || repaired.Messages[1].Content != "after" {
		t.Fatalf("repaired history mismatch: %+v", repaired)
	}
	if !strings.Contains(output.String(), "2 messages") {
		t.Fatalf("repair output = %q", output.String())
	}
}

func TestMaskAPIKey(t *testing.T) {
	cases := map[string]string{
		"":                 "(none",    // prefix — full text has guidance
		"abc":              "····",     // ≤4 → fully hidden
		"sk-virt-0123DEAD": "····DEAD", // reveal only the last 4
	}
	for key, wantPrefix := range cases {
		got := maskAPIKey(key)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("maskAPIKey(%q) = %q, want prefix %q", key, got, wantPrefix)
		}
		// The full secret must never appear.
		if key != "" && len(key) > 4 && strings.Contains(got, key) {
			t.Errorf("maskAPIKey leaked the full key: %q", got)
		}
	}
}

func TestReconciliationInputsAreStrict(t *testing.T) {
	if certainty, err := parseMutationCertainty("not-started"); err != nil || certainty != contract.MutationNotStarted {
		t.Fatalf("parse mutation certainty = %q, %v", certainty, err)
	}
	if _, err := parseMutationCertainty("probably"); err == nil {
		t.Fatal("ambiguous mutation certainty was accepted")
	}
	if approved, err := parseApprovalDecision("approved"); err != nil || !approved {
		t.Fatalf("parse approval decision = %v, %v", approved, err)
	}
	if _, err := parseApprovalDecision("yes"); err == nil {
		t.Fatal("ambiguous approval decision was accepted")
	}
}

func TestDiagnoseShadowStaleVendorWarns(t *testing.T) {
	// The command resolves to the npm shim; the vendored exe it runs reports a
	// DIFFERENT commit than this build → the D3 stale-binary warning must fire.
	lines := diagnoseShadow(shadowInputs{
		resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true,
		vendorExe: `C:\npm\...\vendor\muhiyacode.exe`, vendorExists: true, vendorSize: 28_000_000, vendorMod: "2026-07-14 15:39",
		thisCommit: "72c7d51", vendorVersion: "MuhiyaCode 1.0.2 (commit aaaaaaa, built 2026-07-14)",
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "DIFFERENT build") || !strings.Contains(joined, "swap-global") {
		t.Fatalf("stale vendor must warn with a fix hint:\n%s", joined)
	}
}

func TestDiagnoseShadowInSyncOK(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{
		resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true,
		vendorExists: true, vendorSize: 1, vendorMod: "x",
		thisCommit: "72c7d51", vendorVersion: "MuhiyaCode 1.0.2 (commit 72c7d51+dirty, built 2026-07-16)",
	})
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "DIFFERENT build") {
		t.Fatalf("matching commits must NOT warn:\n%s", joined)
	}
	if !strings.Contains(joined, "matches THIS build") {
		t.Fatalf("matching commits should confirm sync:\n%s", joined)
	}
}

func TestDiagnoseShadowMissingVendorWarns(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true, vendorExists: false, thisCommit: "72c7d51"})
	if !strings.Contains(strings.Join(lines, "\n"), "vendored exe is missing") {
		t.Fatalf("missing vendor exe must warn to reinstall: %v", lines)
	}
}

func TestDiagnoseShadowEnvOverrideNoted(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{envBinary: `F:\build\muhiyacode.exe`, resolved: "", thisCommit: "72c7d51"})
	if !strings.Contains(strings.Join(lines, "\n"), "MUHIYACODE_BINARY") {
		t.Fatalf("MUHIYACODE_BINARY override must be surfaced: %v", lines)
	}
}

func TestWorkspaceDiagnosticsWritableAndGit(t *testing.T) {
	dir := t.TempDir()
	lines := workspaceDiagnostics(dir)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "writable") {
		t.Fatalf("temp dir must read as writable: %s", joined)
	}
	if !strings.Contains(joined, "not a git repository") {
		t.Fatalf("bare temp dir must read as non-git: %s", joined)
	}
	// Now make it a git repo (a .git dir is enough for the walk-up detector).
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(workspaceDiagnostics(dir), "\n"), "inside a git repository") {
		t.Fatal("a .git dir must be detected as a git repository")
	}
}
