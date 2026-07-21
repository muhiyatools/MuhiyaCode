package orchestrator

import (
	"strings"
	"testing"
)

// TestSystemPromptCarriesConcreteShellGuidance pins issue #2 end-to-end: a
// Windows/PowerShell session's system prompt carries the concrete PowerShell
// command guidance (so the model avoids POSIX idioms that fail there), while a
// POSIX session gets the lean default and none of the Windows-specific warnings.
func TestSystemPromptCarriesConcreteShellGuidance(t *testing.T) {
	win := SystemPrompt(PromptContext{Workspace: `F:\w`, OS: "Windows", Shell: "pwsh", Model: "m"})
	if !strings.Contains(win, "PowerShell") || !strings.Contains(win, "New-Item") {
		t.Fatalf("Windows/pwsh prompt lacks concrete PowerShell command guidance")
	}
	nix := SystemPrompt(PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "m"})
	if strings.Contains(nix, "PowerShell") {
		t.Fatal("a bash session must not carry PowerShell-specific guidance")
	}
	if !strings.Contains(nix, "native to this shell") {
		t.Fatal("a bash session should carry the default shell guidance")
	}
}
