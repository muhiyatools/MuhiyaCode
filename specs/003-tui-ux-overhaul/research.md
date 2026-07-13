# Phase 0 Research: MuhiyaCode TUI & Agent Experience Overhaul

**Feature**: `003-tui-ux-overhaul` | **Date**: 2026-07-12
**Sources**: full exploration of `F:\MuhiyaCode Agent Go` (TUI/orchestrator/contract) and `F:\MuhiyaWorkspace\MuhiyaWorkspace` (MuhiyaLLM gateway, api.muhiya.com); `.specify/memory/constitution.md`; feature 001 research (Reasonix reference study, Part A) and feature 002 determinism requirements; terminal TUI design guidance (tui-design craft process, Go ecosystem + visual patterns references).

## Part A — Current state (evidence)

### A1. Rendering stack

Bubble Tea v2 fork stack ([go.mod:5-16](../../go.mod)): `charm.land/bubbletea/v2 v2.0.8`, `charm.land/bubbles/v2 v2.1.1`, `charm.land/lipgloss/v2 v2.0.5`, `github.com/charmbracelet/x/ansi v0.11.7`, `go-runewidth`. **No markdown library** — rendering is hand-rolled in `internal/tui/render.go`. View composition: `View()` at `internal/tui/view.go:20-61` assembles header → viewport (transcript) → commands/notice/activity/usage-footer → input → mode line; `layout()` (`view.go:558-580`) recomputes viewport height by counting rendered lines of all other panes. Alt-screen with forced `BackgroundColor #0B100E` / `ForegroundColor #E7F0EB` (`view.go:58-59`). RTL support exists (`internal/tui/rtl.go`) and must be preserved.

### A2. The `**bold**` defect (FR-004)

`internal/tui/render.go:80`: `plain = strings.ReplaceAll(plain, "**", "")` — bold markers are **stripped, not styled**. `renderInline` (`render.go:186-200`) handles only backtick code spans. Consequences: `**bold**` → unstyled text; `*italic*`/`_x_`/`__x__` render **raw**; a lone `*` can flash mid-stream; table cells bypass inline handling entirely (`renderTable`, `render.go:117-184`) so `**`/backticks inside cells stay raw. Tables also collapse whitespace and hard-truncate cells (`render.go:158-161`).

### A3. Palette (FR-001)

`internal/tui/render.go:13-32`: 12 hardcoded truecolor hex styles (`brand #43D17D`, `text #E7F0EB`, `muted`, `faint`, `border`, `surface`, `surface2`, `warning`, `danger`, `add`, `remove`). **No semantic-token indirection, no light/dark adaptivity, no `NO_COLOR` honoring, no limited-color fallback.** `Settings.Theme` and `Settings.UI.BorderMode/Density` exist in `contract/types.go:59,69-72` but are never consulted.

### A4. Header (FR-002, FR-021, FR-022)

`renderHeader()` (`view.go:63-101`): line 1 = `◆ MuhiyaCode v… · model … agents … reasoning … · context …`; line 2 = **workspace basename only** (`filepath.Base`, `view.go:75`) + agent counter; then a full-width `─` rule. "Padding" is literal space characters; lines truncated via ANSI-aware `fitLine` (`view.go:701-706`). The label to rename is `faint.Render("agents")` at `view.go:72`. Resize floors are 40×14 (`model.go:242-243`).

### A5. Warp/terminal risk inventory (FR-003)

- Forced full-screen background fill + per-cell backgrounds (`view.go:58-59`, surface bands, modal) — alt-screen background repaint seams are a known Warp-class quirk.
- Full-width rule `strings.Repeat("─", m.width)` (`view.go:99`) assumes width-1 glyphs.
- Glyphs at risk of emoji-presentation width-2 rendering: `◆ ● × ▎ ▸ › └ •` + braille spinner (`view.go:18`). runewidth treats them as width-1; a terminal that disagrees desynchronizes `fitLine` and table math.
- No breakpoint ladder: single layout at all widths ≥40 cols.

### A6. Tool entries (FR-006..008)

