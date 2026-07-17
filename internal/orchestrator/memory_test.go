package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Helper coverage for T028 (part of T032): appendMemoryEntry create-if-absent,
// byte-for-byte preservation, whitespace-normalized dedupe, size bound,
// reserved-tag escaping, and title formatting (memory-tool.md MT-4..7), plus
// engine-level checks for the approval gate (MT-9) and the one-shot
// memory-update emission after a save (MT-8).

func readMemoryFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, projectMemoryFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeMemoryFile(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, projectMemoryFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAppendMemoryEntryCreatesFileWithHeading(t *testing.T) {
	root := t.TempDir()
	status, err := appendMemoryEntry(root, "", "prefer table-driven tests")
	if err != nil || status != memoryStatusSaved {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if got, want := readMemoryFile(t, root), "# Project Memory\n\n- prefer table-driven tests\n"; got != want {
		t.Fatalf("created file mismatch:\ngot  %q\nwant %q", got, want)
	}
}

func TestAppendMemoryEntryPreservesExistingContentByteForByte(t *testing.T) {
	root := t.TempDir()
	existing := "# Project Memory\n\n<!-- manual note kept verbatim -->\n-   spaced   entry\n\n## Manual Section\nfree text the tool must not touch\n"
	writeMemoryFile(t, root, existing)
	status, err := appendMemoryEntry(root, "", "new durable fact")
	if err != nil || status != memoryStatusSaved {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if got, want := readMemoryFile(t, root), existing+"- new durable fact\n"; got != want {
		t.Fatalf("append did not preserve prior bytes:\ngot  %q\nwant %q", got, want)
	}
}

func TestAppendMemoryEntryAddsSeparatorWhenTrailingNewlineMissing(t *testing.T) {
	root := t.TempDir()
	existing := "# Project Memory\n\n- first fact" // no trailing newline
	writeMemoryFile(t, root, existing)
	if _, err := appendMemoryEntry(root, "", "second fact"); err != nil {
		t.Fatal(err)
	}
	if got, want := readMemoryFile(t, root), existing+"\n- second fact\n"; got != want {
		t.Fatalf("missing-newline append mismatch:\ngot  %q\nwant %q", got, want)
	}
}

func TestAppendMemoryEntryDedupesWhitespaceNormalized(t *testing.T) {
	root := t.TempDir()
	existing := "# Project Memory\n\n-   use    the   race detector\n- **Build**:  run   make lint\n"
	writeMemoryFile(t, root, existing)
	status, err := appendMemoryEntry(root, "", "use the race detector")
	if err != nil || status != memoryStatusKnown {
		t.Fatalf("bare-content dedupe: status=%q err=%v", status, err)
	}
	status, err = appendMemoryEntry(root, "Build", "run make lint")
	if err != nil || status != memoryStatusKnown {
		t.Fatalf("titled dedupe: status=%q err=%v", status, err)
	}
	if got := readMemoryFile(t, root); got != existing {
		t.Fatalf("duplicate save changed the file:\ngot  %q\nwant %q", got, existing)
	}
}

func TestAppendMemoryEntryRejectsOversizedContent(t *testing.T) {
	root := t.TempDir()
	_, err := appendMemoryEntry(root, "", strings.Repeat("a", maxMemoryEntryChars+1))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "summarize") {
		t.Fatalf("oversized content must be rejected with summarize guidance, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, projectMemoryFile)); !os.IsNotExist(statErr) {
		t.Fatal("a rejected save must not create MEMORY.md")
	}
	if _, err := appendMemoryEntry(root, "", "   \n\t "); err == nil {
		t.Fatal("empty content must be rejected")
	}
	// The bound is a limit, not off-by-one: exactly maxMemoryEntryChars saves.
	if status, err := appendMemoryEntry(root, "", strings.Repeat("b", maxMemoryEntryChars)); err != nil || status != memoryStatusSaved {
		t.Fatalf("content at the exact bound must save: status=%q err=%v", status, err)
	}
}

func TestAppendMemoryEntryEscapesReservedTags(t *testing.T) {
	root := t.TempDir()
	if _, err := appendMemoryEntry(root, "<project-instructions>", "closing </project-memory> and opening <memory-update> must not break blocks"); err != nil {
		t.Fatal(err)
	}
	got := readMemoryFile(t, root)
	for _, raw := range []string{"</project-memory", "<memory-update", "<project-instructions"} {
		if strings.Contains(got, raw) {
			t.Fatalf("raw reserved tag %q survived in %q", raw, got)
		}
	}
	// escapeReservedTags inserts a zero-width space after the < / </.
	for _, escaped := range []string{"</​project-memory", "<​memory-update", "<​project-instructions"} {
		if !strings.Contains(got, escaped) {
			t.Fatalf("escaped form %q missing from %q", escaped, got)
		}
	}
}

func TestAppendMemoryEntryTitleFormatting(t *testing.T) {
	root := t.TempDir()
	if _, err := appendMemoryEntry(root, "Build", "run make lint before commits"); err != nil {
		t.Fatal(err)
	}
	if got := readMemoryFile(t, root); !strings.Contains(got, "- **Build**: run make lint before commits\n") {
		t.Fatalf("titled entry mismatch: %q", got)
	}
	// Untitled entries are plain bullets; multi-line content folds to one line.
	if _, err := appendMemoryEntry(root, "", "line one\n\t  line two"); err != nil {
		t.Fatal(err)
	}
	if got := readMemoryFile(t, root); !strings.Contains(got, "- line one line two\n") {
		t.Fatalf("untitled/multi-line entry mismatch: %q", got)
	}
}

// TestSaveMemoryToolApprovalGateParity (MT-9): in normal permission mode the
// save prompts through callbacks.Confirm — the same approver surface the
// workspace Guard routes edit_file approvals to — and a decline blocks the
// write and surfaces as a standard permission-denied failure (MT-12).
func TestSaveMemoryToolApprovalGateParity(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("m1", "save_memory", `{"content":"use the race detector in CI"}`)}},
		{Content: "done"},
	}}
	settings := engineSettings()
	settings.PermissionMode = contract.PermissionNormal
	root := t.TempDir()
	var prompts []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "mem-deny", WorkspacePath: root}, Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Callbacks: contract.Callbacks{Confirm: func(_ context.Context, message string) (bool, error) {
			prompts = append(prompts, message)
			return false, nil
		}},
		Prompt: PromptContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "save the project convention to memory"); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], projectMemoryFile) {
		t.Fatalf("expected one approval prompt naming MEMORY.md, got %v", prompts)
	}
	if _, statErr := os.Stat(filepath.Join(root, projectMemoryFile)); !os.IsNotExist(statErr) {
		t.Fatal("a declined save must not write MEMORY.md")
	}
	denied := false
	for _, message := range engine.history.All() {
		if message.Role == contract.RoleTool && strings.Contains(message.Content, "permission denied") {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("declined save did not surface permission denied: %+v", engine.history.All())
	}
}

