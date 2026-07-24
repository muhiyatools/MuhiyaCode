# MuhiyaCode ⚡

**MuhiyaCode** is an interactive, token-efficient terminal coding agent built in Go. It empowers developers to navigate, analyze, and build software in large codebases with minimal context overhead and high execution precision.

---

## Key Features

- **AST & Code Intelligence (`inspect_code`)**: Native Go AST structural parser (`outline`, `definition`, `references`) providing lightweight line-number declaration signatures without sending whole-file code bodies, reducing token consumption by **85-90%**.
- **Token Economy & Deferred Tool Hydration**: Lean system prompts (≤2,500 estimated tokens) with deferred tool schema hydration to keep prompt caches warm across turns.
- **Native Agent Architecture**: Frontend-neutral core seam (`internal/app`) powering terminal UI and headlessly scriptable agent execution.
- **Workspace Security Containment**: Path authorization, secret redaction, and strict read/write containment bounds.

---

## Installation

### Via NPM (Recommended)

```bash
npm install -g muhiyacode
```

### Via Pre-built Binaries

Download the latest binary for Windows, macOS, or Linux from the [GitHub Releases](https://github.com/muhiyatools/MuhiyaCode/releases) page.

---

## Quickstart

Launch the interactive terminal UI:

```bash
muhiyacode
```

Start a new session:

```bash
muhiyacode --new
```

Check version:

```bash
muhiyacode --version
```

---

## Available Tools & Capabilities

- `inspect_code` — AST structure parsing (`outline`, `definition`, `references`)
- `read_file` — Focused UTF-8 file reader with line-range limits
- `edit_file` / `multi_edit` — Exact pattern replacement editing
- `write_file` — File creation and full regeneration
- `apply_patch` — Unified diff patch execution
- `grep` / `search_text` / `glob` — High-speed code search
- `run_shell` — Streaming workspace shell execution

---

## License

[MIT License](LICENSE) © Muhiya Tools
