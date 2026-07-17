package instructions

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// update regenerates both goldens in this file: run
// `go test ./internal/instructions/... -run TestDump -update` after a
// reviewed, intentional text change. A diff in either golden IS the review
// surface (IS-9) — the Prefix-bytes golden's diff is, specifically, the one
// recorded epoch this feature is allowed to spend (FR-018).
var update = flag.Bool("update", false, "regenerate the instruction-system goldens")

var audienceOrder = []Audience{MainStatic, MainDynamic, Subagent, Gate}
var cacheOrder = []Cache{Prefix, Tail, Sidecar}

// renderFullDump renders every registered Text, grouped by Audience then
// Cache, sorted by ID within each group — a full reviewable inventory of the
// instruction system (IS-9's first golden). Fixed inputs only: this is the
// registry's static content, not a live-composed prompt, so it needs no
// PromptContext or engine.
func renderFullDump() string {
	byGroup := map[Audience]map[Cache][]Text{}
	for _, tx := range All() {
		if byGroup[tx.Audience] == nil {
			byGroup[tx.Audience] = map[Cache][]Text{}
		}
		byGroup[tx.Audience][tx.Cache] = append(byGroup[tx.Audience][tx.Cache], tx)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "INSTRUCTION SYSTEM DUMP — %d texts, %d examples\n", len(All()), len(AllExamples()))
	for _, aud := range audienceOrder {
		group := byGroup[aud]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n################ AUDIENCE: %s ################\n", aud)
		for _, cache := range cacheOrder {
			texts := group[cache]
			if len(texts) == 0 {
				continue
			}
			sort.Slice(texts, func(i, j int) bool { return texts[i].ID < texts[j].ID })
			fmt.Fprintf(&b, "\n---- cache: %s ----\n", cache)
			for _, tx := range texts {
				writeTextEntry(&b, tx)
			}
		}
	}
	fmt.Fprintf(&b, "\n################ EXAMPLES ################\n")
	examples := append([]Example(nil), AllExamples()...)
	sort.Slice(examples, func(i, j int) bool { return examples[i].ID < examples[j].ID })
	for _, ex := range examples {
		fmt.Fprintf(&b, "\n[example %s]\n%s\n", ex.ID, ex.Body)
	}
	return b.String()
}

func writeTextEntry(b *strings.Builder, tx Text) {
	fmt.Fprintf(b, "\n[%s]\n", tx.ID)
	if tx.StatesRule != "" {
		fmt.Fprintf(b, "  states: %s\n", tx.StatesRule)
	}
	if tx.EnforcesRule != "" {
		fmt.Fprintf(b, "  enforces: %s\n", tx.EnforcesRule)
	}
	if tx.Example != "" {
		fmt.Fprintf(b, "  example: %s\n", tx.Example)
	}
	if len(tx.MentionsTools) > 0 {
		tools := append([]string(nil), tx.MentionsTools...)
		sort.Strings(tools)
		fmt.Fprintf(b, "  mentions: %s (ctx: %s)\n", strings.Join(tools, ", "), tx.AllowlistCtx)
	}
	fmt.Fprintf(b, "  ---\n  %s\n", indentBody(tx.Body))
}

func indentBody(body string) string {
	return strings.ReplaceAll(body, "\n", "\n  ")
}

// renderPackagePrefixDump renders only this package's Prefix-class Text
// bodies, concatenated in ID order — the instructions-package half of the
// Prefix-bytes golden (IS-9). It is NOT the full wire prefix (system prompt
// + serialized tool JSON): composing that requires PromptContext and the
// tool-definition JSON shape, both of which live in orchestrator, a package
// this foundation-layer package cannot import (internal/arch/layering_test.go).
// The actual system-prompt+tools wire golden is
// internal/orchestrator/instructions_wiring_test.go's
// TestWiring_PrefixBytesGolden, which renders SystemPrompt(fixedCtx) plus
// json.Marshal(sessionDefinitions()) using these same registered bodies —
// this is IS-3's documented split applied to the golden rather than the
// audit, exactly the fallback the contract allows when a package boundary
// makes a single site impossible.
func renderPackagePrefixDump() string {
	texts := ByCache(Prefix)
	sort.Slice(texts, func(i, j int) bool { return texts[i].ID < texts[j].ID })
	var b strings.Builder
	fmt.Fprintf(&b, "PREFIX-CLASS TEXT DUMP (instructions package only) — %d texts\n", len(texts))
	for _, tx := range texts {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", tx.ID, tx.Body)
	}
	return b.String()
}

func TestDump_AllTexts(t *testing.T) {
	checkGolden(t, filepath.Join("testdata", "instructions_dump.golden"), renderFullDump())
}

func TestDump_PrefixBytes(t *testing.T) {
	checkGolden(t, filepath.Join("testdata", "prefix_bytes.golden"), renderPackagePrefixDump())
}

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s is stale — rerun with -update after reviewing the diff is an intentional change (feature 010's ONE sanctioned Prefix epoch is exactly this kind of reviewed diff)\n--- got ---\n%s", path, got)
	}
}

// TestNoDynamicSentinel_PackagePrefixDumpIsClean is a second, independent
// pass of the IS-10 sentinel check run directly over the rendered dump
// (rather than per-Text in audit_test.go), so a sentinel that only appears
// at a section boundary or via concatenation across two adjacent Texts is
// still caught.
func TestNoDynamicSentinel_PackagePrefixDumpIsClean(t *testing.T) {
	dump := renderPackagePrefixDump()
	for _, ex := range AllExamples() {
		dump = strings.ReplaceAll(dump, ex.Body, "")
	}
	for _, re := range dynamicSentinelRes {
		if re.MatchString(dump) {
			t.Errorf("the rendered Prefix-class dump contains a dynamic sentinel match for %s outside any registered worked example (IS-10)", re.String())
		}
	}
}
