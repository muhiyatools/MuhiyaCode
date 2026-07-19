package instructions

import (
	"regexp"
	"strings"
	"testing"
)

// Audit suite for feature 010 US3 (contracts/instruction-system.md IS-4..8,
// IS-10). Every check here walks the SAME registry the golden dump in
// dump_test.go renders, so a reintroduced contradiction, an unavailable-
// capability reference, a rule enforced without being stated in advance, a
// worked example that fails its own validator, or a dynamic sentinel leaking
// into the cached prefix fails the build.
//
// Two checks (worked-example-passes-validator and capability-reference) are
// necessarily mirrored here with a local copy of the real validator logic
// (this package imports only contract, so it cannot call the unexported
// orchestrator functions directly). internal/orchestrator/
// instructions_wiring_test.go cross-checks the SAME examples and the SAME
// MentionsTools/AllowlistCtx data against the real, live functions
// (missingPlanStepRequirements, labeledPlanSection, subagentSpecs) — that
// test is the "wired from real allowlists" half of IS-6/IS-7; a drift
// between the mirror and the real implementation fails there, not here.

// ByID, ByCache, AllExamples, and exampleByID are query helpers over the
// registry that only this audit suite and dump_test.go's golden dump ever
// call (feature 010 T038) — `All` stays in types.go because
// internal/orchestrator/instructions_wiring_test.go also calls it
// cross-package, which requires it to exist in the regular (non-test) build;
// these four have no such cross-package test consumer, so they live here
// instead of cluttering the production registry file with test-only surface.
// exampleByID is unexported (renamed from the former ExampleByID) because Go's
// test tooling treats any EXPORTED ExampleXxx func in a _test.go file as a
// runnable doc-example, requiring a niladic, no-return signature — `go vet`
// rejects this one's real (id string) (Example, bool) signature under that
// name; lowercasing it sidesteps the convention entirely since it was never
// genuine public API (its only caller was already this file).

// ByID returns the registered Text with the given ID.
func ByID(id string) (Text, bool) {
	mu.Lock()
	defer mu.Unlock()
	t, ok := registry[id]
	return t, ok
}

// exampleByID returns the registered Example with the given ID.
func exampleByID(id string) (Example, bool) {
	e, ok := examples[id]
	return e, ok
}

// ByCache returns every registered Text for one Cache class, in registration
// order.
func ByCache(c Cache) []Text {
	var result []Text
	for _, t := range All() {
		if t.Cache == c {
			result = append(result, t)
		}
	}
	return result
}

// AllExamples returns every registered Example in registration order.
func AllExamples() []Example {
	result := make([]Example, 0, len(exampleOrder))
	for _, id := range exampleOrder {
		result = append(result, examples[id])
	}
	return result
}

// --- IS-4: stated-in-advance -----------------------------------------------

func TestStatedInAdvance_EveryEnforcedRuleHasAStatedRule(t *testing.T) {
	texts := All()
	stated := map[string]bool{}
	for _, tx := range texts {
		if tx.StatesRule != "" {
			stated[tx.StatesRule] = true
		}
	}
	for _, tx := range texts {
		if tx.EnforcesRule == "" {
			continue
		}
		if !stated[tx.EnforcesRule] {
			t.Errorf("text %q enforces rule %q but no registered text states that rule in advance (IS-4)", tx.ID, tx.EnforcesRule)
		}
		// A rule that governs a concrete output format must carry a worked
		// example somewhere the reader sees before the gate fires.
		if tx.EnforcesRule == RulePlanStepShape || tx.EnforcesRule == RulePlanNoteShape {
			if tx.Example == "" && !statesRuleHasExample(texts, tx.EnforcesRule) {
				t.Errorf("text %q enforces format rule %q with no Example on the gate or on any text that states the rule (IS-4)", tx.ID, tx.EnforcesRule)
			}
		}
	}
}

func statesRuleHasExample(texts []Text, rule string) bool {
	for _, tx := range texts {
		if tx.StatesRule == rule && tx.Example != "" {
			return true
		}
	}
	return false
}

// --- IS-5: contradiction / canonical-copy ----------------------------------

// quotedPlanStepExampleRE finds a quoted, plan-step-shaped worked example
// (an [F#]-citation plus a Verify:/Acceptance: check) embedded in prose —
// the shape every plan-step example site historically used to show the
// model. Before feature 010 this matched FOUR distinct literal strings
// across four files; one of them (the exit_plan_mode content-bar rejection)
// had silently drifted to a different file/finding/check than the other
// three.
var quotedPlanStepExampleRE = regexp.MustCompile(`"([^"]*\[F\d+][^"]*(?:Verify|Acceptance)[^"]*)"`)

