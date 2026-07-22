# Validation Quickstart: Token Economy Overhaul

This guide validates implementation slices. It does not claim the current tree implements the plan.

## 1. Prerequisites

- Go version from `go.mod` with `GOTOOLCHAIN=auto` allowed.
- A clean copy of each benchmark workspace fixture.
- Provider credentials only for explicitly approved paid live runs.
- Same model, gateway, effort, permission mode, and prompt text for baseline/candidate comparisons.
- `MUHIYA_BENCH_JSON=1` or the repository's equivalent machine-summary flag.
- Raw provider usage observer enabled for benchmark runs.

Never put API keys, `.env` content, or session secrets in benchmark artifacts.

## 2. Baseline freeze

PowerShell target workflow:

```powershell
git status --short
git rev-parse HEAD
go version
go build -o bin/muhiyacode-baseline-014.exe .
Get-FileHash bin/muhiyacode-baseline-014.exe -Algorithm SHA256
```

Record dirty-tree provenance exactly if applicable. The binary checksum is authoritative when the worktree is not clean.

Run the baseline twice per configuration through the future `scripts/bench_014.ps1`. A valid result must include every fixture, every provider request, provider-reported usage, task completion, changed-file state, and rubric score.

## 3. Fast local gates after each slice

```powershell
gofmt -w <changed-go-files>
go test ./internal/contract ./internal/orchestrator ./internal/gateway ./internal/workspace ./internal/state ./internal/tui -count=1
go vet ./...
go test ./... -count=1
```

Run repository `scripts/check.ps1` before handoff. Run staticcheck and govulncheck using the pinned versions in CI/check scripts.

## 4. Phase-specific tests

### Phase 0: measurement only

Expected:

- Wire request bytes exactly match pre-change golden.
- Usage rows preserve null versus zero.
- Replay amplification equals `sum(main prompt)/max(main prompt)` from provider fields.
- Context category estimates plus residual equal exact prompt total.
- `off` and `observe` produce identical provider requests.

Suggested focused tests:

```powershell
go test ./internal/contract -run 'Usage|Replay|Cache' -count=1 -v
go test ./internal/orchestrator -run 'Manifest|ContextReport|Prefix|Usage' -count=1 -v
go test ./internal/gateway -run 'Usage|Marshal|Prefix' -count=1 -v
```

### Phase 1: governor

Trace fixtures:

1. greeting -> one finish response
2. named-file typo -> inspect/change/verify/finish within four main requests
3. CSS tweak -> no onboarding/model-generated planning/review
4. provider truncation -> one doubled-cap retry; incomplete call not executed
5. required test after budget -> one attributed verification override
6. repeated no-evidence turns -> converge/finish without accumulating nudge prose

Expected request plans are golden JSON fixtures and contain phase, reasoning, output cap, recovery, and reason codes.

### Phase 2: evidence store

For each tool result:

- raw artifact hash resolves to exact bytes;
- card status equals raw status;
- exact excerpts are raw subsequences;
- omission counts are correct;
- secret-blocked values are absent;
- artifact fetch enforces session/workspace ownership;
- deleting/restarting cannot leave metadata pointing to a half-committed blob.

Suggested tests:

```powershell
go test ./internal/evidence -count=1 -v
go test ./internal/workspace -run 'Reducer|Artifact|Shell|Read|Search|Diff' -count=1 -v
```

### Phase 3: context epochs

Run a single visible session:

1. Fix a function in `a.go`.
2. "Also rename that helper" -> must continue epoch.
3. "Now update the unrelated README title" -> settled new epoch.
4. "Why did we keep the old API name?" -> retrieve the relevant earlier capsule.

Assert:

- same model and upstream pin throughout;
- only declared message history resets;
- prior checkpoint committed before reset;
- required decision is present; unrelated raw tool outputs are absent;
- crash at transition resumes old or new complete epoch.

### Phase 4: prompt/tools

Measure actual wire bytes for:

| Scenario | Expected |
|---|---|
| zero MCP, zero skills | <=10,000-byte target |
| 100 configured unused MCP tools | core prefix delta <=256 bytes |
| 50 installed unused skills | core prefix delta <=256 bytes |
| one discovered MCP tool | core top-level tool hash unchanged |
| broker invocation | original schema/permission/audit identity used |

Run all permission, secret, shell, path, patch, and MCP tests.

### Phase 5: provider adapters

Simulator cases:

- MiniMax OpenAI automatic cache fields
- MiniMax Anthropic cache creation/read/input fields
- DeepSeek hit/miss fields
- absent/malformed cache usage
- assistant thinking/signature/tool-use replay
- streaming reset and retry

Paid MiniMax canary must run twice before enabling the transport for new sessions. Never change transport inside an existing session.

### Phase 6: code intelligence

Fixtures must cover:

- definition and references
- imports/dependencies
- related tests
- external file change invalidation
- unsupported language fallback
- no-result truthfulness

Compare target-file recall and inspection call count against grep/read baseline.

### Phase 7: economic checkpoints

Table-test the keep/reset formula for:

- read cheap/miss expensive
- credit-per-request
- missing weights
- one versus many remaining requests
- uncertain dependencies
- cooldown active
- checkpoint persist failure
- active provider reasoning chain

Observe mode and enforced mode must record the same decision inputs. Enforced mode alone applies it.

## 5. Tic-tac-toe acceptance run

Use the exact frozen prompt and fixture twice for baseline and candidate.

Required task rubric:

- board renders
- alternating legal turns
- occupied cells cannot be reused
- all win lines detected
- draw detected
- reset works
- no unrequested dependency/runtime/harness
- proportionate verification passed

Required economy gates:

```text
main_requests <= 6
provider_input_tokens <= 120000
provider_output_tokens <= 6000
credits <= 2.0  # only when the same provider credit schedule is in force
replay_amplification <= 6.0
```

If correctness fails, the token result is rejected.

## 6. Full comparison report

The future report must include:

```text
build/config hashes
model/transport/upstream policy
fixture version and prompts
raw run links
completion/correctness/safety
main and auxiliary requests
prompt, cache read, cache write, uncached, output
replay amplification
credits/cost and availability rules
context live/max/cumulative
prefix bytes
tool calls and inline observation tokens
invalidations/epochs/compactions/retries
median, p75, p95, run variance
regressions and excluded/invalid runs
```

Do not average missing values as zero. Do not combine provider configurations into one headline without per-provider rows.

## 7. Rollout checks

Before changing the default from `observe` to `balanced`:

1. Two repeated live runs per supported provider/configuration pass.
2. SC-001 through SC-014 pass.
3. Multi-OS full CI and race tests pass.
4. Crash/persistence/security gauntlet passes.
5. No P0/P1 correctness, safety, cache-accounting, or state-loss issue remains.
6. `off` rollback path is exercised against state written by `balanced`.
7. Documentation and `/context` labels match actual accounting.

Before exposing `aggressive`, repeat the same gates independently. Balanced results do not authorize aggressive defaults.

