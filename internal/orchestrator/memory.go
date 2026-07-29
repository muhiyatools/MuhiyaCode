package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// Agent-managed durable memory (feature 008 US5, extended by the Experience
// Overhaul B1/B2/B3). Memory lives in the per-project store directory
// (~/.muhiya/projects/<id>/memory), NOT the workspace, so it never pollutes a repo
// and is unreachable by the model's file tools — the model reaches it ONLY through
// save_memory / recall_memory, exactly as before. The store holds:
//   - MEMORY.md   the always-loaded index: standing facts + "- [topic] …" pointers.
//   - <topic>.md  topic files, loaded on demand via recall_memory.
// A plain save_memory (no topic) appends a bullet to the index; a topic save files
// the entry into <topic>.md and keeps only a one-line pointer in the index, so the
// always-loaded prefix stays small (token efficiency). The definition, executors,
// and append helpers live here; engine.go only wires the definitions into
// sessionDefinitions and the executors into the executeOne synthetic-tool switch.

const (
	projectMemoryFile    = "MEMORY.md" // the index filename, inside the store dir
	projectMemoryHeading = "# Project Memory"
	maxMemoryEntryChars  = 500       // one entry's content bound (MT-6)
	maxTopicFileBytes    = 32 * 1024 // per-topic-file cap (reject, never truncate)
	maxRecallBytes       = 24 * 1024 // recall_memory returns at most this, then a note
)

const (
	memoryStatusSaved = "saved"
	memoryStatusKnown = "already known"
)

// slugPattern is the single spelling of the topic-slug grammar: lowercase
// letters, digits, and dashes, not starting with a dash, up to 64 chars. Both
// the input validator and the pointer-line parser derive from it so the
// grammar can never fork.
const slugPattern = `[a-z0-9][a-z0-9-]{0,63}`

// topicSlug validates a topic name. Used everywhere a topic is accepted.
var topicSlug = regexp.MustCompile(`^` + slugPattern + `$`)

// memoryTypes are the entry kinds save_memory accepts (Memory Parity N3),
// mirroring the Claude Code memory taxonomy. A typed entry renders with a
// trailing "#<type>" tag so its kind survives in the single-line format.
var memoryTypes = map[string]bool{"user": true, "feedback": true, "project": true, "reference": true}

// memoryCommentRE matches HTML comment blocks. Kept local (mirrors the
// workspace loader's htmlCommentRE) so orchestrator stays workspace-free: a
// memory file whose content is only comments is a pristine template, and the
// first real entry brings the section heading with it.
var memoryCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

func memoryDocIsPristine(s string) bool {
	return strings.TrimSpace(memoryCommentRE.ReplaceAllString(s, "")) == ""
}

// memoryStoreDir is the directory holding the index and topic files. It falls back
// to the session workspace root when no store is configured, preserving the legacy
// behavior for callers (and tests) that do not set EngineConfig.MemoryDir.
func (e *Engine) memoryStoreDir() string {
	if e.memoryDir != "" {
		return e.memoryDir
	}
	return e.session.WorkspacePath
}

// confirmMemoryMutation is the shared MT-9 gate: outside auto-accept, a memory
// mutation needs the same interactive approval surface as the file edits it
// parallels. tool is the caller's tool name ("save_memory"/"edit_memory"); the
// human verb in the decline message derives from it. A nil error means the
// mutation may proceed.
func (e *Engine) confirmMemoryMutation(ctx context.Context, tool, prompt string) error {
	if e.permissionMode() == contract.PermissionAutoAccept {
		return nil
	}
	if e.callbacks.Confirm == nil {
		return contract.ToolNotStarted(fmt.Errorf("permission denied: %s requires interactive approval and no approver is available", tool))
	}
	approved, err := e.callbacks.Confirm(ctx, prompt)
	if err != nil {
		return err
	}
	if !approved {
		return contract.ToolNotStarted(fmt.Errorf("permission denied: the user declined the memory %s", strings.TrimSuffix(tool, "_memory")))
	}
	return nil
}