// TestSaveMemoryToolAutoAcceptSaves (MT-9): auto-accept honors the save with no
// prompt, and the tool result's first line is the entry state (MT-13).
func TestSaveMemoryToolAutoAcceptSaves(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("m1", "save_memory", `{"title":"Testing","content":"use the race detector in CI"}`)}},
		{Content: "done"},
	}}
	settings := engineSettings() // PermissionAutoAccept
	root := t.TempDir()
	confirms := 0
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "mem-save", WorkspacePath: root}, Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Callbacks: contract.Callbacks{Confirm: func(context.Context, string) (bool, error) {
			confirms++
			return false, nil
		}},
		Prompt: PromptContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "save the project convention to memory"); err != nil {
		t.Fatal(err)
	}
	if confirms != 0 {
		t.Fatalf("auto-accept must not prompt, got %d prompt(s)", confirms)
	}
	if got := readMemoryFile(t, root); !strings.Contains(got, "- **Testing**: use the race detector in CI\n") {
		t.Fatalf("save did not land in MEMORY.md: %q", got)
	}
	var result string
	for _, message := range engine.history.All() {
		if message.Role == contract.RoleTool {
			result = message.Content
		}
	}
	if !strings.HasPrefix(result, memoryStatusSaved+"\n- **Testing**: ") {
		t.Fatalf("tool result must lead with the entry state then the entry, got %q", result)
	}
}

