#!/bin/sh
# bench_011.sh - Benchmark runner for feature 011 (competitive agent audit). T004(b).
#
# Drives the fixed fixture task matrix through the muhiyacode binary in one-shot
# (-p) mode and assembles one JSON run record per invocation, exactly per
# specs/011-competitive-agent-audit/contracts/benchmark-run.md section 1.
#
# USAGE
#   scripts/bench_011.sh -config single
#   scripts/bench_011.sh -config mixed -binary bin/muhiyacode.exe
#
# FLAGS
#   -config    single|mixed  (required; recorded in the run record's config snapshot)
#   -binary    path to the agent binary          (default: bin/muhiyacode.exe, falls back to bin/muhiyacode)
#   -fixtures  path to the fixture matrix JSON   (default: specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json)
#   -outdir    directory for run records         (default: specs/011-competitive-agent-audit/benchmarks/runs)
#
# REQUIRES: jq, git.
#
# FIXTURE FORMAT (tasks.json): a JSON array of entries
#   { "task_id": "...", "category": "trivial-docs|...|greenfield-scaffold",
#     "prompt": "...", "workspace": "relative/dir under fixtures/ (optional; omitted => empty temp workspace)" }
#
# CONSUMED AGENT OUTPUT (emitted when MUHIYA_BENCH_JSON=1; T004(a)): the LAST
# stdout line of each one-shot run is one JSON object:
#   {"muhiya_bench":{"task_class":"chat|tiny|small|standard|large|epic","completed":true,"turns":N,
#     "usage":{"prompt_tokens":N,"completion_tokens":N,"cache_read_tokens":N,"cache_miss_tokens":N,"cache_write_tokens":N,"reported":true},
#     "cost_usd":N,"cost_estimated":bool,
#     "review":{"tier":"skip|focused|deep|none","rationale":"...","spend_tokens":N,"ceiling_hit":false},
#     "violations":{"terminal_read_when_tool_exists":N,"duplicate_reads":N},
#     "per_pairing":[{"model":"...","pin":":main","prompt_tokens":N,"cache_read_tokens":N,"reported":true}]}}
#
# RULES HONOURED (contract section 2):
#   - Missing summary line => task recorded with completed:false, usage.reported:false. Nothing fabricated.
#   - Class segmentation (2.7): "small" = task_class in {tiny,small}; "medium" = standard; "chat"
#     excluded from code-task aggregates. Segmentation uses task_class, never category.
#   - trivial_auto_review_rate: trivial-* categories with review.tier neither "skip" nor "none".
#   - high_risk_review_retention: risky-* categories where a review actually ran (tier neither "skip" nor "none").
#   - Variance band + noise-vs-win reporting (T038) live in scripts/bench_011_report.ps1.

set -u

usage() {
  cat <<'EOF'
Usage: scripts/bench_011.sh -config <single|mixed> [-binary <path>] [-fixtures <tasks.json>] [-outdir <dir>]
  -config    Model configuration label for the run record (required: single or mixed).
  -binary    Agent binary (default: bin/muhiyacode.exe, falls back to bin/muhiyacode).
  -fixtures  Fixture task matrix JSON (default: specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json).
  -outdir    Output directory for run records (default: specs/011-competitive-agent-audit/benchmarks/runs).
Requires: jq, git.
EOF
}

CONFIG=""
BIN="bin/muhiyacode.exe"
FIXTURES="specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json"
OUTDIR="specs/011-competitive-agent-audit/benchmarks/runs"

need_val() {
  if [ "$1" -lt 2 ]; then
    echo "error: $2 requires a value" >&2
    usage
    exit 1
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    -config|--config|-Config)     need_val $# "$1"; CONFIG="$2";   shift 2 ;;
    -binary|--binary|-Binary)     need_val $# "$1"; BIN="$2";      shift 2 ;;
    -fixtures|--fixtures|-Fixtures) need_val $# "$1"; FIXTURES="$2"; shift 2 ;;
    -outdir|--outdir|-OutDir)     need_val $# "$1"; OUTDIR="$2";   shift 2 ;;
    -h|--help)                    usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

case "$CONFIG" in
  single|mixed) ;;
  *) echo "error: -config is required and must be 'single' or 'mixed'." >&2; usage; exit 1 ;;
