# Contract: Visual System

**Feature**: `003-tui-ux-overhaul` | Serves FR-001..005, FR-014/015, FR-017, FR-021/022, SC-001/002/010 | Research D3/D4/D5/D9/D13

The normative definition of MuhiyaCode's look. Implementation maps these tokens in `internal/tui/render.go` (palette) and a single glyph table; no hex literal or non-ASCII glyph may appear elsewhere in `internal/tui`.

## 1. Semantic tokens

| Token | Dark | Light | 16-color | NO_COLOR | Used for |
|---|---|---|---|---|---|
| `text.primary` | `#E7F0EB` | `#1A241E` | default fg | default | body text |
| `text.muted` | `#8A9B92` | `#5A6B61` | bright black | dim | labels, paths, metadata |
| `text.faint` | `#617068` | `#8A968F` | bright black | dim | hints, tertiary |
| `accent.primary` | `#43D17D` | `#1F8A4C` | green | bold | brand mark, focus, headings |
| `accent.soft` | `#91E7B4` | `#3FA36B` | green | (none) | targets, links, secondary accent |
| `status.success` | `#6CDE98` | `#1F8A4C` | green | (word/symbol) | ok markers |
| `status.warning` | `#E7B65D` | `#9A6B00` | yellow | (word/symbol) | warnings, auto-accept badge |
| `status.error` | `#F07878` | `#B03030` | red | (word/symbol) | failures, danger |
| `status.info` | `#7FB8E0` | `#2E6FA3` | cyan | (none) | informational notes |
| `diff.add` | `#6CDE98` | `#1F8A4C` | green | `+` prefix carries it | added lines |
| `diff.remove` | `#E67A83` | `#B03030` | red | `-` prefix carries it | removed lines |
| `diff.meta` | `#E7B65D` | `#9A6B00` | yellow | dim | `@@` hunks |
| `bg.surface` | `#121A16` | `#EAF2ED` | (none) | reverse | user-message band, input |
| `bg.overlay` | `#19231E` | `#DFEAE3` | (none) | reverse | modals |
| `border.default` | `#314239` | `#B9C9BF` | bright black | (ASCII rule) | rules, boxes |
| `border.focus` | `#43D17D` | `#1F8A4C` | green | bold | focused input border |

Rules:
- Selection: dark/light resolved once at startup (`HasDarkBackground` → `LightDark`); `Settings.Theme` ∈ {`auto`,`dark`,`light`} overrides. **No forced full-screen background** — the terminal's own base is respected (removes `view.go:58-59` fills); `bg.*` applies only to bands/overlays that carry meaning.
- `NO_COLOR` set ⇒ all color drops; hierarchy carried by bold/dim/reverse + the words/symbols column above. Monochrome legibility is a release gate (SC-001 run includes a `NO_COLOR` pass).
- 256/16-color terminals: `colorprofile` downsampling with the 16-color column as the reviewed intent.
- Never color-alone: every status pairs with a marker/word (`×`, `exit 1`, `interrupted`).

## 2. Glyph table

| Key | Unicode | ASCII | Use |
|---|---|---|---|
| `brand` | `◆` (pending Warp width verification; fallback `▪`) | `*` | header mark |
| `marker.ok` | `●` | `*` | completed tool |
| `marker.fail` | `×` | `x` | failed tool |
| `spinner` | `⠋⠙⠹⠸⠼⠴⠦⠧` | `-\|/` | running |
| `rule.h` | `─` | `-` | header rule, table rules |
| `gutter` | `▏` | `\|` | thinking row, expanded detail block |
| `bullet` | `·` | `.` | separators, list bullets |
| `ellipsis` | `…` | `...` | truncation |
| `border.*` | `╭╮╰╯│─` | `+ + + + \| -` | input box, modals, tables |

Selection: unicode by default; ASCII via `MUHIYA_ASCII=1` or `Settings.UI.BorderMode=ascii`. Every unicode entry must be verified width-1 in Warp, Windows Terminal, iTerm2 before release (quickstart scenario 1); a glyph failing verification is replaced table-wide, not per-call-site.

## 3. Layout & spacing

- **Composition** (top→bottom, unchanged order): header (2 lines + rule) · transcript viewport · [command palette | notice] · activity row (busy only) · input box · mode line. The usage footer is removed (task-summary.md §4).
- **Single width source**: all panes derive from `contentWidth()` (`view.go:130-132`); `layout()` line-counting stays authoritative for viewport height.
- **Header**: line 1 `◆ MuhiyaCode v… · <model> · Subagent <sub> · reasoning <effort> · context NN%` — the delegated-model label is the literal `Subagent` (FR-022/US7 exact string; sibling labels stay lowercase); line 2 full workspace path (`text.muted`), left-truncated with leading `…` only when needed; consistent 1-space gutter via style padding, not literal spaces (FR-002/021/022).
- **User messages**: `bg.surface` band, `Padding(0,1)`, **no `> ` prefix** (FR-015).
- **Input area**: rounded border (`border.default`; `border.focus` while busy/steering); placeholder `Type a request · / for commands` — separator composed from the glyph table's `bullet` entry so the §5 grep gate holds (FR-014); mode line keeps mode/plan/goal/skill badges but drops `Enter send` and `Ctrl+P commands` hints; retained hints: `Esc stop · Tab agents · Ctrl+O details` (keybindings themselves unchanged).
- **Markdown**: headings `accent.primary` bold (H1) / bold (H2) / `text.primary` bold (H3); inline: `**bold**`→bold, `*x*`/`_x_`→italic (fallback: `accent.soft` where italic unsupported), `` `code` ``→`bg.surface` span, `~~x~~`→strikethrough (fallback dim); unclosed markers render literally without flash (streaming rule, research D3). Tables: header row bold + `border.default` rules, zebra via alternating `text.primary`/`text.muted` (no row backgrounds), cells get the full inline pass, cell truncation with `ellipsis` (FR-004/005).
- **Density**: transcript padded (prose), tool lines packed (one line), modals padded with grouped sections (`accent.primary` group titles, `text.muted` labels, `text.primary` values) — applies to `/context` (FR-017) and `/usage` modals.

## 4. Responsive breakpoints (D13)

| Width | Behavior |
|---|---|
| >120 | full layout |
| 80–120 | baseline (design target 80×24) |
| 60–80 | header drops `reasoning`, then `Subagent` segments; tool outcome column dropped before target; tables shrink low-priority columns first |
| <60 cols or <20 rows | single clean pane: `terminal too small · MuhiyaCode needs at least 60x20` (bullet from the glyph table; ASCII `x` — §5 grep gate) |

Resize re-layouts every update (existing); no cached absolute positions; truncate-don't-wrap in all single-line contexts.

## 5. Test obligations (D14)

- Golden frames at 80×24 and 60×20 (Ascii profile): header (including a deep-path fixture ≥5 segments pinning full-path display and left-truncation), transcript with markdown corpus, tool lines both detail levels, summary line, `/context` + `/usage` modals, too-small pane.
- Markdown corpus goldens: bold/italic/code/strike in paragraphs, lists, tables, wrapped lines, RTL text, streaming half-tokens.
- Grep gate: no hex color or non-ASCII glyph literals in `internal/tui` outside the palette/glyph tables.
- Manual matrix (quickstart): Warp/WT/iTerm2 × dark/light × 80/120/200 cols × `NO_COLOR`.
