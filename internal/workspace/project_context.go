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

// Loaded reports whether the instructions produced an injectable prompt block.
func (p ProjectInstructions) Loaded() bool { return p.State == InstructionLoaded }

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

// LoadProjectMemory reads the root MEMORY.md — the agent-managed durable project
// memory — with the same containment, UTF-8/size, and secret-screening rules as
// MUHIYA.md. Like instructions it is non-fatal on every failure and never blocks
// agent work; an unreadable or secret-bearing memory file simply is not injected.
func LoadProjectMemory(workspaceRoot string, secretValues ...string) ProjectInstructions {
	return loadRootDoc(workspaceRoot, ProjectMemoryFile, MaxProjectMemoryBytes, secretValues)
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
// runs. MEMORY.md is intentionally NOT created here — it is agent-managed and the
// model creates it with its file tools on the first durable fact, so an untouched
// project stays clean. Called once per session at startup.
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
