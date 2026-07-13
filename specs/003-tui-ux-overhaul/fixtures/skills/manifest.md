# Skills Trial Manifest (SC-009, quickstart Scenario 6)

Install `workspace/.agents/skills/*` into the test workspace's `.agents/skills/`.
Point `MUHIYA_SKILLS_DIR` at `external/skills` for the external-root check.

## Matching tasks (agent should read the right SKILL.md and follow it) — expect ≥9/10

1. "Write table-driven tests for the parser package." → **go-test-writer**
2. "Add unit tests covering the error paths in auth.go." → **go-test-writer**
3. "Cover edge cases for the pagination helper with tests." → **go-test-writer**
4. "Draft a changelog entry for the three PRs I just merged." → **changelog-drafter**
5. "Turn these merged changes into release notes grouped by type." → **changelog-drafter**
6. "This query is slow — suggest an index." → **sql-optimizer**
7. "Optimize this Postgres SELECT that scans the whole table." → **sql-optimizer**
8. "The report query times out; propose a rewrite." → **sql-optimizer**
9. "Add tests for the new rate limiter, including the boundary." → **go-test-writer**
10. "Write a changelog section for the 1.4.0 release." → **changelog-drafter**

## Non-matching tasks (zero skill reads, no skill mentions) — expect 0/5

1. "What's the difference between a slice and an array in Go?"
2. "Rename the variable `tmp` to `buffer` in main.go."
3. "Explain what this regex does."
4. "Run go vet and tell me if it passes."
5. "Summarize the git diff on the current branch."

## Notes

- `bare-skill` verifies the no-description listing form renders deterministically.
- `external-only` verifies workspace-only advertisement: present in `/skills`, absent from the system prompt.
