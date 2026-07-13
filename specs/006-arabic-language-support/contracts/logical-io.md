# Contract: Logical-Text I/O Guarantees

Everything that leaves the terminal's screen — the model request, session history, project context, and the clipboard — MUST carry **logical** Unicode, never the visual presentation used for display. This contract makes that explicit and testable.

## Send to model (FR-013, FR-016)

- The content transmitted for a turn is the composer's logical `Value()` (plus the normal task brief), normalized to Unicode **NFC**. It contains standard code points in logical order.
- MUST NOT contain Arabic presentation forms (U+FB50–U+FEFF ranges) introduced by display shaping.
- MUST NOT be visually reversed.
- Arabic input MUST NOT change model routing, tool schemas, effort, or the stable prefix — it rides the user message exactly like English input.
- **Test**: submit an Arabic prompt; assert the recorded request bytes are NFC logical Arabic with zero presentation-form code points and pass a logical-equality check against the typed input.

## History & project context (FR-014)

- Persisted transcript, session history, `MUHIYA.md`, and `MEMORY.md` store logical text. Re-loading a session or re-sending context reproduces the original logical bytes.
- The boot block / cached prefix is unaffected: Arabic content that appears in project files is delivered logically (as any content is) and never triggers a prefix change beyond the normal content it already represents.
- **Test**: round-trip a session containing Arabic (write → read) and assert byte-equality of stored content; assert the stable prefix hash is unchanged by Arabic transcript growth (SC-006).

## Clipboard copy (FR-015, SC-004)

- Copying a selection (Arabic or mixed) places **logical, reading-order** text on the clipboard via OSC 52, recovered through the display pass's column map — NOT the on-screen visual string.
- Pasting into an external editor reproduces the original logical text byte-for-byte.
- **Test**: select an Arabic (and a mixed Arabic+`code`) passage, copy, and assert the clipboard payload equals the logical source; assert no presentation-form code points are present.

## Digit & normalization policy

- User-authored digits (Arabic-Indic ٠-٩ or Western 0-9) are preserved as written in what is sent/stored — no silent conversion. (Out-of-scope note in spec.)
- Only NFC normalization is applied to model-bound text, and only where it does not alter user-visible meaning.

## Failure & degradation

- If the display pass cannot shape (incapable terminal), the model-bound / stored / copied text is STILL logical and correct — degradation is display-only and never corrupts the logical layer. (FR-021.)
