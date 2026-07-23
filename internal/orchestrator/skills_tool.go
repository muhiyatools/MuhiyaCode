package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// SkillCatalog is the session's frozen skill index (013 data-model §1.3). It is
// built once at session start from the SAME slice that renders the SKILLS
// prefix section, so a name the model reads in its prompt always resolves to
// the file that produced that line — the catalog and the prompt cannot drift.
//
// Frozen for the session lifetime on purpose: the listing lives in the cached
// prefix, so re-discovering mid-session would rewrite cached bytes on every
// task (Constitution III). Skills installed mid-session take effect next
// session.
type SkillCatalog struct {
	entries []SkillListing
	byName  map[string]SkillListing
}

// NewSkillCatalog indexes the session listing. Names are matched
// case-insensitively because the model retypes them from the prompt.
func NewSkillCatalog(entries []SkillListing) *SkillCatalog {
	catalog := &SkillCatalog{entries: entries, byName: make(map[string]SkillListing, len(entries))}
	for _, entry := range entries {
		key := strings.ToLower(strings.TrimSpace(entry.Name))
		if key == "" {
			continue
		}
		if _, taken := catalog.byName[key]; !taken {
			catalog.byName[key] = entry
		}
	}
	return catalog
}

// Lookup resolves a model-supplied name to its listing. Nil-safe: a zero-value
// Engine (which the test suite constructs directly) has no catalog, and "no
// skills installed" is the correct answer there, not a panic.
func (c *SkillCatalog) Lookup(name string) (SkillListing, bool) {
	if c == nil {
		return SkillListing{}, false
	}
	entry, ok := c.byName[strings.ToLower(strings.TrimSpace(name))]
	return entry, ok
}

// Len reports how many skills the session advertises. Nil-safe for the same
// reason as Lookup.
func (c *SkillCatalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

// skillWrapperMarker opens the block app.AssemblePrompt writes around a skill
// body. Both places that need to know which skills a piece of text already
// carries — a submitted prompt, and a replayed subagent transcript — read it
// through skillNamesIn, so the wrapper's shape is encoded once.
const skillWrapperMarker = `<skill name="`

// skillNamesIn returns the lowercased names of every skill block in text.
func skillNamesIn(text string) map[string]bool {
	names := make(map[string]bool)
	for rest := text; ; {
		start := strings.Index(rest, skillWrapperMarker)
		if start < 0 {
			return names
		}
		rest = rest[start+len(skillWrapperMarker):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return names
		}
		if name := strings.TrimSpace(rest[:end]); name != "" {
			names[strings.ToLower(name)] = true
		}
		rest = rest[end+1:]
	}
}

// SkillLoader reads one skill's instructions from its absolute path. It is
// injected (like Rescue and Redact) because the file layer lives in
// internal/workspace, which the orchestrator must not import — the loader owns
// the size bound that keeps a skill from swallowing the context window.
type SkillLoader func(path string) (string, error)

// errNoSkillLoader means the engine was built without one. Only reachable in
// tests and misconfigured callers; the model still gets an honest failure.
var errNoSkillLoader = errors.New("no skill loader configured")

// loadSkill resolves a listing's body through the injected loader.
func (e *Engine) loadSkill(entry SkillListing) (string, error) {
	if e.skillLoader == nil {
		return "", errNoSkillLoader
	}
	return e.skillLoader(entry.Path)
}

func readSkillDefinition() contract.ToolDefinition {
	return definition("read_skill", instructions.ToolReadSkillDescription, map[string]any{
		"name": map[string]any{"type": "string", "description": instructions.ToolReadSkillNamePropertyDescription},
	}, []string{"name"})
}

type readSkillInput struct {
	Name string `json:"name"`
}

// readSkillTool loads one cataloged skill's instructions (013 contract
// skills-autouse §3).
//
// It takes a NAME, never a path. That is the security property that lets skills
// outside the workspace — the user's home skill folders — be readable at all:
// the model cannot steer this tool at an arbitrary file, because the only
// reachable paths are the SKILL.md files this session already catalogued and
// advertised. File-tool containment is untouched (Constitution IX).
func (e *Engine) readSkillTool(_ context.Context, raw json.RawMessage) (string, error) {
	var input readSkillInput
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", fmt.Errorf("invalid read_skill arguments: %w", err)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", errors.New(instructions.ToolReadSkillEmptyNameBody)
	}
	entry, ok := e.skills.Lookup(name)
	if !ok {
		return "", fmt.Errorf(instructions.ToolReadSkillUnknownTmpl, name)
	}
	// Already in hand: the manual /skills flow put this skill in the prompt, or
	// the model already read it this task. Re-sending the body would retransmit
	// content already in context for no benefit (Constitution V).
	if e.skillAlreadyProvided(entry.Name) {
		return fmt.Sprintf(instructions.ToolReadSkillAlreadyProvidedTmpl, entry.Name), nil
	}
	body, err := e.loadSkill(entry)
	if err != nil {
		return "", fmt.Errorf(instructions.ToolReadSkillUnreadableTmpl, entry.Name)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf(instructions.ToolReadSkillEmptyBodyTmpl, entry.Name)
	}
	e.markSkillProvided(entry.Name)
	// Wrap the body in the same <skill name="..."> marker the manual /skills flow
	// and subagent equipping use, so a later task can see (via seedProvidedSkills)
	// that this skill is already in the settled history and must not be re-sent
	// (A-3). Cache-safe: the result is per-turn dynamic content, not the prefix.
	return skillWrapperMarker + entry.Name + "\">\n" + strings.TrimSpace(body) + "\n</skill>", nil
}

// skillAlreadyProvided / markSkillProvided track the per-task provided set.
// Seeded at task start from the manual-flow markers already in the prompt, then
// extended by each successful read.
func (e *Engine) skillAlreadyProvided(name string) bool {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.taskSkillsProvided[strings.ToLower(name)]
}

func (e *Engine) markSkillProvided(name string) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskSkillsProvided == nil {
		e.taskSkillsProvided = make(map[string]bool)
	}
	e.taskSkillsProvided[strings.ToLower(name)] = true
}

// seedProvidedSkills records the skills the user's prompt already carries.
// Reading the prompt is enough — no extra plumbing between the TUI, the app
// layer, and the engine, and no signature churn.
func (e *Engine) seedProvidedSkills(prompt string) {
	provided := skillNamesIn(prompt)
	// A-3: a skill whose body is already in the settled history — read via read_skill
	// or equipped in a prior task — is still in context (history is append-only), so
	// a fresh read_skill must not re-send it. Scan the history for the same
	// <skill name="..."> marker. If compaction later folds a skill body out, the
	// scan no longer finds it and read_skill correctly re-sends.
	if e.history != nil {
		for _, message := range e.history.All() {
			for name := range skillNamesIn(message.Content) {
				provided[name] = true
			}
		}
	}
	e.taskMu.Lock()
	e.taskSkillsProvided = provided
	e.taskMu.Unlock()
}

// renderEquippedSkills and equippedSkillSection were removed with the subagent
// system: they packed a chosen skill's body into a delegated brief because a
// subagent had no catalog and no read_skill of its own. The one session reads
// skills for itself through read_skill, so there is nothing to equip.
