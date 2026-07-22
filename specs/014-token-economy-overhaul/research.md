# Phase 0 Research: Token Economy Overhaul

**Date**: 2026-07-22
**Scope**: MuhiyaCode request economics, context management, prompt/tool surface, provider caching, coding-agent ACI, and verification strategy
**Evidence rule**: Provider totals are measurements only when returned by the provider. Local byte/token category splits are estimates unless an exact provider tokenizer is available.

## 1. Diagnosis from the supplied session

The screenshot reports:

| Metric | Value | Interpretation |
|---|---:|---|
| Live context | 34,209 | What the last request approximately held, not cumulative spend |
| Cumulative input | 494,156 | Sum of input across all session requests |
| Cumulative output | 19,810 | Generated reasoning/text/tool-call tokens |
| Cache read | 444,941 | Replayed prefix served from cache, still counted and credit-bearing |
| Uncached input | 49,215 | New or non-matching prompt tokens |
| Hit rate | 90.04% | Cache matching is healthy |
| Credits | 6.46 | User-visible economic outcome |

Derived indicators:

- Approximate replay amplification: `494,156 / 34,209 = 14.44x`. The denominator is the final in-use value rather than the exact maximum request, so the number is diagnostic, not an exact benchmark result.
- Uncached share: `49,215 / 494,156 = 9.96%`.
- Cache-read share: `444,941 / 494,156 = 90.04%`.
- Output/input ratio: `19,810 / 494,156 = 4.01%`.

**Decision**: Optimize cumulative input, output, request count, and credits. Cache hit rate remains a health metric but is not the objective function.

**Rationale**: A 99% hit rate can coexist with excessive consumption if a large context is replayed many times. Eliminating a request saves both cached and uncached tokens.

**Alternative rejected**: Focus only on raising cache hit rate. It cannot address the 444,941 cached tokens already dominating this session.

## 2. Repository findings

### 2.1 Request loop and turn budgets

- `internal/orchestrator/turnloop.go` sends one complete chat request per loop iteration and replays system prompt, all stable tool definitions, selected history, tool results, and provider-required reasoning blocks.
- `internal/orchestrator/classify.go` currently permits 10 tiny, 16 small, 26 standard, 44 large, and 64 epic turns before effort ceilings. Escalation can extend runway while files change.
- `internal/orchestrator/effort.go` gives medium effort 24 turns, high 36, and max 48; the hard ceiling can be higher. The request uses the global effort-to-reasoning mapping rather than a distinct phase budget.
- Empty-final, narrated-intent, review, failure, over-budget, converge, and final-governor paths can add further tail messages and requests.

**Finding R1**: The controller budgets for maximum survival, not minimum sufficient work. The task class changes ceilings but does not enforce a compact execution graph.

### 2.2 Fixed prefix

- `internal/orchestrator/testdata/prefix_bytes_wire.golden` is 22,005 bytes.
- Its header reports a 5,966-character system prompt in the golden scenario.
- `sessionDefinitions()` loads all workspace tools, synthetic session tools, all current MCP definitions, and `read_skill` when skills exist.
- `internal/orchestrator/prompt_budget_test.go` guards only the system prompt character count and explicitly notes that tool-definition JSON is larger.

**Finding R2**: Prefix stability is strong, but the stable prefix is too large. Cached prefix bytes still ride every request and still count toward the user's consumption.

### 2.3 Tool output and reasoning replay

- File reads include every selected line with line numbers.
- grep/glob/list can return hundreds or thousands of rows within caps.
- edits may return full diffs; shell commands return capped raw output.
- full tool output is appended to history. Aging/pressure maintenance happens later, because rewriting history invalidates cache.
- `assistantReplayMessage` retains reasoning content/details, which is required by some providers for interleaved tool chains.

**Finding R3**: The system uses transcript as its evidence database. That is cache-friendly but causes every subsequent request to pay for raw observations.

### 2.4 Skills, MCP, memory, and auxiliary calls

