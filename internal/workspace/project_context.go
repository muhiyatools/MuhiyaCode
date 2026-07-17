package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ProjectInstructionsFile is the single root-local file MuhiyaCode reads for
// per-project instructions (the user-authored, CLAUDE.md-style doc). It is
// read-only from the agent's side and never watched: loading happens at runtime
// construction and user-submit boundaries only (project-context contract §2),
// never on a timer.
const ProjectInstructionsFile = "MUHIYA.md"

// ProjectMemoryFile is the single root-local file MuhiyaCode uses for durable
// project memory (feature 006). Unlike MUHIYA.md it is agent-managed: the model
// reads it from the boot context and updates it through its ordinary file tools.
// It is loaded with the exact same containment, size, and secret-screening rules.
const ProjectMemoryFile = "MEMORY.md"

// MaxProjectInstructionsBytes bounds the on-disk instructions file. A file at
// exactly this size loads; strictly larger is reported oversized and never
// truncated or injected (project-context contract §2). MEMORY.md shares the same
// bound so both boot files stay small enough to keep the cached prefix cheap.
const MaxProjectInstructionsBytes = 32 * 1024
const MaxProjectMemoryBytes = 32 * 1024

// The project-memory index (Experience Overhaul B1) lives in the store, not the
// workspace, and is loaded with a bounded HEAD so it stays cheap in the cached
// prefix even if it grows: only the first MemoryIndexHeadLines lines or
// MemoryIndexHeadBytes bytes (whichever comes first) are injected; a larger file
// still loads its head and surfaces one consolidate-into-topics notice. A file
// beyond MaxMemoryIndexBytes is treated as oversized (read is capped, not injected)
// so a pathological index can never be slurped whole.
const (
	MemoryIndexHeadLines = 200
	MemoryIndexHeadBytes = 24 * 1024
	MaxMemoryIndexBytes  = 256 * 1024
)

// InstructionState is the resolved outcome of a project-instructions load. Only
// InstructionLoaded contributes a prompt block; every other state keeps the
// session fully usable and, except missing/empty, exposes one safe diagnostic
// that never echoes file content (data-model §5.1).
type InstructionState string

const (
	InstructionMissing        InstructionState = "missing"
	InstructionEmpty          InstructionState = "empty"
	InstructionLoaded         InstructionState = "loaded"
	InstructionInvalid        InstructionState = "invalid"
	InstructionUnreadable     InstructionState = "unreadable"
	InstructionOversized      InstructionState = "oversized"
	InstructionUntrustedPath  InstructionState = "untrusted-path"
	InstructionSecretRejected InstructionState = "secret-rejected"
)

// ProjectInstructions is the canonicalized result of loading MUHIYA.md for one
// workspace. CanonicalContent/ContentHash are populated only when State is
// InstructionLoaded; Diagnostic carries a safe, content-free explanation for
// every non-usable state (project-context contract §2, data-model §5.1).
type ProjectInstructions struct {
	WorkspaceKey     string
	SourcePath       string
	State            InstructionState
	RawSize          int
	CanonicalContent string
	ContentHash      string
	Diagnostic       string
}

