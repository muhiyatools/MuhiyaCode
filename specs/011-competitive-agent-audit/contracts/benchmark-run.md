# Contract: Benchmark Run Record

**Consumers**: the benchmark runner (D9), all SC-001..SC-009 verdicts, Constitution X before/after evidence. Storage: `specs/011-competitive-agent-audit/benchmarks/` (procedures + raw results committed with the feature).

## 1. Record schema (one JSON per run)

```jsonc
{
  "run_id": "…",
  "suite_version": "011-v1",          // fixed task matrix version — bump on any task change
  "timestamp": "…",                    // stamped by the runner
  "config": {                          // held constant within any before/after pair (Constitution X)
    "main_model": "…", "sub_model": "…",
    "effort": "low|medium|high|max",
    "review_gating": "off|conservative|default",
    "agent_build": "<git sha>", "gateway_build": "<health version>"
  },
  "tasks": [{
    "task_id": "…",
    "category": "trivial-docs|trivial-comment|rename|format|config|standard-logic|risky-auth|risky-billing|large-multifile|greenfield-scaffold",
    "task_class": "chat|tiny|small|standard|large|epic",  // from the LIVE classifier, emitted in T004's per-task summary — the size axis all segmented metrics use
    "completed": true,
    "turns": 0,
    "usage": {                         // provider-reported ONLY; absent fields stay absent
      "prompt_tokens": 0, "completion_tokens": 0,
      "cache_read_tokens": 0, "cache_miss_tokens": 0, "cache_write_tokens": 0,
      "reported": true                 // false ⇒ no cache fields from this provider; never fabricate
    },
    "cost_usd": 0.0,                   // from muhiya_log when present; else labeled estimate
    "review": { "tier": "skip|focused|deep", "rationale": "…", "spend_tokens": 0, "ceiling_hit": false },
    "per_pairing": [{ "model": "…", "pin": ":main", "steady_state_hit_rate": 0.0, "reported": true }],
    "violations": { "terminal_read_when_tool_exists": 0, "duplicate_reads": 0 }
  }],
  "aggregates": {
    "completion_rate": 0.0, "cost_per_completed_task": 0.0,
    "trivial_auto_review_rate": 0.0, "high_risk_review_retention": 0.0,
    "median_small_task_tokens": 0,             // over task_class ∈ {tiny, small} — the SC-003 population
    "review_overhead_median_pct_small": 0.0,   // task_class ∈ {tiny, small}
    "review_overhead_median_pct_medium": 0.0   // task_class == standard — SC-004 clause (i) covers both segments
  }
}
```

## 2. Rules

1. **Baseline first**: the suite runs on the unchanged build BEFORE any transformation change merges; that record is the denominator for every SC delta.
2. **Variance band**: every configuration gets a double run on an unchanged build; the band (max observed delta per aggregate) is recorded and reused as the significance threshold — a "win" smaller than the band is reported as noise (SC-008).
3. **Honest fields**: usage numbers come from provider-reported payloads (Constitution VI); anything estimated is labeled `estimate`; absent cache fields are reported absent, never zero-filled as data.
4. **One variable per comparison**: before/after pairs hold config identical except the change under test.
5. **Matrix**: ≥ 20 tasks spanning every category above × 2 model configs (single-model; mixed MiniMax-main/DeepSeek-sub) × gating modes as needed per SC.
6. **Reproducibility**: task prompts, workspace fixtures, and runner invocation are committed alongside results; any reviewer can rerun a record from its `suite_version` + `config`.
7. **Class segmentation (canonical)**: "small tasks" = `task_class ∈ {tiny, small}`; "medium" = `standard`; "large" = `{large, epic}`; `chat` is excluded from code-task aggregates. Every size-segmented metric (SC-003's median, SC-004's overhead medians) and T021's per-class ceiling derivation MUST use the `task_class` field — never the semantic `category`, which describes fixture intent, not size.
