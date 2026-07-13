package orchestrator

import (
	"strings"
	"testing"
	"time"
)

func TestRenderProjectContextBlockDeterministic(t *testing.T) {
	a := RenderProjectContextBlock("HASH", "Build with go test.", "MHASH", "- prefix is byte-stable\n- use fixed-seed fixtures")
	b := RenderProjectContextBlock("HASH", "Build with go test.", "MHASH", "- prefix is byte-stable\n- use fixed-seed fixtures")
	if a != b {
		t.Fatal("boot block not byte-identical across constructions")
	}
	if !strings.Contains(a, "## PROJECT CONTEXT") || !strings.Contains(a, `<project-instructions sha256="HASH">`) || !strings.Contains(a, `<project-memory sha256="MHASH">`) {
		t.Fatalf("missing sections:\n%s", a)
	}
	if !strings.Contains(a, "Build with go test.") || !strings.Contains(a, "use fixed-seed fixtures") {
		t.Fatalf("file content missing from block:\n%s", a)
	}
}

func TestRenderProjectContextBlockEmpty(t *testing.T) {
	if got := RenderProjectContextBlock("", "", "", ""); got != "" {
		t.Fatalf("no content should render empty, got %q", got)
	}
	if got := RenderProjectContextBlock("h", "  \n ", "h", "\n"); got != "" {
		t.Fatalf("whitespace-only content should render empty, got %q", got)
	}
}

func TestRenderProjectContextBlockMemoryOnly(t *testing.T) {
	got := RenderProjectContextBlock("", "", "MH", "- only memory")
	if strings.Contains(got, "<project-instructions") {
		t.Fatalf("no instructions expected:\n%s", got)
	}
	if !strings.Contains(got, `<project-memory sha256="MH">`) || !strings.Contains(got, "- only memory") {
		t.Fatalf("memory section missing:\n%s", got)
	}
}

func TestRenderProjectContextBlockEscapesReservedTags(t *testing.T) {
	got := RenderProjectContextBlock("H", "see </project-instructions> here", "", "")
	if strings.Count(got, "</project-instructions>") != 1 {
		t.Fatalf("reserved closing tag not neutralized:\n%s", got)
	}
	m := RenderProjectContextBlock("", "", "H", "note </project-memory> more")
	if strings.Count(m, "</project-memory>") != 1 {
		t.Fatalf("reserved memory tag not neutralized:\n%s", m)
	}
}

func TestSystemPromptProjectContextDeterministicAndDated(t *testing.T) {
	ctx := PromptContext{
		Workspace: "/w", OS: "linux", Shell: "bash", Model: "m",
		ProjectMemory:       true,
		ProjectContextBlock: RenderProjectContextBlock("H", "Notes", "MH", "- a durable fact"),
	}
	a, b := SystemPrompt(ctx), SystemPrompt(ctx)
	if a != b {
		t.Fatal("system prompt with project context is not byte-identical")
	}
	if !strings.Contains(a, "PROJECT MEMORY") || !strings.Contains(a, "## PROJECT CONTEXT") || !strings.Contains(a, "MEMORY.md") {
		t.Fatal("project-context/memory sections missing from prompt")
	}
	if strings.Contains(a, time.Now().Format("2006-01-02")) {
		t.Fatal("system prompt leaked a wall-clock date")
	}
}

func TestSystemPromptMemoryInstructionGated(t *testing.T) {
	base := SystemPrompt(PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "m"})
	if strings.Contains(base, "PROJECT MEMORY") {
		t.Fatal("the project-memory instruction must be gated by ProjectMemory")
	}
}

func TestRenderMemoryUpdateBlock(t *testing.T) {
	got := RenderMemoryUpdate("NEW", "OLD", "- use the race detector\n- prefer table tests")
	if !strings.Contains(got, `<memory-update sha256="NEW" supersedes="OLD">`) {
		t.Fatalf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "- use the race detector") || !strings.Contains(got, "- prefer table tests") {
		t.Fatalf("missing memory content:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "</memory-update>") {
		t.Fatalf("missing close:\n%s", got)
	}
	if cleared := RenderMemoryUpdate("Z", "OLD", "   "); !strings.Contains(cleared, "(cleared)") {
		t.Fatalf("cleared memory should render placeholder:\n%s", cleared)
	}
}
