<div align="center">

# MuhiyaCode

**An interactive terminal coding agent.**

[![Release](https://img.shields.io/badge/release-latest-blue)](https://github.com/muhiyatools/MuhiyaCode/releases)
[![Go build](https://img.shields.io/badge/go-build-brightgreen)](https://github.com/muhiyatools/MuhiyaCode/actions)
[![License](https://img.shields.io/badge/license-MIT-lightgrey)](LICENSE)

Agentic coding in your terminal — DeepSeek-tuned, cache-efficient, and self-contained in one Go binary.

</div>

## Install

```sh
npm i -g muhiyacode
```

Prebuilt binaries are also on the [GitHub releases page](https://github.com/muhiyatools/MuhiyaCode/releases). Runs in Windows, macOS, and Linux terminals.

## Quick start

1. **Install** — `npm i -g muhiyacode`
2. **Sign in** — `muhiyacode login` opens your browser; approve the request and you're set. No API key to copy.
3. **Build** — `cd` into your project and run `muhiyacode`.

## Sign in

MuhiyaCode signs you in through your browser — there's nothing to copy or paste. Approving the request provisions your access automatically and stores it locally under `~/.muhiya` with user-only permissions.

```sh
muhiyacode login     # opens your browser — approve, and you're in
muhiyacode logout    # remove the stored credential
```

You can also sign in from inside the app with `/login`.

## Features

- **Browser sign-in** — one command, no API keys or endpoints to manage.
- **Guarded agentic tools** — read, search, exact edits, patches, and shell, with a permission mode you control.
- **DeepSeek prefix-cache optimized** — byte-stable prompts keep the cache warm across a whole session.
- **Token Economy Overhaul & Deferred Broker** — lean fixed prompt prefix (≤10,000 bytes / ≤2,500 est. tokens) and deferred tool hydration via `--lean-prefix` or `tokenEconomyMode`.
- **One agent, no hand-offs** — the same model reads, searches, edits, runs shells, and verifies its own work in a single continuous session. There is no planner/executor split, no delegation, and nothing waiting for a report.
- **A checklist you can read** — for multi-step work the agent keeps a plain `tasks.md` (`- [ ]` lines, ordinary markdown, yours to edit) — usually at your workspace root, or beside the work for a task scoped to one part of it — and the live to-do panel follows it.
- **Task-tuned models** — a cheap advisor checks once per task whether the current model still fits, then its choice is frozen for that whole task so the provider's cache stays warm while the work runs. It only ever moves you for free: to a small conversation, or back to a model already warm this session.
- **Right-sized reviews** — a deterministic gate decides when a review pass is warranted and how deep it should go (`reviewGating`: off/conservative/default); trivial changes skip with a visible rationale, and risk-area changes (auth, billing, concurrency, security config, migrations) always get one.
- **MCP servers** — stdio and Streamable HTTP, with OAuth and per-tool permissions.
- **Persistent project memory** — root-local `MEMORY.md` and `MUHIYA.md` travel with your repo.

## Commands

Commands are typed inside MuhiyaCode (not your shell).

| Command | Description |
|---|---|
| `/login` | Sign in through your browser |
| `/logout` | Sign out and clear the credential |
| `/usage` | View account usage |
| `/reasoning` (`/effort`) | Set reasoning effort (low–max) |
| `/context` | Inspect context usage |
| `/compact` | Compact the conversation |
| `/skills` | Assign skills to the next prompt |
| `/mcp` | Manage MCP servers |
| `/paste` | Inspect or remove pasted blocks |
| `/diff` | Summarize the git diff |
| `/resume` | Resume a workspace session |
| `/new` | Start a new session |
| `/rewind` | Restore the latest checkpoint |

Permission mode has no command: **Shift+Tab** cycles it, and the footer shows the
current mode with that shortcut named underneath.

### Keys

| Key | Action |
|---|---|
| `Shift+Tab` | Cycle permission mode (normal ⇄ auto-accept) |
| `Tab` | Complete the highlighted `/command` |
| `Esc` | Stop the running task, or clear the input |
| `Ctrl+C` | Copy the selection, else cancel; `Ctrl+D` quits |

## How a task runs

Ask for something and the agent does it — there is no mode to enter and no plan to approve first. What happens under the hood:

- **One model does the whole task.** It reads, searches, edits, runs shells, and verifies its own work, all in the same session — there is no one to hand work to, and no gate stops it from touching a file.
- **Planning is applied when it's warranted** — genuinely large work, or when you ask for a plan. It recalls what it already knows about the project before designing and saves the durable decisions afterward. A plan is always **written to `tasks.md` first** and summarized second, so you get a file you can read, edit, and commit rather than a wall of chat — and if you asked for a plan, that file *is* the answer: the agent stops there instead of starting to build.
- **Multi-step work gets a checklist.** The agent keeps `tasks.md` as plain markdown and updates it as it goes — usually at your workspace root, or created beside the work when a task is scoped to one part of it; the to-do panel reflects it, and a finished task tells you what's still open.
- **It verifies its own work.** After a change it runs the check that proves it works and ticks the item, never reporting a result it did not observe — then it stops: no re-reading files or re-running checks that already passed.

## Skills

A skill is a folder with a `SKILL.md` inside — a name, a one-line description, and
your instructions for a kind of work. MuhiyaCode finds them at session start in:

```text
<workspace>/.agents/skills/       <workspace>/.codex/skills/
~/.agents/skills/                 ~/.codex/skills/        $MUHIYA_SKILLS_DIR
```

**You don't invoke them.** The agent sees the list of installed skills and decides
for itself when one applies: ask it to build a landing page with a frontend-design
skill installed, and it reads that skill before it starts designing, then works
from it for the rest of the task. Ask it something no skill covers and it loads
nothing.

A skill informs the work; it doesn't script it. The model applies your guidance
with its own reasoning and voice, so the result reads like good work that followed
your conventions — not a filled-in template.

`/skills` still exists for the times you want to force the issue: pick skills for
the next prompt and they're applied whether or not the agent would have chosen
them (and it won't load them twice).

## Configuration

Signing in sets up your endpoint and credential for you — there's nothing to wire up by hand. Beyond that, tune the agent with `muhiyacode config set <key> <value>` or the matching in-app command:

```sh
muhiyacode config set effort high              # or /reasoning
```

**Models are not something you manage.** One model runs your work. A cheap advisor checks it at the start of every task — keeping it or proposing a better fit — and then that choice is frozen for the rest of the task so the cache stays warm while it runs. The TUI no longer displays or selects a model, and `/model` is gone. The advisor only ever moves you when the switch is free: the conversation is still small, or you're returning to a model already warm this session; anything more expensive and it leaves you where you are. If a later request is a genuinely different piece of large work, you'll get a one-line note that `/new` would give it a clean start.

To take manual control:

```sh
muhiyacode config set model <model-id>   # pin the model
muhiyacode config set advisor off        # never propose a change
```

Setting the model pins it: the advisor stops running entirely, so nothing moves it out from under you.

Settings live under `~/.muhiya` (or `$MUHIYA_HOME`). See [docs/](docs/) for the full reference, including advanced overrides.

The header shows a one-line notice when a newer version is on npm (one cached check per day, silent if it fails). Set `MUHIYACODE_NO_UPDATE_CHECK=1` to turn it off.

## Documentation

Full guides for configuration, agent design, security, and prompt caching live in [docs/](docs/).

## Contributing

Issues and pull requests are welcome — see the repository for build and test instructions.

Built with Go. Licensed under MIT.
