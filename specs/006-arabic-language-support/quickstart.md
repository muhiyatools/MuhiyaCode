# Quickstart: Validate Arabic Language Support

A runnable validation guide proving the feature works end to end. It references [contracts/](./contracts/) and [data-model.md](./data-model.md) for details rather than repeating them. No implementation code here.

## Prerequisites

- A built `muhiyacode` binary (`go build ./cmd/muhiyacode`).
- A terminal that can display Arabic glyphs (Windows Terminal, iTerm2, or a VTE terminal with an Arabic-capable font). Keep a legacy `conhost` handy for the degradation check.
- A configured API key (for the live model round-trip scenarios) — the rendering scenarios need no key.
- Optional: `xxd`/a hex viewer to inspect logical bytes, and an external text editor to paste into.

## Fast automated gate

```sh
go fmt ./... && go vet ./... && go test ./... -count=1
```

Expected: all packages pass, including the new RTL golden-render, logical round-trip (copy), width/harakat, robustness/fuzz, and prefix-stability tests.

## Scenario 1 — Read Arabic (US1, FR-001..FR-006)

1. Start the TUI; send a prompt that elicits an Arabic reply (or resume a session containing Arabic).
2. Observe the streamed reply, an echoed user message, a tool row, the thinking line, and a modal.

**Expected**: On every surface, Arabic letters are **joined** into words, read **right-to-left**, and dominant-RTL blocks are **right-aligned**. Diacritics attach without shifting columns. A long Arabic line wraps with each wrapped line individually shaped and aligned.

## Scenario 2 — Type Arabic in the composer (US2, FR-007..FR-009)

1. In the composer, type an Arabic sentence character by character.
2. Move the cursor into the middle, backspace, and retype.

**Expected**: Letters join live, the text reads right-to-left and sits right-aligned, and the cursor lands on the perceived-adjacent grapheme. Editing re-shapes correctly. The reported screenshot defect (disconnected, mis-ordered, left-anchored input) is gone.

## Scenario 3 — Mixed Arabic + English/code/numbers/paths (US3, FR-010..FR-012)

Render/type a corpus including:

- `افتح الملف main.go الآن`
- `عدّل السطر رقم 42 في internal/tui/rtl.go`
- ``شغّل الأمر `go test ./...` من فضلك``
- A predominantly English line with one Arabic word.

**Expected**: Arabic reads right-to-left; `main.go`, `42`, the path, and the `code` span stay **left-to-right and intact** in their correct positions. Pure-LTR lines are unchanged from today. A single foreign token does not flip an otherwise readable line.

## Scenario 4 — Clean model I/O + logical copy (US4, FR-013..FR-016)

1. Submit an Arabic prompt; inspect the recorded request bytes (session log / usage record).
2. Select an Arabic passage in the transcript and copy it; paste into an external editor.
3. Put Arabic into `MEMORY.md` / `MUHIYA.md` and confirm it loads.

**Expected**: The transmitted bytes are **NFC logical Arabic** — zero presentation-form code points, no reversal — and the Arabic task completes as an English one would. The pasted clipboard text is the **original logical** text, byte-for-byte. Project-context Arabic reaches the model logically. (See [logical-io.md](./contracts/logical-io.md).)

## Scenario 5 — Modes, alignment & degradation (US5, FR-017..FR-021)

```sh
muhiyacode doctor              # shows mode/align, a mixed sample, and the copy round-trip check
muhiyacode config set rtlMode visual
muhiyacode config set rtlMode native
muhiyacode config set rtlMode off
muhiyacode config set rtlAlign right
```

**Expected**:
- `auto` engages RTL only when Arabic is present; pure-LTR output stays byte-identical.
- `native` (and `auto` on a BiDi-capable terminal) does **not** double-reverse — text is correct exactly once.
- `off` disables all RTL handling.
- Changes take effect without a restart.
- On the legacy `conhost`, output degrades to a legible fallback rather than corrupted glyphs; no crashes.

## Scenario 6 — Robustness / fuzz (SC-007)

Feed a corpus of Arabic + harakat + emoji + Arabic-Indic digits + very long mixed lines through render and composer input.

**Expected**: no crash, hang, or layout corruption; unrelated UI is unaffected.

## Scenario 7 — Cache neutrality (SC-006, Constitution III)

Run a realistic multi-turn Arabic session against a real endpoint and compare against an equivalent English session (same model/gateway/effort).

**Expected**: the stable prompt prefix is byte-identical across turns in both; Arabic introduces **no** new prefix-cache invalidation. Cache read/write/token numbers are taken from provider usage and reported honestly (both efficiency and quality), per the Measurement standards. Store the before/after evidence under this feature directory.

## Sign-off

The feature is done when Scenarios 1–7 pass, the automated gate is green, `docs/` reflect Arabic/RTL behavior, and the cache-neutrality + logical-I/O evidence is recorded here.