// saveMemoryDefinition is the session-stable save_memory schema (MT-1, B2). The
// optional topic routes an entry into a topic file while leaving a one-line pointer
// in the always-loaded index.
func saveMemoryDefinition() contract.ToolDefinition {
	return definition("save_memory", instructions.ToolSaveMemoryDescription, map[string]any{
		"content": map[string]any{"type": "string", "description": "The durable fact to save — one concise, specific entry of at most 500 characters."},
		"title":   map[string]any{"type": "string", "description": "Optional short label for the entry, shown in the transcript row and used as the index pointer text when a topic is set."},
		"topic":   map[string]any{"type": "string", "description": "Optional topic slug (lowercase letters, digits, dashes, e.g. build-system). When set, the entry is filed into that topic file and only a one-line pointer stays in the always-loaded index — use a topic for anything beyond a one-liner."},
		"type":    map[string]any{"type": "string", "enum": []string{"user", "feedback", "project", "reference"}, "description": "Optional kind tag rendered as a trailing #tag: user (who the user is, their preferences), feedback (guidance on how to work — include the why), project (ongoing state or constraints), reference (pointer to an external resource)."},
	}, []string{"content"})
}

// recallMemoryDefinition is the session-stable recall_memory schema (B3): read one
// topic file whose pointer appears in the always-loaded index.
func recallMemoryDefinition() contract.ToolDefinition {
	return definition("recall_memory", instructions.ToolRecallMemoryDescription, map[string]any{
		"topic": map[string]any{"type": "string", "description": "The topic slug to read, as listed in the memory index's \"- [topic] …\" pointer lines."},
	}, []string{"topic"})
}

