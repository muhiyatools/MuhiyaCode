# US2 task-epoch offline acceptance

Date: 2026-07-22

This report contains local deterministic evidence only. No provider request was
made and no paid acceptance result is claimed.

The six-task fixture was executed twice by
`TestTaskEpochSixTaskOfflineMatrixTwice`. It covers two direct follow-up
chains, unrelated capsule distractors, retrieval of an older historical
database decision, a 1,200-token retrieval budget, and immutable model/upstream
identity.

Both repetitions passed:

- direct follow-up and correction prompts stayed in the current epoch;
- the required `database=sqlite` fact was selected from signed canonical
  capsules while unrelated visual-task capsules were excluded;
- the capsule-only first-prompt estimate was at least 60% smaller than the
  synthetic full-history continuation;
- model `fixed` and upstream pin `s:main` remained unchanged.

Live provider cache attribution is intentionally deferred because the user
prohibited paid requests and tests.
