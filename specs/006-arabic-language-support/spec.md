# Feature Specification: Arabic Language Support (RTL & Bidirectional)

**Feature Branch**: `006-arabic-language-support`

**Created**: 2026-07-13

**Status**: Draft

**Input**: User description: "Do a full Ultimate implementation of arabic language support in the maximum way in the MuhiyaCode TUI and make sure its added in a perfect way and even when arabic message in chat TUI in response or sent prompt to be optimized too in the best way and to work with english perfectly each other"

## Overview

MuhiyaCode is a terminal coding agent. Today it has partial right-to-left (RTL) support: a display-time shaper joins Arabic letters and reorders bidirectional text for the transcript, and a setting selects an RTL rendering mode and alignment. However, Arabic is not treated as a first-class language end to end. The most visible gap is the **composer**: as the user types Arabic, the input box shows disconnected, wrongly ordered, left-anchored characters (see the reported screenshot). Beyond input, RTL handling is uneven across surfaces, mixed Arabic-English lines are not consistently correct, and there is no explicit guarantee that what the model *receives* and what the user *copies* is clean logical text.

This feature makes Arabic a fully supported language across the entire terminal experience — reading, writing, mixed content with English/code/numbers, and clean interchange with the model — without changing agent behavior, tools, model routing, security, or the deterministic prompt-cache guarantees.

## Clarifications

### Session 2026-07-13

- Q: Is translating MuhiyaCode's own interface strings (hints, labels, command descriptions) into Arabic in scope? → A: Out of scope for this feature except as an optional stretch (User Story 6). The primary scope is rendering, inputting, and exchanging Arabic **content** correctly; the interface chrome may remain English while Arabic content is fully supported.
- Q: Which RTL scripts are committed targets? → A: Arabic (incl. Arabic-script languages such as Persian/Urdu letters already present in the shaping table) is the validated, committed target. Other RTL scripts (e.g., Hebrew) benefit from the same bidirectional pipeline as a side effect but are not separately validated in this feature.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Read Arabic content correctly everywhere (Priority: P1)

An Arabic-speaking developer runs a task and the agent replies with Arabic (or Arabic mixed with code). Every place text appears — the streamed assistant reply, the user's own echoed messages, tool result details, the thinking line, plan steps, notices, modal dialogs, the command palette, and headers — shows Arabic with correct **letter joining** (contextual initial/medial/final/isolated forms), correct **right-to-left visual order**, and correct **right alignment**, so the content is actually readable rather than appearing as disconnected, reversed glyphs.

**Why this priority**: Reading the agent's output is the core of "language support." If Arabic responses are unreadable, nothing else matters. It is also independently valuable and testable on its own.

**Independent Test**: Feed representative Arabic passages (plain sentences, sentences with diacritics, Arabic with embedded code spans) into each display surface and confirm every one renders joined, right-to-left, and right-aligned, matching a reference rendering.

**Acceptance Scenarios**:

1. **Given** the agent streams an Arabic reply, **When** it appears in the transcript, **Then** letters are joined into words, the visual order reads right-to-left, and the block is right-aligned — including while the text is still streaming token by token.
2. **Given** an Arabic reply longer than the terminal width, **When** it wraps, **Then** each wrapped line is individually shaped, ordered, and aligned, and no word is split in a way that breaks its joining.
3. **Given** Arabic text with diacritics (harakat), **When** it renders, **Then** the diacritics attach to their base letters without consuming extra columns or shifting alignment.
4. **Given** Arabic appears inside a tool row, notice, modal, plan step, or the thinking line, **When** that surface renders, **Then** the Arabic is shaped and ordered identically to the main transcript (no surface is left unhandled).

---

### User Story 2 - Type Arabic in the composer correctly (Priority: P1)

The user types an Arabic prompt in the input box. As they type, the characters **join into words**, read **right-to-left**, sit **right-aligned** in the box, and the **cursor** lands at the correct insertion point. Editing operations (insert, delete/backspace, move by character/word, home/end) behave intuitively for RTL text. This is the specific defect shown in the screenshot, where Arabic input currently appears disconnected, mis-ordered, and left-anchored.

