<div align="center">

# MuhiyaCode

**An interactive terminal coding agent.**

[![Release](https://img.shields.io/badge/release-latest-blue)](https://github.com/muhiya/muhiyacode/releases)
[![Go build](https://img.shields.io/badge/go-build-brightgreen)](https://github.com/muhiya/muhiyacode/actions)
[![License](https://img.shields.io/badge/license-MIT-lightgrey)](LICENSE)

Agentic coding in your terminal — DeepSeek-tuned, cache-efficient, and self-contained in one Go binary.

</div>

## Install

```sh
npm i -g muhiyacode
```

Prebuilt binaries are also on the [GitHub releases page](https://github.com/muhiya/muhiyacode/releases). Runs in Windows, macOS, and Linux terminals.

## Quick start

1. Install: `npm i -g muhiyacode`
2. `cd` into your project.
3. Run `muhiyacode`, then `/login` to store your API key.

## Features

- Agentic coding with guarded tools — read, search, exact edits, patches, and shell.
- DeepSeek prefix-cache optimized: byte-stable prompts keep the cache warm across a session.
- Plan mode researches and proposes before any mutating tool runs.
- Autonomous goals the agent works toward across bounded, self-continuing turns.
- Built-in subagents (`explore`, `plan`, `review`, `general`) that can run concurrently.
- MCP servers over stdio and Streamable HTTP, with OAuth and per-tool permissions.
- Persistent project context via root-local `MEMORY.md` and `MUHIYA.md`.

## Commands

Commands are typed inside MuhiyaCode (not your shell).

| Command | Description |
|---|---|
| `/reasoning` (`/effort`) | Set reasoning effort (low–max) |
| `/goal` | Set an autonomous goal |
| `/plan` | Plan before editing |
| `/resume` | Resume a workspace session |
| `/new` | Start a new session |
| `/context` | Inspect context & provider capability |
| `/compact` | Compact the conversation |
| `/model` | Choose or refresh models |
| `/login` | Store your API key |
| `/logout` | Clear your API key |
| `/usage` | View account usage |
| `/permissions` (`/mode`) | Change permission mode |
| `/skills` | Assign skills to the next prompt |
| `/mcp` | Manage MCP servers |
| `/paste` | Inspect or remove pasted blocks |
| `/diff` | Summarize the git diff |
| `/rewind` | Restore the latest checkpoint |

## Configuration

Settings live under `~/.muhiya` (or `$MUHIYA_HOME`) and can be set with `muhiyacode config set <key> <value>`. Point at any OpenAI-compatible gateway and pick a concrete model and effort level:

```sh
muhiyacode config set baseUrl https://api.muhiya.com/v1
muhiyacode config set model deepseek-v4-pro
muhiyacode config set effort high
```

See [docs/](docs/) for the full configuration reference.

## Documentation

Full guides for configuration, agent design, security, and prompt caching live in [docs/](docs/).

## Contributing

Issues and pull requests are welcome — see the repository for build and test instructions.

Built with Go. Licensed under MIT.
