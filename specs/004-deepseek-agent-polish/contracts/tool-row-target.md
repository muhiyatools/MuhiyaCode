# Contract: Tool Row Target (delta on 003 tool-display)

**Feature**: `004-deepseek-agent-polish` | Decision: [research D7](../research.md) | Base contract: [003 tool-display](../../003-tui-ux-overhaul/contracts/tool-display.md)

003 already specifies and largely implements the collapsed row `marker · label · target · outcome` with the file path between the tool name and the `+A −R` counts for `edit_file`/`multi_edit`/`write_file`. This contract covers only the two gaps (research A13) plus the ordering guarantee the 004 spec makes explicit (FR-006/FR-007; SC-007).

## 1. Ordering guarantee (all file-mutation tools)

Collapsed row segments render strictly as: **tool label → affected file path → `+A −R` counts**. Expanded (Ctrl+O) view keeps the identical header line. Test: stripped-ANSI index of the path precedes the index of the `+`-count (note: minus is U+2212 `−`).

## 2. `apply_patch` target derivation (new)

- At tool-start, derive the target from the `patch` argument: first `+++ ` header path, with `a/`/`b/` prefixes stripped; `/dev/null` headers skipped (deletions fall back to the `--- ` path).
- Multi-file patches: first file's path + suffix ` (+N more)` where N = additional distinct files.
- Underivable (malformed patch, no headers): target stays empty and the segment is **omitted cleanly** — no placeholder, no artifacts (FR-007).
- Derivation happens where `toolTarget` already runs (tool-start), keeping `toolView` shape unchanged.

## 3. Path truncation (new helper, path-like targets only)

- `truncateMiddle(path, width)`: preserves the leading path root and the trailing filename around a single ellipsis glyph (`…`, ASCII fallback per existing glyph table); guarantees the filename (final segment) survives at any width ≥ the existing 12-cell floor; single-line, ANSI-safe.
- Replaces head-anchored `oneLine` **only** for the target segment of path-bearing tools; command/query targets keep `oneLine` (head-relevance differs).
- Width interplay unchanged: target ≤ `max(12, width/2)`, outcome ≤ `max(12, width/3)`, whole row clamped by `fitLine` — counts must remain visible whenever the row fits at all.
- Unusual paths (spaces, non-Latin scripts): truncation is rune-safe via existing width helpers.

## 4. Out of scope

`git_diff` optional-path behavior (003 contract marks its target "—"); expanded-view diff body rendering; any prompt/prefix surface (this contract is cache-inert).

## 5. Test obligations

Existing conventions (`stripANSI` substrings on `renderTool` output): ordering assertion (§1) for `write_file`/`edit_file`/`multi_edit`/`apply_patch`; `apply_patch` derivation table (single-file, multi-file suffix, deletion fallback, malformed → omitted); `truncateMiddle` table (filename preserved at narrow widths, ellipsis glyph, rune safety); narrow-terminal row: counts visible, no wrap.