// instructionSecretPatterns mirrors internal/state.secretPatterns. It is kept
// local so this loader stays free of a workspace→state import (state is a
// higher layer that already depends on lower-level path logic). The canonical
// source of these patterns is internal/state/session.go; keep them in sync.
var instructionSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)\s*[:=]\s*[^\s,}]+`),
}

// WorkspaceKey is the canonical, case-normalized identity used to scope project
// instructions and durable project memory to one workspace. A moved or renamed
// workspace produces a different key; identity is never matched by basename or
// content fingerprint (project-context contract §1).
func WorkspaceKey(path string) (string, error) {
	canon, err := CanonicalPath(path)
	if err != nil {
		return "", err
	}
	return normalizePath(canon), nil
}

// LoadProjectInstructions reads the root MUHIYA.md for workspaceRoot and returns
// its canonicalized, validated form. secretValues are the configured provider/
// MCP credentials to reject verbatim in addition to the generic secret patterns
// (project-context contract §2). It never returns an error: every failure mode
// is a non-fatal ProjectInstructions state so agent work is never blocked.
func LoadProjectInstructions(workspaceRoot string, secretValues ...string) ProjectInstructions {
	return loadRootDoc(workspaceRoot, ProjectInstructionsFile, MaxProjectInstructionsBytes, secretValues)
}

// LoadMemoryIndex reads the project-memory index (MEMORY.md) from the per-project
// store directory (Experience Overhaul B1). The store dir is itself trusted
// (~/.muhiya/projects/<id>/memory, unreachable by the model's file tools), so there
// is no workspace-containment check; the same UTF-8/size/secret/hash validation
// applies, plus a bounded head so the always-loaded index stays cheap. HTML
// comments are stripped from the injected form (Memory Parity N1): the index
// template's guidance comment — and any future comment — is for humans reading the
// file, never the model, so it costs zero prefix tokens. The content hash is over
// the injected (stripped) head, so a change within the head re-fires the one-shot
// memory-update; comment-only edits and edits beyond the head do not.
func LoadMemoryIndex(memoryDir string, secretValues ...string) ProjectInstructions {
	return loadTrustedDoc(filepath.Join(memoryDir, ProjectMemoryFile), MaxMemoryIndexBytes, secretValues, MemoryIndexHeadLines, MemoryIndexHeadBytes, true)
}

// LoadUserInstructions reads an explicit, already-trusted instructions file — the
// user-level ~/.muhiya/MUHIYA.md that applies across every project (Experience
// Overhaul B1). Same validation as the project files, no bounded head, 32 KiB cap.
func LoadUserInstructions(path string, secretValues ...string) ProjectInstructions {
	return loadTrustedDoc(path, MaxProjectInstructionsBytes, secretValues, 0, 0, false)
}

// loadTrustedDoc validates and loads a document at an already-trusted absolute path
// (the memory store or ~/.muhiya) — like loadRootDoc but WITHOUT the workspace
// real-path containment check, and with an optional bounded head. It reuses the
// same UTF-8/NUL/secret/normalization/hash core so the two paths behave
// identically. stripComments removes HTML comments from the injected form (after
// secret screening, which always runs over the full content); it is a mid-pipeline
// variant, so it rides as a parameter rather than a wrapper — if a third variant
// ever appears, fold these knobs into an options struct. Every failure is a
// non-fatal state; nothing blocks the session.
func loadTrustedDoc(target string, maxBytes int64, secretValues []string, headLines, headBytes int, stripComments bool) ProjectInstructions {
	result := ProjectInstructions{State: InstructionMissing, SourcePath: target}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return result // missing: silent empty
		}
		result.State = InstructionUnreadable
		result.Diagnostic = filepath.Base(target) + " could not be read"
		return result
	}
	if !info.Mode().IsRegular() {
		result.State = InstructionUnreadable
		result.Diagnostic = filepath.Base(target) + " is not a regular file; it was not loaded"
		return result
	}
	result.RawSize = int(info.Size())
	if info.Size() > maxBytes {
		result.State = InstructionOversized
		result.Diagnostic = fmt.Sprintf("%s is %d bytes; the %d KiB limit was exceeded, so it was not loaded", filepath.Base(target), info.Size(), maxBytes/1024)
		return result
	}
	data, err := os.ReadFile(target)
	if err != nil {
		result.State = InstructionUnreadable
		result.Diagnostic = filepath.Base(target) + " could not be read"
		return result
	}
	result.RawSize = len(data)
	data = stripUTF8BOM(data)
	if !utf8.Valid(data) || containsNUL(data) {
		result.State = InstructionInvalid
		result.Diagnostic = filepath.Base(target) + " is not valid UTF-8 text; it was not loaded"
		return result
	}
	canonical := normalizeNewlines(string(data))
	if !hasMeaningfulDocContent(canonical) {
		result.State = InstructionEmpty
		return result
	}
	// Secret screening runs over the FULL content (a credential anywhere — even
	// inside a comment — rejects the whole file), before any comment-stripping or
	// head-bounding.
	if containsSecret(canonical, secretValues) {
		result.State = InstructionSecretRejected
		result.Diagnostic = filepath.Base(target) + " appears to contain a credential; it was not loaded"
		return result
	}
	if stripComments {
		// The meaningful-content check above already ignores comments, so the
		// stripped form is guaranteed non-empty here.
		canonical = strings.TrimSpace(htmlCommentRE.ReplaceAllString(canonical, "")) + "\n"
	}
	injected, bounded := boundDocHead(canonical, headLines, headBytes)
	sum := sha256.Sum256([]byte(injected))
	result.State = InstructionLoaded
	result.CanonicalContent = injected
	result.ContentHash = hex.EncodeToString(sum[:])
	if bounded {
		result.Diagnostic = "the project memory index is over its auto-load bound — consolidate entries into topics"
	}
	return result
}

// boundDocHead returns the first maxLines lines or maxBytes bytes of s (whichever
// binds first), reporting whether it truncated. A zero max disables that bound.
// Byte truncation backs up to a valid UTF-8 boundary so a multi-byte rune is never
// split.
func boundDocHead(s string, maxLines, maxBytes int) (string, bool) {
	bounded := false
	if maxLines > 0 {
		if lines := strings.Split(s, "\n"); len(lines) > maxLines {
			s = strings.Join(lines[:maxLines], "\n")
			bounded = true
		}
	}
	if maxBytes > 0 && len(s) > maxBytes {
		s = s[:maxBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		bounded = true
	}
	return s, bounded
}

// loadRootDoc is the shared, contained loader behind MUHIYA.md and MEMORY.md.
// filename names the root-local file; every diagnostic references it so the two
// callers report the correct file. The logic is identical for both: real-path
// containment, regular-file + UTF-8/NUL + size checks, BOM/CRLF normalization,
// secret screening, and a deterministic content hash.
func loadRootDoc(workspaceRoot, filename string, maxBytes int64, secretValues []string) ProjectInstructions {
	result := ProjectInstructions{State: InstructionMissing}
	rootCanon, err := CanonicalPath(workspaceRoot)
	if err != nil {
		result.State = InstructionUnreadable
		result.Diagnostic = "workspace root could not be resolved; " + filename + " was not loaded"
		return result
	}
	if key, err := WorkspaceKey(workspaceRoot); err == nil {
		result.WorkspaceKey = key
	}
	target := filepath.Join(rootCanon, filename)
	result.SourcePath = target

	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return result // missing: silent empty
		}
		result.State = InstructionUnreadable
		result.Diagnostic = filename + " could not be read"
		return result
	}
	if !info.Mode().IsRegular() {
		result.State = InstructionUnreadable
		result.Diagnostic = filename + " is not a regular file; it was not loaded"
		return result
	}

	// Containment: resolve symlinks/junctions and require the real target to stay
	// inside the canonical root. An outside resolution is rejected without a
	// prompt (project-context contract §1).
	realTarget, err := CanonicalPath(target)
	if err != nil {
		result.State = InstructionUnreadable
		result.Diagnostic = filename + " could not be resolved"
		return result
	}
	if !IsInside(rootCanon, realTarget) {
		result.State = InstructionUntrustedPath
		result.Diagnostic = filename + " resolves outside the workspace root; it was not loaded"
		return result
	}

	result.RawSize = int(info.Size())
	if info.Size() > maxBytes {
		result.State = InstructionOversized
		result.Diagnostic = fmt.Sprintf("%s is %d bytes; the %d KiB limit was exceeded, so it was not loaded", filename, info.Size(), maxBytes/1024)
		return result
	}

	data, err := os.ReadFile(target)
	if err != nil {
		result.State = InstructionUnreadable
		result.Diagnostic = filename + " could not be read"
		return result
	}
	result.RawSize = len(data)

	data = stripUTF8BOM(data)
	if !utf8.Valid(data) || containsNUL(data) {
		result.State = InstructionInvalid
		result.Diagnostic = filename + " is not valid UTF-8 text; it was not loaded"
		return result
	}

	canonical := normalizeNewlines(string(data))
	if !hasMeaningfulDocContent(canonical) {
		// Empty, whitespace-only, or a still-pristine template (only HTML-comment
		// guidance): treat as silent empty so it never rides the cached prefix or is
		// read as real instructions until the user/agent adds actual content.
		result.State = InstructionEmpty
		return result
	}

	if containsSecret(canonical, secretValues) {
		result.State = InstructionSecretRejected
		result.Diagnostic = filename + " appears to contain a credential; it was not loaded"
		return result
	}

	sum := sha256.Sum256([]byte(canonical))
	result.State = InstructionLoaded
	result.CanonicalContent = canonical
	result.ContentHash = hex.EncodeToString(sum[:])
	return result
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func containsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// normalizeNewlines converts CRLF and lone CR to LF, leaving all other bytes
// untouched (data-model §5.1: BOM removed, CRLF/CR normalized to LF, otherwise
// preserved).
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func containsSecret(content string, secretValues []string) bool {
	for _, value := range secretValues {
		if len(value) >= 8 && strings.Contains(content, value) {
			return true
		}
	}
	for _, pattern := range instructionSecretPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

var htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// hasMeaningfulDocContent reports whether s carries real instructions/memory once
// HTML comments are removed. A freshly-created template (only guidance comments)
// therefore reads as empty and is never injected — it costs no prefix tokens and
// cannot be misread as instructions — until the user or agent adds real content.
func hasMeaningfulDocContent(s string) bool {
	return strings.TrimSpace(htmlCommentRE.ReplaceAllString(s, "")) != ""
}

// HasMeaningfulDocContent is the exported form of the template-emptiness check,
// for callers that must distinguish a pristine comment-only template from a doc
// with real content (e.g. the legacy-memory migration, which may replace a
// pristine store index but never a real one).
func HasMeaningfulDocContent(s string) bool {
	return hasMeaningfulDocContent(s)
}

// muhiyaTemplate is the starter MUHIYA.md written into a workspace that has none.
// It is a single comment block, so while it stays pristine hasMeaningfulDocContent
// reports it empty and it is never injected.
const muhiyaTemplate = `<!--
MUHIYA.md — MuhiyaCode project instructions (like CLAUDE.md).

