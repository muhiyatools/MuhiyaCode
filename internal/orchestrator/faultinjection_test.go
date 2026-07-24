package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// Feature 010 US2 (FR-005..008): the fault-injection suite. Every row drives a
// FULL Engine.Run against a scripted provider and passes through ONE shared
// invariant helper, so a new fault CANNOT be added without inheriting the
// three-outcome + liveness assertions (FI-1..4). New incidents become new rows.

// recoveryOutcome is the required resolution class for a fault.
type recoveryOutcome int

const (
	// outcomeAny accepts any of the three valid outcomes (used when a fault may
	// legitimately resolve more than one way depending on scripted timing).
	outcomeAny recoveryOutcome = iota
	outcomeGuidedSuccess
	outcomeRecordedDegradation
	outcomeUserDecision
)

func (o recoveryOutcome) String() string {
	switch o {
	case outcomeGuidedSuccess:
		return "guided-success"
	case outcomeRecordedDegradation:
		return "recorded-degradation"
	case outcomeUserDecision:
		return "user-decision"
	default:
		return "any"
	}
}

// faultCase is one chaos-catalog row.
type faultCase struct {
	name      string
	responses []contract.ChatResponse
	errors    []error
	prompt    string
	tools     []contract.Tool
	want      recoveryOutcome
}

// hardTurnCeiling bounds liveness: no fault may ride the loop up toward the old
// removed ~120 ceiling. Max effort's turn budget is 48; 80 leaves headroom for
// bounded retries while still failing a genuine loop.
const faultTurnCeiling = 80

// recoveryT is the minimal testing surface assertRecoveryInvariant needs.
// *testing.T satisfies it, so every real call site is unaffected; T023's
// mutation-guard self-test (FI-14) substitutes a fake recorder instead, since
// a real *testing.T subtest's failure would propagate to and fail the OUTER
// (real) test that is trying to prove the invariant catches an unbounded case.
type recoveryT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// assertRecoveryInvariant is the single guard every fault row passes through.
// It proves the harness never produces a fourth outcome (error loop, silent
// drop, stall, hard crash) and that the flow stays live.
func assertRecoveryInvariant(t recoveryT, engine *Engine, stats contract.TaskStats, runErr error, answer string, want recoveryOutcome) {
	t.Helper()

	// (1) No hard crash: Run returns nil, or a clean cancellation.
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		t.Fatalf("fault produced a hard error (not a recovery outcome): %v", runErr)
	}

	// (2) Classify by priority into exactly one of the three allowed outcomes.
	degraded := stats.TerminatedReason != "" || historyHasLoopGuard(engine)
	decision := stats.StopCause == contract.StopCauseUserStop
	var got recoveryOutcome
	switch {
	case decision:
		got = outcomeUserDecision
	case degraded:
		got = outcomeRecordedDegradation
	default:
		got = outcomeGuidedSuccess
	}
	if want != outcomeAny && got != want {
		t.Errorf("recovery outcome = %s, want %s (answer=%q terminated=%q stop=%q)",
			got, want, truncateForLog(answer), stats.TerminatedReason, stats.StopCause)
	}

	// (3) Liveness: bounded turns.
	if stats.Turns >= faultTurnCeiling {
		t.Errorf("fault rode the loop to %d turns (ceiling %d) — a gate is not bounded", stats.Turns, faultTurnCeiling)
	}

	// (4) Liveness: no gate rejects forever — no identical substantial message
	// repeats more than 3× verbatim in history (the machine-checkable form of
	// FR-007). This is exactly the loop the H5 breaker exists to prevent.
	if msg, n := mostRepeatedMessage(engine); n > 3 {
		t.Errorf("message repeated %d× verbatim (a gate rejecting forever): %q", n, truncateForLog(msg))
	}
}

