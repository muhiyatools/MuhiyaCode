# Task epoch and capsule recovery

Date: 2026-07-22

All checks below are offline filesystem and unit fixtures.

| Boundary | Injected outcome | Recovery invariant |
|---|---|---|
| capsule write before epoch precommit | write failure | old history and old active epoch remain |
| epoch precommit | active ID remains old | both records may exist, but old context stays authoritative |
| history candidate write | persistence failure | in-memory history is not mutated |
| final ledger commit | commit failure | previous history snapshot is restored |
| final commit success | active ID becomes new | history snapshot carries the same task epoch ID |
| missing epoch ledger | legacy session | readable epoch 0 is synthesized; epoch 1 opens only on execution |
| corrupt ledger | invalid JSON/version | corrupt copy is preserved and runtime falls back safely |
| missing/corrupt capsule index | projection loss | index is rebuilt from immutable canonical capsule files |
| duplicate capsule write | identical/different bytes | identical is idempotent; conflicting overwrite is rejected |

The transaction uses capsule-first persistence, an epoch precommit whose active
ID still points to the old epoch, transactional history replacement, and a
final active-ID commit. The previous context is retained whenever a required
write fails. No provider calls are involved.
