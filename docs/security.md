# Security model

MuhiyaCode combines model instructions with enforcement in Go. Prompt instructions are not treated as the security boundary.

## Enforced boundaries

- Every path is made absolute and canonicalized through the nearest existing ancestor before policy checks. Workspace containment is therefore symlink/junction aware, including for new files.
- `~/.muhiya`, SSH, GnuPG, AWS, Azure, and Kubernetes credential roots are denied even when the user would otherwise approve outside access.
- Reads/searches inside the workspace are allowed. Mutations require durable workspace trust; normal mode additionally confirms the individual action.
- Auto-accept affects safe workspace operations only. The shell risk classifier and protected roots remain fail-closed.
- Existing files require read-before-write. Session inspection memory is fingerprinted; a detected external change requires a fresh read.
- Shell processes run in the canonical workspace with bounded output, a maximum timeout, cancellation, and platform process-tree termination.
- Clearly destructive or credential-oriented shell patterns are blocked, including recursive-force deletion, disk formatting/raw writes, force pushes, downloaded-code piping, environment enumeration, system power/registry policy changes, and direct MuhiyaCode secret access.
- Provider and MCP secrets are stored separately with user-only permissions. Transcripts/events redact configured exact secrets and common token/key patterns.
- OAuth uses PKCE through the official MCP Go SDK, a loopback-only callback, constant-time state comparison, bounded callback lifetime, protected persistence, and refresh-token rotation when metadata is available.

## Permission modes

`normal` is the default and appropriate for unfamiliar repositories. `auto-accept` is intended for trusted disposable or version-controlled workspaces. Shift+Tab changes the live mode; the footer always shows which mode is active.

MCP tools are external capabilities. In normal mode every invocation is confirmed; in auto-accept they may run without another prompt. Remove servers that are broader than the task requires.

## Skill reading (`read_skill`)

Skills may live outside the workspace (`~/.agents/skills`, `~/.codex/skills`, `$MUHIYA_SKILLS_DIR`), which the file tools cannot read — real-path containment is unchanged and was **not** widened for this feature.

Instead the model loads a skill through `read_skill`, which is name-keyed, not path-keyed:

- It accepts only a skill **name**, matched against the catalog discovered once at session start. There is no path argument, so the tool cannot be steered at an arbitrary file.
- The only readable files are the `SKILL.md` documents already discovered under the documented skill roots and already advertised to the model.
- Bodies are size-bounded (32 KiB) and pass through the same redaction and output pipeline as any other tool result.

Treat an installed skill as trusted instruction content: anyone who can write to your skill folders can influence how the agent works, exactly as with `MUHIYA.md`.

## Non-goals

MuhiyaCode is not a kernel, container, or mandatory-access-control sandbox. A permitted compiler, package manager, test runner, Git hook, or MCP server can execute arbitrary code with the user's OS privileges. Use a VM/container and least-privileged credentials for untrusted code.

## Reporting

Do not open a public issue containing credentials, private source, or exploit details. Use the repository owner's private security-reporting channel and include the MuhiyaCode version, OS, reproduction, impact, and relevant redacted logs.