// TestSaveMemoryFeedsMemoryUpdateBlock (MT-8): a completed save changes the
// on-disk hash the existing submit-boundary probe compares, so the NEXT task's
// user tail carries exactly one <memory-update> block — without touching the
// boot block or the cached prefix.
func TestSaveMemoryFeedsMemoryUpdateBlock(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("m1", "save_memory", `{"content":"use the race detector in CI"}`)}},
		{Content: "saved it"},
		{Content: "hello"},
	}}
	settings := engineSettings()
	root := t.TempDir()
	probe := func(context.Context) (contract.ProjectContextProbe, error) {
		data, err := os.ReadFile(filepath.Join(root, projectMemoryFile))
		if err != nil {
			return contract.ProjectContextProbe{}, nil // missing file: empty probe
		}
		sum := sha256.Sum256(data)
		return contract.ProjectContextProbe{MemoryHash: hex.EncodeToString(sum[:]), MemoryContent: string(data)}, nil
	}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "mem-update", WorkspacePath: root}, Provider: provider,
		Registry:            NewRegistry(&recordingTool{name: "read_file"}),
		Prompt:              PromptContext{},
		ProjectContextProbe: probe,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "save the project convention to memory"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(provider.requests))
	}
	secondTaskTail := provider.requests[2].Messages[len(provider.requests[2].Messages)-1]
	if !strings.Contains(secondTaskTail.Content, "<memory-update") || !strings.Contains(secondTaskTail.Content, "use the race detector in CI") {
		t.Fatalf("next task's tail did not carry the memory-update block: %q", secondTaskTail.Content)
	}
	// One-shot: the block must not ride the first task's messages.
	for _, request := range provider.requests[:2] {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "<memory-update") {
				t.Fatalf("memory-update leaked into the saving task's own request: %q", message.Content)
			}
		}
	}
}

func readStoreFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func topicEngine(t *testing.T, storeDir string) *Engine {
	t.Helper()
	settings := engineSettings() // auto-accept, so saves need no approval
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "topic-" + t.Name(), WorkspacePath: t.TempDir()},
		MemoryDir: storeDir, Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// TestSaveMemoryTopicRouting (B2 T070/T071): a topic save writes the entry into
// <topic>.md and leaves exactly one "- [topic] …" pointer in the index; a second
// save to the same topic never adds a second pointer.
func TestSaveMemoryTopicRouting(t *testing.T) {
	dir := t.TempDir()
	status, err := appendTopicEntry(dir, "build-system", "", "run make lint before commits")
	if err != nil || status != memoryStatusSaved {
		t.Fatalf("topic append: status=%q err=%v", status, err)
	}
	if got := readStoreFile(t, dir, "build-system.md"); !strings.Contains(got, "# build-system") || !strings.Contains(got, "- run make lint before commits") {
		t.Fatalf("topic file content wrong:\n%s", got)
	}
	if err := ensureIndexPointer(dir, "build-system", "how the build works"); err != nil {
		t.Fatal(err)
	}
	if idx := readStoreFile(t, dir, projectMemoryFile); !strings.Contains(idx, "- [build-system] how the build works") {
		t.Fatalf("index missing the topic pointer:\n%s", idx)
	}
	if err := ensureIndexPointer(dir, "build-system", "a different description"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readStoreFile(t, dir, projectMemoryFile), "- [build-system]"); n != 1 {
		t.Fatalf("expected exactly one pointer for the topic, got %d", n)
	}
}