func TestCanonicalCopy_PlanStepExampleIsSingleSource(t *testing.T) {
	seen := map[string]bool{}
	for _, tx := range All() {
		for _, match := range quotedPlanStepExampleRE.FindAllStringSubmatch(tx.Body, -1) {
			seen[match[1]] = true
			if match[1] != PlanStepExampleBody {
				t.Errorf("text %q embeds a plan-step example %q that differs from the canonical instructions.PlanStepExampleBody %q (IS-5 canonical-copy)", tx.ID, match[1], PlanStepExampleBody)
			}
		}
	}
	if !seen[PlanStepExampleBody] {
		t.Fatal("no registered text embeds the canonical plan-step example; the audit's positive-match assumption is broken")
	}
}

// TestCanonicalCopy_ReportFormatFieldListsAreSingleSource guards against the
// pre-010 drift where run_subagent's tool description paraphrased the
// report-format field lists with "/" separators and silently dropped
// "/unknowns" from the research format, stating a different shape than the
// one the subagent's own handoff contract actually produced.
func TestCanonicalCopy_ReportFormatFieldListsAreSingleSource(t *testing.T) {
	cases := []struct {
		signature string // a substring unique to this format that should only ever appear as part of the exact canonical string
		canonical string
	}{
		{"Findings", ReportFormatResearch},
		{"Changes made", ReportFormatImplementation},
		{"Verified findings by severity", ReportFormatReview},
	}
	for _, tx := range All() {
		for _, c := range cases {
			if strings.Contains(tx.Body, c.signature) && !strings.Contains(tx.Body, c.canonical) {
				t.Errorf("text %q mentions %q but does not contain the canonical field list %q verbatim (IS-5 canonical-copy)", tx.ID, c.signature, c.canonical)
			}
		}
	}
}

// TestCanonicalCopy_WriteFilePermissionSentenceIsTokenIdentical pins fix (d):
// the write_file tool description IS the canonical sentence, and the system
// prompt's CONTEXT AND EDIT DISCIPLINE section contains it verbatim.
func TestCanonicalCopy_WriteFilePermissionSentenceIsTokenIdentical(t *testing.T) {
	if ToolWriteFileDescription != WriteFilePermissionRuleBody {
		t.Errorf("tool.write_file.desc (%q) is not token-identical to the canonical write_file rule sentence (%q)", ToolWriteFileDescription, WriteFilePermissionRuleBody)
	}
	if !strings.Contains(PromptContextEditDisciplineBody, WriteFilePermissionRuleBody) {
		t.Error("the system prompt's CONTEXT AND EDIT DISCIPLINE section no longer contains the canonical write_file rule sentence verbatim")
	}
}

// --- IS-6: capability-reference ---------------------------------------------

// realAllowlists mirrors the production tool surface per reader context.
// "main-loop" is the full main-loop tool set (workspace registry + the eight
// synthetic tools + web_search); "subagent.general" is the workspace
// registry ONLY — orchestrator/subagent.go's subagentSpecs() builds it from
// e.registry.Names(), and none of run_subagent/exit_plan_mode/ask_user/
// propose_changes/save_memory/edit_memory are ever added to that registry
// (they dispatch only through the main loop's synthetic-tool switch in
// engine.go executeOne). internal/orchestrator/instructions_wiring_test.go
// cross-checks this mirror against the real, live subagentSpecs() output.
var realAllowlists = map[string]map[string]bool{
	"main-loop": setOf("list_files", "read_file", "grep", "search_text", "glob", "edit_file", "multi_edit", "write_file",
		"apply_patch", "run_shell", "git_status", "git_diff", "read_plan", "update_plan", "ask_user", "propose_changes",
		"save_memory", "recall_memory", "edit_memory", "run_subagent", "exit_plan_mode", "web_search"),
	"subagent.general": setOf("list_files", "read_file", "grep", "search_text", "glob", "edit_file", "multi_edit",
		"write_file", "apply_patch", "run_shell", "git_status", "git_diff", "read_plan", "web_search"),
	"subagent.explore":   setOf("list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell", "read_plan"),
	"subagent.plan":      setOf("list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell", "read_plan"),
	"subagent.review":    setOf("list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell", "read_plan"),
	"subagent.read-only": setOf("list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell", "read_plan"),
}

func setOf(names ...string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, n := range names {
		result[n] = true
	}
	return result
}

func TestCapabilityReference_MentionedToolsAreInRealAllowlist(t *testing.T) {
	for _, tx := range All() {
		if len(tx.MentionsTools) == 0 {
			continue
		}
		if tx.AllowlistCtx == "" {
			t.Errorf("text %q names tools %v but has no AllowlistCtx to check them against (IS-6)", tx.ID, tx.MentionsTools)
			continue
		}
		allowed, known := realAllowlists[tx.AllowlistCtx]
		if !known {
			t.Errorf("text %q references unknown AllowlistCtx %q — add it to realAllowlists", tx.ID, tx.AllowlistCtx)
			continue
		}
		for _, tool := range tx.MentionsTools {
			if !allowed[tool] {
				t.Errorf("text %q (context %q) names tool %q, which is not in that reader's real allowlist (IS-6)", tx.ID, tx.AllowlistCtx, tool)
			}
		}
	}
}

