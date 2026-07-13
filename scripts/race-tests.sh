#!/usr/bin/env bash
# Race detector runner for the concurrency-heavy packages (OVERHAUL_PLAN V1).
#
# The existing unit tests already cover the concurrency surface (MCP manager
# refresh/connect goroutines, orchestrator executeBatch parallel subagents,
# goal/plan mutex discipline). The -race flag adds Go's data-race detector on
# top, which requires CGO + a C compiler (gcc/clang).
#
# This Windows development host does not have GCC installed, so -race cannot
# run here. Run this script on a CGO-capable host: Linux/macOS (gcc/clang
# standard) or Windows with MinGW-w64/TDM-GCC installed.
#
# Usage:
#   scripts/race-tests.sh              # focused: mcpclient + orchestrator
#   scripts/race-tests.sh --full       # all packages (equivalent to: make test-race)
set -euo pipefail

mode="${1:-focused}"

echo "==> Checking for C compiler..."
if ! command -v gcc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then
  echo "ERROR: No C compiler (gcc/clang) found. The race detector requires CGO." >&2
  echo "Install gcc (Linux/macOS: build-essential/xcode; Windows: MinGW-w64 or TDM-GCC)." >&2
  exit 1
fi

export CGO_ENABLED=1

if [ "$mode" = "--full" ]; then
  echo "==> Running race detector across ALL packages..."
  go test -race ./... -count=1
else
  echo "==> Running race detector on concurrency-heavy packages (V1)..."
  go test -race ./internal/mcpclient/... ./internal/orchestrator/... -count=1
fi

echo "==> Race detector passed."