// TestSaveMemoryToolFilesTopic (B2): the save_memory tool with a topic files the
// entry into the topic file (in the store, not the workspace), updates the index
// pointer with the title, and names the topic file in its result.
func TestSaveMemoryToolFilesTopic(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	out, err := engine.saveMemory(context.Background(), []byte(`{"title":"Build system","content":"cmake then make; tests need dockerd","topic":"build-system"}`))
	if err != nil {
		t.Fatalf("topic save failed: %v", err)
	}
	if !strings.Contains(out, "→ build-system.md") {
		t.Fatalf("result should name the topic file: %q", out)
	}
	if got := readStoreFile(t, dir, "build-system.md"); !strings.Contains(got, "cmake then make") {
		t.Fatalf("entry not in topic file:\n%s", got)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); !strings.Contains(got, "- [build-system] Build system") {
		t.Fatalf("index pointer should use the title:\n%s", got)
	}
}

// TestSaveMemoryRejectsInvalidTopic (B2): a non-slug topic is rejected with guidance.
func TestSaveMemoryRejectsInvalidTopic(t *testing.T) {
	engine := topicEngine(t, t.TempDir())
	if _, err := engine.saveMemory(context.Background(), []byte(`{"content":"x","topic":"Bad Topic!"}`)); err == nil || !strings.Contains(err.Error(), "invalid topic") {
		t.Fatalf("invalid topic should be rejected, got %v", err)
	}
}

