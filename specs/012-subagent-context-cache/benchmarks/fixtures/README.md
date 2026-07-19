# Feature 012 continuation fixtures

Five multi-phase fixtures over the feature-011 workspaces (referenced by relative
path — the workspaces are copied fresh per run exactly as the 011 harness does).
Each fixture's `expect.links` states the link outcome the run must record in its
`muhiya_bench` JSON `links[]` array; `expect.notes` maps it to the quickstart
scenario and success criterion it feeds.

| Fixture | Scenario | Expected link outcome |
|---|---|---|
| cl-001 | S1 sequential phases | phase 2 `continued (same-kind)`; SC-001 cache share |
| cl-002 | S2 self-edit chain | `continued`, changed-fraction 0, zero re-reads (R-D5) |
| cl-003 | S2 external edits | `digest-seeded / stale:60%` after runner touches 6/10 files |
| cl-004 | S3 review chain | validation review `continued (review-after-implement)`, mutations masked |
| cl-005 | Q2 follow-up task | second task links via relatedness; unrelated → `relatedness-miss` |

Runner requirements beyond the 011 harness (documented for the gated run):
`cl-003` needs a between-phase file-touch hook; `cl-005` needs two sequential
one-shot prompts against one session directory. Both are runner-side only — no
agent behavior changes.

Runs are **[GATED: live cost]** (tasks.md T003/T039) — owner go-ahead required.