**Why this priority**: A conversation is impossible if the user cannot see what they are typing. This is the most visible reported defect and blocks any Arabic workflow. It is co-critical with reading (both P1).

**Independent Test**: Type a multi-word Arabic sentence and confirm live joining, right-to-left order, right alignment, and a correctly placed cursor; then edit in the middle and confirm the edit lands where expected and the text re-shapes correctly.

**Acceptance Scenarios**:

1. **Given** an empty composer, **When** the user types an Arabic word, **Then** the letters visibly join and the word reads right-to-left, anchored to the right edge of the input.
2. **Given** Arabic text in the composer, **When** the user presses Backspace or moves the cursor, **Then** the cursor moves and deletes the grapheme the user perceives as adjacent, and the text re-shapes correctly after the edit.
3. **Given** a large Arabic passage is pasted, **When** it is captured, **Then** it is handled by the existing large-paste flow (compact placeholder) without corrupting the underlying Arabic, and the stored/sent content remains the original logical text.
4. **Given** the composer contains Arabic, **When** the user submits, **Then** what is sent is the original logical Arabic (see User Story 4), not the on-screen visual presentation.

---

### User Story 3 - Mixed Arabic and English (and code, numbers, paths) reads correctly (Priority: P2)

Real messages mix scripts: an Arabic sentence that names an English identifier, a file path, a URL, a number, or an inline `code` span. Each segment must display in its natural direction — Arabic segments right-to-left, Latin/code/number/path segments left-to-right — arranged so the whole line reads correctly, both in the transcript and in the composer.

**Why this priority**: The user explicitly requires Arabic and English to "work perfectly with each other." Coding conversations are inherently mixed, so this is essential, but it builds on the P1 reading/writing foundation.

**Independent Test**: Render and type a corpus of mixed lines (Arabic + English word, Arabic + `code`, Arabic + `path/to/file.go`, Arabic + URL, Arabic + digits) and confirm each segment's direction and the overall reading order match a reference for both directions.

**Acceptance Scenarios**:

1. **Given** a line "«افتح الملف main.go الآن»" (Arabic around an English filename), **When** it renders, **Then** the Arabic reads right-to-left and `main.go` stays intact and left-to-right in its correct position within the line.
2. **Given** Arabic text containing a number or an inline code span, **When** it renders, **Then** the number/code keeps left-to-right order and does not visually merge into the surrounding Arabic.
3. **Given** a file path, URL, or command with no Arabic, **When** it renders, **Then** it is unaffected and reads exactly as before (pure-LTR content is never reordered).
4. **Given** a predominantly English line with one Arabic word, **When** it renders, **Then** the base direction favors readability (the line is not force-flipped) and the Arabic word is still shaped correctly.

---

### User Story 4 - Clean, optimized exchange with the model (Priority: P2)

What the model **receives** and what the user **copies** is always the clean, logical Unicode the user actually meant — never the visual presentation used for the terminal (no reversed order, no isolated presentation-form glyphs, normalized consistently). When the model **responds** in Arabic, the response is displayed via the P1 pipeline but stored and re-sent in its original logical form, so history, context, memory, and copy/paste all round-trip perfectly.

**Why this priority**: "Optimize the sent prompt/response in the best way" means the model must never see corrupted display text, and the user must be able to copy Arabic that pastes correctly elsewhere. Getting this wrong silently degrades every Arabic turn.

**Independent Test**: Submit an Arabic prompt and inspect the exact bytes delivered to the model (via the request record) to confirm they are logical, un-reversed, normalized Unicode with no presentation forms; select and copy an Arabic passage and paste it into an external editor to confirm a byte-for-byte logical round-trip.

**Acceptance Scenarios**:

1. **Given** an Arabic prompt typed in the composer, **When** it is submitted, **Then** the transmitted content is normalized logical Arabic (standard code points, correct order), and re-opening the session shows the same logical text.
2. **Given** an Arabic assistant reply, **When** it is persisted to history and reused as context on later turns, **Then** the stored form is the original logical text, so subsequent turns and cache behavior are unaffected.
3. **Given** the user drags to select Arabic (or mixed) transcript text, **When** they copy it, **Then** the clipboard holds the logical text in reading order, and pasting into another application reproduces the original.
4. **Given** Arabic content in project context files (`MUHIYA.md`, `MEMORY.md`), **When** it loads into the model's context, **Then** it is delivered as logical Unicode exactly like any other content, with no visual shaping applied to what the model sees.