esac

command -v jq >/dev/null 2>&1 || { echo "error: jq is required by bench_011.sh (install jq or use scripts/bench_011.ps1)." >&2; exit 1; }

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

abspath() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    *)  printf '%s/%s\n' "$REPO_ROOT" "$1" ;;
  esac
}

BIN=$(abspath "$BIN")
FIXTURES=$(abspath "$FIXTURES")
OUTDIR=$(abspath "$OUTDIR")

if [ ! -f "$BIN" ]; then
  ALT="${BIN%.exe}"
  if [ "$ALT" != "$BIN" ] && [ -f "$ALT" ]; then
    BIN="$ALT"
  else
    echo "error: agent binary not found: $BIN (build it first, e.g. 'make build')." >&2
    exit 1
  fi
fi

if [ ! -f "$FIXTURES" ]; then
  echo "error: fixtures file not found: $FIXTURES (T003 commits the fixture matrix there)." >&2
  exit 1
fi

jq -e 'type == "array" and length > 0' "$FIXTURES" >/dev/null 2>&1 || {
  echo "error: fixtures file is not a non-empty JSON array: $FIXTURES" >&2
  exit 1
}

mkdir -p "$OUTDIR" || { echo "error: cannot create output dir: $OUTDIR" >&2; exit 1; }

N=$(jq 'length' "$FIXTURES")
SHA=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null) || SHA="unknown"
[ -n "$SHA" ] || SHA="unknown"
TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
STAMP=$(date -u +%Y%m%d-%H%M%S)
RUN_ID="011-v1_${CONFIG}_${STAMP}"
FIXDIR=$(dirname "$FIXTURES")

WORK=$(mktemp -d) || { echo "error: mktemp failed" >&2; exit 1; }
trap 'rm -rf "$WORK"' EXIT INT TERM

RECORDS="$WORK/task_records.jsonl"
: > "$RECORDS"

echo "bench_011: run_id=$RUN_ID config=$CONFIG tasks=$N sha=$SHA"

i=0
while [ "$i" -lt "$N" ]; do
  TASK_ID=$(jq -r ".[$i].task_id // empty" "$FIXTURES")
  CATEGORY=$(jq -r ".[$i].category // \"uncategorized\"" "$FIXTURES")
  PROMPT=$(jq -r ".[$i].prompt // empty" "$FIXTURES")
  WS=$(jq -r ".[$i].workspace // empty" "$FIXTURES")

  if [ -z "$TASK_ID" ] || [ -z "$PROMPT" ]; then
    echo "warn: fixture entry $i missing task_id or prompt - skipped" >&2
    i=$((i + 1))
    continue
  fi

  TDIR="$WORK/ws_$i"
  mkdir -p "$TDIR"
  if [ -n "$WS" ]; then
    if [ -d "$FIXDIR/$WS" ]; then
      cp -R "$FIXDIR/$WS/." "$TDIR/"
    else
      echo "warn: workspace '$WS' not found for $TASK_ID - running in empty dir" >&2
    fi
  fi

  OUTF="$WORK/out_$i.txt"
  ERRF="$WORK/err_$i.txt"
  echo "[$((i + 1))/$N] $TASK_ID ($CATEGORY)"
  ( cd "$TDIR" && MUHIYA_BENCH_JSON=1 "$BIN" -p "$PROMPT" ) > "$OUTF" 2> "$ERRF" || \
    echo "warn: non-zero exit for $TASK_ID (see stderr capture; continuing)" >&2

  # Keep the LAST stdout line carrying a muhiya_bench summary.
  LINE=$(grep -a '"muhiya_bench"' "$OUTF" 2>/dev/null | tail -n 1) || LINE=""

  if [ -n "$LINE" ] && printf '%s' "$LINE" | jq -e '.muhiya_bench | type == "object"' >/dev/null 2>&1; then
    printf '%s' "$LINE" | jq -c --arg tid "$TASK_ID" --arg cat "$CATEGORY" \
      '{task_id: $tid, category: $cat} + .muhiya_bench' >> "$RECORDS"
  else
    # Contract section 2.3: never fabricate. No summary => not completed, usage not reported.
    echo "warn: no muhiya_bench summary for $TASK_ID - recording completed:false, reported:false" >&2
    jq -nc --arg tid "$TASK_ID" --arg cat "$CATEGORY" \
      '{task_id: $tid, category: $cat, task_class: null, completed: false, turns: 0,
        usage: {reported: false}, cost_usd: null, cost_estimated: false,
        review: {tier: "none", rationale: "no muhiya_bench summary line emitted", spend_tokens: 0, ceiling_hit: false},
        violations: null}' >> "$RECORDS"
  fi

  rm -rf "$TDIR"
  i=$((i + 1))
