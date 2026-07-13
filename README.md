# MuhiyaCode

MuhiyaCode is a production-oriented terminal coding agent written entirely in Go. It combines a responsive TUI, guarded workspace tools, durable sessions, MCP integrations, and a token-efficient agent runtime in one self-contained binary.

This repository is the Go rebuild of the original TypeScript/Bun product. The binary does not import, execute, bundle, or require TypeScript, Bun, Node.js, or npm. Existing user state under `~/.muhiya` remains the compatibility boundary.

## Highlights

- Polished Bubble Tea v2 interface with streaming answers, flat tool timelines, live diffs, real plans, subagent views, modal approvals, command search, narrow-terminal layout, non-TTY line mode, and Arabic/RTL shaping.
- OpenAI-compatible streamed chat completions with virtual model IDs, discovery, provider-specific reasoning profiles, retries, idle cancellation, usage/cache accounting, `<think>` separation, and DSML/plain-JSON tool-call rescue.
- Guarded local tools for listing, reading, grep/search, globbing, exact edits, tolerant atomic multi-edit, full writes, unified patches, shell commands, Git status/diff, checkpoints, and rewind.
- Durable SQLite sessions and JSON session bundles with history folding, structured compaction, inspected-range memory, shared agent knowledge, transcript redaction, and trusted workspaces.
- Four effort levels (`low`, `medium`, `high`, `max`) that bound turns, tools, reasoning, verification, context retention, plan gates, and subagent runs.
- Built-in `explore`, `plan`, `review`, and `general` subagents. Independent subagent calls can run concurrently at higher effort; budgets and tool restrictions are enforced in code.
- stdio and Streamable HTTP MCP, OAuth with localhost state validation and refresh persistence, gateway web search, and installed skill discovery.
- CGO-free builds through `modernc.org/sqlite`; release archives target Windows, Linux, and macOS on amd64 and arm64.

## Install

Download the archive for your platform from GitHub Releases, verify it against `checksums.txt`, and place `muhiyacode` on `PATH`.

PowerShell installer:

```powershell
irm https://raw.githubusercontent.com/muhiya/muhiyacode/main/scripts/install.ps1 | iex
```

Linux/macOS installer:

```sh
curl -fsSL https://raw.githubusercontent.com/muhiya/muhiyacode/main/scripts/install.sh | sh
```

Or build from source with Go 1.25+:

```sh
go build -trimpath -o muhiyacode ./cmd/muhiyacode
```

## Quick start

MuhiyaCode defaults to the hosted MuhiyaLLM gateway at `https://api.muhiya.com/v1`, so a fresh install only needs an API key:

```sh
muhiyacode config set apiKey YOUR_API_KEY
muhiyacode config discover
muhiyacode
```

To point at a different OpenAI-compatible endpoint instead (for example a self-hosted MuhiyaLLM gateway), set `baseUrl` first:

```sh
muhiyacode config set baseUrl https://your-gateway.example/v1
muhiyacode config set apiKey YOUR_API_KEY
muhiyacode config discover
muhiyacode
```

If the endpoint does not expose `/models`, configure a virtual model ID and its real context limit:

```sh
muhiyacode config set model your-model-id
muhiyacode config set contextLimit 128000
```

The `model` sent upstream is always the configured virtual ID, never its display label. A separately configurable subagent model can be selected with `config set subagentModel <id>` or `/model`.

## Commands

```text
muhiyacode [prompt...]                 Open the workspace TUI, optionally running a prompt
muhiyacode -p "prompt"                Run one prompt and print the final answer
muhiyacode --new                       Start a fresh workspace session
muhiyacode --simple                    Use the line UI
muhiyacode resume <session-id>         Resume a stored session
muhiyacode sessions [--all]            List sessions
muhiyacode config                      Show redacted configuration
muhiyacode config set <key> <value>    Update provider, model, effort, UI, or permission config
muhiyacode config discover             Discover and assign endpoint models
muhiyacode config path                 Print the settings path
muhiyacode mcp ...                     List/add/remove/authorize MCP servers
muhiyacode doctor [--offline]          Validate shell, configuration, streaming, and tool calls
muhiyacode doctor rtl                  Compare native and visual Arabic rendering
```

The original shorthand remains supported:

```sh
muhiyacode config set https://gateway.example/v1 API_KEY optional-model-id
```

### TUI controls

| Key | Action |
|---|---|
| Enter | Send; while running, queue a steering message |
| Esc | Clear a text selection, leave agent view, stop the task, or clear input |
| Tab | Complete a slash command or cycle agent views |
| Alt+1..9 | Jump to a subagent view |
| Ctrl+P | Open command search |
| Ctrl+S | Open session search |
| Ctrl+C | Copy the current selection (if any), else stop the task / clear input / exit |
| Shift+Tab | Toggle normal/auto-accept permission mode |
| Page Up/Down / ↑ ↓ | Scroll the transcript |

