# Contract: RTL Display Pass

The single, centralized transform that turns one **logical** line into what appears on screen. Every user-visible surface routes through it so behavior is uniform and no surface is left unhandled.

## Inputs

- `logical` — the logical-order Unicode line (already wrapped to the target width).
- `width` — the display column budget for this line.
- `mode` — `auto` | `visual` | `native` | `off`.
- `align` — `auto` | `right` | `left`.
- `capability` — whether the terminal does its own BiDi (only consulted when `mode == auto`).

## Output

A `DisplayLine` (see data-model.md): the on-screen string, the originating logical string, a visual↔logical column map, and the resolved alignment.

## Guarantees (MUST)

1. **Purity / determinism**: Output is a pure function of the inputs. Same inputs ⇒ byte-identical output. No timestamps, no map-iteration order, no locale calls. (Constitution III.)
2. **LTR no-op**: If `logical` contains no RTL character (`IsRTL == false`), the visual string is byte-identical to today's rendering and alignment defaults to left. Pure-LTR sessions are unaffected. (FR-011, SC-008.)
3. **Off no-op**: `mode == off` ⇒ no shaping, no reordering, no re-alignment; output equals input. (FR-017.)
4. **Native = logical**: `mode == native` (or `auto` on a BiDi-capable terminal) ⇒ NO app-side reordering or presentation-form substitution; the terminal renders the logical text. Prevents double-reversal. (FR-018, SC-008.)
5. **Visual shaping**: `mode == visual` (or default `auto`) and the line has RTL ⇒ apply contextual joining (isolated/initial/medial/final, plus the LAM+ALEF ligature) then reorder runs **per run, grapheme-aware** (base+combining marks stay together — never a whole-line `bidi.ReverseString`) so RTL reads right-to-left while LTR runs (Latin/`code`/numbers/paths/URLs) stay intact and correctly positioned. Technical tokens (paths, URLs, `code`, bracketed calls) are pre-segmented as atomic LTR runs so `x/text/bidi`'s bracket/neutral gaps can't fragment them. Base direction is forced RTL for Arabic-primary lines. (FR-001, FR-002, FR-010, FR-012.)
6. **Alignment**: With `align == auto`, a dominant-RTL line is right-aligned to `width`; others are left-aligned. `right`/`left` force it. Alignment padding uses **grapheme-cluster display width** (`uniseg`), treating combining marks as zero-width — never per-rune `RuneWidth` (which over-counts harakat). (FR-003, FR-006.)
7. **Streaming safety**: Applying the pass to a growing prefix of a line must not corrupt earlier glyphs; partial Arabic is readable and does not visibly flip incorrectly as more text arrives. (FR-004.)
8. **Grapheme safety**: The pass never splits a grapheme cluster (base + combining marks stay together); wrapping happens on logical text before this pass, and this pass does not re-wrap. (FR-005.)
9. **Column map fidelity**: The returned map lets a caller recover the exact logical substring for any visual column span (used by selection-copy and composer caret). (FR-015, FR-008.)
10. **No request-path reach**: The pass lives in the TUI layer only; it can never be invoked on, or alter, anything sent to the model or placed in the stable prefix. (FR-016.)

## Surfaces that MUST route through the pass (FR-001)

Transcript: user messages, assistant replies (streamed + settled), system lines, tool rows (label, target/path, outcome, expanded output/diff), agent chips, task-summary line, first-run hint. Chrome: thinking line, plan/goal line, transient notices, modal (title/message/choices/descriptions), command palette rows, header (workspace path, agent view labels), and the composer (see rtl-render note below).

## Composer specifics

- The composer keeps its logical `Value()` as the source of truth; RTL lines are **custom-rendered** from that logical buffer + the caret index (post-processing the textarea's rendered output is impossible — the widget has already baked in the cursor column, wrapping, and ANSI/padding). Pure-LTR lines keep the textarea's own rendering. (FR-009.)
- The caret's logical column (from the editor) maps through the cell map to a visual column so the cursor sits where the user expects. **Editing stays logical**: Backspace/Delete and cursor motion act on logical indices (unchanged textarea semantics); only display + caret *position* are transformed. Home/End go to logical line start/end. (FR-008.)
- Right alignment applies to the composed line(s) when dominant-RTL. (FR-007.)

## Non-goals

- Not responsible for what is sent to the model or stored (see logical-io.md).
- Not responsible for terminals lacking Arabic glyphs beyond emitting a legible fallback (FR-021).