---

### User Story 5 - Predictable configuration and graceful degradation (Priority: P3)

The user can control RTL behavior (automatic detection, forced visual shaping, reliance on a BiDi-capable terminal, or off) and alignment, and whatever the terminal/font combination, Arabic never crashes, hangs, corrupts the layout, or double-reverses. On terminals that cannot render Arabic well, output degrades to a clearly legible fallback rather than garbage.

**Why this priority**: Terminals and fonts vary enormously; robustness and a sane default make the feature usable everywhere. It refines rather than blocks the core stories.

**Independent Test**: Exercise each RTL mode and alignment setting across representative terminal profiles (a BiDi-capable terminal and a legacy console) and confirm correct, non-corrupt output and no double-reordering; run an Arabic + diacritics + emoji + very-long-line fuzz corpus and confirm no crash or layout break.

**Acceptance Scenarios**:

1. **Given** the automatic RTL mode, **When** a message contains Arabic, **Then** RTL handling engages; **When** it contains none, **Then** rendering is byte-identical to the non-RTL path (zero effect on pure-LTR content).
2. **Given** a terminal that performs its own bidirectional reordering, **When** the corresponding mode is selected, **Then** MuhiyaCode does not also reorder, so text is never double-reversed.
3. **Given** any malformed, extremely long, emoji-laden, or diacritic-heavy Arabic input, **When** it is rendered or edited, **Then** the application stays responsive and never crashes or corrupts unrelated UI.
4. **Given** the RTL setting is changed, **When** the change is applied, **Then** it takes effect for subsequent rendering without requiring a restart and without disturbing an in-flight task.

---

### User Story 6 - Optional Arabic interface locale (Priority: P4, stretch)

As a stretch goal, a user may switch MuhiyaCode's own interface strings (key hints, command descriptions, status labels, first-run guidance) to Arabic, so the whole experience — not just the content — can be Arabic.

**Why this priority**: Nice-to-have that maximizes "full Arabic support," but the primary value is delivered by correct content rendering and input (P1–P4). It is explicitly optional and can ship later.

**Independent Test**: Toggle the interface locale to Arabic and confirm chrome strings display translated, shaped, right-to-left, and right-aligned, with English remaining available.

**Acceptance Scenarios**:

1. **Given** the interface locale is Arabic, **When** the UI renders, **Then** hints, labels, and command descriptions appear in shaped, right-to-left Arabic while functionality is unchanged.
2. **Given** the interface locale is English (default), **When** the UI renders, **Then** chrome is unchanged from today.

---

### Edge Cases

- Arabic containing combining diacritics (harakat, shadda, tanwin) — must attach with zero display width and not shift alignment or column math.
- Arabic-Indic digits (٠١٢٣) versus Western digits (0123) within Arabic text — both display in their correct positions; neither is silently converted in the content sent to the model.
- A single Arabic word inside an otherwise English line, and a single English/number/`code` token inside an otherwise Arabic line (weak/neutral character direction resolution).
- Neutral punctuation and brackets (`.`, `,`, `()`, `«»`, `:`) adjacent to a script boundary — must resolve to the correct side.
- Very long Arabic or mixed lines that must wrap — wrapping must preserve joining and per-line alignment and must not split a grapheme cluster.
- Arabic in a file path or command shown as a tool target, and Arabic file names in diffs.
- Arabic in a modal title/choice/description, transient notice, plan step, and the live thinking tail.
- Cursor placement and text selection that cross an LTR↔RTL boundary (selection must still copy logical text).
- Interaction with the large-paste flow, NO_COLOR mode, and ASCII-glyph mode.
- A terminal/font that cannot shape Arabic at all — output must degrade to a legible fallback, not silent corruption.
- Empty, whitespace-only, or single-character Arabic input.
- Screen readers / assistive tech in the terminal are acknowledged as out of scope for this feature.

