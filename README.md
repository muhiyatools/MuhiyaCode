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
- **Plan and execute, split** — the main model plans, analyzes, and instructs; every change to your workspace is carried out by an execution sub-agent it dispatches. It sizes delegation to the job — no quota to spend down — and trusts a report that shows its checks instead of re-reading the files it just verified.
- **A checklist you can read** — for multi-step work the agent keeps a plain `tasks.md` at your workspace root (`- [ ]` lines, ordinary markdown, yours to edit) and the live to-do panel follows it.
- **Session-stable models** — three roles (main, execution, utility) are settled once at the start of a session and then frozen, so the cache never cold-starts mid-task. Nothing to choose in the UI.
- **Scoped subagents** — `explore`, `general`, and `review`, each on its own cache pin with optional token ceilings and an optional role name you'll see in the transcript.
- **Subagent context linking** — a continuation subagent resumes its predecessor's conversation stream, so the provider bills the shared prefix as cache reads instead of re-reading everything from zero; the execution agent keeps one chain for the whole session. Staleness-checked, provider-verified, and visible per dispatch (`contextLinking`: off/default).
- **Right-sized reviews** — a deterministic gate decides when an automatic review is warranted and at what depth (`reviewGating`: off/conservative/default); trivial changes skip with a visible rationale, risk-area changes always review, and explicit review requests always run.
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
| `/permissions` (`/mode`) | Change permission mode |
| `/context` | Inspect context usage & model capability |
| `/compact` | Compact the conversation |
| `/skills` | Assign skills to the next prompt |
| `/mcp` | Manage MCP servers |
| `/paste` | Inspect or remove pasted blocks |
| `/diff` | Summarize the git diff |
| `/resume` | Resume a workspace session |
| `/new` | Start a new session |
| `/rewind` | Restore the latest checkpoint |
| `/errors` | Inspect recent harness events |

## How a task runs

Ask for something and the agent does it — there is no mode to enter and no plan to approve first. What happens under the hood:

- **The main model plans and instructs; it does not edit.** It reads, searches, runs read-only commands, and then hands the actual work to an execution sub-agent. If it tries to change a file itself, a gate stops it and tells it to delegate.
- **Planning is applied when it's warranted** — genuinely large work, or when you ask for a plan. It recalls what it already knows about the project before designing and saves the durable decisions afterward. A plan is always **written to `tasks.md` first** and summarized second, so you get a file you can read, edit, and commit rather than a wall of chat — and if you asked for a plan, that file *is* the answer: the agent stops there instead of starting to build.
- **Multi-step work gets a checklist.** The agent writes `tasks.md` at your workspace root as plain markdown and updates it as it goes; the to-do panel reflects it, and a finished task tells you what's still open.
- **The sub-agent's report is taken at its word.** Each one ends with a status — done, needs a check you should run, or blocked — and the main model accepts a report that shows the checks it ran instead of re-verifying it from scratch. You stop paying twice for the same verification, and a sub-agent that couldn't verify something says so rather than reporting success.

## Configuration

Signing in sets up your endpoint and credential for you — there's nothing to wire up by hand. Beyond that, tune the agent with `muhiyacode config set <key> <value>` or the matching in-app command:

```sh
muhiyacode config set effort high              # or /reasoning
```

**Models are not something you manage.** Three roles — main (plans), execution (does the work in sub-agents), and utility (cheap auxiliary calls) — are decided once per session and then held fixed, so the models never change out from under a running task. The TUI no longer displays or selects them, and `/model` is gone. On the first prompt of each session a short advisor check either keeps the configured pairing or proposes a better one; from then on the session is frozen. If a later request is a genuinely different piece of large work, you'll get a one-line note that `/new` would give it a clean start.

To take manual control:

```sh
muhiyacode config set model <model-id>          # pin the main model
muhiyacode config set subagentModel <model-id>  # pin the execution model
muhiyacode config set advisor off               # never propose a pairing
```

Setting either model pins both roles — the advisor proposes, but it never overrides what you asked for.

Settings live under `~/.muhiya` (or `$MUHIYA_HOME`). See [docs/](docs/) for the full reference, including advanced overrides.

The header shows a one-line notice when a newer version is on npm (one cached check per day, silent if it fails). Set `MUHIYACODE_NO_UPDATE_CHECK=1` to turn it off.

## Documentation

Full guides for configuration, agent design, security, and prompt caching live in [docs/](docs/).

## Contributing

Issues and pull requests are welcome — see the repository for build and test instructions.

Built with Go. Licensed under MIT.
