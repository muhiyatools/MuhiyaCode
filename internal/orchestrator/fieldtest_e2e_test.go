package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// End-to-end replays of the v1.1.0 field test. Each one is a transcript the
// owner actually hit, scripted so it can never happen again.

func fieldTestEngine(t *testing.T, responses ...contract.ChatResponse) (*Engine, *scriptedProvider, string) {
	t.Helper()
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "fieldtest", WorkspacePath: dir},
		Provider: provider, Registry: NewRegistry(service.Tools()...),
		Knowledge: NewKnowledge(KnowledgeSnapshot{Version: 1}, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, provider, dir
}

// mainLoopReads returns the paths the MAIN model read. Requests are filtered
// by their routing pin: the executor's own reads ride a ":sub:" stream and are
// legitimate — it must read a file before changing it. Only the main stream's
// reads count as re-verification.
func mainLoopReads(provider *scriptedProvider) []string {
	var paths []string
	for _, request := range provider.requests {
		if strings.Contains(request.SessionID, ":sub:") {
			continue
		}
		for _, message := range request.Messages {
			if message.Role != contract.RoleAssistant {
				continue
			}
			for _, call := range message.ToolCalls {
				if call.ToolName() == "read_file" {
					paths = append(paths, call.ArgumentsJSON())
				}
			}
		}
	}
	return paths
}

// TestFieldTestGoAfterPlanExecutes is THE transcript: a plan was presented, the
// user typed "Go", and the agent replied that it could not dispatch agents on
// this turn and the user should send another message. It must now execute.
func TestFieldTestGoAfterPlanExecutes(t *testing.T) {
	engine, provider, dir := fieldTestEngine(t,
		// The main model delegates the queued work.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("d1", "run_subagent", `{"agent":"general","role":"page-builder","task":"Apply the 11 queued layout steps to index.html."}`),
		}},
		// The executor's stream: read, change, then report with STATUS.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("r1", "read_file", `{"path":"index.html"}`),
		}},
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w1", "write_file", `{"path":"index.html","content":"<main>rebuilt</main>"}`),
		}},
		contract.ChatResponse{Content: "Changes made with file:line: index.html:1 rebuilt the shell.\nVerification: opened index.html and confirmed the new markup renders.\nProblems: none.\nSTATUS: COMPLETE"},
		// The main model wraps up.
		contract.ChatResponse{Content: "Layout overhaul applied to index.html and verified."},
	)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<main>old</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.previous = ClassChat // the plan turn that came before "Go"

	answer, stats, err := engine.Run(context.Background(), "Go")
	if err != nil {
		t.Fatalf("the Go turn failed: %v", err)
	}
	if stats.AgentRuns != 1 {
		t.Fatalf("the Go turn did not dispatch (AgentRuns=%d): %q", stats.AgentRuns, answer)
	}
	// The exact refusal text the owner saw must be impossible now.
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			for _, forbidden := range []string{"agents=0", "no subagent budget", "budget exhausted"} {
				if strings.Contains(message.Content, forbidden) {
					t.Fatalf("a budget refusal reached the model: %q", forbidden)
				}
			}
		}
	}
	if strings.Contains(strings.ToLower(answer), "can't dispatch") || strings.Contains(strings.ToLower(answer), "send the next message") {
		t.Fatalf("the agent asked the user for permission to work: %q", answer)
	}
	// The work actually landed.
	body, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil || !strings.Contains(string(body), "rebuilt") {
		t.Fatalf("the executor's change did not land: %q err=%v", string(body), err)
	}
}

// TestFieldTestCompleteReportIsNotReVerified: after an executor reports
// COMPLETE with its verification shown, the main model must not re-read the
// files it just changed. That re-reading is the token waste the owner saw.
func TestFieldTestCompleteReportIsNotReVerified(t *testing.T) {
	engine, provider, dir := fieldTestEngine(t,
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("d1", "run_subagent", `{"agent":"general","role":"config-fixer","task":"Fix the loader default in config.go and run the package tests."}`),
		}},
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("r1", "read_file", `{"path":"config.go"}`),
		}},
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w1", "write_file", `{"path":"config.go","content":"package config // fixed"}`),
		}},
		contract.ChatResponse{Content: "Changes made with file:line: config.go:1 default corrected.\nVerification: go test ./config passed (ok, 0.4s).\nProblems: none.\nSTATUS: COMPLETE"},
		contract.ChatResponse{Content: "Fixed the loader default; the package tests pass."},
	)
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte("package config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "fix the config loader default and make sure the tests pass"); err != nil {
		t.Fatal(err)
	}
	for _, args := range mainLoopReads(provider) {
		if strings.Contains(args, "config.go") {
			t.Fatalf("the main model re-read a file the report already verified: %s", args)
		}
	}
	// The report's own status is what licensed that trust.
	if got := parseReportStatus("Verification: go test ./config passed.\nSTATUS: COMPLETE"); got != ReportStatusComplete {
		t.Fatalf("status parse = %q, want complete", got)
	}
}

// TestReportStatusParsing pins the protocol the trust rule depends on. A
// missing status is UNKNOWN, not COMPLETE — silence must never be read as a
// verification claim.
func TestReportStatusParsing(t *testing.T) {
	for _, row := range []struct {
		name   string
		report string
		want   ReportStatus
	}{
		{"complete", "Changes made.\nSTATUS: COMPLETE", ReportStatusComplete},
		{"needs verify", "Changes made.\nSTATUS: NEEDS-VERIFY: go build ./...", ReportStatusNeedsVerify},
		{"blocked", "Could not proceed.\nSTATUS: BLOCKED: the file is generated", ReportStatusBlocked},
		{"lowercase and spaced", "  status:   complete  ", ReportStatusComplete},
		{"last line wins", "STATUS: NEEDS-VERIFY: x\nmore work happened\nSTATUS: COMPLETE", ReportStatusComplete},
		{"silence is not a claim", "Changes made. Everything looks good.", ReportStatusUnknown},
		{"prose mentioning complete", "The task is complete and working.", ReportStatusUnknown},
		{"empty", "", ReportStatusUnknown},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := parseReportStatus(row.report); got != row.want {
				t.Fatalf("parseReportStatus = %q, want %q", got, row.want)
			}
		})
	}
}
