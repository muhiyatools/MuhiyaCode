# Balanced core tool-surface selection

Date: 2026-07-22

The selection is versioned as the feature-014 balanced prompt/tool epoch.
`off` and `observe` retain the legacy wire surface byte-for-byte;
`balanced` and `aggressive` use this surface only for new request assembly.

## Direct core

| Tool | Why it remains direct |
|---|---|
| `inspect_workspace` | Batches map, search, and bounded multi-file reads; removes common list/search/read chains. |
| `edit_file` | Most existing-file mutations are small surgical replacements. |
| `write_file` | New-file creation is common in greenfield UI tasks. |
| `apply_patch` | Keeps exact multi-file patch application direct. |
| `run_shell` | Required for existing project checks and commands; still permission/security gated. |
| `ask_user` | A blocking decision must not require capability discovery. |
| `fetch_artifact` | Exact evidence recovery must remain available after output virtualization. |
| `discover_tools` | Stable compact entry point for deferred capabilities. |
| `invoke_tool` | Stable compact dispatcher that preserves the original tool identity and gates. |

The serialized system prompt plus direct schemas is asserted at no more than
10,000 bytes and 2,500 estimated tokens. The remaining universal prompt is
asserted below 3,000 characters.

## Deferred

Legacy list/read/grep/glob variants, multi-edit, git helpers, project-memory
operations, skills, web search, MCP tools, and other integrations are described
outside the top-level provider schema. Discovery returns bounded descriptors
containing canonical name, one-line description, risk/read-only status, exact
schema hash, availability, and MCP server/account fingerprint when applicable.

Invocation is rejected if the schema or server fingerprint changed. Successful
invocation is rewritten to the canonical original call before argument
validation, repeat/dedupe gates, workspace permissions, approval, containment,
secret checks, shell policy, auditing, evidence reduction, and history
persistence. The transcript records the original tool name—not
`invoke_tool`.

## Rejected alternatives

- Publishing all MCP schemas: every unused integration permanently enlarges
  the cached prefix and makes refreshes invalidate it.
- Making every tool deferred: common edits would add a discovery request and
  regress small-task request count.
- Executing inside `invoke_tool`: this would attribute risk and audit records
  to the broker and could bypass original permissions.
- Lossy skill summaries: mandatory rules could disappear. Section reads are
  allowed only for explicitly `<!-- section-safe -->` skills; all others
  return the full body.
- Dynamic top-level schema insertion: it breaks prefix identity mid-session.

The benchmark report script now aggregates optional per-tool call counts,
failures, conditional task classes, schema bytes, and discovery opportunities
when raw benchmark rows provide `tool_calls`. Offline fixtures additionally
prove that 100 unused MCP tools change the balanced core by no more than 256
bytes.