- Skill names/descriptions are part of the session prefix; full `SKILL.md` is appended as a tool result and stays in settled history until compaction.
- MCP schemas are part of the fixed tool definitions.
- Clear medium-effort tasks may run a one-shot model advisor before the first main request; vague non-tiny tasks may also run model-based onboarding.
- The advisor is correctly isolated from the main stream and the model selection is immutable after the first main request.

**Finding R4**: Auxiliary calls and specialized instructions are not sufficiently admission-controlled by expected value. Model-selection isolation is correct and must be preserved.

### 2.5 Existing benchmark evidence

The frozen `specs/011-competitive-agent-audit/benchmarks/runs/baseline/011-v1_single_20260719-043135.json` contains provider-reported results before later architecture changes:

- Trivial documentation/comment completions used 7-10 turns.
- Completed examples used 59,927-89,240 prompt tokens and 1,441-2,236 completion tokens.
- Their main cache steady-state hit rates were about 95-98%.
- The suite later failed to emit usage for many fixtures, so its aggregate completion result is not a release baseline for feature 014.

**Finding R5**: High-turn/high-token behavior existed even when cache hits were excellent. The old benchmark is diagnostic evidence only; feature 014 must freeze a current, fully functioning baseline.

## 3. Competitive reference: Claude Code

Anthropic's current Claude Code documentation states that context contains instructions, file reads, responses, and hidden content; old tool outputs are cleared before conversation summarization; MCP tool definitions are deferred by default; code-intelligence plugins replace search/read chains; hooks can preprocess long logs; skills load on demand; and subagents isolate verbose work so only summaries return. It explicitly notes that token costs scale with context size and that thinking tokens are billed as output.

Primary references:

