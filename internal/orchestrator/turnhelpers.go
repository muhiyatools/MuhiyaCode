package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
)

func (e *Engine) drainSteering(ctx context.Context) bool {
	e.mu.Lock()
	queued := append([]string(nil), e.steering...)
	e.steering = nil
	e.mu.Unlock()
	for _, text := range queued {
		_ = e.persistConversation(ctx, conversationPersistence{
			Message:    contract.Message{Role: contract.RoleUser, Content: text},
			Kind:       "message",
			Transcript: map[string]any{"role": "user", "content": text, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)},
		})
		e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[Mid-task message from the user; incorporate now and keep valid completed work]\n" + text})
	}
	if len(queued) > 0 {
		e.callbacks.EmitStatus("Incorporating your message...")
	}
	return len(queued) > 0
}

func (e *Engine) hasSteering() bool {
	e.mu.Lock()
	has := len(e.steering) > 0
	e.mu.Unlock()
	return has
}

// turnRecoveryDelay paces the single in-loop retry of a failed provider call.
// Longer than the provider's own stream retry (that one recovers a dropped
// connection; this one recovers after the provider's whole retry ladder has
// already been exhausted, so the upstream deserves a moment).
const turnRecoveryDelay = 3 * time.Second

// recoverableChatError decides whether a failed turn is worth exactly one more
// attempt. Delegated to the gateway so the retry policy and the user-facing
// error text (FriendlyRequestError) are derived from the same classification —
// they used to be able to disagree about whether a failure was transient.
func recoverableChatError(err error) bool { return gateway.Recoverable(err) }

// explainRateLimit turns a 429 into an honest statement of WHICH limit was hit.
// The gateway sends one shape for "slow down" and for "out of budget", so
// without this the user is told to wait for a window that will never open, and
// any retry loop hammers a permanent condition. /v1/usage is the only surface
// that separates them; failing to reach it degrades to the generic text rather
// than guessing.
func (e *Engine) explainRateLimit(ctx context.Context, err error) string {
	if !gateway.IsRateLimited(err) {
		return ""
	}
	usage, usageErr := gateway.FetchUsage(ctx, *e.settings, e.secrets.ProviderAPIKey)
	if usageErr != nil || usage == nil {
		return ""
	}
	if !usage.BudgetExhausted() {
		return ""
	}
	message := "Your plan's budget window is used up, so this is not a wait-and-retry limit."
	if reset := strings.TrimSpace(usage.NextReset()); reset != "" {
		message += " It resets at " + reset + "."
	}
	if usage.Credits.ExtraRemaining <= 0 {
		message += " Top up credits to continue sooner."
	}
	return message
}

// sleepContext waits, but stays cancellable: a user pressing stop during a
// retry backoff must not have to wait it out.
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// persistMessage records one durable event (and its transcript twin). target is
// the tool's display target for tool events (empty for user/assistant), persisted
// so a resumed row names WHAT the call acted on exactly as it did live.
type conversationPersistence struct {
	Message    contract.Message
	Kind       string
	Target     string
	Transcript map[string]any
}

func (e *Engine) persistConversation(ctx context.Context, record conversationPersistence) error {
	if err := e.persistMessageEvent(ctx, record.Message, record.Kind, record.Target); err != nil {
		return err
	}
	if e.persistence.AddEvent != nil && legacyEventVisible(record.Message) {
		if err := e.persistence.AddEvent(ctx, string(record.Message.Role), record.Kind, e.redact(record.Message.Content), record.Target); err != nil {
			return err
		}
	}
	if e.persistence.AppendTranscript != nil {
		if err := e.persistence.AppendTranscript(ctx, record.Transcript); err != nil {
			return err
		}
	}
	return nil
}

func legacyEventVisible(message contract.Message) bool {
	return message.Role != contract.RoleAssistant || strings.TrimSpace(message.Content) != ""
}