Edit this file by hand to tell the agent how to work in THIS project. It loads
into the agent's context at the start of every session in this workspace, so keep
it short and durable. While this file contains only this comment it is ignored and
costs nothing; delete this block and add your instructions to activate it.

Suggested sections:
  ## Project      what this is, its language/stack, and key entry points
  ## Conventions  coding standards and the build / test commands to run
  ## Do / Don't   anything the agent must always or never do in this repo
-->
`

// EnsureProjectInstructionsTemplate writes the starter MUHIYA.md at the workspace
// root when it is absent. It never overwrites an existing file and treats a write
// failure as non-fatal (returned for logging only) so a read-only workspace still
// runs. The workspace stays clean of MEMORY.md — durable memory lives in the
// per-project store, created by EnsureMemoryIndexTemplate. Called once per session
// at startup.
func EnsureProjectInstructionsTemplate(workspaceRoot string) error {
	root, err := CanonicalPath(workspaceRoot)
	if err != nil {
		return err
	}
	target := filepath.Join(root, ProjectInstructionsFile)
	if _, err := os.Stat(target); err == nil {
		return nil // already present: never overwrite
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(target, []byte(muhiyaTemplate), 0o644)
}

// memoryIndexTemplate is the starter MEMORY.md written into a project's memory
// store that has none (Memory Parity N1). It is a single comment block: while
// pristine the index reads as empty (never injected), and once real entries exist
// LoadMemoryIndex strips comments from the injected form — so this guidance is for
// humans opening the file and costs zero prefix tokens forever.
const memoryIndexTemplate = `<!--
MEMORY.md — MuhiyaCode project memory (agent-managed).

This is the always-loaded index of durable knowledge for this project. The agent
maintains it with its memory tools (save_memory / edit_memory): standing facts
live here as single bullets, and larger clusters live in sibling <topic>.md files,
each marked by a "- [topic] description" pointer line read on demand with
recall_memory. Comments are never shown to the agent.
-->
`

// EnsureMemoryIndexTemplate creates the per-project memory store directory and its
// starter MEMORY.md when absent (Memory Parity N1): every project has its memory
// file from the first session, so the memory habit does not depend on the model
// choosing to save first. It never overwrites an existing index and a failure is
// non-fatal (returned for logging only). Called once per session at startup, after
// any legacy-workspace migration.
func EnsureMemoryIndexTemplate(memoryDir string) error {
	if memoryDir == "" {
		return nil
	}
	target := filepath.Join(memoryDir, ProjectMemoryFile)
	if _, err := os.Stat(target); err == nil {
		return nil // already present: never overwrite
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(memoryIndexTemplate), 0o644)
}
