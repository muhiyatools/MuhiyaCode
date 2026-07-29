package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestTaskLifecycleJournalPersistsStartAndTerminal(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "complete"}}}
	settings := engineSettings()
	var events []contract.ExecutionEvent
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "lifecycle", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(),
		Persistence: Persistence{AppendExecutionEvent: func(_ context.Context, event contract.ExecutionEvent) error {
			events = append(events, event)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, stats, err := engine.Run(context.Background(), "finish the task")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 || events[0].Kind != contract.ExecutionTaskStarted ||
		events[len(events)-2].Kind != contract.ExecutionProjectionSaved ||
		events[len(events)-1].Kind != contract.ExecutionTaskTerminal {
		t.Fatalf("task lifecycle was not journaled in order: %+v", events)
	}
	terminal := events[len(events)-1]
	if events[0].Started.PromptHash == "" || terminal.Terminal.AnswerHash == "" {
		t.Fatalf("task journal omitted safe content hashes: %+v", events)
	}
	if terminal.Terminal.Status != stats.Status || answer != "complete" {
		t.Fatalf("terminal journal disagrees with returned result: event=%+v stats=%+v answer=%q", terminal, stats, answer)
	}
}

func TestTaskTerminalPersistenceFailurePreventsSuccessReport(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "complete"}}}
	settings := engineSettings()
	var callbackStats contract.TaskStats
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "terminal-failure", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(),
		Callbacks: contract.Callbacks{TaskComplete: func(stats contract.TaskStats) {
			callbackStats = stats
		}},
		Persistence: Persistence{AppendExecutionEvent: func(_ context.Context, event contract.ExecutionEvent) error {
			if event.Kind == contract.ExecutionTaskTerminal {
				return errors.New("disk full")
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, stats, err := engine.Run(context.Background(), "finish the task")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("terminal persistence failure was hidden: %v", err)
	}
	if stats.Status != contract.TaskStatusFailed || callbackStats.Status != contract.TaskStatusFailed {
		t.Fatalf("terminal persistence failure reported success: returned=%+v callback=%+v", stats, callbackStats)
	}
}

func TestMutationJournalOrdersIntentBeforeDispatchAndOutcomeAfter(t *testing.T) {
	tool := &recordingTool{name: "write_file"}
	engine := gateTestEngine(t, tool)
	engine.taskExecutionID = "task"
	var events []contract.ExecutionEvent
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		events = append(events, event)
		if event.Kind == contract.ExecutionMutationIntent && tool.calls != 0 {
			t.Fatal("mutation dispatched before intent became durable")
		}
		if event.Kind == contract.ExecutionMutationOutcome && tool.calls != 1 {
			t.Fatal("outcome persisted before dispatch completed")
		}
		return nil
	}
	defs := engine.registry.Definitions(nil)
	outcome := engine.gatedExecute(
		context.Background(),
		contract.NewToolCall("c1", "write_file", `{"path":"x","content":"y"}`),
		defs,
		Profile(contract.EffortMedium),
		subScopeFor(engine, nil),
	)

	if outcome.Status != contract.ToolOutcomeSucceeded || outcome.MutationCertainty != contract.MutationCommitted {
		t.Fatalf("typed mutation outcome is wrong: %+v", outcome)
	}
	want := []contract.ExecutionEventKind{
		contract.ExecutionToolStarted,
		contract.ExecutionMutationIntent,
		contract.ExecutionMutationOutcome,
		contract.ExecutionToolTerminal,
	}
	if len(events) != len(want) {
		t.Fatalf("mutation journal ordering is wrong: %+v", events)
	}
	for index := range want {
		if events[index].Kind != want[index] {
			t.Fatalf("mutation journal ordering is wrong: %+v", events)
		}
	}
}

