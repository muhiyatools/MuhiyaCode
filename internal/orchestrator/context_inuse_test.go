package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// E-2: the /context modal's "in use" figure must match the live footer's. Both
// are request-based (they include the system prompt and tool schemas), so opening
// /context no longer drops the number to a history-only estimate.
func TestContextInUseMatchesFooter(t *testing.T) {
	var footer contract.ContextInfo
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "hi", Usage: contract.Usage{PromptTokens: 5000, PromptTokensAvailable: true, CompletionTokens: 10, TotalTokens: 5010}},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings:  &settings,
		Session:   contract.Session{ID: "e2", WorkspacePath: t.TempDir()},
		Provider:  provider,
		Registry:  NewRegistry(&recordingTool{name: "read_file"}),
		Prompt:    PromptContext{},
		Callbacks: contract.Callbacks{Context: func(info contract.ContextInfo) { footer = info }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	report := engine.ContextReport()
	footerUsed := max(footer.HistoryTokens, footer.LastRequestTokens)
	if report.HistoryTokens != footerUsed {
		t.Fatalf("modal in-use %d != footer used %d — /context disagrees with the footer", report.HistoryTokens, footerUsed)
	}
	if report.HistoryTokens < 5000 {
		t.Fatalf("modal in-use %d is the history-only estimate, not the request-based figure", report.HistoryTokens)
	}
}