## Requirements *(mandatory)*

### Functional Requirements

**Reading (display)**

- **FR-001**: The system MUST render Arabic text with correct contextual letter joining (isolated/initial/medial/final forms) on every user-visible surface: streamed and settled assistant replies, echoed user messages, tool result details and targets, the thinking line, plan/goal lines, transient notices, modal dialogs (title, message, choices), the command palette, headers, and the first-run hint.
- **FR-002**: The system MUST present bidirectional text in correct visual reading order, placing right-to-left runs right-to-left and left-to-right runs left-to-right within each display line.
- **FR-003**: The system MUST right-align predominantly RTL blocks (honoring the alignment setting) while leaving predominantly LTR blocks left-aligned.
- **FR-004**: The system MUST shape and order Arabic correctly during streaming, so partially arrived Arabic is readable and does not visibly "flip" incorrectly as more tokens arrive.
- **FR-005**: The system MUST wrap Arabic and mixed lines without breaking grapheme clusters or letter joining, applying shaping and alignment per wrapped line.
- **FR-006**: The system MUST treat Arabic combining marks (diacritics) as zero-width for column and alignment calculations.

**Writing (composer)**

- **FR-007**: The composer MUST display typed Arabic with live contextual joining, right-to-left order, and right alignment, matching the transcript's rendering quality.
- **FR-008**: The composer MUST keep the text cursor visually and logically consistent for RTL and mixed text, so insertion, deletion, and cursor motion act on the grapheme the user perceives as adjacent.
- **FR-009**: The composer MUST preserve the original logical text as the source of truth; the RTL presentation MUST be display-only and MUST NOT alter the stored/submitted value.

**Mixed content (interoperability)**

- **FR-010**: The system MUST keep Latin text, inline code spans, numbers, file paths, URLs, and commands in left-to-right order when they appear inside Arabic text, positioned correctly within the line.
- **FR-011**: The system MUST leave content that contains no RTL characters byte-identical to the current (non-RTL) rendering — pure-LTR output is never reordered or re-shaped.
- **FR-012**: The system MUST resolve the base direction of a line so a single foreign token does not force-flip an otherwise readable line.

**Model interchange (sent/received/stored)**

- **FR-013**: The system MUST send Arabic to the model as normalized logical Unicode (standard code points in logical order), never presentation forms and never visually reordered text.
- **FR-014**: The system MUST persist Arabic in session history, project context, and memory in its original logical form, so re-sent context and copy operations round-trip exactly.
- **FR-015**: Copying selected Arabic or mixed transcript text MUST place logical, reading-order text on the clipboard that reproduces the original when pasted into another application.
- **FR-016**: The system MUST NOT let Arabic input change the model routing, tool contracts, or the deterministic stable prompt prefix.

**Configuration & robustness**

- **FR-017**: The system MUST support RTL rendering modes covering at least: automatic detection, forced visual shaping, reliance on a BiDi-capable terminal (no app-side reordering), and off; plus alignment control (automatic/right/left).
- **FR-018**: In the "rely on terminal" mode, the system MUST NOT reorder text itself, preventing double-reversal on BiDi-capable terminals.
- **FR-019**: The system MUST remain responsive and MUST NOT crash, hang, or corrupt unrelated UI on any Arabic input, including malformed, extremely long, emoji-laden, or diacritic-heavy text.
- **FR-020**: RTL setting changes MUST take effect for subsequent rendering without a restart and without disrupting an in-flight task.
- **FR-021**: On terminals/fonts that cannot shape Arabic, output MUST degrade to a legible fallback rather than producing corrupted glyph sequences.

**Interface locale (stretch)**

- **FR-022**: The system SHOULD optionally allow the interface chrome (hints, labels, command descriptions, first-run guidance) to be presented in Arabic, rendered through the same shaping/ordering/alignment pipeline, with English remaining the default and always available.

### Key Concepts *(clarifying definitions, not stored data)*

