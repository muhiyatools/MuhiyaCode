# Contract: Tool Activity Display

**Feature**: `003-tui-ux-overhaul` | Serves FR-006..008, FR-016, SC-005 | Data model §2.2 (ToolDisplay)

## 1. The two detail levels

One global toggle (`Ctrl+O`, existing `m.verbose`) switches every entry simultaneously between:

**Collapsed (default)** — exactly one line per tool action:

```
<marker> <Label> <target> <outcome>
● Edit   internal/tui/render.go   +12 −3
● Read   internal/tui/view.go     742 lines
● Shell  go test ./internal/tui   exit 0 · 4.2s
× Shell  go vet ./...             exit 1
⠋ Grep   "CacheRead" internal/    running…
```

**Expanded (Ctrl+O)** — the same header line plus a uniform detail block: left `▏` gutter in `border.default`, content styled per tool class, bounded by the existing 8 KB output cap and viewport scrolling.

Rules:
- Collapsed shows **no content lines** — not even the current 8-line diff preview (`view.go:501-510`), which this contract removes (FR-006).
- Expanded shows **full available detail** for every tool type in the same visual pattern (FR-007); no mixed states (FR-008).
- Markers come from the glyph table ([visual-system.md](visual-system.md)): `●` done (status.success), `×` failed (status.error) — color always paired with the marker shape, spinner while running.

## 2. Per-tool table

| Tool(s) | Label | Target | Outcome (collapsed) | Detail (expanded) |
|---|---|---|---|---|
| `edit_file`, `multi_edit`, `apply_patch` | Edit / Patch | file path (middle-truncated) | `+A −R` from diff payload | full colorized diff |
| `write_file` | Write | file path | `+A` (new file: line count) | full colorized diff / content preview |
| `read_file` | Read | file path | `N lines` | none beyond header (content already in model context; showing it duplicates) — expanded adds byte size + range |
| `list_files`, `glob` | List / Glob | pattern/dir | `N entries` | entry list |
| `grep`, `search_text` | Grep / Search | pattern + scope | `N matches` | match lines (file:line) |
| `run_shell` | Shell | command (first line, middle-truncated) | `exit S · dur` | full stdout/stderr box |
| `git_status`, `git_diff` | Git status / Git diff | — | `N files` / `+A −R` | full output / colorized diff |
| `update_plan` | Plan | — | `done/total steps` | step list with states |
| `ask_user` | Question | question (truncated) | `answered` / `pending` | full Q&A |
| `propose_changes` | Change plan | — | `N changes` | proposed change list |
| `run_subagent` | Delegate | subagent title | `running…` / `done · N tok` | task brief + result summary |
| `web_search` | Web search | query | `N results` | numbered titles + URLs — **provider name never rendered** (FR-016; gateway already returns provider-agnostic results, `web.go:153-184` — this contract forbids any future field leaking) |
| `mcp__a/b` | MCP · a / b | tool-defined | first-line summary | full output box |
| unknown | title-cased name | tool-defined | first-line summary | full output box |

**Outcome derivation**: `+A −R` counts lines with `+`/`-` prefixes in the existing `--- diff ---` payload, excluding `+++`/`---` file headers (classifier precedent: `diffLineStyle`, `view.go:517-530`). Counts unavailable (no diff marker in output) ⇒ outcome falls back to the first-line summary — degrade, never guess.

## 3. Failure display

Failed tools (existing `isFailure` heuristics): collapsed line keeps Label/Target and shows the first error line as outcome in `status.error`; expanded shows the full error output. The `×` marker carries the state for monochrome terminals.

## 4. Streaming states

While running: spinner marker + Label + Target + `running…` (+ elapsed once >2 s). Output streams only into the expanded view; collapsed stays one line throughout (no growth-then-collapse jump).

## 5. Test obligations (D14)

- Update-layer: Ctrl+O flips all entries; per-tool outcome derivation incl. diff-count edge cases (no marker, headers-only diff, binary notice).
- Golden: collapsed transcript with one entry of each class; the same transcript expanded; failure and running states.
- SC-005 check: every tool type in the table renders one line collapsed and full detail expanded.