func TestReadOnlyDispatchHasDurableStartAndTerminal(t *testing.T) {
	tool := &recordingTool{name: "read_file"}
	engine := gateTestEngine(t, tool)
	engine.taskExecutionID = "task"
	var events []contract.ExecutionEvent
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		events = append(events, event)
		return nil
	}
	outcome := engine.gatedExecute(
		context.Background(),
		contract.NewToolCall("read", "read_file", `{"path":"x"}`),
		engine.registry.Definitions(nil),
		Profile(contract.EffortMedium),
		subScopeFor(engine, nil),
	)
	if outcome.Status != contract.ToolOutcomeSucceeded || tool.calls != 1 {
		t.Fatalf("read-only dispatch failed: outcome=%+v calls=%d", outcome, tool.calls)
	}
	if len(events) != 2 || events[0].Kind != contract.ExecutionToolStarted ||
		events[1].Kind != contract.ExecutionToolTerminal {
		t.Fatalf("read-only lifecycle mismatch: %+v", events)
	}
	if events[0].ToolStarted.ExecutionID != events[1].ToolTerminal.ExecutionID {
		t.Fatalf("tool lifecycle records are not linked: %+v", events)
	}
}

func TestMutationOutcomeAndToolTerminalUseOneAtomicBatch(t *testing.T) {
	tool := &recordingTool{name: "write_file"}
	engine := gateTestEngine(t, tool)
	engine.taskExecutionID = "task"
	var batches [][]contract.ExecutionEvent
	engine.persistence.AppendExecutionEvents = func(_ context.Context, events []contract.ExecutionEvent) error {
		batches = append(batches, append([]contract.ExecutionEvent(nil), events...))
		return nil
	}
	outcome := engine.gatedExecute(
		context.Background(),
		contract.NewToolCall("write", "write_file", `{"path":"x","content":"y"}`),
		engine.registry.Definitions(nil),
		Profile(contract.EffortMedium),
		subScopeFor(engine, nil),
	)
	if outcome.Status != contract.ToolOutcomeSucceeded || tool.calls != 1 {
		t.Fatalf("mutation dispatch failed: outcome=%+v calls=%d", outcome, tool.calls)
	}
	if len(batches) != 3 || len(batches[2]) != 2 ||
		batches[2][0].Kind != contract.ExecutionMutationOutcome ||
		batches[2][1].Kind != contract.ExecutionToolTerminal {
		t.Fatalf("post-dispatch journal was not atomic: %+v", batches)
	}
}

func TestMutationIntentIdentityIncludesArgumentsAndShellTargetIsHashed(t *testing.T) {
	engine := gateTestEngine(t)
	engine.taskExecutionID = "task"
	engine.persistence.AppendExecutionEvent = func(context.Context, contract.ExecutionEvent) error { return nil }
	first, err := engine.persistMutationIntent(
		context.Background(),
		contract.NewToolCall("same-call", "run_shell", `{"command":"echo first-secret"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.persistMutationIntent(
		context.Background(),
		contract.NewToolCall("same-call", "run_shell", `{"command":"echo second-secret"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.IntentID == second.IntentID || first.IdempotencyKey == second.IdempotencyKey {
		t.Fatalf("different arguments collided in the mutation journal: first=%+v second=%+v", first, second)
	}
	if !strings.HasPrefix(first.Target, "shell:") || strings.Contains(first.Target, "secret") {
		t.Fatalf("shell command leaked into durable mutation metadata: %q", first.Target)
	}
}

func TestApprovalJournalPairsRequestAndResolutionWithoutShellLeak(t *testing.T) {
	engine := gateTestEngine(t)
	engine.taskExecutionID = "task"
	var events []contract.ExecutionEvent
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		events = append(events, event)
		return nil
	}
	request, err := engine.BeginApproval(context.Background(), "shell", "echo private-value", "allow shell")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ResolveApproval(context.Background(), request, true, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != contract.ExecutionApprovalAsked ||
		events[1].Kind != contract.ExecutionApprovalDecided {
		t.Fatalf("approval journal ordering mismatch: %+v", events)
	}
	if strings.Contains(request.Target, "private-value") || !strings.HasPrefix(request.Target, "shell:") {
		t.Fatalf("shell approval leaked its command: %+v", request)
	}
	if events[1].ApprovalResolution.RequestID != request.RequestID || !events[1].ApprovalResolution.Approved {
		t.Fatalf("approval resolution did not pair with request: %+v", events[1])
	}
}

func TestConversationJournalRedactsStructuredToolArguments(t *testing.T) {
	engine := gateTestEngine(t)
	engine.taskExecutionID = "task"
	engine.redactFn = func(text string) string {
		return strings.ReplaceAll(text, "secret-token", "[REDACTED]")
	}
	var saved contract.ExecutionEvent
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		saved = event
		return nil
	}
	message := contract.Message{
		Role:      contract.RoleAssistant,
		Content:   "using secret-token",
		ToolCalls: []contract.ToolCall{contract.NewToolCall("call", "run_shell", `{"command":"echo secret-token"}`)},
	}
	if err := engine.persistMessageEvent(context.Background(), message, "message", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.Message.Message.Content, "secret-token") ||
		strings.Contains(saved.Message.Message.ToolCalls[0].ArgumentsJSON(), "secret-token") {
		t.Fatalf("structured message journal retained a configured secret: %+v", saved.Message)
	}
}

func TestTerminalJournalPersistsVerificationBeforeCheckpoint(t *testing.T) {
	engine := gateTestEngine(t)
	engine.taskExecutionID = "task"
	var kinds []contract.ExecutionEventKind
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		kinds = append(kinds, event.Kind)
		return nil
	}
	stats := contract.TaskStats{
		Status: contract.TaskStatusSucceeded,
		Verification: contract.VerificationResult{
			Ran: true, Command: "go test ./...", Result: "success",
		},
	}
	if err := engine.persistTaskTerminal(context.Background(), "done", stats, nil); err != nil {
		t.Fatal(err)
	}
	want := []contract.ExecutionEventKind{
		contract.ExecutionVerification,
		contract.ExecutionProjectionSaved,
		contract.ExecutionTaskTerminal,
	}
	if len(kinds) != len(want) {
		t.Fatalf("terminal journal kinds = %v, want %v", kinds, want)
	}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("terminal journal kinds = %v, want %v", kinds, want)
		}
	}
}