// saveMemory executes one save_memory call. Order: redact (MT-10), validate (fail
// fast), approval gate (MT-9), then append — to the index, or to a topic file plus
// a one-line index pointer.
func (e *Engine) saveMemory(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Content string `json:"content"`
		Title   string `json:"title"`
		Topic   string `json:"topic"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid save_memory arguments: %w", err)
	}
	content := strings.TrimSpace(e.redact(input.Content))
	title := strings.TrimSpace(e.redact(input.Title))
	topic := strings.TrimSpace(strings.ToLower(input.Topic))
	memType := strings.TrimSpace(strings.ToLower(input.Type))
	if memType != "" && !memoryTypes[memType] {
		return "", fmt.Errorf("invalid type %q — use one of: user, feedback, project, reference", memType)
	}
	if memType != "" && content != "" {
		// N3: the kind tag rides the entry line itself so it survives the
		// single-line format; it counts toward the entry bound like any content.
		content += " #" + memType
	}
	if err := validateMemoryContent(content); err != nil {
		return "", err
	}
	if topic != "" && !topicSlug.MatchString(topic) {
		return "", fmt.Errorf("invalid topic %q — use lowercase letters, digits, and dashes (e.g. build-system)", topic)
	}
	dir := e.memoryStoreDir()
	entry := renderMemoryEntry(title, content)
	target := filepath.Join(dir, projectMemoryFile)
	if topic != "" {
		target = filepath.Join(dir, topic+".md")
	}
	if err := e.confirmMemoryMutation(ctx, "save_memory", "Save to project memory?\n"+target+"\n\n"+entry); err != nil {
		return "", err
	}
	if topic == "" {
		status, err := appendMemoryEntry(dir, title, content)
		if err != nil {
			return "", err
		}
		return status + "\n" + entry, nil
	}
	status, err := appendTopicEntry(dir, topic, title, content)
	if err != nil {
		return "", err
	}
	if err := ensureIndexPointer(dir, topic, pointerDescription(title, content)); err != nil {
		return "", err
	}
	return status + " → " + topic + ".md\n" + entry, nil
}

// recallMemory reads one topic file from the store (B3). It is read-only, so there
// is no approval prompt; unknown topics report the topics that do exist. The result
// is redacted (defense in depth against a hand-edited credential) and bounded.
func (e *Engine) recallMemory(_ context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid recall_memory arguments: %w", err)
	}
	topic := strings.TrimSpace(strings.ToLower(input.Topic))
	if !topicSlug.MatchString(topic) {
		return "", fmt.Errorf("invalid topic %q — use the slug from an index \"- [topic] …\" line", topic)
	}
	dir := e.memoryStoreDir()
	data, err := os.ReadFile(filepath.Join(dir, topic+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no topic %q — %s", topic, listTopics(dir))
		}
		return "", fmt.Errorf("read topic %q: %w", topic, err)
	}
	text := string(data)
	if len(text) > maxRecallBytes {
		text = text[:maxRecallBytes]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += fmt.Sprintf("\n[truncated — %d bytes total; the topic file is larger than one recall]", len(data))
	}
	return e.redact(text), nil
}

// editMemoryDefinition is the session-stable edit_memory schema (Memory Parity
// N2): the update/correct/consolidate half of durable memory. save_memory is
// append-only; this is the only way a stale entry gets fixed, a wrong one gets
// deleted, or an over-full topic gets consolidated.
func editMemoryDefinition() contract.ToolDefinition {
	return definition("edit_memory", instructions.ToolEditMemoryDescription, map[string]any{
		"old":   map[string]any{"type": "string", "description": "Text of the entry to change — enough of its exact line to match exactly one bullet in the target file."},
		"new":   map[string]any{"type": "string", "description": "Replacement content for the entry (at most 500 characters), or an empty string to delete the entry."},
		"topic": map[string]any{"type": "string", "description": "Topic slug of the file to edit. Omit to edit the always-loaded index (standing facts and \"- [topic] …\" pointer lines)."},
	}, []string{"old", "new"})
}

// editMemory executes one edit_memory call (N2): replace or delete exactly one
// bullet line in the index or a topic file. Same order as saveMemory: validate
// (fail fast), match, approval gate (MT-9 parity), then rewrite. Deleting a
// topic's last entry dissolves the file and unlists it from the index, so
// consolidation leaves no orphans.
func (e *Engine) editMemory(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Old   string `json:"old"`
		New   string `json:"new"`
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid edit_memory arguments: %w", err)
	}
	oldWant := normalizedMemoryLine(strings.TrimSpace(input.Old))
	if oldWant == "" {
		return "", errors.New("edit_memory requires old — enough of the entry's exact line to match it")
	}
	newContent := strings.TrimSpace(e.redact(input.New))
	if newContent != "" {
		if err := validateMemoryContent(newContent); err != nil {
			return "", err
		}
	}
	topic := strings.TrimSpace(strings.ToLower(input.Topic))
	if topic != "" && !topicSlug.MatchString(topic) {
		return "", fmt.Errorf("invalid topic %q — use the slug from an index \"- [topic] …\" line", topic)
	}
	dir := e.memoryStoreDir()
	name := projectMemoryFile
	if topic != "" {
		name = topic + ".md"
	}
	target := filepath.Join(dir, name)
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			if topic != "" {
				return "", fmt.Errorf("no topic %q — %s", topic, listTopics(dir))
			}
			return "", errors.New("the memory index does not exist yet — save_memory creates it")
		}
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	lines := strings.Split(string(data), "\n")
	matched := matchMemoryLine(lines, oldWant)
	if len(matched) == 0 {
		return "", fmt.Errorf("no entry matching %q in %s — pass enough of the exact line to match one bullet", input.Old, name)
	}
	if len(matched) > 1 {
		return "", fmt.Errorf("%q matches %d entries in %s — include more of the line so exactly one matches", input.Old, len(matched), name)
	}
	idx := matched[0]
	oldLine := e.redact(lines[idx])
	newLine := ""
	action := "removed"
	if newContent != "" {
		newLine = "- " + escapeReservedTags(collapseMemoryWhitespace(newContent))
		action = "updated"
		want := normalizedMemoryLine(newLine)
		for i, line := range lines {
			if i != idx && strings.HasPrefix(strings.TrimSpace(line), "- ") && normalizedMemoryLine(line) == want {
				// The replacement already exists as another entry: drop the old
				// line instead of writing a duplicate (dedupe parity with save).
				newLine, action = "", "merged — an identical entry already exists"
				break
			}
		}
	}
	preview := oldLine + "\n→ (removed)"
	if newLine != "" {
		preview = oldLine + "\n→ " + newLine
	}
	if err := e.confirmMemoryMutation(ctx, "edit_memory", "Edit project memory?\n"+target+"\n\n"+preview); err != nil {
		return "", err
	}
	if newLine != "" {
		lines[idx] = newLine
	} else {
		lines = append(lines[:idx], lines[idx+1:]...)
	}
	if topic != "" && !hasMemoryBullet(lines) {
		// Dissolution: the topic's last entry is gone — remove the file and its
		// index pointer so the index never lists an empty topic.
		if err := os.Remove(target); err != nil {
			return "", fmt.Errorf("remove %s: %w", name, err)
		}
		if err := removeIndexPointer(dir, topic); err != nil {
			return action + " — topic " + topic + " dissolved (last entry); its index pointer could not be removed: " + err.Error() + "\n" + oldLine, nil
		}
		return action + " — topic " + topic + " dissolved (last entry) and unlisted from the index\n" + oldLine, nil
	}
	rewritten := strings.Join(lines, "\n")
	if topic != "" && int64(len(rewritten)) > maxTopicFileBytes {
		return "", fmt.Errorf("topic file %s is full (%d KiB limit) — shorten the replacement or split the topic", name, maxTopicFileBytes/1024)
	}
	if err := os.WriteFile(target, []byte(rewritten), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", name, err)
	}
	if newLine != "" {
		return action + "\n" + oldLine + "\n→ " + newLine, nil
	}
	note := ""
	if topic == "" {
		if slug := pointerSlug(oldLine); slug != "" {
			if _, err := os.Stat(filepath.Join(dir, slug+".md")); err == nil {
				note = "\n(the topic file " + slug + ".md still exists — its pointer is gone; a future save under that topic re-lists it)"
			}
		}
	}
	return action + "\n" + oldLine + note, nil
}

// matchMemoryLine returns the indices of bullet lines matching want: lines whose
// normalized form equals want exactly, falling back to substring containment
// when no exact line exists (Edit-tool ergonomics — enough of the line must be
// given to be unique). Only bullet lines are candidates; headings and comments
// are never editable.
func matchMemoryLine(lines []string, want string) []int {
	var exact, contains []int
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "- ") {
			continue
		}
		norm := normalizedMemoryLine(line)
		if norm == want {
			exact = append(exact, i)
		} else if strings.Contains(norm, want) {
			contains = append(contains, i)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return contains
}

// hasMemoryBullet reports whether any line is a memory bullet.
func hasMemoryBullet(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			return true
		}
	}
	return false
}

// pointerSlugRE extracts the topic slug from an index pointer line.
var pointerSlugRE = regexp.MustCompile(`^- \[(` + slugPattern + `)\] `)

// pointerSlug reports the topic slug when the removed line was an index pointer
// (used to note that the topic file itself still exists).
func pointerSlug(removedLine string) string {
	m := pointerSlugRE.FindStringSubmatch(strings.TrimSpace(removedLine))
	if m == nil {
		return ""
	}
	return m[1]
}

// removeIndexPointer deletes the "- [topic] …" pointer line from the index, if
// present. A missing index or missing pointer is not an error (idempotent).
func removeIndexPointer(dir, topic string) error {
	target := filepath.Join(dir, projectMemoryFile)
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", projectMemoryFile, err)
	}
	token := "- [" + topic + "]"
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	removed := false
	for _, line := range lines {
		if !removed && strings.HasPrefix(strings.TrimSpace(line), token) {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return nil
	}
	if err := os.WriteFile(target, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", projectMemoryFile, err)
	}
	return nil
}

// listTopics names the topic files present (excluding the index), for a
// recall_memory miss's guidance.
func listTopics(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "no topics saved yet"
	}
	var topics []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") && name != projectMemoryFile {
			topics = append(topics, strings.TrimSuffix(name, ".md"))
		}
	}
	if len(topics) == 0 {
		return "no topics saved yet"
	}
	return "the index lists: " + strings.Join(topics, ", ")
}

// appendMemoryEntry appends one bullet to the store index (MEMORY.md).
func appendMemoryEntry(dir, title, content string) (string, error) {
	return appendBulletFile(filepath.Join(dir, projectMemoryFile), projectMemoryHeading, title, content, 0)
}

// appendTopicEntry appends one bullet to a topic file, creating it (with an "# <topic>"
// heading) if absent and rejecting once the file would exceed maxTopicFileBytes.
func appendTopicEntry(dir, topic, title, content string) (string, error) {
	return appendBulletFile(filepath.Join(dir, topic+".md"), "# "+topic, title, content, maxTopicFileBytes)
}

// appendBulletFile is the shared append-with-dedupe writer behind the index and
// topic files. Create-if-absent writes the heading; existing bytes are preserved
// verbatim (pure append with one separating newline when needed). An entry whose
// whitespace-normalized form already exists is not appended and reports "already
// known". When maxBytes > 0, a write that would exceed it is rejected (never
// truncated). The store directory is created on first write.
func appendBulletFile(target, heading, title, content string, maxBytes int64) (string, error) {
	if err := validateMemoryContent(content); err != nil {
		return "", err
	}
	entry := renderMemoryEntry(title, content)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("create memory dir: %w", err)
	}
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", filepath.Base(target), err)
	}
	want := normalizedMemoryLine(entry)
	for _, line := range strings.Split(string(existing), "\n") {
		if normalizedMemoryLine(line) == want {
			return memoryStatusKnown, nil
		}
	}
	var addition strings.Builder
	if len(existing) == 0 {
		addition.WriteString(heading + "\n\n")
	} else {
		if existing[len(existing)-1] != '\n' {
			addition.WriteString("\n")
		}
		if memoryDocIsPristine(string(existing)) {
			// N1: the file is a comment-only template — the first real entry
			// brings the section heading with it, below the guidance comment.
			addition.WriteString(heading + "\n\n")
		}
	}
	addition.WriteString(entry)
	addition.WriteString("\n")
	if maxBytes > 0 && int64(len(existing))+int64(addition.Len()) > maxBytes {
		return "", fmt.Errorf("topic file %s is full (%d KiB limit) — start a narrower topic or consolidate it", filepath.Base(target), maxBytes/1024)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filepath.Base(target), err)
	}
	if _, err := file.WriteString(addition.String()); err != nil {
		file.Close()
		return "", fmt.Errorf("append to %s: %w", filepath.Base(target), err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", filepath.Base(target), err)
	}
	return memoryStatusSaved, nil
}

// ensureIndexPointer adds a "- [topic] description" line to the index when the
// topic has no pointer yet. It is create-if-absent only: an existing pointer is
// never rewritten, so a second save to the same topic keeps the first description.
func ensureIndexPointer(dir, topic, description string) error {
	target := filepath.Join(dir, projectMemoryFile)
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", projectMemoryFile, err)
	}
	token := "- [" + topic + "]"
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), token) {
			return nil // pointer already present; never rewrite it
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	pointer := token + " " + escapeReservedTags(collapseMemoryWhitespace(description))
	var addition strings.Builder
	if len(existing) == 0 {
		addition.WriteString(projectMemoryHeading + "\n\n")
	} else {
		if existing[len(existing)-1] != '\n' {
			addition.WriteString("\n")
		}
		if memoryDocIsPristine(string(existing)) {
			// N1: first real content in a template-only index brings the heading.
			addition.WriteString(projectMemoryHeading + "\n\n")
		}
	}
	addition.WriteString(pointer + "\n")
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", projectMemoryFile, err)
	}
	defer file.Close()
	if _, err := file.WriteString(addition.String()); err != nil {
		return fmt.Errorf("append pointer to %s: %w", projectMemoryFile, err)
	}
	return nil
}

// pointerDescription is the index pointer text for a topic: the title when given,
// otherwise the first few words of the content.
func pointerDescription(title, content string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	words := strings.Fields(content)
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

// validateMemoryContent enforces the entry content bound (MT-6): non-empty and at
// most maxMemoryEntryChars, rejected with summarize guidance rather than truncated.
func validateMemoryContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("save_memory requires non-empty content — one concise durable fact")
	}
	if len(content) > maxMemoryEntryChars {
		return fmt.Errorf("memory entry content is %d characters; the limit is %d. Summarize the fact into one or two short sentences and save again — entries are never truncated", len(content), maxMemoryEntryChars)
	}
	return nil
}

// renderMemoryEntry renders the exact line appended to a memory file: one markdown
// bullet, whitespace-collapsed to a single line, with reserved tags escaped (MT-7)
// so an entry can never close or reopen the blocks it renders inside. A title
// becomes a bold label prefix.
func renderMemoryEntry(title, content string) string {
	body := escapeReservedTags(collapseMemoryWhitespace(content))
	if label := escapeReservedTags(collapseMemoryWhitespace(title)); label != "" {
		return "- **" + label + "**: " + body
	}
	return "- " + body
}

// collapseMemoryWhitespace folds any whitespace run (including newlines and CR)
// into a single space: an entry is exactly one bullet line, and the fold doubles as
// the normalization the dedupe comparison uses (MT-5).
func collapseMemoryWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// normalizedMemoryLine is a line's dedupe key: whitespace-collapsed with the
// leading markdown bullet stripped, so "-   fact" and "- fact" compare equal.
func normalizedMemoryLine(line string) string {
	return strings.TrimPrefix(collapseMemoryWhitespace(line), "- ")
}
