# Migration from the TypeScript release

The Go rebuild uses the same `~/.muhiya` contract so users can switch binaries without exporting sessions. It does not use the TypeScript source tree or runtime.

## Before switching

1. Exit every running MuhiyaCode process so SQLite and session JSON files are quiescent.
2. Back up the whole Muhiya home directory (`~/.muhiya`, or `MUHIYA_HOME`).
3. Keep the original TypeScript binary available until the Go build has opened the expected sessions and configuration.
4. Run `muhiyacode doctor --offline`, then `muhiyacode doctor` when endpoint access is available.

No explicit conversion command is needed. The Go binary reads and continues to write the v1 files in place.

## Compatibility matrix

| Artifact | Compatibility |
|---|---|
| `settings.json` | Reads v1 provider/model settings and legacy `provider.model`; maps legacy `min`/`ultra` effort aliases onto the four-level dial |
| `secrets.json` | Reads/writes the existing provider API key shape and reapplies user-only permissions |
| `mcp.json` | Reads/writes stdio and HTTP server definitions; missing HTTP OAuth defaults are normalized |
| `mcp-secrets.json` | Preserves OAuth/client/discovery fields and stdio environment secrets; Go adds an absolute token expiry and refresh metadata when available |
| SQLite | Uses the same trust, session, event, and checkpoint tables/columns; adds only indexes |
| `history.json` | Uses the same v1 structured message/tool-call shape |
| `inspection.json` | Uses the same v3 signatures, line coverage, fingerprints, inspected-file, and full-read shape |
| `knowledge.json` | Uses the same v1 facts/files/edit-epoch shape and task-key algorithm |
| Plans/tasks/transcript/checkpoints | Keeps the existing file names and formats |

Corrupt JSON is moved aside as a timestamped `.corrupt-*.bak` and replaced with a safe default. This behavior never deletes the backup.

## Rollback

The storage schemas remain readable by the last TypeScript release, but always restore the backup if you need a byte-for-byte rollback. Do not alternate two running implementations against the same home directory.

## Changed operational requirements

- Bun and Node.js are no longer needed.
- The distributed binary is self-contained and uses a CGO-free SQLite driver.
- `config discover` can populate models and context windows from the endpoint. A manual model still requires `config set contextLimit`.
- Go uses native OS process-tree cancellation and its own TUI/RTL renderer; terminal appearance may differ while behavior remains equivalent.