func historyHasLoopGuard(engine *Engine) bool {
	for _, m := range engine.history.All() {
		if strings.Contains(m.Content, "[loop guard]") || strings.Contains(m.Content, "[breaker]") ||
			strings.Contains(m.Content, "failed for two turns running") {
			return true
		}
	}
	return false
}

func mostRepeatedMessage(engine *Engine) (string, int) {
	counts := map[string]int{}
	worst, worstN := "", 0
	for _, m := range engine.history.All() {
		if len(m.Content) < 40 {
			continue // ignore short/boilerplate content
		}
		counts[m.Content]++
		if counts[m.Content] > worstN {
			worst, worstN = m.Content, counts[m.Content]
		}
	}
	return worst, worstN
}

func truncateForLog(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func runFaultCase(t *testing.T, tc faultCase) {
	t.Helper()
	provider := &scriptedProvider{responses: tc.responses, errors: tc.errors}
	settings := engineSettings()
	tools := tc.tools
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "fault-" + tc.name, WorkspacePath: seededWorkspace(t)},
		Provider: provider,
		Registry: NewRegistry(tools...),
		Prompt:   PromptContext{},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	prompt := tc.prompt
	if prompt == "" {
		prompt = "do the work"
	}
	answer, stats, runErr := engine.Run(context.Background(), prompt)
	assertRecoveryInvariant(t, engine, stats, runErr, answer, tc.want)
}

// schemaTool (T020/FI-5) exposes a REQUIRED field and an ENUM field so H1's
// missing-field and bad-enum branches (validateCallArgs) have something to
// validate against — recordingTool's schema declares no required/enum fields,
// so it cannot exercise those two checks.
type schemaTool struct{ name string }

func (t *schemaTool) Definition() contract.ToolDefinition {
	return definition(t.name, "test tool with a required field and an enum field", map[string]any{
		"path": map[string]any{"type": "string"},
		"mode": map[string]any{"type": "string", "enum": []any{"a", "b"}},
	}, []string{"path"})
}

func (t *schemaTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "configured.", nil
}