| Mouse | Action |
|---|---|
| Wheel over transcript | Scroll (scrolling up mid-stream stops auto-follow; scroll back to resume) |
| Wheel over command menu / modal | Move the highlighted row |
| Click a command-autocomplete row | Run that command |
| Click a modal / MCP menu choice | Select and confirm it (same as arrow + Enter) |
| Click a tool row | Expand or collapse just that tool |
| Click a subagent chip | Open that subagent's view |
| Drag over transcript text | Select; Ctrl+C copies via OSC 52 |
| Click the composer | Focus the input |
| Hover a command, modal, tool, or subagent row | Highlight it — a hovered tool/subagent underlines to show it's clickable |

Tab switches between the main session and running subagent views at any time, including mid-task. The transcript holds only your messages and the agent's replies and tool activity. System results (settings changes, MCP status, errors) appear on a transient notice line above the input, and the active permission mode is shown directly below it. While a task runs, one unified activity line above the input shows the status, elapsed time, tokens, live thinking, and plan progress; the thinking indicator disappears entirely once the task completes. After each task, a calm one-line summary is appended to the transcript showing exactly three metrics — credits consumed, total tokens, and cache-hit rate — computed across that task's turns. Tool activity is one line each by default (name, file path, and a minimal outcome such as `+A −B` for edits — for Write/Edit/Patch the file path always shows before the counts); click a tool row to expand it.

The interface is mouse-native: command autocomplete, the MCP manager, confirmation dialogs, and tool/subagent chips are all clickable, and dragging over transcript text selects it (Ctrl+C copies through OSC 52). Every mouse action has a keyboard equivalent, so nothing requires a pointer. Because the app captures the mouse for this, in-terminal drag now drives the in-app selection; your terminal's native selection remains available via its usual modifier (Shift-drag in most terminals). On very long sessions the visible scrollback is bounded for responsiveness — a one-line marker shows where older messages were trimmed, and the complete transcript is always preserved in the session log.

Slash commands: `/reasoning`, `/goal`, `/plan`, `/resume`, `/new`, `/context`, `/compact`, `/model`, `/login`, `/logout`, `/usage`, `/permissions`, `/skills`, `/mcp`, `/paste`, `/diff`, and `/rewind`. `/login` is shown only when signed out; `/usage` and `/logout` only when signed in. Press Esc to stop a running task; close the terminal (or Ctrl+C / Ctrl+D) to exit. `/usage` fetches your account usage (plan budget windows, extra credits, and spend today and this billing period) from the gateway using your stored API key. Selecting a command from the autocomplete list and pressing Enter runs it. `/model` can refresh the catalog directly from the gateway (context window, max output, and provider metadata included) without restarting. `/mcp` opens an interactive manager to view, add, enable/disable, authorize, test, and remove servers; the `muhiyacode mcp` subcommands remain available for scripting.

### Arabic and right-to-left text

MuhiyaCode treats Arabic as a first-class language across the whole terminal. Arabic renders with correct contextual letter joining (including the LAM+ALEF ligature), right-to-left order, and right alignment everywhere — streamed replies, your own messages, tool rows, tables, thinking, modals, and the **composer** (typed Arabic joins and reads right-to-left live). Mixed lines keep English identifiers, inline `code`, numbers, file paths, URLs, and calls like `f(x)` intact and left-to-right in their correct positions.

Everything the model receives, everything stored in history, and everything you copy is clean **logical** Unicode (NFC-normalized) — never the on-screen visual form — so prompts, context, and clipboard round-trip perfectly. This is presentation-only: the deterministic prompt prefix stays byte-identical, so Arabic never affects the prefix cache.

Rendering is controlled by two settings (defaults are `auto`):

```sh
muhiyacode config set rtlMode  auto|visual|native|off   # how RTL is rendered
muhiyacode config set rtlAlign auto|right|left          # block alignment
muhiyacode doctor rtl                                   # preview shaping + copy round-trip on your terminal
```

`auto`/`visual` shape and reorder in-app (reliable on Windows Terminal and legacy consoles) and emit a BiDi-suppression sequence so a BiDi-capable terminal never double-reverses; `native` leaves reordering to a terminal that does its own BiDi; `off` disables RTL handling. Pure-LTR content is untouched.

### Reasoning effort, goals, and plan mode

`/reasoning` sets how hard the model thinks — `low` (default), `medium`, `high`, or `max`. The level is sent to the gateway unchanged as `X-Muhiya-Effort`; the gateway maps it onto each provider's thinking ladder (for DeepSeek: `low`/`medium` → `high`, `high`/`max` → `max`). Because it is a request parameter, not a message, changing it never disturbs the prefix cache.

`/goal <objective>` sets a durable objective the agent works toward across turns: after each turn it emits a `[goal:continue|complete|blocked]` marker and the engine auto-continues (bounded) until the goal is met. `/goal clear` ends it, `/goal` shows status. `/plan [task]` toggles read-only plan mode: the agent researches and proposes a concrete plan while every mutating tool is blocked; run `/plan` again to resume editing. Both the goal and the plan-mode instruction ride on the user message, so they never bust the cache.

### Project context and memory

MuhiyaCode carries durable project context across sessions in two root-local Markdown files, loaded once into a byte-stable boot block that rides the first message and never touches the prompt cache:

