package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// skillEngine builds an engine whose catalog holds the given skills, each
// written to a real file so read_skill exercises the actual loader.
func skillEngine(t *testing.T, skills map[string]string) *Engine {
	t.Helper()
	dir := t.TempDir()
	listings := make([]SkillListing, 0, len(skills))
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	// Deterministic construction order so catalog indexing is reproducible.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		path := filepath.Join(dir, name+".md")
		if err := os.WriteFile(path, []byte(skills[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		listings = append(listings, SkillListing{Name: name, Path: path, Description: name + " description"})
	}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "skills", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Prompt: PromptContext{Model: "Test", Skills: listings}, SkillCatalog: listings,
		LoadSkill: testSkillLoader,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// testSkillLoader mirrors the production loader's contract: read the file,
// refuse anything over the bound (command.SkillBodyLimit).
func testSkillLoader(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > testSkillBodyLimit {
		return "", os.ErrInvalid
	}
	return strings.TrimSpace(string(data)), nil
}

const testSkillBodyLimit = 32 * 1024

func readSkillCall(t *testing.T, engine *Engine, name string) (string, error) {
	t.Helper()
	raw, err := json.Marshal(readSkillInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return engine.readSkillTool(context.Background(), raw)
}

// TestReadSkillReturnsCatalogedBody is the core of automatic use: a name from
// the SKILLS listing yields that skill's full instructions.
func TestReadSkillReturnsCatalogedBody(t *testing.T) {
	engine := skillEngine(t, map[string]string{"frontend-design": "Use a warm editorial palette."})
	out, err := readSkillCall(t, engine, "frontend-design")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "warm editorial palette") {
		t.Fatalf("body not returned: %q", out)
	}
}

// Names come back from the model retyped; matching must not be case-brittle.
func TestReadSkillMatchesNameCaseInsensitively(t *testing.T) {
	engine := skillEngine(t, map[string]string{"commit-style": "Plain sentences."})
	if _, err := readSkillCall(t, engine, "Commit-Style"); err != nil {
		t.Fatalf("case-insensitive lookup failed: %v", err)
	}
	if _, err := readSkillCall(t, engine, "  commit-style  "); err != nil {
		t.Fatalf("surrounding whitespace should be tolerated: %v", err)
	}
}

// An unknown name must fail instructively and WITHOUT disclosing a path —
// skills live outside the workspace, and the error is model-visible.
func TestReadSkillUnknownNameIsInstructiveAndPathFree(t *testing.T) {
	engine := skillEngine(t, map[string]string{"known": "body"})
	out, err := readSkillCall(t, engine, "nonexistent")
	if err == nil {
		t.Fatalf("unknown skill must fail, got %q", out)
	}
	message := err.Error()
	if !strings.Contains(message, "SKILLS") {
		t.Fatalf("error should point back at the SKILLS list: %q", message)
	}
	if strings.Contains(message, string(filepath.Separator)) || strings.Contains(message, "SKILL.md") {
		t.Fatalf("error disclosed a filesystem path: %q", message)
	}
}

// FR-008: the manual /skills flow already put the body in the prompt, so
// read_skill must not send a second copy (Constitution V).
func TestReadSkillDoesNotResendWhatThePromptAlreadyCarries(t *testing.T) {
	engine := skillEngine(t, map[string]string{"frontend-design": "Use a warm editorial palette."})
	engine.seedProvidedSkills("Follow the selected skill instructions.\n\n<skill name=\"frontend-design\">\nUse a warm editorial palette.\n</skill>\n\nUser prompt:\nbuild a page")

	out, err := readSkillCall(t, engine, "frontend-design")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "warm editorial palette") {
		t.Fatalf("the body was retransmitted despite already being in context: %q", out)
	}
	if !strings.Contains(out, "already in this task's context") {
		t.Fatalf("the notice should say why nothing was sent: %q", out)
	}
}

// A second read within one task is the same waste as the manual case.
func TestReadSkillSecondCallInSameTaskReturnsPointer(t *testing.T) {
	engine := skillEngine(t, map[string]string{"a-skill": "the guidance"})
	if _, err := readSkillCall(t, engine, "a-skill"); err != nil {
		t.Fatal(err)
	}
	out, err := readSkillCall(t, engine, "a-skill")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "the guidance") {
		t.Fatalf("same-task repeat resent the body: %q", out)
	}
}

// Per-task state: a fresh task may legitimately read the same skill again.
func TestReadSkillProvidedSetResetsBetweenTasks(t *testing.T) {
	engine := skillEngine(t, map[string]string{"a-skill": "the guidance"})
	if _, err := readSkillCall(t, engine, "a-skill"); err != nil {
		t.Fatal(err)
	}
	engine.resetTaskState(Budget{})
	engine.seedProvidedSkills("a new task with no skill blocks")
	out, err := readSkillCall(t, engine, "a-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the guidance") {
		t.Fatalf("a new task must be able to read the skill again: %q", out)
	}
}

// A-3: a skill whose body is already in the settled history must NOT be re-sent
// by a fresh task's read_skill — it is still in context.
func TestReadSkillNotResentWhenInSettledHistory(t *testing.T) {
	engine := skillEngine(t, map[string]string{"a-skill": "the guidance"})
	engine.history.Append(contract.Message{Role: contract.RoleTool, Content: "<skill name=\"a-skill\">\nthe guidance\n</skill>"})
	engine.resetTaskState(Budget{})
	engine.seedProvidedSkills("a new task with no skill blocks")
	out, err := readSkillCall(t, engine, "a-skill")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "the guidance") {
		t.Fatalf("read_skill re-sent a body already in the settled history (A-3): %q", out)
	}
}

