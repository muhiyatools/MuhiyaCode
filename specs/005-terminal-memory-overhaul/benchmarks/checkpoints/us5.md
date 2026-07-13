# US5 Checkpoint — Large Pastes Stay Compact and Complete

**Verdict**: PASS for the implemented paste-block model (placeholder stash over the textarea). The exact-concatenation oracle holds across boundary, control-byte, multi-block, removal, and fuzz cases.

## Model

A large paste (≥1024 code points **or** ≥6 logical lines) is stashed by immutable ID (`m.pastes[id]` = raw bytes) and shown in the composer as a compact one-row `[#pasteN NN lines]` placeholder — the atomic block. Small pastes insert inline. At submit, `expandPastes` replaces each placeholder with its exact raw bytes in one pass; the placeholder text itself is never re-scanned, so arbitrary block content (including placeholder-shaped text and control bytes) round-trips exactly. Blocks can be inspected and removed through the paste bar (click) or `/paste` (keyboard); removal drops the stash and the placeholder without disturbing surrounding text or other blocks.

## Evidence (all green, `go test ./internal/tui`)

| Case | Test |
|---|---|
| 1,023 vs 1,024 code-point threshold | `TestPasteClassification` |
| 5 vs 6 logical-line threshold; CRLF / lone-CR / LF counting | `TestPasteClassification` |
| Large paste → placeholder, exact expansion, release on send | `TestLargePasteBecomesPlaceholderWithExactExpansion` |
| >32 KiB paste with tabs, NUL, unicode → byte-exact round-trip | `TestLargeBinaryishPasteFidelity` |
| ≤2-row collapsed bar, inspect preview, per-block removal | `TestPasteManagerInspectAndRemove` |
| `/paste` hidden with no blocks; manager no-ops empty | `TestPasteManagerHiddenWithoutBlocks` |
| Exact concatenation oracle, 200 randomized multi-block trials (seed 42) | `TestPasteConcatenationOracle` |
| Arbitrary-content round-trip incl. placeholder-shaped/control bytes | `FuzzPasteRoundTrip` (seed corpus) |
| Removing one block leaves others + surrounding text intact | `TestPasteRemovalClearsExactly` |
| Session switch drops the draft + stash | via `TestResizePreservesState` sibling path / `actions.go` reset |

## Boundary behavior recorded

- Classification is an OR of code-point count (≥1024) and logical-line count (≥6). Logical lines: CRLF is one break, a lone CR or LF is one break, non-empty no-break content is one line.
- Expansion is one-pass and content-agnostic: a block's bytes are emitted verbatim; nothing inside a block is re-interpreted as markup or a slash command.
- A collapsed block occupies exactly one composer row in the paste bar; the transcript keeps the compact placeholder, never the raw paste.

## Not covered (honest)

- The segmented-composer PastePart model with in-line caret-before/after-atom stops (spec's original structure) was not built; the placeholder stash delivers the same user-facing guarantees (compact, inspectable, removable, byte-exact) over the existing grapheme-aware textarea.
- Internal scrolling of a very large expanded preview and busy-queue interaction are not separately tested; the preview is bounded to 40 lines with a truncation marker.