func (e *Engine) persistAssistant(ctx context.Context, content string) error {
	record := conversationPersistence{
		Message:    contract.Message{Role: contract.RoleAssistant, Content: content},
		Kind:       "message",
		Transcript: map[string]any{"role": "assistant", "content": content, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	if err := e.persistConversation(ctx, record); err != nil {
		return err
	}
	e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: content})
	return nil
}

func (e *Engine) persistAssistantReplay(ctx context.Context, message contract.Message, content string) error {
	record := conversationPersistence{
		Message: message,
		Kind:    "message",
		Transcript: map[string]any{
			"role": "assistant", "content": content, "toolCalls": message.ToolCalls,
			"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	return e.persistConversation(ctx, record)
}

func (e *Engine) persistToolConversation(ctx context.Context, outcome toolOutcome, target string) error {
	record := conversationPersistence{
		Message: contract.Message{Role: contract.RoleTool, ToolCallID: outcome.Call.ID, Content: outcome.Output},
		Kind:    outcome.Call.ToolName(),
		Target:  target,
		Transcript: map[string]any{
			"role": "tool", "name": outcome.Call.ToolName(), "input": outcome.Call.ArgumentsJSON(),
			"output": outcome.Output, "indeterminate": outcome.IsIndeterminate(),
			"createdAt": time.Now().UTC().Format(time.RFC3339Nano), "durationMs": outcome.DurationMS,
		},
	}
	return e.persistConversation(ctx, record)
}

func (e *Engine) finalize(ctx context.Context, content string) string {
	if strings.TrimSpace(content) == "" {
		content = "No final response was produced."
	}
	// FR-017: reconcile the answer against the tasks.md checklist so a finished
	// answer never implies completion while checklist items are still open. Uses
	// only recorded state — no extra model call, no turns.
	content = e.appendCompletionDisclosure(content)
	// The task is ending regardless: the user must still see the answer even
	// if it could not be persisted, so a persistence failure here is
	// surfaced as a warning rather than turned into a task error (which
	// would discard the successful answer along with it).
	if err := e.persistAssistant(ctx, content); err != nil {
		e.callbacks.EmitStatus("Warning: failed to save the final answer to session history: " + err.Error())
	}
	return content
}

// appendCompletionDisclosure keeps the final answer honest against the
// workspace checklist: when tasks.md still has open items, the answer states
// which ones instead of implying the work is finished. Research B3 recorded
// that the model over-claims completion; this is the recorded-state-only
// guard against it (no extra model call, no extra turn).
func (e *Engine) appendCompletionDisclosure(content string) string {
	open := e.openChecklistItems()
	if len(open) == 0 || strings.Contains(strings.ToLower(content), "incomplete:") {
		return content
	}
	e.mu.Lock()
	total := len(e.checklist.Steps)
	e.mu.Unlock()
	shown := open
	if len(shown) > 3 {
		shown = shown[:3]
	}
	return content + fmt.Sprintf("\n\n— %d of %d to-dos incomplete: %s", len(open), total, strings.Join(shown, "; "))
}

func (e *Engine) redact(value string) string {
	if e.redactFn != nil {
		return e.redactFn(value)
	}
	return value
}

// persistProjectCursor (005 US3 / 006) atomically persists the advanced applied
// instruction/memory hashes into the project-context sidecar, preserving the boot
// snapshot's RenderedBootContext and other fields. It writes only when a hash
// actually changed, off the hot path under writeMu like the goal sidecar.
func (e *Engine) persistProjectCursor(ctx context.Context) {
	if e.persistence.WriteProjectContext == nil || e.projectContext == nil {
		return
	}
	if e.projectContext.AppliedMemoryHash == e.appliedMemoryHash && e.projectContext.AppliedInstructionHash == e.appliedInstructionsHash {
		return
	}
	snapshot := *e.projectContext
	snapshot.AppliedMemoryHash = e.appliedMemoryHash
	snapshot.AppliedInstructionHash = e.appliedInstructionsHash
	e.writeMu.Lock()
	_ = e.persistence.WriteProjectContext(ctx, snapshot)
	e.writeMu.Unlock()
	e.projectContext = &snapshot
}

// recordTaskFailure (H5) appends the current turn to the per-task failure
// window. Called once per failed tool outcome. The slice is pruned of stale
// entries by taskFailureWindowCount on the threshold check.
func (e *Engine) recordTaskFailure(turn int) {
	e.taskMu.Lock()
	e.taskFailures = append(e.taskFailures, turn)
	e.taskMu.Unlock()
}

// taskFailureWindowCount (H5) returns the count of failed tool calls within
// the last taskFailureWindow turns (current turn inclusive), pruning entries
// that have aged out of the window as it goes.
func (e *Engine) taskFailureWindowCount(currentTurn int) int {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	cutoff := currentTurn - taskFailureWindow
	kept := e.taskFailures[:0]
	for _, turn := range e.taskFailures {
		if turn > cutoff {
			kept = append(kept, turn)
		}
	}
	e.taskFailures = kept
	return len(e.taskFailures)
}

func (e *Engine) trackKnowledge(outcome toolOutcome) {
	name := outcome.Call.ToolName()
	if outcome.Succeeded() && name == "read_file" {
		e.knowledge.NoteFile(e.workspaceCallPath(outcome.Call), summarizeRead(outcome.Output))
	}
	if isMutation(name) {
		if path := e.workspaceCallPath(outcome.Call); path != "" && (name == "edit_file" || name == "multi_edit" || name == "write_file") {
			e.knowledge.NoteFile(path, "edited")
		} else {
			e.knowledge.MarkWorkspaceChanged()
		}
	}
}

func (e *Engine) workspaceCallPath(call contract.ToolCall) string {
	path := pathArgument(call)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.session.WorkspacePath, filepath.FromSlash(path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func fallbackAnswer(content string, changed map[string]bool) string {
	if strings.TrimSpace(content) != "" {
		return strings.TrimSpace(content)
	}
	if len(changed) > 0 {
		return fmt.Sprintf("Changed %d file(s), but no final summary was produced.", len(changed))
	}
	return "No final response was produced."
}

func trackChanged(outcome toolOutcome, files map[string]bool) {
	if outcome.IsFailure() {
		return
	}
	name := outcome.Call.ToolName()
	if name == "edit_file" || name == "multi_edit" || name == "write_file" {
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(outcome.Call.ArgumentsJSON()), &args)
		if args.Path != "" {
			files[filepathSlash(args.Path)] = true
		}
	} else if name == "apply_patch" {
		if match := regexp.MustCompile(`Applied patch to \d+ file\(s\): (.+)\.`).FindStringSubmatch(outcome.Output); len(match) > 1 {
			for _, path := range strings.Split(match[1], ", ") {
				files[filepathSlash(strings.TrimSpace(path))] = true
			}
		}
	}
}

func summarizeRead(output string) string {
	if match := regexp.MustCompile(`\(lines (\d+-\d+) of (\d+)\)`).FindStringSubmatch(output); len(match) > 2 {
		if outline := regexp.MustCompile(`(?m)^Outline: (.+)$`).FindStringSubmatch(output); len(outline) > 1 {
			return contract.TruncateEllipsis("read "+match[1]+"/"+match[2]+" · map: "+outline[1], 240)
		}
		return "read " + match[1] + "/" + match[2]
	}
	return "read"
}

func filepathSlash(value string) string { return strings.ReplaceAll(value, "\\", "/") }

func evaluateToolLoopBreakers(successfulToolCalls map[string][]int, turns int) (string, bool) {
	for name, callTurns := range successfulToolCalls {
		cutoff := turns - 15
		count := 0
		kept := callTurns[:0]
		for _, t := range callTurns {
			if t > cutoff {
				kept = append(kept, t)
				count++
			}
		}
		successfulToolCalls[name] = kept
		if count >= 15 {
			return fmt.Sprintf("repeated successful calls to %s without progress — %d calls in the last 15 turns", name, count), true
		}
	}
	return "", false
}
