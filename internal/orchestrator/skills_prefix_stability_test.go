package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// This file is the constitutional workflow gate for 013's cache-affecting
// change (contract skills-autouse §6): the skill catalog and the two tool-schema
// changes must be byte-stable, or every turn after the first pays a cold prefix.

// stablePromptContext is a fully-populated, session-fixed PromptContext for the
// byte-stability tests. It replaced delegationPromptContext, which set the
// subagent fields that no longer exist.
func stablePromptContext() PromptContext {
	return PromptContext{
		Workspace: `F:\ws`, OS: "Windows", Shell: "pwsh",
		Model: "deepseek-v4-pro", ModelAddendum: "", HasWeb: true,
	}
}

// TestSystemPromptByteStableWithAllRootCatalog: the catalog now spans home and
// configured roots, so it must still serialize identically for identical input.
func TestSystemPromptByteStableWithAllRootCatalog(t *testing.T) {
	ctx := stablePromptContext()
	ctx.Skills = []SkillListing{
		{Name: "commit-style", Path: `C:\Users\dev\.agents\skills\commit-style\SKILL.md`, Description: "House commit style."},
		{Name: "frontend-design", Path: `F:\ws\.agents\skills\frontend-design\SKILL.md`, Description: "Distinctive frontends."},
	}
	first, second := SystemPrompt(ctx), SystemPrompt(ctx)
	if first != second {
		t.Fatal("the all-roots SKILLS section is not byte-identical across constructions (contract §6.1)")
	}
}

// TestReadSkillDefinitionByteStable: a schema that re-serializes differently
// would invalidate the cached prefix on every turn.
func TestReadSkillDefinitionByteStable(t *testing.T) {
	first, err := json.Marshal(readSkillDefinition())
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(readSkillDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("read_skill definition must marshal byte-identically (contract §6.2)")
	}
}

// TestSessionToolsetByteStableAcrossTurns is the end-to-end version: the whole
// advertised tool block, including read_skill and the extended run_subagent,
// must be identical on turn 1 and turn 2 of a live session.
func TestSessionToolsetByteStableAcrossTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: demo\n---\nguidance"), 0o644); err != nil {
		t.Fatal(err)
	}
	listings := []SkillListing{{Name: "demo", Path: path, Description: "A demo skill."}}
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "one"}, {Content: "two"}}}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "stable", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{Model: "Test", Skills: listings}, SkillCatalog: listings,
		LoadSkill: testSkillLoader,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(engine.sessionDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	firstPrompt := SystemPrompt(engine.prompt)
	for _, prompt := range []string{"first task", "second task"} {
		if _, _, err := engine.Run(t.Context(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	second, err := json.Marshal(engine.sessionDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the session toolset drifted across turns — the cached prefix would be invalidated every request")
	}
	if SystemPrompt(engine.prompt) != firstPrompt {
		t.Fatal("the system prompt drifted across turns (Constitution III)")
	}
}

// TestUpgradeEpochIsAttributedNotSilent (013 contract skills-autouse §6.3,
// quickstart §5.4) is the constitutional gate on this feature's ONE sanctioned
// cache epoch.
//
// Adding read_skill and the run_subagent skills argument changes the tool block,
// and the new SKILLS listing changes the system prompt — so the first request of
// a session resumed across the upgrade cold-starts. That is acceptable exactly
// once and only if it is ATTRIBUTED: a silent cold start is indistinguishable
// from a regression, which is what makes it a constitution issue rather than a
// cosmetic one. This proves the resumed session names both changed regions.
func TestUpgradeEpochIsAttributedNotSilent(t *testing.T) {
	ctx := context.Background()
	engine := skillEngine(t, map[string]string{"frontend-design": "guidance"})

	// The shape a pre-013 session persisted: no read_skill in the tool block, and
	// a system prompt without the new SKILLS listing/hint.
	preUpgradeTools := []contract.ToolDefinition{definition("read_file", "read", map[string]any{}, nil)}
	preUpgrade, err := NewPrefixShape("pre-013 system prompt", preUpgradeTools, 0, "m")
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewPrefixShape(SystemPrompt(engine.prompt), engine.sessionDefinitions(), 0, "m")
	if err != nil {
		t.Fatal(err)
	}
	if preUpgrade.ToolsHash == current.ToolsHash || preUpgrade.SystemHash == current.SystemHash {
		t.Fatal("fixture is wrong: the upgrade must actually change both regions")
	}

	engine.priorSessionShape = &preUpgrade
	engine.checkResumeDrift(ctx, current)

	events := engine.invalidations.Events()
	if !hasCause(events, contract.InvalidationToolsetChange) {
		t.Fatalf("the read_skill toolset change was not recorded: %+v", events)
	}
	if !hasCause(events, contract.InvalidationPromptRebuild) {
		t.Fatalf("the SKILLS prompt change was not recorded: %+v", events)
	}
	engine.taskMu.Lock()
	cause := engine.lastResumeCause
	engine.taskMu.Unlock()
	if !strings.Contains(cause, "tool set") || !strings.Contains(cause, "system prompt") {
		t.Fatalf("the cold start must name what changed, got %q", cause)
	}
}

// And the epoch is strictly one-time: a session that starts fresh on the new
// binary and runs two turns must show no drift at all (the first half of §6.3).
func TestNoEpochWithinASessionOnTheNewBinary(t *testing.T) {
	engine := skillEngine(t, map[string]string{"frontend-design": "guidance"})
	first, err := NewPrefixShape(SystemPrompt(engine.prompt), engine.sessionDefinitions(), 0, "m")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPrefixShape(SystemPrompt(engine.prompt), engine.sessionDefinitions(), 0, "m")
	if err != nil {
		t.Fatal(err)
	}
	if first.SystemHash != second.SystemHash || first.ToolsHash != second.ToolsHash {
		t.Fatal("the prefix shape drifted within one session — every turn would cold-start")
	}
}

// The catalog is frozen at session start: a skill installed mid-session must
// NOT appear, because re-discovering would rewrite cached prefix bytes.
func TestSkillCatalogIsFrozenForTheSession(t *testing.T) {
	engine := skillEngine(t, map[string]string{"original": "body"})
	before := engine.skills.Len()
	if _, ok := engine.skills.Lookup("original"); !ok {
		t.Fatal("fixture skill missing from the catalog")
	}
	if _, ok := engine.skills.Lookup("added-later"); ok {
		t.Fatal("a skill never catalogued must not resolve")
	}
	if engine.skills.Len() != before {
		t.Fatal("catalog size changed without a new session")
	}
}
