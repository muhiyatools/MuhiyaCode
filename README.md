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
- **Autonomous goals** — the agent works toward a goal across bounded, self-continuing turns.
- **Concurrent subagents** — `explore`, `plan`, `review`, and `general` run in parallel for research, planning, and review, each on its own cache pin with optional token ceilings.
- **Subagent context linking** — a continuation subagent resumes its predecessor's conversation stream, so the provider bills the shared prefix as cache reads instead of re-reading everything from zero; staleness-checked, provider-verified, and visible per dispatch (`contextLinking`: off/default).
- **Right-sized reviews** — a deterministic gate decides when an automatic review is warranted and at what depth (`review_gating`: off/conservative/default); trivial changes skip with a visible rationale, risk-area changes always review, and explicit review requests always run.
- **MCP servers** — stdio and Streamable HTTP, with OAuth and per-tool permissions.
- **Persistent project memory** — root-local `MEMORY.md` and `MUHIYA.md` travel with your repo.

## Commands

Commands are typed inside MuhiyaCode (not your shell).

| Command | Description |
|---|---|
| `/login` | Sign in through your browser |
| `/logout` | Sign out and clear the credential |
| `/usage` | View account usage |
| `/model` | Choose or refresh models |
| `/reasoning` (`/effort`) | Set reasoning effort (low–max) |
| `/permissions` (`/mode`) | Change permission mode |
| `/goal` | Set an autonomous goal |
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

## Configuration

Signing in sets up your endpoint and credential for you — there's nothing to wire up by hand. Beyond that, tune the agent with `muhiyacode config set <key> <value>` or the matching in-app command:

```sh
muhiyacode config set model deepseek-v4-pro    # or /model
muhiyacode config set effort high              # or /reasoning
```

Settings live under `~/.muhiya` (or `$MUHIYA_HOME`). See [docs/](docs/) for the full reference, including advanced overrides.

## Documentation

Full guides for configuration, agent design, security, and prompt caching live in [docs/](docs/).

## Contributing

Issues and pull requests are welcome — see the repository for build and test instructions.

Built with Go. Licensed under MIT.