// TestAppendTopicEntryRejectsWhenFull (B2): a topic file at its cap rejects further
// entries with guidance rather than truncating.
func TestAppendTopicEntryRejectsWhenFull(t *testing.T) {
	dir := t.TempDir()
	big := "# t\n\n" + strings.Repeat("- filler entry padding padding\n", 2000) // > 32 KiB
	if err := os.WriteFile(filepath.Join(dir, "t.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := appendTopicEntry(dir, "t", "", "one more"); err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("a full topic file must reject, got %v", err)
	}
}

// TestRecallMemoryRoundTrip (B3 T081): save under a topic, then recall it back.
func TestRecallMemoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if _, err := engine.saveMemory(context.Background(), []byte(`{"content":"cmake then make","topic":"build"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := engine.recallMemory(context.Background(), []byte(`{"topic":"build"}`))
	if err != nil {
		t.Fatalf("recall failed: %v", err)
	}
	if !strings.Contains(out, "cmake then make") {
		t.Fatalf("recall did not return the saved content: %q", out)
	}
}

// TestRecallMemoryUnknownTopic (B3): an empty store and an unknown topic both guide
// the model to the topics that exist.
func TestRecallMemoryUnknownTopic(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if _, err := engine.recallMemory(context.Background(), []byte(`{"topic":"nope"}`)); err == nil || !strings.Contains(err.Error(), "no topics saved yet") {
		t.Fatalf("empty store should say no topics, got %v", err)
	}
	if _, err := engine.saveMemory(context.Background(), []byte(`{"content":"x","topic":"build"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.recallMemory(context.Background(), []byte(`{"topic":"missing"}`)); err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatalf("unknown topic should list existing topics, got %v", err)
	}
}

// TestRecallMemoryInvalidSlug (B3): a non-slug topic is rejected before any read.
func TestRecallMemoryInvalidSlug(t *testing.T) {
	engine := topicEngine(t, t.TempDir())
	if _, err := engine.recallMemory(context.Background(), []byte(`{"topic":"../etc"}`)); err == nil || !strings.Contains(err.Error(), "invalid topic") {
		t.Fatalf("path-ish topic must be rejected, got %v", err)
	}
}

// TestRecallMemoryNeedsNoApproval (B3): recall is read-only, so it works in normal
// permission mode with no approver configured.
func TestRecallMemoryNeedsNoApproval(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.md"), []byte("# build\n\n- cmake then make\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	settings.PermissionMode = contract.PermissionNormal
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "recall-normal", WorkspacePath: t.TempDir()},
		MemoryDir: dir, Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.recallMemory(context.Background(), []byte(`{"topic":"build"}`))
	if err != nil || !strings.Contains(out, "cmake then make") {
		t.Fatalf("recall in normal mode should not need approval: out=%q err=%v", out, err)
	}
}

// --- Memory Parity (N1/N2/N3) ------------------------------------------------

// TestSaveMemoryTypeTag (N3): a typed save renders a trailing #tag on the entry
// line, and an unknown type is rejected with the valid set.
func TestSaveMemoryTypeTag(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if _, err := engine.saveMemory(context.Background(), []byte(`{"title":"Style","content":"tabs over spaces","type":"feedback"}`)); err != nil {
		t.Fatal(err)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); !strings.Contains(got, "- **Style**: tabs over spaces #feedback\n") {
		t.Fatalf("typed entry mismatch:\n%s", got)
	}
	if _, err := engine.saveMemory(context.Background(), []byte(`{"content":"x","type":"random"}`)); err == nil || !strings.Contains(err.Error(), "user, feedback, project, reference") {
		t.Fatalf("unknown type must be rejected with the valid set, got %v", err)
	}
}

// TestAppendAfterTemplateAddsHeading (N1): the first real entry (and the first
// pointer) written into a pristine comment-only template brings the section
// heading with it, below the guidance comment.
func TestAppendAfterTemplateAddsHeading(t *testing.T) {
	dir := t.TempDir()
	template := "<!--\nMEMORY.md — guidance for humans.\n-->\n"
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := appendMemoryEntry(dir, "", "first durable fact"); err != nil {
		t.Fatal(err)
	}
	got := readStoreFile(t, dir, projectMemoryFile)
	if want := template + projectMemoryHeading + "\n\n- first durable fact\n"; got != want {
		t.Fatalf("template append mismatch:\ngot  %q\nwant %q", got, want)
	}
	// A pointer into a fresh template behaves the same.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, projectMemoryFile), []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureIndexPointer(dir2, "build", "how the build works"); err != nil {
		t.Fatal(err)
	}
	if got := readStoreFile(t, dir2, projectMemoryFile); !strings.Contains(got, projectMemoryHeading+"\n\n- [build] how the build works\n") {
		t.Fatalf("pointer into template missing heading:\n%s", got)
	}
}

// TestEditMemoryReplaceAndDelete (N2): replace one entry in the index by a
// partial match, then delete another; untouched lines survive byte-for-byte.
func TestEditMemoryReplaceAndDelete(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	seed := "# Project Memory\n\n- **Build**: run make lint before commits\n- use the race detector in CI\n- prefer table-driven tests\n"
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := engine.editMemory(context.Background(), []byte(`{"old":"run make lint","new":"**Build**: run make lint && make vet before commits"}`))
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	if !strings.HasPrefix(out, "updated\n") || !strings.Contains(out, "→ - **Build**: run make lint && make vet before commits") {
		t.Fatalf("replace output shape wrong: %q", out)
	}
	got := readStoreFile(t, dir, projectMemoryFile)
	if !strings.Contains(got, "- **Build**: run make lint && make vet before commits\n") || strings.Contains(got, "lint before commits") {
		t.Fatalf("replacement not applied:\n%s", got)
	}
	if !strings.Contains(got, "- use the race detector in CI\n") || !strings.Contains(got, "- prefer table-driven tests\n") {
		t.Fatalf("edit disturbed untouched lines:\n%s", got)
	}
	out, err = engine.editMemory(context.Background(), []byte(`{"old":"prefer table-driven tests","new":""}`))
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !strings.HasPrefix(out, "removed\n") {
		t.Fatalf("delete output shape wrong: %q", out)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); strings.Contains(got, "table-driven") {
		t.Fatalf("deleted entry survived:\n%s", got)
	}
}

