# Contract: First-Class Memory Tool

Feature 008 (US5, FR-019..021). A dedicated save path for durable memory with
distinct presentation — replacing "edit MEMORY.md with your normal file tools"
(current prompt.go:13 instruction). Grounded in feature 006's memory plumbing:
workspace-root `MEMORY.md`, boot `## PROJECT CONTEXT` block, one-shot
`memory-update` in-session blocks, reserved-tag escaping (project_context.go).

## 1. Tool surface

| ID | Requirement |
|---|---|
| MT-1 | One new orchestrator tool `save_memory` with schema: `content` (string, required — the concise durable fact), `title` (string, optional — short label used in the transcript row). `additionalProperties:false` |
| MT-2 | The tool description teaches WHAT belongs in memory (conventions, decisions, durable user preferences — never task progress, speculation, transcript, or secrets) and that saves should be rare and concise — deliverable-focused, Reasonix-style specificity |
| MT-3 | The prompt's memory instruction is rewritten to route writes through `save_memory` (hand-editing the file stays possible for restructuring but is no longer the taught path). Ships in the same static-prefix epoch as the US1 guidance when released together |

## 2. Behavior

| ID | Requirement |
|---|---|
| MT-4 | Append semantics into the existing `MEMORY.md` format (created in that format when absent); existing content and manual edits preserved verbatim; file stays human-editable |
| MT-5 | Exact-duplicate content (after whitespace normalization) is NOT appended; the tool reports "already known" as a success |
| MT-6 | Entry size bounded (~≤500 chars of content); oversized input is rejected with guidance to summarize — never silently truncated |
| MT-7 | Reserved-tag escaping applied to saved content (feature 006 rule) so an entry can never break the boot/update block structure |
| MT-8 | A completed save feeds the EXISTING one-shot memory-update mechanism so the model's in-session view refreshes without boot-block resend or file re-read (prefix stays byte-stable) |
| MT-9 | Permission: `save_memory` is a mutating tool under the SAME approval gate as file edits (normal mode prompts; auto-accept honors it) — no security weakening (Principle IX) |
| MT-10 | Secret redaction applies to saved content via the existing screening path (root.go memory-candidate redaction precedent) |

## 3. Presentation (the "cool" part)

| ID | Requirement |
|---|---|
| MT-11 | Transcript renders `save_memory` as its own labeled row — label "Memory", collapsed line showing the saved title/first words (glyph + palette tokens from the existing tables) — never as a generic Write row |
| MT-12 | Expanded view shows the full saved entry; failed/denied saves render with the standard failure/cancel markers |
| MT-13 | The row's outcome measure is the entry state ("saved" / "already known" / denied) rather than a diff count |

## 4. Acceptance (maps to spec)

- US5 scenarios 1–5; SC-009 (distinct rendering in 100% of saves; restart
  persistence; format validity after 10 mixed tool/manual writes).
- Unit tests: schema validation, dedupe, size bound, tag escaping, absent-file
  creation, approval-gate parity, update-block emission; TUI test for the Memory
  row label/summary.
