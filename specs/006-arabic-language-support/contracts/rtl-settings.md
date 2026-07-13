# Contract: RTL Settings & Diagnostics

Covers the user-facing controls for Arabic/RTL behavior and the diagnostic surface, extending what already exists rather than redefining it.

## Settings (persisted, existing schema — `settings.json` → `RTL`)

| Key | Values | Default | Meaning |
|-----|--------|---------|---------|
| `rtlMode` | `auto` \| `visual` \| `native` \| `off` | `auto` | How RTL is rendered (see rtl-render.md). `auto` picks `visual` or `native` from terminal capability. |
| `rtlAlign` | `auto` \| `right` \| `left` | `auto` | Block alignment for RTL content. **New:** this value is now actually applied (previously stored but unused). |

**MUST**:
- Validation membership rules are unchanged (`internal/state/config.go`).
- Setting either value via `config set rtlMode …` / `config set rtlAlign …` takes effect on the next render without a restart and without disturbing an in-flight task. (FR-020.)
- Defaults preserve today's behavior for existing users except that RTL content now also becomes right-aligned under `auto` (an intended fix, documented in the changelog/docs).
- No setting value causes a crash, double-reversal, or corruption on any terminal. (FR-019, FR-018.)

## Terminal BiDi negotiation (no runtime detection)

Runtime capability detection is unreliable (terminal-wg autodetection is unresolved; `DECRQM` mostly unanswered), so instead of detecting, MuhiyaCode **negotiates**:

- In `auto`/`visual` (app owns BiDi): emit **BDSM explicit** `CSI 8 l` at startup (restore `CSI 8 h` on exit) so BiDi-capable terminals (VTE, mlterm, Konsole, Terminal.app, iTerm2-experimental) don't re-run the algorithm on already-visual output — preventing double-reversal. Ignored by BiDi-agnostic terminals, so it is safe everywhere.
- In `native` (terminal owns BiDi): emit **BDSM implicit** `CSI 8 h` and do NO app-side reordering, emitting logical text.
- These control sequences are terminal-display negotiation only; they MUST NOT enter the model request or the stable prefix.
- The default (`auto`) is app-side visual shaping because the majority of terminals — including the Windows target — are BiDi-agnostic (see research.md R9).

## Diagnostics (`muhiyacode doctor` — existing surface, extended)

The existing RTL diagnostics block (`internal/command/root.go`) MUST:
- Print the active `mode` and `align`.
- Render a representative **mixed Arabic+English+number+path** sample so a user can eyeball shaping, order, and alignment on their terminal.
- **New:** show a copy round-trip check — the logical text recovered from a shaped sample equals the original — so users can confirm clipboard fidelity. (FR-015.)
- **New (optional):** report the detected terminal BiDi capability and which mode `auto` resolved to.

## Optional interface locale (User Story 6 — stretch)

- If implemented, an interface-locale control (e.g., `config set uiLocale ar|en`, default `en`) switches chrome strings to Arabic, rendered through the same display pass. English remains default and always available. (FR-022.)
- MUST NOT alter any model-bound content or the stable prefix.
