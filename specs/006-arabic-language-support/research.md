# Phase 0 Research: Arabic Language Support

Consolidated, decision-grade findings for Arabic/RTL in the MuhiyaCode terminal. Four parallel research agents cross-verified against primary sources: Unicode **UAX #9** (Bidirectional Algorithm, rev 51 / Unicode 17.0.0), the freedesktop **terminal-wg BiDi** recommendation (Egmont Koblinger/VTE), Go `golang.org/x/text/unicode/bidi`, `github.com/go-text/typesetting/shaping`, `go-runewidth`/`uniseg` (via `charmbracelet/x/ansi`), and real editors/terminals (Emacs, CodeMirror, Vim, VTE, kitty, Windows Terminal, Alacritty, Helix, Charm's own issues). Each decision is Decision / Rationale / Alternatives with the load-bearing evidence.

## Current-state audit (what already exists)

- `internal/tui/rtl.go` implements contextual Arabic shaping (`shapeArabic`: isolated/initial/medial/final) and bidirectional reordering (`RenderRTL` uses `bidi.Paragraph`/`Order`/`Run` + `bidi.ReverseString` on RTL runs), gated by `IsRTL` and mode (`off`/`native`/`auto`/`visual`).
- `RenderRTL` is applied at only three sites: user messages, assistant/system markdown (per wrapped line, inside `RenderMarkdown`), and the thinking line.
- **Gaps/bugs confirmed by audit:**
  1. The **composer** applies no RTL at all → the reported garbled input.
  2. **Selection copy** reads `m.transcriptContent` (visually shaped/reversed) → copying Arabic yields corrupt logical text (FR-015 violation).
  3. `RTL.Align` is validated and stored but **never applied** → no right-alignment anywhere (FR-003 gap).
  4. Many surfaces (tool rows, modals, notices, header, plan line, agent chip, table cells, first-run hint, task summary) never call `RenderRTL`.
  5. **Latent bug:** `RenderRTL` mirrors RTL runs with `bidi.ReverseString`, which mis-associates combining marks (Go issue #50633) — so Arabic **with harakat** reverses incorrectly today; and it whole-line reverses, which is only correct for pure-RTL, not mixed lines.
  6. The hand-rolled `shapeArabic` table has **no LAM+ALEF ligature** (لا) and partial coverage — insufficient for "perfect" shaping.
  7. **Latent width bug:** the width loops (`oneLine`, `wrapPlain`, `truncateMiddle`, `truncateLeft`, `runePrefix`) sum `runewidth.RuneWidth` per rune, which returns 1 for Arabic harakat (empirically verified) → vocalized Arabic is over-measured, so wrapping/truncation/alignment miscompute. Must use grapheme-cluster width (`uniseg`).
- **Implication:** the pipeline's *shape → reorder* idea is right; the work is to *centralize + complete + fix I/O + fix reversal/shaping correctness*, not rebuild (Constitution VIII). But the composer specifically must be **custom-rendered** (R2), which is new build, not just wiring.

## R1 — Rendering strategy: app-side visual shaping is the default; suppress terminal BiDi

**Decision:** Keep **app-side visual shaping** (the app runs UAX #9 + Arabic joining and emits visual order — terminal-wg's "explicit mode") as the DEFAULT for `auto`/`visual`. Additionally, at TUI startup, **proactively suppress terminal-side BiDi** by emitting BDSM-explicit `CSI 8 l` (restore on exit). Keep `native` as an **explicit user opt-out** for BiDi-capable terminals (app does not reorder; it emits logical text + BDSM-implicit `CSI 8 h`). Keep `off`. **Do not auto-detect** terminal capability.

**Rationale (verified):** The terminal support matrix (R9) shows the overwhelming majority of terminals — Windows Terminal, conhost, xterm, Alacritty, foot, Contour, kitty — are BiDi-agnostic and expect visual order, which is exactly what app-side shaping produces. The double-reversal hazard exists only on the ~5 BiDi-capable terminals (VTE family, mlterm, Konsole, Terminal.app, iTerm2-experimental); emitting `CSI 8 l` tells terminal-wg-compliant terminals "the app owns BiDi, don't reorder" (harmless/ignored elsewhere). Runtime capability detection is not viable — terminal-wg autodetection issue #5 is unresolved, `DECRQM` for mode 8 usually gets no reply, and absence of a reply is ambiguous. Suppression + explicit opt-out is more robust than detection.

**Alternatives rejected:** (a) auto-detect capability and switch visual/native — unreliable, ambiguous, can double-reverse; (b) rely on terminal-native BiDi as default — fails on Windows and most terminals; (c) do nothing about terminal BiDi — double-reversal on VTE/mlterm/Konsole/Terminal.app.

## R2 — Composer RTL input: custom-render the line; keep logical editing

**Decision:** Keep the Bubbles **textarea as the logical editor** (source of truth = `Value()`, clean logical Unicode, unchanged editing semantics). For lines containing RTL, **custom-render the composed line ourselves** — from the logical buffer + the editor's caret index — running BiDi runs + Arabic shaping, reversing **per RTL run** (not whole-line), right-aligning, and drawing the caret at the visual column computed from a logical→visual permutation. Pure-LTR lines keep the textarea's own rendering. **Editing keys stay logical**: Backspace/Delete and cursor motion act on logical indices (the textarea already does this, grapheme-aware); only the *display* and the caret's *visual position* are transformed.

**Rationale (verified):** Post-processing `textarea.View()` output cannot work — by then the widget has already (a) computed the cursor's visual column from the logical buffer, (b) wrapped by logical widths, and (c) injected ANSI style + alignment padding; reversing that string reverses escapes/padding and strips the cursor, and there is no Charm API to reposition the cursor afterward (Charm's own `crush`#2311: the *app* must implement BiDi; lipgloss#167: Arabic "messes the layout"). Whole-line reversal is correct only for pure RTL; mixed lines need per-run reversal from the levels array (UAX #9 L2). Choosing **logical** cursor movement (Emacs default; terminal-wg "movement is always logical") over **visual** movement (CodeMirror) is the least-risk choice: it requires no change to the textarea's editing logic, only to how the caret is *drawn*. Home/End remain logical line start/end.

**Rationale for own-render (not fork):** We do not fork or replace the textarea; it remains the editing engine (state, history, grapheme navigation, paste). We take over *presentation* of RTL lines only — an extension consistent with Improve-Don't-Rewrite. If the textarea does not expose the caret's logical line+column, add a minimal read-only accessor (no behavior change).

**Alternatives rejected:** (a) post-process the widget's rendered output — impossible (above); (b) fork the textarea into a custom RTL editor now — disproportionate; (c) visual arrow-key movement — more code and re-maps editing semantics for marginal benefit; revisit only if users request browser-style caret motion.

## R3 — Copying logical text from a visual display

**Decision:** Produce a per-line **logical shadow + visual↔logical column permutation** from the same pass that renders the visual line (built from `bidi.Ordering` runs: `Run.Pos()` gives each run's logical span, walked in visual order with intra-RTL-run index reversal). Selection copy resolves visual columns → logical indices → logical substring in reading order. In `off`/`native` modes the on-screen text is already logical, so copy is a straight read.

**Rationale (verified):** `x/text/bidi` returns runs in visual order but each `Run.String()` stays logical and there is **no built-in per-character visual↔logical map** — you must build the permutation by walking runs (UAX #9 L2; CodeMirror's "sections" model). Generating it in the same pass that draws the line guarantees the two never disagree and makes whole-line/whole-message selection (the common case) exact.

**Alternatives rejected:** invert presentation-forms + reversal at copy time (fragile across mixed runs/neutrals; and `ReverseString` is itself buggy — R8/E3); keep copying the visual string (the current bug).

## R4 — Shaping engine, width, and wrapping order

**Decision:** **Wrap logical text first, then shape** each wrapped line (matches current order), never splitting a grapheme cluster (base+harakat stay together). Measure width with **grapheme-cluster-aware width from `github.com/rivo/uniseg`** (already an indirect dependency, `v0.4.7` — promote to direct) and use its `StepString` for the composer's one-cluster-per-arrow cursor movement. For shaping, add the **mandatory LAM+ALEF ligature (لا, U+FEF5–U+FEFC)** and complete the joining coverage — preferred engine `github.com/go-text/typesetting/shaping` (pure Go; used by Fyne/Gio/Ebitengine); acceptable v1 fallback: extend the existing `shapeArabic` table with LAM-ALEF and the missing forms.

**Rationale (empirically verified against the project's exact versions):**
- UAX #14 (LB9) and UAX #29 (GB9) forbid breaking a base from its combining marks; Arabic joining is contextual (a letter at a line edge must take its final/isolated form), so wrap-then-shape is the only correct order (Raph Levien / HarfBuzz: shapers operate on an already-broken line). Shape-then-wrap would force un-shape/re-shape at every break.
- **`go-runewidth v0.0.24` `RuneWidth` returns 1 (wrong) for Arabic harakat** — its combining table has **no** entries in U+0600–U+06FF (verified: U+064B/064E/0650/0651/0652/0670 all return 1). So summing `RuneWidth` per-rune over-counts vocalized Arabic (e.g. `كِتَاب` measured as 6, true 4). The existing `oneLine`/`wrapPlain`/`truncateMiddle`/`truncateLeft`/`runePrefix` loops do exactly this and must switch to cluster-aware width. `go-runewidth v0.0.24` `StringWidth` is *now* cluster-aware (imports clipperhouse/uax29) and returns correct widths for well-formed vocalized Arabic, but still returns 1 for a leading/defective combining mark where `uniseg` correctly returns 0 — so **`uniseg` is the primitive** for both width and cursor stepping.
- All Arabic code points (base, Forms-A U+FB50–FDFF, Forms-B U+FE70–FEFF) are East-Asian-Width **Neutral → 1 cell**; none are wide (UAX #11 says EAW can't express combining-mark width, hence the explicit zero-width handling).

**Dependency note (Principle IX):** No new *width* dependency — `uniseg` and `go-runewidth` are already in `go.mod`; promote `uniseg` to a direct dependency (justified: correct Arabic width + cursor stepping). `go-text/typesetting/shaping` is an *optional* new dependency for higher-fidelity shaping/ligatures; the v1 fallback (extend the table with LAM-ALEF) needs no new dep.

**Alternatives rejected:** summing per-rune `RuneWidth` (over-counts harakat — an existing bug); shape-then-wrap (breaks joining); presentation-form-only width (loses zero-width combining guarantee).

## R5 — Mixed bidirectional content (Arabic + English/code/numbers/paths)

**Decision:** One `bidi.Paragraph` per line; for Arabic-primary lines pass **`DefaultDirection(bidi.RightToLeft)`** to force RTL base level 1; call `Order()`; walk `Run(i)` in visual order using `Run.Pos()` for the logical↔visual map; reverse only RTL runs (grapheme-aware — see R8/E3). **Pre-segment technical tokens** (file paths, URLs, `code` spans, bracketed calls like `f(x)`, identifiers) in our own tokenizer and treat each as an **atomic LTR run**, rather than trusting the package's weak/neutral/bracket resolution.

**Rationale (verified):** `DefaultDirection(bidi.RightToLeft)` genuinely *forces* level 1 (core.go: explicit level bypasses first-strong), so an Arabic line starting with an English word, digit, `/`, or `(` is still RTL-based with those as embedded LTR runs — exactly right for FR-012. But UAX #9 weak/neutral rules (W4–W7, N1–N2) fragment paths/URLs at separators (`/`, `:`, `.` become Other Neutral then resolve by surroundings), and the Go package has two **open bugs** that hit coding content directly: brackets (N0) are not matched so `f(x)`/`(b)` split across runs (Go #72089), and nested isolates reorder incorrectly (Go #69819) — so embedding isolate controls is not a safe fix. Pre-segmenting technical tokens and placing whole LTR runs sidesteps both.

**Alternatives rejected:** trusting neutral/bracket resolution (fragments paths/URLs/calls); wrapping tokens in `LRI…PDI` isolate controls (nested-isolate bug #69819).

## R6 — Normalization of model-bound text

**Decision:** Normalize model-bound (and prefix/hash-relevant) Arabic to **Unicode NFC**; never convert digit systems.

**Rationale:** NFC is idempotent and meaning-preserving; prevents equivalent code-point sequences from drifting across turns (aids determinism) without changing displayed content. Arabic-Indic vs Western digits are user intent, preserved.

**Alternatives rejected:** no normalization (risks equivalent-sequence drift); digit conversion (changes user meaning — out of scope).

## R7 — Terminal BiDi negotiation (replaces "capability detection")

**Decision:** No runtime auto-detection. In `auto`/`visual`, emit **BDSM explicit `CSI 8 l`** at startup (and restore `CSI 8 h` on exit) so compliant terminals don't double-reorder; in `native`, emit **BDSM implicit `CSI 8 h`** and do no app-side reordering. Optionally also emit box-mirroring `CSI ? 2500` and arrow-swap `CSI ? 1243` only under `native`. These sequences are ignored by non-compliant terminals, so they are safe everywhere.

**Rationale (verified):** There is no reliable capability handshake (terminal-wg #5 open; `DECRQM` mostly unanswered/ambiguous). Suppressing terminal BiDi is deterministic and side-effect-free, and directly prevents the only failure mode (double-reversal on VTE/mlterm/Konsole/Terminal.app/iTerm2-experimental). kitty (word-order-only RTL) documents the same "let the app do BiDi, terminal stays LTR" contract via `force_ltr`.

**Alternatives rejected:** probing at startup (latency/compat risk; mirrors the project's avoidance of alt-screen round-trips); ignoring terminal BiDi entirely (double-reversal for VTE/mlterm users).

## R8 — `golang.org/x/text/bidi` known bugs & required mitigations (verified)

The package header self-declares **"UNDER CONSTRUCTION … API may change."** Load-bearing open bugs and mitigations:

- **E3 — `ReverseString` mis-associates combining marks (Go #50633):** `ReverseString("äu")` → wrong base for the mark. **Mitigation:** do **grapheme-cluster-aware** reversal (reverse clusters, keep each base+marks together) — never `bidi.ReverseString` on text with harakat. *This fixes an existing latent bug in `rtl.go`.*
- **E2 — Bracket pairs (N0) not matched (Go #72089):** `"ع a (b)"` splits the closing bracket into a separate run — hits `f(x)`, paths, calls. **Mitigation:** pre-segment technical tokens (R5).
- **E1 — Nested isolates reorder incorrectly (Go #69819):** don't rely on embedded `LRI/RLI/PDI` isolate controls. **Mitigation:** segment tokens ourselves (R5).
- **E4 — Runs return in logical order:** `Run.String()` is logical; the package reorders *runs* but leaves intra-run RTL glyph mirroring to us (do it grapheme-aware per E3).
- **Version:** tables are Unicode 17.0.0; base direction forcing via `DefaultDirection(RightToLeft)` is genuine (not just a fallback), but `DefaultDirection(LeftToRight)` does **not** force level 0 (first-strong still governs) — so for English-dominant lines, rely on first-strong.

## R9 — Terminal support matrix (informs the default)

BiDi/Arabic behavior (Reshapes = cursive joining; Reorders = runs UAX #9):

| Terminal | Reshapes | Reorders (UBA) | Note |
|---|---|---|---|
| Windows Terminal / conhost | No | No | Disconnected + logical order (MS #19076/#20302) — the reported environment |
| xterm, Alacritty, foot, Contour, kitty* | No | No | *kitty reverses word order only; `force_ltr` escape hatch |
| macOS Terminal.app | Partial | Partial (implicit LTR L1) | |
| iTerm2 (≥3.6.0, experimental) | Yes | Yes | "no mixed RTL/LTR" |
| VTE family (GNOME Terminal, Tilix, xfce4-terminal) | Yes (FriBidi) | Yes (implicit L1 + `?2501`) | reference terminal-wg impl |
| Konsole | Partial | Yes (implicit LTR L1) | |
| mlterm | Yes (FriBidi) | Yes (implicit L2) | classic BiDi terminal |

**Takeaway:** BiDi-agnostic terminals dominate (and include the primary Windows target), so **app-side visual shaping is the correct default**; treat terminal-native BiDi as the exception to suppress (R1/R7) or explicitly opt into (`native`).

## Cache neutrality (Constitution III, IV, VII)

**Finding:** Presentation-only. Arabic content rides the user message (dynamic, Principle IV); no RTL transform touches the system prompt, tool schemas, skills listing, or the project-context boot block that form the cached prefix. Therefore:

- The **stable prefix stays byte-identical** with or without Arabic — enforced by a new prefix-stability test that grows an Arabic transcript and asserts the prefix hash is unchanged (SC-006).
- The DeepSeek **Reasonix cache study (Principle VII)** is *not triggered*: no cache-affecting design is undertaken. Recorded here to satisfy the gate rather than skipped.
- NFC normalization (R6) applies only to model-bound content on the user message, never retroactively to settled prefix bytes.
- The BDSM/`CSI` control sequences (R7) are terminal-display negotiation on the TUI's output stream — they never enter the model request.

## Bottom line for the plan

1. Default = app-side visual shaping; emit `CSI 8 l` to suppress terminal BiDi; `native` = explicit opt-out; no auto-detection (R1/R7/R9).
2. Centralize one display pass; route **every** surface through it; implement right-alignment (`RTL.Align`).
3. Composer = **custom-render** RTL lines from the logical buffer with a mapped caret; **logical** editing/movement unchanged (R2).
4. Reverse **per RTL run, grapheme-aware** (fix the `ReverseString`+harakat bug); shape with LAM-ALEF ligature support (engine or extended table); wrap-then-shape; grapheme-aware width (R4/R8).
5. Pre-segment technical tokens as atomic LTR runs so paths/URLs/`code`/`f(x)` stay intact (R5/R8).
6. Copy resolves to logical via a per-line permutation map (R3); model-bound text is NFC logical (R6); prefix stays byte-identical (cache neutrality).