- [Claude Code: manage costs effectively](https://code.claude.com/docs/en/costs)
- [Claude Code: explore the context window](https://code.claude.com/docs/en/context-window)
- [Claude Code: how Claude Code works](https://code.claude.com/docs/en/how-claude-code-works)
- [Anthropic tool use with prompt caching](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching)

**Decision**: Adopt five principles, not a product imitation:

1. Defer specialized tools and instructions.
2. Treat tool output as evictable evidence with lossless backing storage.
3. Use symbol/code intelligence to reduce search-read chains.
4. Isolate or epoch-bound verbose work.
5. Scale reasoning to task difficulty.

**Difference from Claude Code**: MuhiyaCode's current architecture intentionally uses one main model and one stable execution stream. Feature 014 retains the immutable session model but virtualizes provider context within the visible session. Subagents are not required for the core savings and are not reintroduced merely to copy a competitor.

**Alternative rejected**: Blindly match Claude Code's mechanisms. MuhiyaCode serves MiniMax, DeepSeek, and generic OpenAI-compatible providers with different cache and reasoning semantics.

## 4. Provider cache research

### 4.1 MiniMax

MiniMax documents automatic prefix caching for its OpenAI-compatible interface and explicit `cache_control` through its Anthropic-compatible interface. It states that cache order is tools, system, then messages; automatic caching applies at 512 or more input tokens; explicit cache has a five-minute lifetime; cache reads are priced below uncached input; and usage can separate creation, read, and uncached input. MiniMax also documents that multi-turn function calling must append the complete assistant response, including thinking/tool-use blocks, to preserve reasoning continuity.

Primary references:

- [MiniMax prompt caching](https://platform.minimax.io/docs/api-reference/text-prompt-caching)
- [MiniMax explicit Anthropic-compatible caching](https://platform.minimax.io/docs/api-reference/anthropic-api-compatible-cache)
- [MiniMax Anthropic API compatibility](https://platform.minimax.io/docs/api-reference/text-anthropic-api)

**Decision**: Add an optional Anthropic-compatible MiniMax transport for explicit breakpoints and richer accounting, but ship it only after paid canaries prove parity. Preserve full reasoning/tool-use blocks inside an active MiniMax chain.

**Alternative rejected**: Delete thinking/reasoning details from history to save tokens. MiniMax's official multi-turn tool guidance makes that unsafe.

### 4.2 DeepSeek

DeepSeek documents automatic disk context caching based on overlapping prefixes, persistence at request/output boundaries and fixed intervals, and `prompt_cache_hit_tokens` plus `prompt_cache_miss_tokens`. It is best-effort and not guaranteed to hit 100%.

Primary reference: [DeepSeek context caching](https://api-docs.deepseek.com/guides/kv_cache/)

**Decision**: Keep automatic prefix behavior and optimize the bytes sent. Do not invent explicit cache controls on DeepSeek.

### 4.3 Anthropic reference semantics

Anthropic documents a tools -> system -> messages cache hierarchy, automatic or explicit breakpoints, five-minute/one-hour TTL options, minimum cacheable lengths, and up to four explicit breakpoints. It also documents that deferred tool definitions are appended as tool references without changing the stable prefix.

Primary references:

- [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [Anthropic tool use with prompt caching](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching)

**Decision**: Encode cache semantics as provider capabilities. Do not assume Anthropic-specific `defer_loading`, lookback, TTL, or tool-reference features exist on a compatible endpoint unless verified.

## 5. Research on long-context quality and compression

### 5.1 Lost in the middle

Liu et al. found that long-context models can perform best when relevant evidence is near the beginning or end and worse when it is buried in the middle.

Primary reference: [Lost in the Middle](https://arxiv.org/abs/2307.03172)

**Decision**: Higher context utilization is not automatically higher quality. The compiler should keep universal rules first, place the current task and current-step evidence near the tail, and retrieve only a diverse relevant evidence set.

### 5.2 Query-aware compression

LongLLMLingua proposes question-aware coarse-to-fine selection, document reordering, dynamic compression ratios, and subsequence recovery. Its results show that query-aware compression can reduce tokens and sometimes improve quality compared with carrying irrelevant long context.

Primary references:

- [LLMLingua](https://arxiv.org/abs/2310.05736)
- [LongLLMLingua](https://arxiv.org/abs/2310.06839)

**Decision**: Use query-aware selection and dynamic per-segment budgets. Do not implement learned token deletion in the first release. Start with lossless artifact eviction, exact excerpts, deterministic structured summaries, and retrieval. Evaluate model-based fine compression only as an opt-in later phase.

**Alternative rejected**: Apply a generic prompt compressor to system rules, code, diffs, or commands. Token deletion can alter exact literals and create correctness failures.

### 5.3 Hierarchical memory

MemGPT frames context as a hierarchy analogous to virtual memory: a small active context backed by larger external storage and explicit movement between tiers.

Primary reference: [MemGPT](https://arxiv.org/abs/2310.08560)

**Decision**: Use four tiers: stable prefix, active task working set, addressable evidence/task capsules, and durable project memory. Promotion and eviction are deterministic and observable.

## 6. Research on coding-agent interfaces and retrieval

SWE-agent demonstrates that the agent-computer interface materially affects software-engineering performance. RepoCoder demonstrates iterative repository retrieval rather than unconditional whole-repository context.

Primary references:

- [SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering](https://arxiv.org/abs/2405.15793)
- [RepoCoder: Repository-Level Code Completion Through Iterative Retrieval and Generation](https://aclanthology.org/2023.emnlp-main.151.pdf)

**Decision**: Reduce tokens by improving tools, not merely telling the model to be frugal. Add bounded multi-read/search, symbol navigation, semantic command reducers, and artifact retrieval. Use hybrid retrieval with exact path/symbol signals before optional embeddings.

**Alternative rejected**: Add more prompt prose instructing the model to read less. Existing prompt guidance already says this; tool affordances and runtime budgets are the stronger control plane.

## 7. Target objective function

For request `i`, define provider-reported members when available:

- `R_i`: cache-read input tokens
- `W_i`: cache-creation/write tokens
- `U_i`: uncached input tokens
- `O_i`: output tokens
- `Q_i`: provider request/credit charge not expressible per token, if supplied

Provider profile weights:

- `p_r`, `p_w`, `p_u`, `p_o`: price or normalized credit weights
- `q`: per-request fixed charge

Economic cost:

`C_i = p_r R_i + p_w W_i + p_u U_i + p_o O_i + q + Q_i`

When only prompt total `P_i` and cache read `R_i` exist, derive `U_i = max(0, P_i - R_i)` only if the provider contract says those fields are complementary. Otherwise record unavailable.

Optimization is constrained:

`min sum(C_i)` subject to correctness, safety, task completion, provider protocol, persistence integrity, and explicit user scope.

Secondary metrics:

- total input `sum(P_i)`
- output `sum(O_i)`
- main and auxiliary request count
- replay amplification `sum(P_i)/max(P_i)`
- useful-action density = successful mutations/checks divided by main requests
- evidence density = selected relevant evidence tokens divided by model-visible evidence tokens
- cache efficiency = read/(read+write+uncached) where defined

**Decision**: Weighted economic cost is the governor's objective; raw hit rate is a constraint/diagnostic.

## 8. Context-epoch break-even algorithm

At a possible epoch boundary, estimate:

- `S`: tokens in the current serialized request
- `K`: tokens retained in a new context (stable prefix + current task + selected capsules)
- `D`: expected new tail growth per remaining request
- `n`: expected remaining main requests from the phase graph
- `r`: provider cache-read weight
- `u`: uncached/write weight for the reset request
- `X`: cost of capsule creation/retrieval, normally local and zero-provider-token
- `Risk`: quality penalty when dependencies are uncertain

Approximate keep cost:

`Keep = n * r * S + D * n*(n+1)/2 * u`

Approximate reset cost:

`Reset = u*K + (n-1)*r*K + D*n*(n+1)/2*u + X + Risk`

Reset only when:

1. `Reset + safety_margin < Keep`,
2. dependency confidence exceeds threshold,
3. capsule/evidence persistence succeeded,
4. hysteresis prevents another reset for a configured minimum number of requests unless the user explicitly changes task.

For providers/credit plans with unknown weights, shadow mode uses neutral weights and enforcement defaults to conservative task-boundary resets only.

**Decision**: Make context resets economic and state-aware, not threshold-only.

## 9. Retrieval algorithm

Candidate units are exact file ranges, symbols, task capsules, project-memory facts, and observation cards. For task query `q`, score candidate `d`:

`score(d) = 0.35*BM25(q,d) + 0.25*pathSymbolMatch + 0.15*dependencyGraph + 0.10*recency + 0.10*workspaceValidity + 0.05*userPinned`

Apply hard filters first:

- same workspace/security owner
- current fingerprint or explicitly historical
- not secret-bearing
- dependency/type compatible

Then select within token budget using maximal marginal relevance:

`MMR(d) = lambda*score(d) - (1-lambda)*max_similarity(d, selected)`

Use exact lexical/path/symbol match before embeddings. An optional local embedding index may be evaluated later, but it cannot be a correctness dependency.

**Decision**: Hybrid deterministic retrieval plus diversity. Preserve exact source coordinates and artifact handles.

## 10. Output/reasoning control

Per-request output caps should follow the next decision, not the total task size:

- `orient/inspect`: enough for compact reasoning and a batched tool call
- `change`: enough for bounded patch/tool arguments
- `verify`: enough for one check call or failure diagnosis
- `finish`: enough for a concise user report
- `recover`: previous cap plus a bounded multiplier only after confirmed truncation

The user effort level sets a maximum reasoning envelope. Tiny/small, non-risky steps default to low reasoning. Risk, ambiguity, repeated failed hypotheses, or architecture work can escalate one tier with a reason code.

**Decision**: Add a request planner with provider-specific floors and one-time truncation escalation.

**Alternative rejected**: A single very small global `max_tokens`. It can truncate tool-call JSON and increase total cost through retries.

## 11. Tool-surface strategy

Telemetry determines the final direct core, but the candidate design is:

- direct: `inspect_workspace` (bounded search/read-many/symbol query), `apply_patch`, `run_shell`, `git_diff`, `ask_user`
- stable broker: `discover_tools`, `invoke_tool`
- deferred behind broker: list/glob variants, full artifact fetch, memory writes, skill sections, MCP tools, rare workflow tools

The broker returns compact descriptors and validates against the original exact schema. Direct tools remain for the high-frequency path so discovery does not add turns to ordinary coding.

**Decision**: Application-layer deferral works on all providers and does not depend on Anthropic `defer_loading` support.

**Alternative rejected**: Dynamically add/remove ordinary tool schemas in the top-level tools array per turn. That destabilizes the earliest cache tier.

## 12. Tool-result virtualization

Every tool execution produces:

1. A raw `EvidenceArtifact`, stored locally and content-addressed.
2. Structured facts such as exit code, changed files, failure locations, warnings, truncation, skipped inputs, and fingerprints.
3. A bounded `ObservationCard` included in model history.

Reducer classes:

- source read: exact requested ranges, outline, remaining range handle
- search: top diverse matches, count, skipped/truncated truth flags
- diff/edit: changed paths, line counts, compact hunks around changed regions
- tests/build: status, failing identifiers, locations, diagnostics, warning count, raw handle
- shell generic: exit/duration plus prioritized error lines and bounded head/tail
- MCP/web: typed fields where known, otherwise bounded JSON/text with handle

**Decision**: Store first, reduce second, append only the card. Fetching more is explicit and range-bounded.

## 13. Task-epoch relatedness

No model call is used. Signals include:

- explicit continuation phrase
- overlap in named paths/symbols/issues
- overlap with files changed/read in the current epoch
- shared checklist/task ID
- recency and user correction language
- new domain/path with low overlap

Classification:

- `continue` at score >=0.70
- `new_epoch` at score <=0.30 and current task settled
- `retain_or_ask` between thresholds

Hysteresis: once an epoch starts, require stronger contrary evidence for the next two prompts. A short continuation token ("yes", "do it", "make it blue") inherits the current epoch.

**Decision**: Visible session continuity is preserved while unrelated transcript is excluded from future requests.

## 14. What not to do

- Do not strip provider-required reasoning blocks from an active tool chain.
- Do not summarize code, commands, hashes, paths, or error literals with an unconstrained model and treat the result as exact.
- Do not use a model call to decide whether to save tokens on a small task.
- Do not optimize hit rate by padding prompts or keeping useless history.
- Do not introduce embeddings before exact lexical/path/symbol retrieval is measured.
- Do not add a new subagent architecture solely for context isolation.
- Do not combine all phases into a big-bang rewrite.
- Do not report estimated category attribution as provider-measured usage.

## 15. Consolidated decisions

| ID | Decision |
|---|---|
| D1 | Optimize weighted total cost and replay amplification, not hit rate alone |
| D2 | Freeze a current provider-reported, execution-scored baseline before behavior changes |
| D3 | Add shadow `RequestEconomyRecord`, `ContextManifest`, and budget-decision telemetry |
| D4 | Replace ceiling-led looping with a deterministic phase graph and request planner |
| D5 | Make reasoning/output phase-specific with one bounded truncation escalation |
| D6 | Keep model selection pre-session, once-only, isolated, and immutable |
| D7 | Virtualize raw tool output into content-addressed artifacts plus bounded observation cards |
| D8 | Add task epochs inside the visible session and retrieve signed relevant task capsules |
| D9 | Use lossless eviction/retrieval before query-aware lossy compaction |
| D10 | Slim universal prompt and always-on tools; defer rare/MCP/skill surfaces through a stable broker |
| D11 | Improve the ACI with read-many/search/symbol and semantic command reducers |
| D12 | Add provider cache capabilities and an optional verified MiniMax Anthropic transport |
| D13 | Use an explicit economic break-even rule for resets/compaction with quality gates and hysteresis |
| D14 | Roll out off -> observe -> balanced -> aggressive with correctness non-inferiority gates |

## 16. Unknowns resolved by implementation experiments

No design-blocking clarification remains. These values must be measured before enforcement defaults are frozen:

1. MiniMax's deployed gateway support for Anthropic-compatible streaming, tool signatures, explicit breakpoints, and cache usage fields.
2. Provider-specific safe minimum output caps for each phase/model family.
3. Actual frequency distribution of built-in/MCP/skill tool use, which chooses the direct core.
4. Task-epoch relatedness thresholds and retrieval weights.
5. Token/credit break-even weights for the user's active MiniMax plan.
6. Quality-preserving observation-card caps for failure-heavy workflows.

These are Phase 0/observe-mode measurements, not user questions and not reasons to defer the architecture.
