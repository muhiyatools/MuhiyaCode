# Delegation benchmark workload (008 · T001)

One max-effort task over the OrderDesk fixture tree (`fixture/`). The task has
four genuinely independent sub-scopes — one audit per module, no shared code —
so delegating the four audits to subagents is the efficient strategy. The
before build is expected to run this entirely on the main loop (~0 subagent
runs, the SC-001 baseline); the after build is expected to delegate
(AgentRuns >= 2).

The runner (`benchmarks/delegationbench`) sends ONLY the numbered prompt below
to the agent. `expect:` and `check:` lines are parsed by the runner for
verification and are never shown to the model. The fixture is copied to a
throwaway workspace per run; this workload file is NOT copied, so the agent
cannot read the expected-outcome checklist.

## Runner line format

- `1. <prompt>` — the scripted user prompt (exactly one per workload file).
- `expect: <substring>` — optional case-insensitive substring the final
  assistant answer must contain.
- `check: <path> :: contains :: <needle>` — after the run, at least one file
  under `<path>` (workspace-relative file or directory) must contain
  `<needle>`, case-insensitive.
- `check: <path> :: absent :: <needle>` — after the run, no file under
  `<path>` may contain `<needle>`, case-insensitive.

## Prompt

1. This project has four independent modules: auth/, billing/, reports/, and notify/. They share no code and must each be audited in isolation. Audit EACH of the four modules independently: read every file in the module and check every user-facing string (log messages and error messages) for misspelled English words — each module is believed to contain at least one misspelling. Fix each misspelling by correcting only the misspelled word inside the string literal; do not rename identifiers, change behavior, reformat code, or rewrite files wholesale. When all four audits are complete, report per module what you found and fixed.
expect: notify

## Planted issues

| Module | File | Planted string | Correct string |
|---|---|---|---|
| auth | `auth/token.go` | `token has exipred; request a new one` | `token has expired; request a new one` |
| billing | `billing/invoice.go` | `invoice %s not fond` | `invoice %s not found` |
| reports | `reports/export.go` | `expot complete: wrote %d rows` | `export complete: wrote %d rows` |
| notify | `notify/email.go` | `faild to send email to %s` | `failed to send email to %s` |

None of the misspelled fragments (`exipred`, `not fond`, `expot`, `faild`) is a
substring of its corrected form or of any other fixture text, so the greps
below are unambiguous before and after the fix.

## Expected outcome checklist

check: auth :: absent :: exipred
check: auth :: contains :: token has expired
check: billing :: absent :: not fond
check: billing :: contains :: not found
check: reports :: absent :: expot
check: reports :: contains :: export complete
check: notify :: absent :: faild
check: notify :: contains :: failed to send email

## Delegation expectations (recorded by the runner, asserted in T015)

- Before build (T004 baseline): agentRuns ~ 0; the whole audit rides the main
  context (SC-001 problem shape: every module read inflates the main prompt).
- After build (T015): agentRuns >= 2, final main-stream PromptTokens <= 75% of
  the before leg, billed tokens <= before + 10%, steady-state hit rate >= before.
- Edit discipline (SC-008): the four fixes are one-word string edits — zero
  `write_file` calls on already-existing files are expected in the after leg.
- All eight checklist greps must pass on both legs (correctness is not allowed
  to regress in exchange for delegation).