// TestCapabilityReference_GeneralSubagentNeverAdvertisesSyntheticTools pins
// incoherence fix (a) directly: the general subagent's Description and
// System texts must never name run_subagent, exit_plan_mode, ask_user, or
// propose_changes — none of which its executor can honor (subagent.go
// dispatches only through registry.Execute, restricted to spec.Allowed,
// never the main loop's synthetic-tool switch).
func TestCapabilityReference_GeneralSubagentNeverAdvertisesSyntheticTools(t *testing.T) {
	forbidden := []string{"run_subagent", "exit_plan_mode", "ask_user", "propose_changes"}
	for _, id := range []string{"subagent.general.description", "subagent.general.system"} {
		tx, ok := ByID(id)
		if !ok {
			t.Fatalf("expected registered text %q", id)
		}
		for _, tool := range forbidden {
			if strings.Contains(tx.Body, tool) {
				t.Errorf("text %q advertises %q, which the general subagent's real executor rejects as an unknown tool (fix a, IS-6)", id, tool)
			}
		}
	}
}

// --- IS-7: worked-example-passes-validator ----------------------------------

// These three regexes mirror internal/orchestrator/pipeline.go's
// missingPlanStepRequirements exactly (path/function/package/symbol target,
// an [F#] citation, an Acceptance:/Verify:/Check: label). The mirror is
// cross-checked against the REAL function in
// internal/orchestrator/instructions_wiring_test.go.
var (
	mirrorPathRE            = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])([\w.-]+[/\\])*[\w.-]+\.(ts|tsx|js|jsx|json|go|py|rb|rs|java|kt|cs|cpp|c|h|css|html|vue|svelte|md|yml|yaml|toml|sql|sh|ps1|env)\b|[\w.-]+[/\\][\w.-]+[/\\][\w/\\.-]+`)
	mirrorFindingCitationRE = regexp.MustCompile(`(?i)\[f\d+(\s*[,&/+]\s*f?\d+)*]`)
	mirrorAcceptanceLabelRE = regexp.MustCompile(`(?i)\b(accept|acceptance|verify|check):`)
)

func mirrorMissingPlanStepRequirements(title string) []string {
	lower := strings.ToLower(title)
	var missing []string
	if !mirrorPathRE.MatchString(title) && !strings.Contains(lower, "function") && !strings.Contains(lower, "package ") && !strings.Contains(lower, "symbol ") {
		missing = append(missing, "an exact file/function target")
	}
	if !mirrorFindingCitationRE.MatchString(title) {
		missing = append(missing, "a [F#] finding citation")
	}
	if !mirrorAcceptanceLabelRE.MatchString(title) {
		missing = append(missing, "an observable Acceptance:/Verify: check")
	}
	return missing
}

func TestWorkedExample_PlanStepPassesValidator(t *testing.T) {
	if missing := mirrorMissingPlanStepRequirements(PlanStepExampleBody); len(missing) > 0 {
		t.Fatalf("ExamplePlanStep %q fails the plan-step validator: missing %v (IS-7)", PlanStepExampleBody, missing)
	}
}

// mirrorLabeledPlanSection mirrors pipeline.go's labeledPlanSection: does the
// note contain a non-empty <label>: section (case-insensitive)?
func mirrorLabeledPlanSection(note, label string) bool {
	lower := strings.ToLower(note)
	start := strings.Index(lower, strings.ToLower(label)+":")
	return start >= 0 && strings.TrimSpace(note[start+len(label)+1:]) != ""
}

func TestWorkedExample_PlanNotePassesValidator(t *testing.T) {
	if !mirrorLabeledPlanSection(PlanNoteExampleBody, "verification") {
		t.Errorf("ExamplePlanNote is missing a non-empty Verification: section (IS-7): %q", PlanNoteExampleBody)
	}
	if !mirrorLabeledPlanSection(PlanNoteExampleBody, "risks") {
		t.Errorf("ExamplePlanNote is missing a non-empty Risks: section (IS-7): %q", PlanNoteExampleBody)
	}
}

// --- IS-8: gate-message quality (terse gates) -------------------------------

// gateQualityRequirement is one required-field check applied to a specific
// gate Text. "N/A" fields (this gate has no budget concept, or nothing to
// enumerate) are represented by a nil matcher.
type gateQualityRequirement struct {
	id         string
	nextAction *regexp.Regexp // names an affordable next action
	reason     *regexp.Regexp // states the exact trigger/reason
}

func TestGateMessageQuality_TerseGates(t *testing.T) {
	reqs := []gateQualityRequirement{
		{
			id:         "gate.repeat-limiter",
			reason:     regexp.MustCompile(`(?i)repeated three times`),
			nextAction: regexp.MustCompile(`(?i)take a different action|or finish`),
		},
		{
			id:         "gate.duplicate-read",
			reason:     regexp.MustCompile(`(?i)already in context`),
			nextAction: regexp.MustCompile(`(?i)use that result`),
		},
		{
			id:         "gate.plan-mode.mutation",
			reason:     regexp.MustCompile(`(?i)read-only`),
			nextAction: regexp.MustCompile(`(?i)exit_plan_mode`),
		},
	}
	for _, req := range reqs {
		tx, ok := ByID(req.id)
		if !ok {
			t.Fatalf("expected registered gate text %q", req.id)
		}
		if req.reason != nil && !req.reason.MatchString(tx.Body) {
			t.Errorf("gate %q does not state its exact trigger/reason (IS-8): %q", req.id, tx.Body)
		}
		if req.nextAction != nil && !req.nextAction.MatchString(tx.Body) {
			t.Errorf("gate %q does not name an affordable next action (IS-8): %q", req.id, tx.Body)
		}
		if tx.EnforcesRule == "" {
			t.Errorf("gate %q has no EnforcesRule linkage, so the stated-in-advance audit cannot cover it (IS-8)", req.id)
		}
	}
}

// --- IS-10: no dynamic sentinel in Prefix-class bodies ----------------------

// dynamicSentinelRes match a REAL populated per-turn value, not a fixed
// worked example or a bare marker-name reference (e.g. the system prompt's
// "[task-brief]" and "agents<=N" are static teaching references present in
// the pinned prompt since before this feature — they name the marker
// syntax, they do not carry a substituted value). A colon/digit immediately
// following the sentinel is what distinguishes an actually-interpolated
// value from a static reference.
var dynamicSentinelRes = []*regexp.Regexp{
	regexp.MustCompile(`\[task-brief:\s*\S`),
	regexp.MustCompile(`\bphase=\w`),
	regexp.MustCompile(`agents<=\d`),
	regexp.MustCompile(`\[goal:(continue|complete|blocked)`),
	regexp.MustCompile(`\[F\d+]`),
}

func TestNoDynamicSentinel_PrefixBodiesAreClean(t *testing.T) {
	for _, tx := range ByCache(Prefix) {
		// A fixed worked example is allowed to LOOK dynamic — that is the
		// entire point of showing the model a concrete, realistic shape
		// (IS-4 requires exactly this for format-governing rules). Strip the
		// known, registered example bodies before scanning for a leak.
		body := tx.Body
		for _, ex := range AllExamples() {
			body = strings.ReplaceAll(body, ex.Body, "")
		}
		for _, re := range dynamicSentinelRes {
			if re.MatchString(body) {
				t.Errorf("Prefix-class text %q contains a dynamic sentinel match for %s outside any registered worked example (IS-10): %q", tx.ID, re.String(), tx.Body)
			}
		}
	}
}

// TestNoDuplicateExampleBodies is a direct structural pin for IS-5: two
// different Example IDs must never carry the same literal Body (that would
// be the canonical-copy discipline defeating itself via a second name for
// the same text) and RegisterExample already panics on a duplicate ID, so
// this only needs to check body uniqueness across distinct IDs.
func TestNoDuplicateExampleBodies(t *testing.T) {
	seen := map[string]string{}
	for _, ex := range AllExamples() {
		if prior, ok := seen[ex.Body]; ok {
			t.Errorf("examples %q and %q share the identical body (IS-5): %q", prior, ex.ID, ex.Body)
		}
		seen[ex.Body] = ex.ID
	}
}

// TestRegistryHasNoEmptyBodies is a cheap sanity net: every registered Text
// must carry a non-empty Body (an accidentally-forgotten wire-up would
// otherwise silently register an empty string).
func TestRegistryHasNoEmptyBodies(t *testing.T) {
	for _, tx := range All() {
		if strings.TrimSpace(tx.Body) == "" {
			t.Errorf("text %q has an empty Body", tx.ID)
		}
	}
}

// TestExampleReferencesResolve ensures every Text.Example points at a
// registered Example (a typo'd ID would otherwise silently mean "no
// example").
func TestExampleReferencesResolve(t *testing.T) {
	for _, tx := range All() {
		if tx.Example == "" {
			continue
		}
		if _, ok := exampleByID(tx.Example); !ok {
			t.Errorf("text %q references Example %q, which is not registered", tx.ID, tx.Example)
		}
	}
}

func TestRegistrySanity(t *testing.T) {
	if len(All()) < 40 {
		t.Fatalf("expected a substantial registered instruction surface, got %d texts — did package init not run?", len(All()))
	}
}
