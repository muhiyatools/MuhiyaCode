# Control workload (008 · T002)

A single-file, single-concern, low-effort fix over the same OrderDesk fixture
tree (`fixture/`). This is the anti-overcorrection control (SC-003): a healthy
build completes it directly on the main loop with ZERO subagent runs. If the
after build starts delegating this, the delegation instructions overshot.

Runner line format is shared with `delegation-workload.md` (numbered prompt,
optional `expect:`, `check:` greps). The runner sends only the numbered prompt
to the agent; it runs this workload at `-effort low` by default.

## Prompt

1. In notify/sms.go the constant maxSMSLength is 150, which is wrong: a single GSM-7 SMS segment fits 160 characters. Change maxSMSLength from 150 to 160. Make exactly that one change and nothing else.
expect: 160

## Expected outcome checklist

check: notify/sms.go :: contains :: maxSMSLength = 160
check: notify/sms.go :: absent :: maxSMSLength = 150

## Control expectations (recorded by the runner, asserted in T016)

- agentRuns == 0 and agentRunsReused == 0 (SC-003).
- filesChanged is exactly `["notify/sms.go"]`.
- Zero `write_file` calls on already-existing files (a one-line constant
  change is an `edit_file`, never a whole-file rewrite — SC-008).
- Both checklist greps pass.

Note: the fixture's planted misspellings (see `delegation-workload.md`) are
still present during a control run. The prompt pins the agent to exactly one
change, and the control checklist only inspects `notify/sms.go`, so an
overeager drive-by fix elsewhere does not affect the control greps — but it
WOULD show up in `filesChanged`, which T016 asserts has exactly one entry.
