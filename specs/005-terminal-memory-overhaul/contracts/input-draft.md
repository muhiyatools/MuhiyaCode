# Contract: Segmented Input Draft and Large Paste

**Feature**: `005-terminal-memory-overhaul`  
**Status**: Normative

## 1. Draft representation

The main full-screen prompt is an ordered list of atomic parts:

- `TextPart`: editable text.
- `PastePart`: immutable raw pasted content with stable ID and presentation state.

The cursor is a grapheme boundary in a text part or a boundary before/after a paste part. Adjacent text parts merge after edits. Placeholder labels are presentation only and can never enter stored/sent content.

Modal text fields and simple-line input remain on their existing paths; this contract governs the full-screen main composer.

## 2. Paste classification

- Count decoded Unicode code points in `PasteMsg.Content`.
- Logical line count treats CRLF as one break and lone CR or LF as one break; non-empty content with no break is one line.
- A paste is large if `codePoints >= 1024 OR logicalLines >= 6`.
- A paste below both thresholds merges into the active TextPart as ordinary editable content.
- A large paste creates one PastePart at the cursor and preserves the exact received Go string, including CRLF/lone CR/LF, tabs, NUL, Unicode, combining sequences, and trailing newlines.
- Render unsafe control characters visibly/safely without changing raw storage.

The former 32,000-character textarea limit does not apply to PastePart content; content must never be silently truncated.

## 3. Paste block behavior

- New large blocks start collapsed.
- Collapsed display uses at most two input rows and labels the exact logical line count.
- Focus + Enter/click toggles collapsed/expanded.
- Expanded display is height-bounded and internally scrollable so the rest of the layout remains stable.
- Delete/Backspace on a focused atomic block removes only that block.
- Cursor navigation stops before/after the block. Normal text can be inserted and edited on either side.
- Multiple blocks retain stable ordering and independent state.
- Resize preserves raw content, focus, part order, and cursor boundary.

## 4. Assembly and lifecycle

On submit:

1. Validate that every part is live and ordered.
2. Precompute total byte length.
3. Concatenate TextPart text and PastePart raw content exactly once in part order.
4. Resolve slash commands only when the resulting message is command text without paste blocks; pasted data cannot accidentally become a UI command.
5. Pass exactly the assembled string to the existing engine/queue path.
6. Release the draft only after the submit/queue path accepts ownership.

Clear, Esc-abandon, successful send, and session switch release all parts. A rejected/waiting submission retains the entire draft. Released paste data cannot be attached to a later message.

## 5. Fidelity oracle

For a draft `T0, P1, T2, P3, T4`, expected sent content is exactly:

```text
T0.Raw + P1.RawContent + T2.Raw + P3.RawContent + T4.Raw
```

Comparison is byte-for-byte on the UTF-8 encoding of the received Go strings after no additional newline or whitespace trimming. The input history stores the assembled message, not labels or hidden object IDs.

## 6. Required cases

- 1,023/1,024 code points and 5/6 logical lines.
- Empty, one huge line, LF, CRLF, lone CR, mixed separators, trailing line breaks.
- Tabs, NUL, Arabic, CJK, emoji, combining characters, RTL display.
- Content larger than the old 32 KiB limit.
- Multiple paste blocks with text before/between/after.
- Expand/collapse, independent removal, clear, abandoned session, busy-message queue.
- Click and keyboard caret positions at wrapped/wide-character boundaries.
- Fuzzed part/edit sequences whose assembled oracle always matches.
