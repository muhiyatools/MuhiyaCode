package orchestrator

import (
	"fmt"
	"strings"
)

// Deterministic project-context rendering (features 005/006). Every function here
// is a pure function of its inputs so the boot block is byte-identical across
// processes and OS locales (project-context contract §3-§4). Both project files —
// MUHIYA.md (user-authored instructions) and MEMORY.md (agent-managed memory) —
// render here so the boot block and the one-shot mid-session update blocks format
// identically. Timestamps, provenance, and any ordering that could vary are never
// emitted.

// reservedContextTags are the tag base names whose accidental appearance in file
// content must not close/reopen a real reserved block (contract §4). A leading
// zero-width space neutralizes an embedded tag without changing the visible text.
// "project-instructions" also covers "project-instructions-update".
var reservedContextTags = []string{"project-instructions", "project-memory", "memory-update"}

func escapeReservedTags(s string) string {
	for _, name := range reservedContextTags {
		s = strings.ReplaceAll(s, "<"+name, "<​"+name)
		s = strings.ReplaceAll(s, "</"+name, "</​"+name)
	}
	return s
}

// RenderProjectContextBlock produces the optional "## PROJECT CONTEXT" boot block
// from the two project files. It returns "" when neither file has content, so the
// surrounding prompt is unchanged for a project with no context. The exact bytes
// are persisted as the session's RenderedBootContext and restored verbatim on
// resume, so the cached prefix is never recomputed from mutable workspace state.
func RenderProjectContextBlock(instructionsHash, instructionsContent, memoryHash, memoryContent string) string {
	instructions := strings.TrimRight(instructionsContent, "\n")
	memory := strings.TrimRight(memoryContent, "\n")
	hasInstructions := strings.TrimSpace(instructions) != ""
	hasMemory := strings.TrimSpace(memory) != ""
	if !hasInstructions && !hasMemory {
		return ""
	}
	var b strings.Builder
	b.WriteString("## PROJECT CONTEXT\n")
	b.WriteString("Project context is lower priority than safety, the user's current explicit request, and the operating contract. Apply it when relevant.")
	if hasInstructions {
		b.WriteString(fmt.Sprintf("\n\n<project-instructions sha256=%q>\n", instructionsHash))
		b.WriteString(escapeReservedTags(instructions))
		b.WriteString("\n</project-instructions>")
	}
	if hasMemory {
		b.WriteString(fmt.Sprintf("\n\n<project-memory sha256=%q>\n", memoryHash))
		b.WriteString(escapeReservedTags(memory))
		b.WriteString("\n</project-memory>")
	}
	return b.String()
}

// RenderInstructionsUpdate renders the one-shot mid-session instructions-change
// block appended to the newest user tail (project-context contract §5).
func RenderInstructionsUpdate(newHash, oldHash, content string) string {
	return renderFileUpdate("project-instructions-update", newHash, oldHash, content)
}

// RenderMemoryUpdate renders the one-shot mid-session memory-change block. Feature
// 006: memory is a file, so a change surfaces exactly like an instructions change —
// by content, gated on the file hash — rather than as ledger events. It rides the
// user-message tail (never the cached prefix), then folds into the prefix on the
// next session.
func RenderMemoryUpdate(newHash, oldHash, content string) string {
	return renderFileUpdate("memory-update", newHash, oldHash, content)
}

func renderFileUpdate(tag, newHash, oldHash, content string) string {
	body := escapeReservedTags(strings.TrimRight(content, "\n"))
	if strings.TrimSpace(body) == "" {
		body = "(cleared)"
	}
	return fmt.Sprintf("<%s sha256=%q supersedes=%q>\n%s\n</%s>", tag, newHash, oldHash, body, tag)
}