done

if [ ! -s "$RECORDS" ]; then
  echo "error: no runnable fixture tasks - nothing to record." >&2
  exit 1
fi

# --- Aggregates (contract section 1 + section 2.7 class segmentation) -------

AGG_JQ='
def median:
  if length == 0 then null
  else (sort | length as $n |
    if ($n % 2) == 1 then .[(($n - 1) / 2)]
    else ((.[($n / 2) - 1] + .[$n / 2]) / 2)
    end)
  end;
def tokens: ((.usage.prompt_tokens // 0) + (.usage.completion_tokens // 0));
def tier: (.review.tier // "none");
. as $tasks
| [ $tasks[] | select(.task_class != "chat") ] as $code
| [ $code[]  | select(.completed == true) ] as $done
| ([ $code[] | select(.cost_usd != null) | .cost_usd ]) as $costs
| [ $tasks[] | select((.category // "") | startswith("trivial-")) ] as $triv
| [ $tasks[] | select((.category // "") | startswith("risky-")) ] as $risky
| [ $code[]  | select(.task_class == "tiny" or .task_class == "small") ] as $small
| [ $small[] | select(.usage.reported == true) | tokens ] as $smalltok
| [ $small[] | select(.usage.reported == true) | select(tokens > 0)
    | ((.review.spend_tokens // 0) * 100.0 / tokens) ] as $ovS
| [ $code[]  | select(.task_class == "standard") | select(.usage.reported == true) | select(tokens > 0)
    | ((.review.spend_tokens // 0) * 100.0 / tokens) ] as $ovM
| {
    completion_rate: (if ($code | length) > 0 then (($done | length) / ($code | length)) else null end),
    cost_per_completed_task: (if (($done | length) > 0 and ($costs | length) > 0) then (($costs | add) / ($done | length)) else null end),
    trivial_auto_review_rate: (if ($triv | length) > 0
      then (([ $triv[] | select(tier != "skip" and tier != "none") ] | length) / ($triv | length)) else null end),
    high_risk_review_retention: (if ($risky | length) > 0
      then (([ $risky[] | select(tier != "skip" and tier != "none") ] | length) / ($risky | length)) else null end),
    median_small_task_tokens: ($smalltok | median),
    review_overhead_median_pct_small: ($ovS | median),
    review_overhead_median_pct_medium: ($ovM | median),
    violations_terminal_read_total: ([ $tasks[] | (.violations.terminal_read_when_tool_exists // 0) ] | add // 0),
    violations_duplicate_reads_total: ([ $tasks[] | (.violations.duplicate_reads // 0) ] | add // 0)
  }
'

AGGS=$(jq -s "$AGG_JQ" "$RECORDS") || { echo "error: aggregate computation failed" >&2; exit 1; }

OUT="$OUTDIR/$RUN_ID.json"
jq -s \
  --arg run_id "$RUN_ID" \
  --arg ts "$TS" \
  --arg mode "$CONFIG" \
  --arg sha "$SHA" \
  --arg bin "$BIN" \
  --arg fix "$FIXTURES" \
  --argjson aggs "$AGGS" \
  '{
     run_id: $run_id,
     suite_version: "011-v1",
     timestamp: $ts,
     config: {
       mode: $mode,
       main_model: null, sub_model: null, effort: null, review_gating: null,
       agent_build: $sha,
       binary: $bin,
       fixtures: $fix
     },
     tasks: .,
     aggregates: $aggs
   }' "$RECORDS" > "$OUT" || { echo "error: failed to write run record" >&2; exit 1; }

echo "run record written: $OUT"
exit 0