- **`MUHIYA.md`** — your project instructions, like `CLAUDE.md`. You edit it by hand; MuhiyaCode writes a clear commented template into a workspace that has none (and never overwrites an existing one). It is root-only, UTF-8, ≤32 KiB, and screened for secrets. While it holds only the template comment it is ignored, so it costs nothing until you fill it in.
- **`MEMORY.md`** — the agent's durable memory. The model reads it from context and keeps it current with its ordinary file tools (`write_file` / `edit_file`), recording only durable, non-secret project truth and pruning entries that go stale. There is no separate memory command, answer-trailer, or database — memory management is part of the agent's normal execution flow, which keeps the prompt lean (the Reasonix / Claude Code model).

Both files are workspace-local — another project never sees them. A mid-session edit to either (yours or the agent's) is surfaced once as a one-shot update block on the next turn and folds into the next session's cached prefix at no per-turn cost. None of this changes tools, model routing, or the deterministic prompt prefix.

### Prefix caching

The system prompt and tool schemas are composed once per session and stay byte-identical on every turn and every task (no lite/full switching), and settled history is rewritten only under real context pressure. That keeps DeepSeek's implicit prefix cache warm across the whole session. Point the client at a concrete model (`deepseek-v4-pro`, `deepseek-v4-flash`) rather than the gateway's `muhiya-ai-router`, which re-selects a model per request and fragments the provider-side cache. `/context` shows provider-faithful cache accounting and attributable prefix changes; see [prompt-caching.md](docs/prompt-caching.md) for interpretation and provider limits.

## MCP

```sh
muhiyacode mcp add-stdio filesystem npx -y @modelcontextprotocol/server-filesystem /project
muhiyacode mcp add-http supabase "https://mcp.supabase.com/mcp?project_ref=..." --oauth
muhiyacode mcp auth supabase
muhiyacode mcp list
```

Use repeatable `--env KEY=VALUE` flags for stdio credentials; they are written to the protected `mcp-secrets.json`, not normal MCP config. Slow, invalid, unauthorized, and broken servers are isolated so they cannot prevent the core agent from starting. MCP tools are exposed as `mcp__server__tool` and still pass through the permission layer.

## State and migration

MuhiyaCode stores data under `~/.muhiya`, or the directory named by `MUHIYA_HOME`:

```text
settings.json          non-secret settings and model catalog
secrets.json           provider API key (user-only permissions)
mcp.json               MCP server definitions
mcp-secrets.json       MCP OAuth tokens and stdio credentials
state/muhiyacode.sqlite sessions, events, trust, checkpoint metadata
sessions/<id>/         history, knowledge, inspection, plan, transcript, checkpoints
```

The Go binary opens the TypeScript release's v1 state in place. Read [the migration guide](docs/migration.md) before switching an important installation.

## Security model

- Real-path containment prevents traversal through symlinks/junctions. Access outside the workspace requires a one-time approval.
- Mutations require workspace trust. Normal mode confirms each mutation; auto-accept allows safe workspace operations but never bypasses the destructive-command or credential-location blocks.
- Existing files cannot be overwritten until the current session has read them. External file changes invalidate recorded read coverage.
- Recursive force deletion, force pushes, downloaded-code piping, credential enumeration, registry/power/disk operations, and MuhiyaCode secret-file access are blocked.
- Model-visible persistence is redacted for configured and common secret patterns. Secret files receive user-only permissions.
- This is a policy boundary, not an OS sandbox. Run the agent with the least operating-system privileges appropriate for the repository.

See [security.md](docs/security.md) for the complete threat model.

## Agent and token design

The single full system prompt has a regression ceiling below roughly 1,900 estimated tokens. Stable instructions stay byte-identical for provider prefix caching, while task classification and budgets are appended to the user turn. Completed tasks fold tool payloads, stale reads are superseded, large context compacts into a durable structured summary, and identical/covered reads are blocked only while their source results remain intact.

Workspace skills (`.agents/skills/*/SKILL.md`, `.codex/skills/*/SKILL.md`) are advertised to the model as a compact, deterministic listing in the stable system prompt — one line of name, path, and purpose per skill, discovered once per session and sorted by name so it never disturbs the prefix cache. The agent reads a skill's full instructions on demand with `read_file` only when a task matches its purpose; nothing else is added to the prompt. The `/skills` command still lets you attach a skill's full text to the next prompt manually (including skills from outside the workspace), loading only the selected skills.

See [agent-design.md](docs/agent-design.md) and [architecture.md](docs/architecture.md).

## Development

```sh
go mod verify
go fmt ./...
go vet ./...
go test ./... -count=1
go build -trimpath ./cmd/muhiyacode
```

On a CGO-capable Linux/macOS runner, also run:

```sh
CGO_ENABLED=1 go test -race ./... -count=1
```

Create a local release snapshot with GoReleaser 2.17:

```sh
goreleaser release --snapshot --clean
```

## Scope

MuhiyaCode currently targets terminal workflows, OpenAI-compatible chat-completions gateways, local workspaces, gateway-hosted web search, and MCP. ACP, an LSP implementation, IDE extensions, and hosted credit/billing systems are not part of this repository.
