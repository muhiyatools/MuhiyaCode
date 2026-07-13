# Contract: Mouse Interaction and Copy

**Feature**: `005-terminal-memory-overhaul`  
**Status**: Normative

## 1. Mouse mode and coordinate model

- Full-screen views use Bubble Tea `MouseModeCellMotion`.
- Screen coordinates are zero-based terminal cells and use actual, unclamped `WindowSizeMsg` dimensions.
- The immutable interaction map is generated from the same layout/frame revision that is displayed.
- Target precedence is: modal button > command item > input caret/paste block > tool/subagent chip > transcript cell.
- A resize, session switch, page eviction, modal change, or input reflow invalidates the old map atomically.
- `MouseClickMsg` with the left button invokes actions. Release/repeat events never invoke the same action again.

`MouseModeAllMotion` is prohibited because unpressed pointer movement must not generate idle application traffic.

## 2. Interaction targets

### Transcript wheel

- Wheel up/down acts only when coordinates are inside the transcript rectangle.
- Each detent scrolls the configured row delta and sets `followOutput=false` when leaving the bottom.
- Wheel over input, modal, header, or mode line must not move the transcript unless that surface explicitly owns a scrollable region.

### Input caret

- Clicking any rendered composer cell focuses input and chooses the nearest recorded caret stop.
- Caret stops are grapheme boundaries, not byte/rune offsets.
- Wrapped lines, tabs, CJK/emoji double-width cells, combining sequences, RTL display, borders, labels, and padding map to a valid logical position or nearest before/after boundary.
- Clicking a paste block focuses the atomic block; it never places a cursor inside raw paste content while collapsed.

### Command menu

- Each visible row maps to its exact command name.
- Clicking invokes the same resolve/selection path as keyboard Enter and executes once.
- Rows clipped out of the current command window have no hit region.

### Tool and subagent chips

- Tool targets use stable transcript event IDs and toggle that row's expanded state.
- Subagent targets use stable run IDs and open/collapse the same detail state reached by keyboard focus/activation.
- Clicking padding outside a chip does nothing.

### Modal buttons

- Every visible choice/action has one region matching its rendered row/button.
- Click calls the same validated callback as arrow selection + Enter.
- Secret/text modal input retains input focus and does not submit from a non-button padding click.

## 3. Transcript selection and copy

- Left press on transcript plain text starts an in-app selection; pressed drag extends it; release finalizes it.
- Selection references stable event IDs and plain-text row/cell spans, never ANSI byte offsets.
- Selected styling is visible without relying on color alone.
- Ctrl+C copies the active selection before applying existing cancel/exit behavior. With no selection, existing Ctrl+C semantics remain unchanged.
- Copy uses Bubble Tea's clipboard/OSC52 command. A failed/unavailable clipboard request keeps the selection and shows a compact fallback hint.
- Native terminal selection remains documented per terminal (typically a modifier override); no single modifier is assumed across the platform matrix.
- Keyboard selection/copy remains available when no mouse events arrive.

## 4. Keyboard-equivalence matrix

| Mouse action | Keyboard path |
|---|---|
| transcript wheel | Up/Down when input is empty, PageUp/PageDown, End/follow |
| input click/caret | arrows, Home/End, word/grapheme navigation |
| command click | `/` or Ctrl+P, Up/Down/Tab, Enter |
| tool chip toggle | focus visible target then Ctrl+O; existing global Ctrl+O remains when none focused |
| subagent chip open | Tab/Alt+number or visible-target focus + Enter |
| modal button | arrows/Tab + Enter; Esc cancels where supported |
| paste block focus/action | visible-target focus, Enter to expand/collapse, Delete/Backspace to remove |
| transcript selection/copy | selection-mode keys + Ctrl+C |

Terminals that do not report mouse events require no detection handshake: the keyboard paths remain present and the app never waits for a mouse capability response.

## 5. Required matrix

Automated cases cover wheel coordinates, all target priorities, press/release single-fire, stale maps, padding/edge clicks, resize between render and click, and caret placement on ASCII/wrapped/CJK/combining/emoji/RTL text. Manual/PTY runs cover Windows Terminal and one representative macOS and Linux terminal, OSC52 accepted/rejected, terminal-native modifier selection, and mouse-disabled keyboard completion of every row above.
