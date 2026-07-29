# Generated MuhiyaCode Runtime Capabilities

Generated from the command, tool, sandbox, and language registries. Do not edit by hand.

## CLI commands

| Command | Description |
|---|---|
| `muhiyacode benchmark` | Run a pinned benchmark manifest against the typed JSONL protocol |
| `muhiyacode checkpoint` | List or restore session checkpoints |
| `muhiyacode checkpoint create` | Create a manual code checkpoint |
| `muhiyacode checkpoint delete` | Delete a checkpoint snapshot |
| `muhiyacode checkpoint list` | List selectable checkpoints |
| `muhiyacode checkpoint restore` | Restore code, conversation, or both |
| `muhiyacode config` | Show or set configuration |
| `muhiyacode config discover` | Discover models from the configured endpoint |
| `muhiyacode config path` | Print the settings path |
| `muhiyacode config set` | Set a configuration value |
| `muhiyacode docs` | Generate or check runtime capability documentation |
| `muhiyacode docs check` | Fail when generated runtime documentation is stale |
| `muhiyacode docs generate` | Regenerate runtime capability documentation |
| `muhiyacode doctor` | Validate local setup and endpoint access |
| `muhiyacode login` | Sign in to Muhiya through your browser (no API key needed) |
| `muhiyacode logout` | Remove the stored MuhiyaCode credential |
| `muhiyacode mcp` | Manage MCP servers |
| `muhiyacode mcp add-http` | Register a Streamable HTTP MCP server |
| `muhiyacode mcp add-stdio` | Register a stdio MCP server |
| `muhiyacode mcp auth` | Authorize an OAuth-enabled HTTP server |
| `muhiyacode mcp list` | List registered servers |
| `muhiyacode mcp remove` | Remove a registered server |
| `muhiyacode model` | Inspect or select configured models |
| `muhiyacode model list` | List configured model capabilities |
| `muhiyacode model use` | Select and pin a model |
| `muhiyacode process` | List or stop session-owned background processes |
| `muhiyacode process list` | List background processes owned by a session |
| `muhiyacode process stop` | Stop an owned background process tree |
| `muhiyacode resume` | Resume a stored session |
| `muhiyacode sessions` | List stored sessions |
| `muhiyacode sessions archive` | Archive a session |
| `muhiyacode sessions delete` | Guarded deletion of a session and its sidecars |
| `muhiyacode sessions export` | Export a versioned session bundle |
| `muhiyacode sessions fork` | Fork a session at its latest durable event |
| `muhiyacode sessions import` | Import a versioned session bundle |
| `muhiyacode sessions sanitize` | Export a versioned session bundle |
| `muhiyacode sessions search` | Search session titles and workspace paths |
| `muhiyacode sessions stats` | Show durable session event statistics |
| `muhiyacode sessions unarchive` | Restore an archived session |

## Built-in tools

| Tool | Description |
|---|---|
| `apply_patch` | Apply a standard unified diff to files already read. |
| `edit_file` | Replace exact text in a file already read. Returns a compact diff. oldString must match exactly once; if it is not found the result names the nearest region — retry from that, adding surrounding lines to disambiguate a repeated match. |
| `git_diff` | Show the workspace git diff. |
| `git_status` | Show concise git status. |
| `glob` | Find files by doublestar glob, scoped to the narrowest directory that can hold them, e.g. src/**/*.go. Prefer a directory prefix over a bare **/ scan. |
| `grep` | Regex/literal search with file, line, and text results. Patterns are Go RE2: lookaround ((?= and (?!) and backreferences do not exist — express the intent another way, or set literal=true for exact text. |
| `inspect_code` | Inspect supported source structure. Go results use syntax-aware AST indexing; TypeScript, JavaScript, Python, Rust, C/C++, and Java use explicitly labeled lexical outlines and symbol matches. This is not compiler/LSP semantic resolution. |
| `list_files` | List a directory. Use once to map a workspace. |
| `multi_edit` | Apply several ordered exact replacements to one read file. |
| `read_file` | Read a UTF-8 file with line numbers. Use offset/limit for large files. |
| `run_shell` | Run a shell command in the workspace with streaming output and cancellation. Each call runs fresh at the workspace root; a cd affects only that one command, so combine cd and the command in a single call. For reading or searching files use read_file/grep instead — they are cheaper and cache-tracked. |
| `search_text` | Fast literal text search; prefer grep for regex. |
| `write_file` | write_file creates a new file, or replaces an existing file only after reading it for a full regeneration the user explicitly asked for; never rewrite a whole existing file to change part of it — use edit_file/multi_edit instead. |

## Code intelligence

| Language | Outline | Definition | References |
|---|---|---|---|
| c | lexical declaration extraction | lexical declaration match | lexical identifier match |
| cpp | lexical declaration extraction | lexical declaration match | lexical identifier match |
| go | syntax-aware AST | syntax-aware top-level declaration match | syntax-aware identifier match (not type-resolved) |
| java | lexical declaration extraction | lexical declaration match | lexical identifier match |
| javascript | lexical declaration extraction | lexical declaration match | lexical identifier match |
| python | lexical declaration extraction | lexical declaration match | lexical identifier match |
| rust | lexical declaration extraction | lexical declaration match | lexical identifier match |
| typescript | lexical declaration extraction | lexical declaration match | lexical identifier match |

## Sandbox backends

| OS | Backend | Runtime contract |
|---|---|---|
| linux | bubblewrap | enforced when bwrap is installed |
| darwin | sandbox-exec | enforced when sandbox-exec is installed |
| windows | container | enforced with configured Docker/Podman image; native backend unavailable |