- **Logical text**: The text in memory/keystroke/reading order using standard Unicode code points. This is the source of truth — what is stored, submitted to the model, and copied.
- **Visual (presentation) text**: A display-only transformation of logical text — letters replaced by contextual presentation forms and RTL runs reversed — used solely to render on terminals that do not shape/reorder themselves. Never stored, sent, or copied.
- **Directional run**: A maximal span of a display line with a single resolved direction (RTL or LTR); a bidirectional line is a sequence of such runs.
- **RTL rendering mode**: The user setting governing whether and how the app produces visual text (auto / visual / rely-on-terminal / off) plus alignment.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of Arabic-containing display lines across all listed surfaces render with correct contextual joining and right-to-left order in the supported terminal profiles (verified against a reference corpus).
- **SC-002**: A user can type a 10-word Arabic sentence and see live joining, right-to-left order, right alignment, and a correctly placed cursor, with no perceptible added input latency versus English typing (typing stays instant per the existing responsiveness bar).
- **SC-003**: Across a corpus of at least 20 representative mixed Arabic-English lines (identifiers, code spans, numbers, paths, URLs), each segment displays in its correct direction and the whole line reads correctly, in both transcript and composer.
- **SC-004**: Selecting and copying any Arabic or mixed passage yields a byte-for-byte logical round-trip when pasted into an external editor (0 corruption across the corpus).
- **SC-005**: The exact content delivered to the model for Arabic prompts is normalized logical Unicode with zero presentation forms and zero visual reversal, and Arabic-language tasks complete successfully at parity with equivalent English tasks.
- **SC-006**: The deterministic stable prompt prefix remains byte-identical across turns whether or not the conversation contains Arabic, and Arabic content introduces no new prefix-cache invalidation (verified by the existing prefix-stability check and honest before/after cache measurement).
- **SC-007**: A robustness/fuzz corpus (Arabic + diacritics + emoji + Arabic-Indic digits + very long lines + mixed direction) produces zero crashes, hangs, or layout corruption.
- **SC-008**: On a BiDi-capable terminal and on a legacy console, no output is ever double-reversed, and pure-LTR output is unchanged from today (byte-identical) in both.
- **SC-009**: In usability testing, Arabic-primary users rate reading and composing Arabic messages at parity (target ≥90% task success and satisfaction) with English-primary users on the same tasks.

## Assumptions

- The existing RTL scaffolding (contextual letter shaping/joining, bidirectional reordering, RTL-character detection, and the RTL mode/alignment settings) is the foundation to **extend and complete**, not to replace — consistent with the "improve, don't rewrite" principle.
- Arabic rendering is **presentation-only** within the terminal; the model always receives logical Unicode carried on the user message (dynamic content), and the cached stable prompt prefix, tool schemas, model routing, and security model are untouched — consistent with the deterministic-prefix and dynamic/cached-separation principles.
- The default RTL mode is automatic with a visual-shaping fallback, chosen because it is the most reliable default across common Windows/legacy terminal and font combinations; users on fully BiDi-capable terminals can opt into the "rely on terminal" mode.
- Primary scope is Arabic **content** (reading, writing, mixed interop, and model interchange). Full translation of the interface chrome into Arabic is an optional stretch (User Story 6) and may be deferred without blocking the feature.
- Arabic is the validated, committed script; other RTL scripts benefit from the same bidirectional pipeline as a side effect but are not separately validated here.
- No new external services or network calls are introduced; the feature is local-only and cross-platform, consistent with existing constraints.
- Verification will use realistic multi-turn Arabic and mixed sessions (not synthetic single-shots), holding model, gateway, and effort constant, with cache/quality measured honestly — consistent with the project's measurement and verified-improvement standards.
- Terminal-level assistive technology (screen readers) is out of scope for this feature.

## Out of Scope

- Automatic machine translation of message content between Arabic and English.
- Converting or normalizing the user's choice of Arabic-Indic vs Western digits in the content sent to the model (digits are preserved as written).
- Full localization of all documentation and marketing surfaces.
- Guaranteeing pixel-perfect rendering on terminals/fonts that lack Arabic glyph coverage (a legible fallback is provided instead).
- Support commitments for RTL scripts other than Arabic beyond the shared pipeline's incidental behavior.