func TestTerminalJournalUsesOneAtomicBatchWhenAvailable(t *testing.T) {
	engine := gateTestEngine(t)
	engine.taskExecutionID = "task"
	batches := 0
	var saved []contract.ExecutionEvent
	engine.persistence.AppendExecutionEvents = func(_ context.Context, events []contract.ExecutionEvent) error {
		batches++
		saved = append(saved, events...)
		return nil
	}
	stats := contract.TaskStats{Status: contract.TaskStatusSucceeded}
	if err := engine.persistTaskTerminal(context.Background(), "done", stats, nil); err != nil {
		t.Fatal(err)
	}
	if batches != 1 || len(saved) != 2 ||
		saved[0].Kind != contract.ExecutionProjectionSaved ||
		saved[1].Kind != contract.ExecutionTaskTerminal {
		t.Fatalf("terminal was not committed as one ordered batch: batches=%d events=%+v", batches, saved)
	}
}

func TestMutationDoesNotDispatchWhenIntentPersistenceFails(t *testing.T) {
	tool := &recordingTool{name: "write_file"}
	engine := gateTestEngine(t, tool)
	engine.taskExecutionID = "task"
	engine.persistence.AppendExecutionEvent = func(context.Context, contract.ExecutionEvent) error {
		return errors.New("disk full")
	}
	defs := engine.registry.Definitions(nil)
	outcome := engine.gatedExecute(
		context.Background(),
		contract.NewToolCall("c1", "write_file", `{"path":"x","content":"y"}`),
		defs,
		Profile(contract.EffortMedium),
		subScopeFor(engine, nil),
	)

	if !outcome.WasRejected() || tool.calls != 0 {
		t.Fatalf("mutation escaped a failed intent commit: outcome=%+v calls=%d", outcome, tool.calls)
	}
}

func TestMutationBecomesIndeterminateWhenOutcomePersistenceFails(t *testing.T) {
	tool := &recordingTool{name: "write_file"}
	engine := gateTestEngine(t, tool)
	engine.taskExecutionID = "task"
	engine.persistence.AppendExecutionEvent = func(_ context.Context, event contract.ExecutionEvent) error {
		if event.Kind == contract.ExecutionMutationOutcome {
			return errors.New("disk full")
		}
		return nil
	}
	defs := engine.registry.Definitions(nil)
	outcome := engine.gatedExecute(
		context.Background(),
		contract.NewToolCall("c1", "write_file", `{"path":"x","content":"y"}`),
		defs,
		Profile(contract.EffortMedium),
		subScopeFor(engine, nil),
	)

	if tool.calls != 1 || !outcome.IsIndeterminate() {
		t.Fatalf("uncommitted mutation outcome was not marked indeterminate: outcome=%+v calls=%d", outcome, tool.calls)
	}
}