// TestMalformedArgsGuidedRetry is the FI-5 invariant: H1 argument validation
// guides a wrong-type, missing-required, or bad-enum call, and the task still
// ends with a usable answer — never a loop or a hard failure. It ran against a
// subagent's dispatch gate while subagents existed; the gate is shared and the
// invariant is unchanged, so it now runs where every tool call runs.
func TestMalformedArgsGuidedRetry(t *testing.T) {
	for _, tc := range []struct{ name, badArgs string }{
		{"wrong-type", `{"path":123}`},
		{"missing-required", `{"mode":"a"}`},
		{"bad-enum", `{"path":"x","mode":"z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scriptedProvider{responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", tc.badArgs)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("b", "configure_widget", `{"path":"x","mode":"a"}`)}},
				{Content: "Widget configured after correcting the arguments; the tool call succeeded."},
			}}
			settings := engineSettings()
			engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "h1-" + tc.name, WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&schemaTool{name: "configure_widget"})})
			if err != nil {
				t.Fatal(err)
			}
			answer, _, err := engine.Run(context.Background(), "configure the widget at x in mode a")
			if err != nil {
				t.Fatalf("guided retry failed: %v", err)
			}
			if !strings.Contains(answer, "configured") {
				t.Fatalf("task did not recover from the malformed call: %q", answer)
			}
			// The guidance reached the model as the malformed call's tool result.
			guided := false
			for _, request := range provider.requests {
				for _, message := range request.Messages {
					if message.Role == contract.RoleTool && strings.Contains(message.Content, "Re-emit the call with well-formed arguments") {
						guided = true
					}
				}
			}
			if !guided {
				t.Fatal("H1 guidance never reached the transcript")
			}
		})
	}
}

// TestFaultInjectionCatalog runs the chaos catalog. Rows that depend on the
// unified lifecycle (US1) are added in T009/US2; the rows below are the
// lifecycle-independent MISSING/PARTIAL cells the R5 matrix flagged.
func TestFaultInjectionCatalog(t *testing.T) {
	// FI-9: a REAL workspace.Grep (not a test stub) so the "invalid search
	// pattern" RE2-compile guidance actually fires, per contracts/fault-
	// injection.md's instruction to wire a real workspace registry.
	regexRoot := t.TempDir()
	regexTrust := workspace.NewMemoryTrustStore()
	if err := regexTrust.Trust(context.Background(), regexRoot); err != nil {
		t.Fatal(err)
	}
	regexWorkspace, err := workspace.New(regexRoot, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: regexTrust})
	if err != nil {
		t.Fatal(err)
	}

	cases := []faultCase{
		{
			// FI-5: malformed tool arguments through the main loop — the H1
			// validator guides a re-emit rather than looping or crashing. The
			// registered read_file gives H1 a schema (path:string) to catch the
			// wrong-type and bad-JSON payloads against.
			name:  "malformed-args-main-loop",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{"path": 123}`)}},
				{Content: "recovered and done"},
			},
			prompt: "read the file",
			want:   outcomeGuidedSuccess,
		},
		{
			// FI-10: a tool that keeps failing — the storm breaker / loop guard
			// bounds it and the task ends with the degradation recorded, never a
			// loop up to the ceiling.
			name:  "repeated-tool-failure-degrades",
			tools: []contract.Tool{&failingTool{name: "write_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("b", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("c", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("d", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("e", "write_file", `{"path":"x","content":"1"}`)}},
				{Content: "gave up on the tool and finished directly"},
			},
			prompt: "write the file",
			want:   outcomeAny, // storm breaker → degradation, or the model's clean recovery
		},

		// --- T020 (FI-5): malformed tool arguments through the untested
		// validateCallArgs branches — wrong primitive type (already covered by
		// malformed-args-main-loop above), missing required field, and a bad
		// enum value — through BOTH the main loop and the subagent scope.

		{
			name:  "malformed-args-missing-required-main-loop",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"mode":"a"}`)}},
				{Content: "Re-emitted with the required path and finished."},
			},
			prompt: "configure the widget",
			want:   outcomeGuidedSuccess,
		},
		{
			name:  "malformed-args-bad-enum-main-loop",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"path":"x","mode":"z"}`)}},
				{Content: "Re-emitted with a valid enum value and finished."},
			},
			prompt: "configure the widget",
			want:   outcomeGuidedSuccess,
		},

		{
			// FI-9: invalid regex through a FULL Run against a REAL workspace grep
			// tool — regexp.Compile fails and the harness returns the literal/
			// escape guidance rather than crashing or looping.
			name:  "invalid-regex-through-real-grep",
			tools: regexWorkspace.Tools(),
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("g1", "grep", `{"pattern":"[","path":"."}`)}},
				{Content: "The pattern was invalid RE2 syntax; used a literal search instead and confirmed the result."},
			},
			prompt: "search the code for TODO markers",
			want:   outcomeGuidedSuccess,
		},

		// --- T022 (FI-10): blocked capabilities — bounded escalation, never a
		// retry loop.

		{
			// H8/T034: an unknown tool name gets a nearest-match suggestion
			// instead of a bare failure, and the model self-corrects in one step.
			name:  "blocked-unknown-tool-nearest-suggestion",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_fiel", `{"path":"a.txt"}`)}},
				{Content: "Corrected the tool name and confirmed the read."},
			},
			prompt: "read the file",
			want:   outcomeGuidedSuccess,
		},
		{
			// H8: a dead/disconnected MCP tool call is named clearly so the model
			// does not waste turns retrying it.
			name: "blocked-dead-mcp-tool",
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "mcp__deploy__run", `{}`)}},
				{Content: "The MCP deploy tool is disconnected; falling back to a manual status report."},
			},
			prompt: "deploy the service",
			want:   outcomeGuidedSuccess,
		},

		{
			// FI-12/FR-004b: a turn that announces its next action without
			// calling a tool ("Let me fix:") is bounded to 2 retries, then
			// accepted as the final answer rather than looping forever.
			name:  "trailing-intent-bounded-retry",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{"path":"a.txt"}`)}},
				{Content: "Found the issue. Now let me fix the remaining validation gap:"},
				{Content: "Still need to patch the edge case. Let me handle that next:"},
				{Content: "One more pass needed. I'll wrap up the fix now:"},
			},
			prompt: "investigate and fix the bug",
			want:   outcomeGuidedSuccess,
		},

		{
			// T021/FI-6: the turn-governor's escalation-then-final-step notices
			// fire (bounding a task that outgrows its brief), and when the model
			// still does not converge afterward, the H5 distinct-failure
			// terminator (not the raw 120-turn ceiling) is what actually stops
			// it — the composite proves both bounded-recovery mechanisms compose
			// into a single recorded-degradation outcome rather than a runaway
			// loop.
			name:      "governor-escalation-then-failure-terminator-degrades",
			tools:     []contract.Tool{&recordingTool{name: "read_file"}, &failingTool{name: "write_file"}},
			responses: governorEscalationResponses(),
			want:      outcomeRecordedDegradation,
		},
		{
			name:  "empty-turn-bounded-retry",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{Content: ""},
				{Content: ""},
				{Content: ""},
				{Content: "finally recovered"},
			},
			prompt: "do empty turns",
			want:   outcomeGuidedSuccess,
		},
		{
			name:  "provider-timeout-sliding-breaker",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{Content: "done"},
			},
			errors: []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded},
			prompt: "test timeout",
			want:   outcomeRecordedDegradation,
		},
		{
			name:  "provider-503-sliding-breaker",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{Content: "done"},
			},
			errors: []error{&gateway.HTTPError{Status: 503}, &gateway.HTTPError{Status: 503}, &gateway.HTTPError{Status: 503}, &gateway.HTTPError{Status: 503}},
			prompt: "test 503",
			want:   outcomeRecordedDegradation,
		},
		{
			name:  "provider-429-degrades",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{Content: "done"},
			},
			errors: []error{&gateway.HTTPError{Status: 429}},
			prompt: "test 429",
			want:   outcomeRecordedDegradation,
		},
		{
			name:  "stalled-progress-degrades",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: stalledProgressResponses(),
			prompt: "test stalled progress",
			want:   outcomeRecordedDegradation,
		},
		{
			name:  "verification-failure-guided-success",
			tools: []contract.Tool{&recordingTool{name: "read_file"}, &schemaTool{name: "run_shell"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{"path":"a"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("b", "read_file", `{"path":"b"}`)}},
				{Content: "Verification passed on second try"},
			},
			prompt: "test verification",
			want:   outcomeGuidedSuccess,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runFaultCase(t, tc) })
	}
}

// governorEscalationResponses (T021/FI-6) scripts five harmless successful
// turns — enough for the ClassChat default prompt's turnCap=6 to trigger both
// the "[governor] Two steps remain" (turn 4) and "[governor] Final step: do
// not call tools" (turn 6) notices — followed by repeated distinct-tool
// failures (reusing the H5 fixture from h5_circuit_breaker_test.go) that the
// model keeps making in defiance of the final-step notice, until the H5
// failure-terminator fires and force-finalizes the task.
func governorEscalationResponses() []contract.ChatResponse {
	var out []contract.ChatResponse
	for i := 0; i < 5; i++ {
		out = append(out, contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("r", "read_file", `{"path":"f.txt"}`)}})
	}
	out = append(out, repeatedFailingCalls(4)...)
	return out
}

func stalledProgressResponses() []contract.ChatResponse {
	var out []contract.ChatResponse
	for i := 0; i < 16; i++ {
		out = append(out, contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall(fmt.Sprintf("r%d", i), "read_file", fmt.Sprintf(`{"path":"f%d.txt"}`, i))}})
	}
	return out
}

// --- T023 (FI-14): the mutation-guard self-test.
//
// The bounded-recovery limits are `const`s, not variables — they
// cannot be flipped at runtime by a test-only hook without editing production
// code, which this suite must not do just to exercise itself. Per the task's own
// fallback, these two tests instead prove the INVARIANT ITSELF bites: they
// feed assertRecoveryInvariant a deliberately-unbounded history/turn-count —
// exactly what an unbounded strike limit or a runaway turn-governor would
// produce — through a fake recoveryT recorder (a real *testing.T subtest's
// failure would propagate to and fail the outer, real test proving this), and
// assert the recorder observed a failure. If either of these ever reports
// "did not catch," the shared invariant has silently stopped enforcing FI-3
// and every other row in this file is only as strong as its own `want`.

// recordingRecoveryT is a fake recoveryT that records whether the invariant
// reported a failure, instead of failing the real test that invokes it.
// Fatalf only records (does not runtime.Goexit) — neither meta-test below
// exercises the hard-crash branch, which is the only one that calls Fatalf.
type recordingRecoveryT struct{ failed bool }

func (r *recordingRecoveryT) Helper()                           {}
func (r *recordingRecoveryT) Errorf(format string, args ...any) { r.failed = true }
func (r *recordingRecoveryT) Fatalf(format string, args ...any) { r.failed = true }

// TestFaultInvariantCatchesUnboundedRejectionLoop proves assertRecoveryInvariant
// fails a scenario where the same gate-rejection text repeats more than 3x —
// the machine-checkable form of "no gate rejects forever" (FR-007) that every
// bounded-recovery constant exists to prevent in production.
func TestFaultInvariantCatchesUnboundedRejectionLoop(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	denial := "that call was rejected — fix ALL of the following before re-emitting it: the argument payload is missing an observable Acceptance:/Verify: check for the change you are proposing"
	for i := 0; i < 4; i++ { // 4 identical rejections is what an UNBOUNDED strike limit would produce
		engine.history.Append(contract.Message{Role: contract.RoleUser, Content: denial})
	}
	stats := contract.TaskStats{Turns: 10}
	recorder := &recordingRecoveryT{}
	assertRecoveryInvariant(recorder, engine, stats, nil, "", outcomeAny)
	if !recorder.failed {
		t.Fatal("assertRecoveryInvariant did not catch a >3x identical gate rejection — the liveness bar would silently accept an unbounded strike limit")
	}
}

// TestFaultInvariantCatchesTurnCeilingBreach is the sibling liveness check:
// riding the loop up to the ceiling — what an unbounded turn-governor or
// failure-terminator would do — must also fail the shared invariant.
func TestFaultInvariantCatchesTurnCeilingBreach(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	stats := contract.TaskStats{Turns: faultTurnCeiling}
	recorder := &recordingRecoveryT{}
	assertRecoveryInvariant(recorder, engine, stats, nil, "", outcomeAny)
	if !recorder.failed {
		t.Fatal("assertRecoveryInvariant did not catch a turn count at the ceiling — the liveness bar would silently accept a runaway loop")
	}
}

// FI-15 row template: every new incident becomes a PERMANENT catalog row by
// filling this shape and appending it to the `cases` slice in
// TestFaultInjectionCatalog (or, when the incident is triggered by a direct
// engine-API call rather than a model tool call, a dedicated Test function
// like the ones above that still ends by calling assertRecoveryInvariant).
// The shared helper makes the three-outcome + liveness assertions
// unavoidable by construction — no row can opt out of them.
//
//	{
//	    name:      "descriptive-fault-name",              // required, unique
//	    tools:     []contract.Tool{...},                  // optional
//	    prompt:    "the user prompt",                      // optional, defaults to "do the work"
//	    responses: []contract.ChatResponse{...},           // the scripted provider script
//	    want:      outcomeGuidedSuccess,                   // or outcomeRecordedDegradation / outcomeUserDecision / outcomeAny
//	},