// FR-009: an oversized skill degrades to an instructive failure, never a
// context-swallowing dump and never a crash.
func TestReadSkillRejectsOversizedBody(t *testing.T) {
	engine := skillEngine(t, map[string]string{"huge": strings.Repeat("x", testSkillBodyLimit+1)})
	if _, err := readSkillCall(t, engine, "huge"); err == nil {
		t.Fatal("an oversized skill must not be returned")
	} else if !strings.Contains(err.Error(), "Continue without it") {
		t.Fatalf("failure should tell the model what to do next: %v", err)
	}
}

func TestReadSkillRequiresAName(t *testing.T) {
	engine := skillEngine(t, map[string]string{"a": "b"})
	if _, err := readSkillCall(t, engine, "   "); err == nil {
		t.Fatal("an empty name must fail")
	}
}

func TestReadSkillSectionAddressingAndMandatoryFallback(t *testing.T) {
	safe := skillEngine(t, map[string]string{"safe": "<!-- section-safe -->\n# Rules\nmandatory\n# Examples\nexample body\n# Other\nother"})
	raw, _ := json.Marshal(readSkillInput{Name: "safe", Section: "Examples"})
	out, err := safe.readSkillTool(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "example body") || strings.Contains(out, "other") || strings.Contains(out, "mandatory") {
		t.Fatalf("section output=%q", out)
	}
	unsafe := skillEngine(t, map[string]string{"unsafe": "# Rules\nmandatory\n# Examples\nexample body"})
	raw, _ = json.Marshal(readSkillInput{Name: "unsafe", Section: "Examples"})
	out, err = unsafe.readSkillTool(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mandatory") || !strings.Contains(out, "example body") {
		t.Fatalf("mandatory fallback was lossy: %q", out)
	}
}

func TestSkillBodyLoadedOncePerSession(t *testing.T) {
	loads := 0
	listing := SkillListing{Name: "cached", Path: "virtual"}
	e := &Engine{skills: NewSkillCatalog([]SkillListing{listing}), history: NewHistory(HistorySnapshot{Version: 1}, nil),
		skillLoader: func(string) (string, error) { loads++; return "body", nil }}
	if _, err := e.loadSkill(listing); err != nil {
		t.Fatal(err)
	}
	if _, err := e.loadSkill(listing); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loads=%d", loads)
	}
}

// The tool is advertised only when skills exist: otherwise it is prefix weight
// the model can only misuse.
func TestReadSkillDefinitionOnlyWhenSkillsExist(t *testing.T) {
	withSkills := skillEngine(t, map[string]string{"one": "body"})
	if !hasDefinition(withSkills.sessionDefinitions(), "read_skill") {
		t.Fatal("read_skill must be advertised when the session has skills")
	}
	settings := engineSettings()
	bare, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "bare", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasDefinition(bare.sessionDefinitions(), "read_skill") {
		t.Fatal("read_skill must not be advertised when no skills are installed")
	}
}

// seedProvidedSkills reads the manual wrapper the app layer emits. Multiple
// blocks in one prompt must all register.
func TestSeedProvidedSkillsParsesEveryManualBlock(t *testing.T) {
	engine := skillEngine(t, map[string]string{"one": "1", "two": "2"})
	engine.seedProvidedSkills(`<skill name="one">a</skill>` + "\n" + `<skill name="two">b</skill>`)
	if !engine.skillAlreadyProvided("one") || !engine.skillAlreadyProvided("two") {
		t.Fatal("both manual skill blocks should register as provided")
	}
	if engine.skillAlreadyProvided("three") {
		t.Fatal("an unmentioned skill must not register as provided")
	}
}