// TestEditMemoryDissolvesEmptyTopic (N2): deleting a topic's last entry removes
// the topic file AND its index pointer, so the index never lists an empty topic.
func TestEditMemoryDissolvesEmptyTopic(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if _, err := engine.saveMemory(context.Background(), []byte(`{"title":"Build","content":"cmake then make","topic":"build"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := engine.editMemory(context.Background(), []byte(`{"old":"cmake then make","new":"","topic":"build"}`))
	if err != nil {
		t.Fatalf("dissolving delete failed: %v", err)
	}
	if !strings.Contains(out, "dissolved") {
		t.Fatalf("dissolution not reported: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build.md")); !os.IsNotExist(statErr) {
		t.Fatal("empty topic file must be removed")
	}
	if idx := readStoreFile(t, dir, projectMemoryFile); strings.Contains(idx, "- [build]") {
		t.Fatalf("dissolved topic still listed in the index:\n%s", idx)
	}
}

// TestEditMemoryApprovalGateParity (N2/MT-9): normal permission mode prompts
// through Confirm with a before→after preview; a decline blocks the rewrite.
func TestEditMemoryApprovalGateParity(t *testing.T) {
	dir := t.TempDir()
	seed := "# Project Memory\n\n- use the race detector in CI\n"
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	settings.PermissionMode = contract.PermissionNormal
	var prompts []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "edit-deny", WorkspacePath: t.TempDir()},
		MemoryDir: dir, Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Confirm: func(_ context.Context, message string) (bool, error) {
			prompts = append(prompts, message)
			return false, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"race detector","new":"race detector runs only in CI"}`)); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("declined edit must be permission denied, got %v", err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Edit project memory?") || !strings.Contains(prompts[0], "→ ") {
		t.Fatalf("expected one before→after approval prompt, got %v", prompts)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); got != seed {
		t.Fatalf("declined edit changed the file:\ngot  %q\nwant %q", got, seed)
	}
}

