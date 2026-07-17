# Contract: Wiring Inventory — Everything Advertised Works or Is Removed

Feature 010 (US5; FR-015..016). Evidence: [research.md](../research.md) R5;
shape in [data-model.md](../data-model.md) §5.

## 1. The inventory

| ID | Requirement |
|---|---|
| WI-1 | A checked-in inventory enumerates the ENTIRE advertised surface: 12 registry tools (+ conditional web_search + dynamic MCP), 6 synthetic tools, every slash command and alias, every keybinding, every Settings field, every AgentEvent kind, every Callbacks member |
| WI-2 | Every entry has `Verified-by` (test:line or a named manual check) and `Status` ∈ {wired, removed} — no third status may exist (FR-015) |
| WI-3 | Guard tests enumerate the LIVE surface at runtime (Registry.Names, synthetic names, palette + runSlash cases, handleKey cases, Settings field paths, event kinds, callback members) and fail when live and inventory diverge in EITHER direction |
| WI-4 | Tool descriptions match real behavior and real limits exactly (FR-016); conditional surfaces record their gate (web_search's probe; `--simple`-mode "unavailable" commands name WHICH interface verifies them) |

## 2. Known resolutions (from R5, must land as wired or removed)

| ID | Requirement |
|---|---|
| WI-5 | `Settings.UI.Density`: wire an observable rendering effect or remove the field and its config key (currently plumbed — default/validate/set — with zero renderer consumers) |
| WI-6 | `AgentEvent.CallID/Turns/ToolCalls`: remove (no consumer) or wire a consumer; `AgentEvent.Handoff` stays with its delegation-benchmark consumer recorded as its Verified-by |
| WI-7 | The grep invalid-pattern guidance path and the per-command/per-keybinding UNVERIFIED cells from the R5 matrix each gain an exercising test or a manual-check entry |

## Acceptance

- SC-005: 100% of entries verified-working or removed; zero unresolved.
- The guard tests are part of `go test ./...` (a new tool/command/keybinding
  without an inventory entry fails CI the moment it appears).
