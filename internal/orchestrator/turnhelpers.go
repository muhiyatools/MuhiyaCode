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
)

func (e *Engine) drainSteering(ctx context.Context) bool {
	e.mu.Lock()
	queued := append([]string(nil), e.steering...)
	e.steering = nil
	e.mu.Unlock()
	for _, text := range queued {
		_ = e.persistMessage(ctx, "user", "message", text, "", map[string]any{"role": "user", "content": text, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)})
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

// persistMessage records one durable event (and its transcript twin). target is
// the tool's display target for tool events (empty for user/assistant), persisted
// so a resumed row names WHAT the call acted on exactly as it did live.
func (e *Engine) persistMessage(ctx context.Context, role, kind, content, target string, transcript map[string]any) error {
	if e.persistence.AddEvent != nil {
		if err := e.persistence.AddEvent(ctx, role, kind, e.redact(content), target); err != nil {
			return err
		}
	}
	if e.persistence.AppendTranscript != nil {
		if err := e.persistence.AppendTranscript(ctx, transcript); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) persistAssistant(ctx context.Context, content string) error {
	if err := e.persistMessage(ctx, "assistant", "message", content, "", map[string]any{"role": "assistant", "content": content, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return err
	}
	e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: content})
	return nil
}

func (e *Engine) finalize(ctx context.Context, content string) string {
	if strings.TrimSpace(content) == "" {
		content = "Done."
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
// workspace checklist: if tasks.md still has open items, the answer says so
// instead of implying the work is finished. Placeholder until the checklist
// feed lands; see NATIVE_AGENT_PLAN.md §4.
func (e *Engine) appendCompletionDisclosure(content string) string {
	return content
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
	if !outcome.Failed && name == "read_file" {
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
		return fmt.Sprintf("Work completed with %d changed file(s).", len(changed))
	}
	return "Done."
}

func trackChanged(outcome toolOutcome, files map[string]bool) {
	if outcome.Failed {
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
