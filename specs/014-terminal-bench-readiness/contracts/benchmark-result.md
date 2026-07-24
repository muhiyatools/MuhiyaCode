# Benchmark Result Contract

## Invocation

`muhiyacode bench` accepts one task from exactly one of `--task <path>` or standard input. It may write the final record to `--out <path>` and always emits one final JSON record on standard output.

The adapter is process-per-task. The harness must supply a dedicated workspace and state root. The adapter must not infer benchmark identity from task content, repository names, or fixture paths.

## Exit Codes

- `0`: `pass` — the agent completed cleanly and its required internal verification, if any, did not fail. This is not an objective benchmark-grade verdict.
- `2`: `fail` — the agent completed but reported a failed internal verification or task failure condition.
- `3`: `timeout` — the task context exceeded its configured wall-clock deadline.
- `4`: `blocked` — required configuration is missing, a requested fixed model is unavailable, permission/safety policy blocked the task, or progress cannot continue.
- `1`: `error` — an unexpected runtime, provider, serialization, or adapter failure occurred.

## Result Shape

```json
{
  "status": "pass|fail|timeout|blocked|error",
  "completed": true,
  "stop_cause": "user stop|error|disconnect|timeout|budget",
  "terminated_reason": "string",
  "session_id": "string",
  "trajectory_path": "path to transcript.jsonl",
  "duration_ms": 0,
  "turns": 0,
  "tool_calls": 0,
  "checks_run": 0,
  "files_changed": ["relative/path"],
  "usage": {
    "prompt_tokens": 0,
    "completion_tokens": 0,
    "cache_read_tokens": 0,
    "cache_miss_tokens": 0,
    "cache_write_tokens": 0,
    "total_tokens": 0,
    "reported": true
  },
  "cost_usd": 0.0,
  "cost_estimated": false,
  "models_used": [],
  "model_switches": [],
  "config": {},
  "verification": {
    "ran": false,
    "command": "",
    "result": "pass|fail|none",
    "output_truncated": ""
  },
  "errors": []
}
```

`completed` preserves legacy loop semantics and is never an external grader verdict. `status` describes the adapter lifecycle. The harness's objective repository-state grader remains authoritative.

## Trajectory and Redaction

`trajectory_path` must resolve to the durable JSONL transcript for this exact `session_id`. Each tool record includes a redacted input/output, failure/gate-rejection state, timestamp, and duration. API keys, authorization values, home-directory secrets, environment dumps, and secret-like file values must never appear in the result or trajectory.

## Configuration and Isolation

All benchmark configuration is supplied in memory. A run may not mutate persisted provider settings or secrets. Fixed-model runs must use the requested model or finish `blocked`; routed runs must state every model switch and its reason.

Verification commands are selected only from generic project markers such as `go.mod`, `package.json`, `Cargo.toml`, `pytest` configuration, or a project `Makefile`. The contract forbids task-specific command selection and benchmark fixture knowledge.
