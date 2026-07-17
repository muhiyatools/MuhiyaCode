// Package instructions is the single, audited home for every model-facing
// text in MuhiyaCode: system-prompt sections, tool + property descriptions,
// subagent system/handoff/capability texts, phase preludes, mode blocks, gate
// and denial and loop-guard texts, model-facing tool errors, and the gateway
// per-family prompt addenda (feature 010 US3, contracts/instruction-system.md).
//
// It is a foundation-layer package: it imports only internal/contract, so
// orchestrator, workspace, and gateway can each depend on it without creating
// an import cycle (see internal/arch/layering_test.go). Composer functions
// that render session-invariant Prefix-class output stay pure functions of
// their inputs, per IS-2.
//
// Every text is registered as a Text record alongside its literal Body
// constant. Registration gives internal/instructions/audit_test.go and
// internal/instructions/dump_test.go one reviewable surface: the
// stated-in-advance check, the contradiction/canonical-copy check, the
// capability-reference check, the worked-example-passes-validator check, the
// gate-message-quality check, and the two goldens (the full instruction dump
// and the separate Prefix-bytes golden) all walk this same registry.
package instructions

import (
	"fmt"
	"sync"
)

// Audience is who reads a Text: the main-loop model reading the session-
// stable prompt, the main-loop model reading per-turn dynamic tail content,
// a delegated subagent, or a gate/denial surfaced as a tool result.
type Audience string

const (
	// MainStatic is main-loop content that is byte-identical every turn
	// (system-prompt sections, tool + property descriptions).
	MainStatic Audience = "main-static"
	// MainDynamic is main-loop content that varies per turn (task-brief
	// markers, the plan block, the goal block, pipeline phase preludes).
	MainDynamic Audience = "main-dynamic"
	// Subagent is content composed into a delegated subagent's own system
	// message or handoff contract for one run.
	Subagent Audience = "subagent"
	// Gate is a denial/block/loop-guard text returned as a tool result.
	Gate Audience = "gate"
)

// Cache is the wire region a Text's rendered bytes ride on.
type Cache string

const (
	// Prefix is the session-stable region: the system prompt and the
	// serialized tool JSON. Its bytes must be identical across turns within a
	// session (the DeepSeek implicit prefix cache invariant) and must never
	// contain a dynamic sentinel (IS-10).
	Prefix Cache = "prefix"
	// Tail is the per-turn dynamic region riding on the user message (the
	// task brief, plan block, goal block, pipeline phase preludes).
	Tail Cache = "tail"
	// Sidecar is per-call or per-run dynamic content that is neither the
	// stable prefix nor the turn tail: subagent system/handoff text (fresh
	// per subagent run) and gate/denial tool-result text (appended per call).
	Sidecar Cache = "sidecar"
)

// Text is one registered model-facing text.
type Text struct {
	// ID is a stable, human-readable identifier, e.g. "tool.write_file.desc"
	// or "gate.plan-mode.mutation". Referenced by audits and by other Texts
	// that must stay a canonical single copy (IS-5).
	ID string
	// Audience is who reads this text.
	Audience Audience
	// Cache is which wire region the rendered bytes ride on.
	Cache Cache
	// Body is the literal text or template (Sprintf-style verbs allowed for
	// Tail/Sidecar/Gate text whose dynamic parts are filled at call time;
	// Prefix-class Body must be the complete, final literal string with no
	// dynamic sentinel — see IS-10 and NoDynamicSentinel).
	Body string
	// StatesRule is the rule ID this text states IN ADVANCE, before any
	// violation (IS-4). Empty when this text does not proactively state a
	// rule.
	StatesRule string
	// EnforcesRule is the rule ID this text enforces as a gate/denial (IS-4).
	// Every EnforcesRule must have a matching StatesRule visible to the same
	// reader before the violation that triggers this text.
	EnforcesRule string
	// Example is a worked-example ID (from examples.go) this text carries or
	// references, mandatory whenever EnforcesRule governs a concrete output
	// format (IS-4, IS-7).
	Example string
	// MentionsTools lists tool names this text literally names. Every entry
	// must be present in the reader's real allowlist, keyed by AllowlistCtx
	// (IS-6, the capability-reference check).
	MentionsTools []string
	// AllowlistCtx is the key into the real-allowlist map this text's reader
	// operates under (e.g. "subagent.general", "subagent.explore",
	// "main-loop"). Empty when MentionsTools is empty.
	AllowlistCtx string
}

var (
	mu       sync.Mutex
	registry = map[string]Text{}
	order    []string
)

// Register adds a Text to the single audited registry and returns it
// unchanged, so call sites can write:
//
//	var FooBody = "..."
//	var FooText = Register(Text{ID: "foo", Body: FooBody, ...})
//
// It panics on an empty or duplicate ID: every registered text must have a
// stable, unique identifier — a duplicate is a canonical-copy violation
// caught at init time rather than left for the audit suite to find.
func Register(t Text) Text {
	if t.ID == "" {
		panic("instructions: Text registered with empty ID")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[t.ID]; exists {
		panic(fmt.Sprintf("instructions: duplicate Text ID %q", t.ID))
	}
	registry[t.ID] = t
	order = append(order, t.ID)
	return t
}

// All returns every registered Text in registration order (deterministic:
// Go initializes package-level vars in dependency order within a file and in
// file-name order across files, so repeated runs see the same order).
func All() []Text {
	mu.Lock()
	defer mu.Unlock()
	result := make([]Text, 0, len(order))
	for _, id := range order {
		result = append(result, registry[id])
	}
	return result
}