`renderTool()` (`view.go:479-515`): marker (`●`/spinner/`×`) + `toolLabel` + target (capped width/2) + first-line summary (capped width/3). Collapsed diff preview shows **8 lines** + "… N more lines (Ctrl+O to expand)"; expanded (`m.verbose`, toggled at `model.go:401-403`) shows full diff/output. Labels map at `view.go:609`. Tool results are **plain strings** (`contract.Callbacks.ToolEnd(name, output string)`, `types.go:388-390`); diffs are embedded text after a literal `\n--- diff ---\n` marker (`diffLines`, `view.go:637-644`). **No lines-added/removed counts exist anywhere**; `TaskStats.FilesChanged` is paths only.

### A7. Thinking indicator + stats (FR-009..013)

- Thinking: live tail `▎ thinking… …` while busy; **after completion a persistent `▎ thought for <dur>` line remains** (`view.go:181-183`, `m.lastThoughtDuration` cleared only on next submit, `model.go:548-549`) — the exact residue FR-009 removes.
- Post-task footer (`renderUsageFooter` `view.go:111-125` + `taskSummary` `view.go:646-664`): duration · **effort** · **TaskClass** (the word "chat" = `ClassChat`, `orchestrator/classify.go:16`, stamped at `engine.go:556`) · billed tokens · tool count · cached/new breakdown · agents/files · session % — everything FR-010 removes. Rendered **above the input box**, not in the transcript (`view.go:46-49`) — FR-013 moves it.
- Per-task usage delta **already exists**: `TaskStats.Usage = subtractUsage(e.sessionUsage, usageStart)` (`engine.go:556`) — the correct aggregation base for FR-011/FR-012b/c.
- `contract.Usage` (`types.go:159-171`) has `CacheReadTokens`/`CacheMissTokens *int` (nil = unavailable) — matches FR-012 omission semantics. **No cost/credits field exists in `contract.Usage`, `UsageRecord`, or anywhere in the repo.**

### A8. Input area & user messages (FR-014..016)

Placeholder: `"Ask MuhiyaCode to inspect, build, fix, or explain…"` (`model.go:179`). Hint line: `Enter send · Esc stop/back · Tab agents · Ctrl+O details · Shift+Tab mode · Ctrl+P commands` (`view.go:233`). User messages carry a `> ` brandSoft prefix (`view.go:440`) plus a `surface` background band — FR-015 drops the prefix, keeps the band. Web-search provider: **no leak exists today** — the gateway formats results provider-agnostically (`internal/gateway/web.go:153-184`); FR-016 is a guard to keep it that way.

### A9. Commands, auth, context modal (FR-017..020)

Command list at `model.go:164-171` (17 commands, incl. `/login`, `/logout`; **no `/usage`**). `/login` opens a secret modal → `Actions.SetAPIKey`; `/logout` → `Actions.Logout` (`actions.go:77-90`); key stored as `contract.Secrets.ProviderAPIKey`, persisted user-only. `/context` opens an info modal fed by `formatContextReport` (`actions.go:346-383`) over `orchestrator.ContextReport` (`engine.go:456-476`) — dense prose; no credits.

### A10. Skills delivery (FR-023..025)