// TestEditMemoryAmbiguousAndNotFound (N2): a matcher that hits several entries
// or none is rejected with actionable guidance, and nothing is written.
func TestEditMemoryAmbiguousAndNotFound(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	seed := "# Project Memory\n\n- run make lint in CI\n- run make lint locally too\n"
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"make lint","new":"x"}`)); err == nil || !strings.Contains(err.Error(), "matches 2 entries") {
		t.Fatalf("ambiguous match must be rejected with the count, got %v", err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"no such entry","new":"x"}`)); err == nil || !strings.Contains(err.Error(), "no entry matching") {
		t.Fatalf("missing match must be rejected, got %v", err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"","new":"x"}`)); err == nil || !strings.Contains(err.Error(), "requires old") {
		t.Fatalf("empty old must be rejected, got %v", err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"x","new":"y","topic":"Bad Topic!"}`)); err == nil || !strings.Contains(err.Error(), "invalid topic") {
		t.Fatalf("invalid topic slug must be rejected, got %v", err)
	}
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"anything","new":"y","topic":"missing"}`)); err == nil || !strings.Contains(err.Error(), "no topic") {
		t.Fatalf("missing topic file must be rejected with guidance, got %v", err)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); got != seed {
		t.Fatalf("rejected edits changed the file:\n%s", got)
	}
	// An exact full-line match wins over substring ambiguity.
	out, err := engine.editMemory(context.Background(), []byte(`{"old":"run make lint in CI","new":"run make lint in CI (required)"}`))
	if err != nil || !strings.HasPrefix(out, "updated\n") {
		t.Fatalf("exact match should disambiguate: out=%q err=%v", out, err)
	}
}

// TestEditMemoryMergesDuplicate (N2): replacing an entry with content that
// already exists as another entry drops the old line instead of duplicating.
func TestEditMemoryMergesDuplicate(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	seed := "# Project Memory\n\n- old phrasing of the fact\n- canonical phrasing of the fact\n"
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := engine.editMemory(context.Background(), []byte(`{"old":"old phrasing","new":"canonical phrasing of the fact"}`))
	if err != nil {
		t.Fatalf("merge edit failed: %v", err)
	}
	if !strings.HasPrefix(out, "merged") {
		t.Fatalf("merge not reported: %q", out)
	}
	got := readStoreFile(t, dir, projectMemoryFile)
	if strings.Contains(got, "old phrasing") || strings.Count(got, "canonical phrasing of the fact") != 1 {
		t.Fatalf("merge left duplicates or the old line:\n%s", got)
	}
}

// TestEditMemoryPointerRemovalNotesOrphanTopic (N2): deleting an index pointer
// whose topic file still exists says so, instead of silently orphaning it.
func TestEditMemoryPointerRemovalNotesOrphanTopic(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if _, err := engine.saveMemory(context.Background(), []byte(`{"content":"cmake then make","topic":"build"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := engine.editMemory(context.Background(), []byte(`{"old":"[build]","new":""}`))
	if err != nil {
		t.Fatalf("pointer delete failed: %v", err)
	}
	if !strings.Contains(out, "build.md still exists") {
		t.Fatalf("orphaned topic file not noted: %q", out)
	}
	if idx := readStoreFile(t, dir, projectMemoryFile); strings.Contains(idx, "- [build]") {
		t.Fatalf("pointer survived deletion:\n%s", idx)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build.md")); statErr != nil {
		t.Fatal("deleting a pointer must not delete the topic file")
	}
}

// TestEditMemoryRejectsOversizedReplacement (N2): the replacement obeys the same
// entry bound as save_memory, rejected with summarize guidance.
func TestEditMemoryRejectsOversizedReplacement(t *testing.T) {
	dir := t.TempDir()
	engine := topicEngine(t, dir)
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte("# Project Memory\n\n- small fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("a", maxMemoryEntryChars+1)
	if _, err := engine.editMemory(context.Background(), []byte(`{"old":"small fact","new":"`+huge+`"}`)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "summarize") {
		t.Fatalf("oversized replacement must be rejected with summarize guidance, got %v", err)
	}
}

// TestEditMemoryDispatchEndToEnd (N2): the tool routes through the real provider
// loop and executeOne's synthetic-tool switch — not just the executor — and the
// tool result's first line is the action state (TUI outcome contract).
func TestEditMemoryDispatchEndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, projectMemoryFile), []byte("# Project Memory\n\n- the build uses cmake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("e1", "edit_memory", `{"old":"the build uses cmake","new":"the build uses cmake 3.28+"}`)}},
		{Content: "memory corrected"},
	}}
	settings := engineSettings() // auto-accept
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "edit-e2e", WorkspacePath: t.TempDir()},
		MemoryDir: dir, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// No repo keywords in the prompt (a "build the X" phrasing would classify as
	// plan-required, whose read-only phase blocks memory mutations by design).
	if _, _, err := engine.Run(context.Background(), "update one stale note in project memory"); err != nil {
		t.Fatal(err)
	}
	if got := readStoreFile(t, dir, projectMemoryFile); !strings.Contains(got, "- the build uses cmake 3.28+\n") || strings.Contains(got, "cmake\n") {
		t.Fatalf("dispatched edit did not land:\n%s", got)
	}
	var toolResult string
	for _, message := range engine.history.All() {
		if message.Role == contract.RoleTool {
			toolResult = message.Content
		}
	}
	if !strings.HasPrefix(toolResult, "updated\n") {
		t.Fatalf("tool result must lead with the action state, got %q", toolResult)
	}
}
