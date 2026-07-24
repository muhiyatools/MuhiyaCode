# Terminal-Bench Readiness Validation Quickstart

## Fixture Intent

- `fixtures/go-write/` is a generic Go workspace used to prove a contained edit plus `go test ./...`.
- `fixtures/python-test/` is a generic Python workspace used to prove project-marker test discovery without task-specific test names.

The fixtures are intentionally ordinary repositories. They do not embed benchmark task IDs, gold patches, hidden tests, or agent prompts.

## Adapter Smoke Flow

After `muhiyacode bench` is implemented, run it from a fresh task workspace with:

1. Harvester scripts: Use `scripts/bench_terminalbench.sh <task> <out> [workspace]` or `scripts/bench_terminalbench.ps1 -TaskFile <task> -OutFile <out> [-Workspace <workspace>]`.
2. An isolated `MUHIYA_HOME`.
3. Inline model/provider/API-key/context-limit configuration or an injected fake provider.
3. `--task-timeout` set to a small bounded value.
4. A task that changes only a fixture-local file.

Verify that standard output's final JSON record validates against [`contracts/benchmark-result.md`](contracts/benchmark-result.md), the trajectory path belongs to the emitted session ID, and no persisted settings were created.

## Timeout Smoke Flow

Use a fake provider or controlled shell fixture that cannot complete before a small `--task-timeout`. Verify exit code `3`, `status: "timeout"`, a durable final result, and no reliance on an external kill.

## Container Smoke Flow

Build and run the container smoke flow in a non-TTY Linux container:

```bash
# Build the headless benchmark runner image
docker build -t muhiyacode-bench -f Dockerfile.bench .

# Run a task inside a container with an isolated workspace mount
docker run --rm \
  -v $(pwd)/specs/014-terminal-bench-readiness/fixtures/go-write:/workspace \
  -e MUHIYA_API_KEY="your-api-key" \
  -e MUHIYA_MODEL="your-model-id" \
  muhiyacode-bench --task /workspace/task.txt --out /tmp/result.json --task-timeout 300s
```

The task workspace must be the only writable repository mount; state must be per-run; no benchmark metadata or hidden tests may be mounted.