- Discovery: `workspace.DiscoverSkills` (`skills.go:40-92`) — env-dependent roots, dedupe by name, cap 40, **stable sort by lowercased name**.
- Advertisement to the model: **none.** The system prompt (`orchestrator/prompt.go:26-71`) is deliberately session-invariant and contains no skill content. Skills are used only when the user manually multi-selects via `/skills` (`actions.go:111-116, 470-491`); `listSkills` (`command/application.go:744-761`) **eagerly loads every skill's full instructions** on `/skills` open; selected skills are serialized as `<skill name="…">…</skill>` and prepended to that one user turn (`model.go:560-576`), then cleared.
- Cache posture: current mechanism never touches the stable prefix — zero cache risk, but also zero model awareness (fails FR-023/FR-025's "agent recognizes when a skill applies").

### A11. Prefix/cache guards this feature must respect

`PrefixShape` hashes system+tools+tool_choice+settled history+model (`prefixshape.go:19-102`). Guard tests: `prompt_stability_test.go` (byte-identical `SystemPrompt` across constructions; date only in task brief; MCP block order-stable), `cachehit_guard_test.go` (6 scenarios), `marshal_determinism_test.go`, `restart_determinism_test.go`. Any skills-listing change to the system prompt must keep these green (with updated fixtures) and pass the offline cache guard + live benchmark (feature 001 harness, `benchmarks/cachebench`).

### A12. Gateway (MuhiyaLLM, api.muhiya.com) — wire facts

Go 1.22 stdlib gateway; Postgres ledger; routes in `main.go:418-435`; admin API under `/api/*` (HTTP Basic, admin-only).

- **Auth**: virtual keys `sk-virt-…`, presented as `Authorization: Bearer` or `x-api-key` (`proxy/handler.go:371-399`); only `sha256` stored; key → user → plan → budget windows (+ `user_topups`).
- **Per-request cost**: computed for every request — `calculateCost(model, input, output, cacheRead, cacheWrite)` (`handler.go:1702-1717`), persisted to `request_logs` (`db/db.go:1025-1047`) with `virtual_key_id, user_id, model_id, input/output/cache_read/cache_write tokens, cost (USD), usage_estimated, created_at`. **Not emitted to normal clients.** The only wire surface is a non-standard final SSE chunk `{"usage":{…},"muhiya_log":{"cost":<USD>,"log_id":"…"}}` sent **only when `X-Client-App` resolves to "MuhiyaChat"** (`sendMuhiyaMetaChunk`, `handler.go:1883-1910`; gate `getClientAppName`, `handler.go:144-187`). MuhiyaCode already sends `X-Client-App: MuhiyaCode` on every request (`internal/gateway/provider.go:146,315`, `web.go:150`).
- **Credits semantics**: **1 credit = US$0.01** (`db/db.go:1170`; plan-simulator comment). Two tiers: plan **budget windows in USD** (sliding, anchored at `plan_assigned_at`) and **top-up extra credits** consumed only after a window is exceeded (FIFO, `DeductExtraCreditsIfExceeded`, `db/db.go:1126-1219`). User-facing fields: `extra_credits`, `remaining_extra_credits` (`db/db.go:40-41`).
- **Account usage endpoints**: **no self-service (key-authenticated) endpoint exists.** All usage/balance data is admin-Basic-Auth only (`GET /api/users?id=…` incl. `budget_usage[]`; `GET /api/logs`; `GET /api/stats`). Building blocks for a new endpoint already exist: `authenticateVirtualKey`, `db.GetUser`, `db.GetUserBudgetUsage`, `db.GetRemainingExtraCredits`, `db.GetUserSpendingInWindow`.
- **Cache fields**: gateway normalizes OpenAI `prompt_tokens_details.cached_tokens`, DeepSeek `prompt_cache_hit/miss_tokens`, Anthropic `cache_read_input_tokens` across dialects (`translator.go:74-105, 257-262, 623-698`).
- **Errors**: OpenAI-style `{"error":{message,type,code}}`; 429 `rate_limit_error` with no rate-limit headers.

### A13. Reasonix reference (Constitution VII)

The DeepSeek Reasonix reference study is recorded in [001 research.md Part A](../001-prompt-cache-optimization/research.md) (why it reaches ~99% hits: strict static-prefix/dynamic-suffix separation, append-only history) and operationalized by feature 002's FR-013 (all prompt-contributing content — including **skill definitions** — must render deterministically: stable ordering, no timestamps, byte-identical for identical configuration). This feature's only prompt-affecting change (D12 skills listing) is designed directly against those findings: static, sorted, session-pinned content in the prefix; per-use material on the user turn. No new Reasonix study is required; deviations: none.

## Part B — Decisions

### D1: Per-request credits arrive via the gateway meta chunk, gated to MuhiyaCode

- **Decision**: Extend the gateway's `sendMuhiyaMetaChunk` client-app gate (`handler.go:1883-1910`) from `"MuhiyaChat"` to an explicit allowlist `{"MuhiyaChat","MuhiyaCode"}`, and add `"usage_estimated": bool` to the `muhiya_log` object (mirroring `request_logs.usage_estimated`). MuhiyaCode parses the chunk in `internal/gateway/provider.go` streaming path, carries `CostUSD *float64` + `CostEstimated bool` on `contract.Usage`/`contract.UsageRecord`, and sums per task via the existing `subtractUsage` delta. Display unit: credits = USD × 100 (2 decimals).
- **Rationale**: Satisfies clarified FR-012a (per-request gateway usage payload, never estimated) with the smallest possible change on both sides — MuhiyaCode already streams and already sends `X-Client-App: MuhiyaCode`; the gateway already computes exact cost per request. Pointer field preserves the "nil = unavailable → omit" rule (FR-012). `usage_estimated` keeps Constitution VI honesty: estimated rows are surfaced (dimmed "~" marker) or omitted, never silently presented as measured.
- **Alternatives considered**: (a) account-balance diff around each task — rejected in `/speckit-clarify` (concurrent-usage skew); (b) local price table — rejected (estimate; violates FR-012a and Constitution VI); (c) response headers instead of SSE chunk — rejected: headers don't work mid-stream and the chunk mechanism already exists; (d) matching by `Muhiya` prefix — rejected for an explicit allowlist (no accidental grants to future clients).

### D2: New key-authenticated `GET /v1/usage` gateway endpoint powers `/usage`

- **Decision**: Add one self-service endpoint to the gateway: `GET /v1/usage` (also `/usage`), authenticated by the caller's own virtual key via `authenticateVirtualKey`, returning plan budget windows (with `current_spent`/`reset_time`), extra-credit totals, and spend-today (UTC calendar day) — composed from existing `db` methods (`GetUser`, `GetUserBudgetUsage`, `GetRemainingExtraCredits`, plus one new day-bounded `SUM(cost)` query). Wire shape in [contracts/usage-api.md](contracts/usage-api.md). MuhiyaCode gains a `/usage` command + client (`internal/gateway`) that renders it per FR-018 and hides behind sign-in.
- **Rationale**: Today only admin Basic-Auth can read usage — shipping admin credentials in the TUI is unacceptable (Constitution IX security posture). The endpoint reuses four existing DB methods; "session credits" never needs the server (client sums D1 chunks). Placing it under `/v1/*` puts it behind the existing proxy middleware (CORS, auth) with zero new auth machinery.
- **Alternatives considered**: (a) TUI calls admin `/api/users?id=` — rejected: requires distributing admin credentials, breaks least-privilege; (b) no endpoint, show session-only data — rejected: fails clarified FR-018 (today + billing period); (c) computing "today" client-side from `/api/logs` — rejected: needs admin auth and pages of log rows for one scalar.

### D3: Fix markdown by finishing the hand-rolled inline renderer (no glamour)

- **Decision**: Replace the `**`-strip (`render.go:80`) with a proper inline tokenizer in `render.go`: spans for `**bold**`, `*italic*`/`_italic_`, `__bold__`, `` `code` ``, `~~strike~~`, applied via palette styles; make `renderTable` run the same inline pass per cell; handle the streaming half-token case (unclosed marker renders as plain text, no flash); keep headings/bullets/fences. Add a regression corpus test (golden) covering bold/italic/code in paragraphs, lists, tables, wrapped lines, and RTL text.
- **Rationale**: Constitution VIII (improve, don't rewrite): the renderer already handles blocks, width, RTL (`rtl.go`), and streaming re-render; only inline emphasis is missing. Glamour would be a new dependency (IX requires justification), brings its own theme system that fights the semantic palette (D4), is not RTL-aware, and re-parses the full document per streaming token more expensively. The defect is 1 missing tokenizer, not a broken architecture.
- **Alternatives considered**: (a) adopt `glamour` — rejected per above (dependency, theming conflict, RTL, streaming cost); (b) strip all markers uniformly ("plain but clean") — rejected: fails FR-004/SC-002, loses meaning the model intended; (c) full CommonMark library — rejected: over-scope for terminal rendering, same objections as (a).

### D4: Semantic token palette with light/dark adaptivity and graceful degradation

- **Decision**: Rebuild `newPalette()` into a **semantic token** system (`text.primary/muted/faint`, `accent.primary/soft`, `status.success/warning/error/info`, `diff.add/remove`, `bg.base/surface/overlay`, `border.default/focus`) mapped to two tuned palettes (dark = refined current greens; light = new) selected via `lipgloss.HasDarkBackground`/`LightDark` at startup, honoring `Settings.Theme` (`auto|dark|light`) as override. Honor `NO_COLOR` (drop to styling-only: bold/dim/reverse). Rely on `colorprofile` downsampling for 256/16-color terminals but verify the 16-color mapping of every token. **Stop forcing the full-screen `BackgroundColor`** (`view.go:58-59`) in favor of terminal-native background, keeping `bg.surface` bands only where they carry meaning (user messages, modal) — this removes the Warp repaint-seam class of artifacts and respects user themes.
- **Rationale**: FR-001 (coherent, readable on dark+light, degrades gracefully); tui-design guidance (semantic tokens, "the user's terminal theme is sacred", never color-alone). Dropping the forced background is the single highest-leverage Warp fix (A5) and reduces chrome. Identity is preserved through the green accent family + `◆` brand mark, not through owning every cell.
- **Alternatives considered**: (a) keep forced dark background, tune colors — rejected: perpetuates Warp seams, unreadable on light terminals, fights user themes; (b) full theme-file system (Catppuccin & co.) — deferred: valuable but out of scope for this feature's trust goal; the token indirection makes it a follow-up config file, not a code change; (c) ANSI-16-only palette — rejected: wastes truecolor polish available in target terminals.

### D5: One glyph set, width-safe, with ASCII fallback

- **Decision**: Define a single `glyphs` table used by all panes: marker set restricted to box-drawing + simple geometric glyphs verified width-1 across Warp/Windows Terminal/iTerm2 (`●`, `○`, `─`, `│`, `╭╮╰╯`, `▏`, `·`, `…`, `+`, `-`); replace risk glyphs (`◆` brand mark → `▪` or keep `◆` behind the table after live verification; `▎`/`▸`/`›` normalized to table entries); braille spinner kept (verified safe). Provide `MUHIYA_ASCII=1`/`Settings.UI.BorderMode=ascii` fallback mapping. All width math continues through `x/ansi`/`fitLine`.
- **Rationale**: FR-003; A5 risk inventory — Warp's emoji-presentation of some geometric shapes breaks column math. Centralizing glyphs makes the Warp verification a checklist over one table instead of a hunt through render code, and gives the ASCII fallback for free.
- **Alternatives considered**: (a) Nerd Font icons — rejected: no detection exists, opt-in only, off-brand for a trust-focused default; (b) per-call-site fixes — rejected: unauditable, regressions guaranteed.

### D6: Tool display contract — structured summary line + uniform expansion

- **Decision**: Introduce a small display-contract layer in the TUI: `toolDisplay{label, target, outcome}` computed per tool from existing string outputs. Collapsed = exactly one line: state marker + label + target + **outcome measure** (Edit/Write/Patch: `+A −R` computed by counting `+`/`-` lines in the existing `--- diff ---` payload (`diffLines`, `view.go:637-644`); Read: line count; Grep/Glob/Search: match count; Shell: exit status; Delegate: subagent title; Web search: result count). Expanded (Ctrl+O) = same header + full detail in one uniform block style (diff colorized, output boxed, results listed), viewport-scrollable, still capped by the existing 8 KB output cap. `m.verbose` remains the single global toggle. Per-tool rules tabulated in [contracts/tool-display.md](contracts/tool-display.md).
- **Rationale**: FR-006..008. The data already flows as strings; counting diff signs at render time needs no `contract` change (Constitution VIII — smallest change; the classifier at `view.go:517-530` already distinguishes these lines). A uniform expansion pattern is what makes the transcript feel disciplined (spec US3).
- **Alternatives considered**: (a) structured tool-result types through `contract` — rejected for this feature: touches the orchestrator/tool layer and its tests for a display concern (revisit if outcome parsing proves brittle); (b) per-entry expand/collapse keys — rejected: global Ctrl+O is the established, simpler model (FR-008 asks for consistent toggle, not per-entry state).

### D7: Thinking indicator is strictly ephemeral

- **Decision**: Keep the live `▏ thinking… <tail>` row (+ elapsed time; gutter glyph from the D5 table) while reasoning streams; on task completion remove the row entirely — delete the `lastThoughtDuration` render branch (`view.go:181-183`) and its state carry-over. Thinking duration is not shown anywhere post-task.
- **Rationale**: FR-009 verbatim; the clarified spec (US2) wants zero residue. The live indicator already satisfies "show elapsed" guidance for waits; post-hoc duration is noise the user explicitly rejected.
- **Alternatives considered**: fade-out after N seconds — rejected: still residue, adds timer complexity for content the user said to remove.

### D8: Task summary — a one-line transcript entry with exactly three metrics

- **Decision**: Replace `renderUsageFooter`/`taskSummary` with a transcript-resident summary entry appended after the final assistant message on `TaskComplete` (FR-013): `credits <C> · <T> tokens · cache <H>%`, plus a dimmed `interrupted` marker per FR-013a driven by a **new `TaskStats.StopCause` field** the engine stamps on the user-stop/error/disconnect paths (the existing `TerminatedReason` is H5-breaker-only and keeps its separate notice — see data-model §1.3). Formulas: tokens and cache % from the existing per-task delta `TaskStats.Usage` — tokens = `TotalTokens` (all reported input incl. cache reads + output, FR-012c); cache % = `CacheRead / (CacheRead + CacheMiss)` token-weighted (FR-012b), omitted when either pointer is nil (FR-012); **credits from a per-task usage-record range scan** (boundary captured at task start; member-set and nil rules in data-model §1.1 — a costless record affects only its own task, never later ones), `~`-marked when any member is `CostEstimated`. Remove effort, TaskClass ("chat"), tool counts, cached/new breakdown, session %, invalidation notes from the default UI (invalidation/session diagnostics move to `/context`, D10).
- **Rationale**: FR-010..013 + clarifications; `TaskStats.Usage` is already the correct per-task aggregate (A7), so this is a display + one-field (`CostUSD`) change, honest per Constitution VI (provider-reported only, omission over estimation, whole picture available in `/context`).
- **Alternatives considered**: (a) keep footer position with fewer fields — rejected: FR-013 explicitly moves it after the latest message; (b) richer summary with expandable detail — rejected: `/context` already serves depth; the spec demands exactly three metrics (SC-003).

### D9: Header — full path, "subagent", tighter composition

- **Decision**: Line 1 keeps `◆ MuhiyaCode v… · <model> · Subagent <sub> · reasoning <effort> · context NN%` with the label at `view.go:72` renamed to the literal `Subagent` (FR-022/US7 exact string; the user's request specified this capitalization). Line 2 shows the **full workspace path**, left-truncated with a leading `…` only when width requires (FR-021), styled `text.muted`. Consistent single-space gutter via one lipgloss style (not literal spaces); rule kept, drawn in `border.default`. At <80 cols: drop `reasoning` then `subagent` segments before truncating the path (breakpoint ladder, D13).
- **Rationale**: FR-002/021/022; trust = the agent shows exactly where it operates. Left-truncation preserves the most specific (rightmost) segments per spec edge case.
- **Alternatives considered**: two-line collapse into one — rejected: line 2 also carries agent status; cramming both misaligns at standard widths.

### D10: Context modal — grouped sections + session credits

- **Decision**: Restructure `formatContextReport` output into labeled groups — **Context** (history tokens/limit/%, pressure), **This session** (prompt/output totals, cache read/uncached, session + steady-state hit rates, **credits used this session** = Σ session `CostUSD` × 100), **Streams** (main/aux/subagent request counts), **Cache health** (prefix stability, last invalidations w/ causes) — rendered with the D4 tokens (group titles `accent`, values `text.primary`, labels `muted`), keeping every existing datum (FR-017). Diagnostics removed from the task summary (D8) live here.
- **Rationale**: FR-017; the modal already has all data (A9) — this is organization + one derived credits sum from D1's records.
- **Alternatives considered**: full-screen context dashboard — rejected: modal is established UX; scope discipline.

### D11: `/usage` command + conditional `/login` visibility

- **Decision**: Add `/usage` to the command list, calling D2's endpoint via a new `Actions.FetchUsage`; render a grouped modal (Plan windows w/ remaining+reset · Extra credits · Spend: session/today/billing period; credits + USD dual display). Filter the command palette: hide `/login` when `Secrets.ProviderAPIKey` is set, hide `/usage`+`/logout` when it is not (FR-019); direct `/usage` invocation while signed out explains sign-in (edge case). Timeout 5s; friendly error states per FR-020 (unreachable, 401 invalid key, malformed).
- **Rationale**: FR-018..020; command visibility is a pure palette filter over existing state; no auth flow changes (Constitution IX security surface untouched — the key is only ever sent as the existing bearer header).
- **Alternatives considered**: status-bar credits ticker — rejected: adds chrome the overhaul removes; `/usage` + per-task credits cover the need.

### D12: Skills — deterministic compact listing in the stable prefix, full text on use via `read_file`

- **Decision**: Three coordinated changes. (1) **Advertise**: append a deterministic `## Skills` section to the system prompt (`prompt.go`) — discovered once at new-session creation, session-pinned, **persisted with the session and restored verbatim on `/resume`/restart** (001 ProbeSnapshot precedent — prefix stays byte-identical across save/load), sorted by name, one line per skill: `- <name> (<workspace-relative SKILL.md path>): <single-line description>` (fixed no-description form for frontmatter-less skills), plus two fixed guidance sentences: use `read_file` on the skill's path when the task matches its description; ignore skills otherwise. **Advertised skills are workspace-resident only** (`.agents/skills`, `.codex/skills`) so `read_file` stays inside the permission-guard containment — external roots stay reachable via `/skills` manual selection only. Cap at the existing 40-skill limit; identical configuration ⇒ byte-identical section (002 FR-013). (2) **On-demand loading**: the model reads SKILL.md via the existing `read_file` tool when relevant — no new tool, no eager injection; the read is a visible, auditable transcript entry. `/skills` manual selection is kept as explicit override but `listSkills` becomes lazy (load instructions only for *selected* skills at submit, not all 40 on modal open). (3) **Determinism guards**: extend `prompt_stability_test.go` and `restart_determinism_test.go` with skills fixtures (byte-identical across constructions and across save/reload; ordering stable; no env-variance within a session) and run the offline cache guard + live before/after benchmark (Constitution X) since this grows the stable prefix.
- **Rationale**: FR-023..025 + FR-027. Reasonix findings (A13): static config belongs in the stable prefix; per-use material on the turn. The listing *is* configuration (Constitution III allows prefix content deterministic "given identical configuration"); it changes only between sessions, like MCP tool sets already do (002 D1 precedent). Reusing `read_file` is the "just works, nothing more" the user asked for: zero prompt machinery, zero new tools, natural transcript visibility, and the existing duplicate-read blocking (Constitution V) prevents re-fetch churn. Prompt growth = ~1 line/skill (compact listing only) — FR-024's exact bound.
- **Alternatives considered**: (a) a dedicated `use_skill` tool — rejected: grows the tool schema (prefix) *and* adds machinery `read_file` already provides; (b) per-turn skill listing on the user message — rejected: retransmits the listing every turn (Constitution V) and hides skills from the model between turns; (c) semantic auto-injection of full skill bodies — rejected: prompt bloat, wrong-skill risk, violates FR-024; (d) status quo (manual `/skills` only) — rejected: fails FR-023/FR-025 (model never recognizes applicable skills).

### D13: Responsiveness — breakpoint ladder with an 80×24 design floor

- **Decision**: Adopt the ladder: **>120 cols** full layout; **80–120** baseline (current layout, D9 header intact); **60–80** drop optional header segments, tool `summary` column, table low-priority columns (tables already shrink-to-fit); **<60 or <20 rows** render a clean "terminal too small (min 60×20)" pane instead of layout collapse. Keep hard floors but raise render floor from 40×14. Resize re-layout is already per-Update (`model.go:242`, `layout()`); add a teatest golden at 80×24 and 60×20 to pin degradation. Test matrix: Warp, Windows Terminal, iTerm2, VS Code terminal at 80/120/200 cols (SC-001), scripted via the quickstart.
- **Rationale**: FR-003; tui-design "pressure-test the floor" — the current single-layout-at-all-widths is exactly the unfinished state the guidance flags. The transcript-centric layout degrades well (drill-down-like), so the ladder is mostly *removal* rules, not new layouts.
- **Alternatives considered**: reflowing multi-pane redesign — rejected: MuhiyaCode is a single-column conversation app; panes would fight the transcript's primacy and the constitution's improve-don't-rewrite.

### D14: Verification strategy (Constitution VI, X + SC-001..010)

- **Decision**: Four layers. (1) **Unit/golden**: markdown corpus goldens (D3), frame goldens pinned to 80×24 + `colorprofile.Ascii` for the header (incl. deep-path fixture)/tool-line/summary/breakpoints — via `x/exp/teatest/v2` + `x/exp/golden` (test-only additions; if no release compatible with the `charm.land` module rename exists, fall back to stdlib golden comparison of `View()` output, same obligations); update-layer tests for Ctrl+O, palette filtering, summary lifecycle (busy → complete → interrupted via `StopCause`). (2) **Prefix stability**: extended `prompt_stability_test.go` + `restart_determinism_test.go` + existing cachehit guards green (D12). (3) **Live before/after benchmark** on the 001 `benchmarks/cachebench` harness — identical fixture workload, model, gateway, effort — asserting session hit-rate non-regression and prompt growth ≤ skills listing size (FR-027/SC-008), plus credits-sum agreement: Σ `muhiya_log.cost` vs gateway `request_logs` for the run (SC-004). (4) **Manual matrix**: quickstart scenario walk on the three terminals incl. light theme + `NO_COLOR` (SC-001/006), `/usage` failure drills (SC-007), skills trial 10 matching + 5 non-matching tasks (SC-009), and a **qualitative review protocol for SC-010**: ≥5 testers run a fixed walkthrough script and answer one 5-point "polished and trustworthy" item — pass = ≥80% top-two-box AND zero pre-overhaul complaints reproduced; responses stored with the feature's benchmarks.
- **Rationale**: Constitution X demands realistic before/after evidence for the one improvement claim with cache impact (D12); everything visual is pinned by goldens so later refactors can't silently regress the overhaul.
- **Alternatives considered**: VHS-based visual regression — noted as optional demo tooling, not gating (heavier CI footprint; teatest goldens suffice for regressions).

## Part C — Resolution of Technical Context unknowns

| Unknown | Resolution |
|---|---|
| Gateway per-request cost field | Does not exist for normal clients; exists as `muhiya_log.cost` (USD) SSE chunk for allowlisted client apps → D1 extends allowlist to MuhiyaCode + adds `usage_estimated` |
| Credits unit | 1 credit = US$0.01; top-ups in credits, plan budgets in USD (A12) → display converts ×100 |
| Account usage endpoint | None self-service today → D2 adds `GET /v1/usage` (key-authenticated) |
| "Today"/"billing period" semantics | Today = UTC calendar day `SUM(cost)`; billing period = plan's monthly sliding window (`current_spent`/`reset_time` from `GetUserBudgetUsage`) |
| Markdown defect root cause | `render.go:80` strips `**`; no inline emphasis path; tables skip inline entirely → D3 |
| Warp breakage causes | Forced background fill + width-2-risk glyphs + no breakpoints (A5) → D4/D5/D13 |
| Lines added/removed source | Parse existing `--- diff ---` payload at render time → D6 |
| Per-task aggregation base | Already exists (`subtractUsage` delta, `engine.go:556`) → D8 reuses it |
| Skills cache-safe advertisement | Deterministic session-pinned system-prompt listing + `read_file` on demand → D12, guarded by D14 |
