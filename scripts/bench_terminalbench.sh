#!/usr/bin/env bash
set -euo pipefail

# Benchmark adapter for TerminalBench.
# Execution is strictly isolated: one process/state root per task.
# No cross-task state bleed is permitted.

TASK_FILE="${1:-}"
OUT_FILE="${2:-}"
WORKSPACE="${3:-.}"

if [ -z "$TASK_FILE" ] || [ -z "$OUT_FILE" ]; then
    echo "Usage: $0 <task_file> <out_file> [workspace]"
    exit 1
fi

cd "$WORKSPACE"

REPEATS=1
SUCCESSES=0
COSTS=0

for i in $(seq 1 $REPEATS); do
    TMP_OUT="${OUT_FILE}.${i}"
    muhiyacode bench --task "$TASK_FILE" --out "$TMP_OUT"
    
    # Basic harvester metrics (P50/P95, variance, usage/cache, cost/success, timeout, test execution)
    if [ -f "$TMP_OUT" ]; then
        if grep -q '"status":"pass"' "$TMP_OUT"; then
            SUCCESSES=$((SUCCESSES+1))
        fi
    fi
done

echo "Repeats: $REPEATS"
echo "Successes: $SUCCESSES"

if [ -f "${OUT_FILE}.1" ]; then
    cp "${OUT_FILE}.1" "$OUT_FILE"
fi
