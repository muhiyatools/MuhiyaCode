# Contract: Visual State System Delta

**Feature**: `005-terminal-memory-overhaul`  
**Status**: Normative delta to [`003` visual-system.md](../../003-tui-ux-overhaul/contracts/visual-system.md)

This contract does not redesign feature 003's palette, glyph meanings, markdown, tool rows, summaries, or 60x20 floor. It centralizes remaining layout metrics and adds the states/interactions required by feature 005.

## 1. Single Theme owner

One Theme instance owns:

- all existing semantic palette styles and ASCII/Unicode glyphs;
- spacing scale, horizontal/vertical pane padding, gutter widths;
- header, input, mode-line, notice/activity, menu, and modal row metrics;
- borders and focus/selection variants;
- 60x20 minimum and responsive breakpoints;
- mouse focus, transcript selection, paste block, loading, and empty-state styles.

No pane may introduce an independent hex/ANSI color, non-ASCII glyph, padding constant, border, or breakpoint after migration. Derived widths/heights use this Theme plus actual terminal dimensions.

## 2. State presentation

| State | Primary location | Required text/glyph cue | Color role | Interaction |
|---|---|---|---|---|
| first-run/empty | transcript | short start guidance and command discovery | text/brand | input immediately focused |
| runtime loading | activity | loading label + spinner | info/brand | draft editable; submit waits visibly |
| idle | activity/mode | ready label | muted | all navigation/input enabled |
| busy pre-stream | activity | working label + spinner | info | stop/steer enabled |
| streaming | active transcript/activity | streaming label and active block | brand | scroll-away respected |
| tool running | tool row/activity | tool name + running glyph/text | brand/info | chip clickable/focusable |
| subagent running | agent row/activity | subagent title + running glyph/text | brand/info | chip clickable/focusable |
| error | notice/modal | error glyph + actionable message | danger | dismiss/retry where applicable |
| too small | full frame | `MuhiyaCode needs at least 60x20` | warning | exit/recovery keyboard path |

Meaning never relies on color alone. Loading/tool/subagent may coexist; render the most specific activity without hiding stop/error affordances.

## 3. One-frame layout transaction

- `WindowSizeMsg` stores actual dimensions, including below minimum.
- Compute pane geometry, visible transcript range, wrapped blocks, input rows, and interaction map as one layout revision.
- Emit no externally visible intermediate frame with old geometry.
- Below 60x20, omit normal panes and render one centered/clipped warning inside actual bounds.
- Restoring valid size reconstructs the prior draft, paste focus/state, modal state, transcript anchor/selection, and follow-output flag.

## 4. Frame stability obligations

- An input-only frame has byte-identical completed transcript rows.
- A stream frame changes only active stream/activity rows unless new rows legitimately shift the viewport while follow-output is true.
- An idle interval emits no new application frame revision.
- Hit rectangles and visible styles always share the same frame revision.
- Collapsed paste block occupies <=2 input rows; expanded block cannot push transcript below its minimum visible height.
- Focus and selection have non-color cues in `NO_COLOR`/limited-color modes.

## 5. Cross-platform profiles

Validate at minimum:

- Windows Terminal: primary, dark/light, 80x24 and 120x40, live resize.
- One macOS terminal and one Linux terminal with mouse/copy enabled and disabled.
- `NO_COLOR=1` and ASCII glyph mode.
- Widths/heights around 59/60 columns and 19/20 rows.
- RTL/mixed-width input and transcript content.

Visual evidence records terminal/version/theme/dimensions and normalized completed-row hashes in addition to video/screenshot review.
