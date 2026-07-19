# Feature 012 measure-first probes (research.md R-D12; quickstart §0).
# [GATED: live cost] — every invocation spends real provider money; run only
# with the owner's explicit go-ahead. Results land in
# specs/012-subagent-context-cache/benchmarks/probes/ as raw JSON + notes.
#
# P1 (default): DeepSeek continuation probe.
#   Runs a tiny one-shot task that forces one subagent dispatch, then a
#   follow-up prompt on the SAME session that links to it. Compare, in the
#   second run's muhiya_bench JSON: links[1].cache_share (the continuation's
#   provider-reported first-request share) against the predecessor's final
#   prompt size — settling whether DeepSeek's end-of-output cache-unit
#   boundary lets the replayed final assistant turn hit (R-F22).
#
# P3 (-EffortFlip): identical continuation but with the session effort level
#   changed between the two runs. A cold share on an otherwise identical
#   replay means top-level thinking/reasoning_effort params join DeepSeek's
#   prefix identity → EffortPinned stays true (gateway/model.go).
#
# P2 (MiniMax routing truth) is NOT scriptable from here: it starts with the
#   owner querying the production gateway providers/models tables (direct
#   api.minimax.io vs OpenRouter), then re-running this script with the
#   session's models switched to the MiniMax pairing. Record the routing
#   answer in probes/P2-routing.md before the run.

param(
    [switch]$EffortFlip,
    [string]$OutDir = "specs/012-subagent-context-cache/benchmarks/probes"
)

$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force $OutDir | Out-Null
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$work = Join-Path $env:TEMP "muhiya-probe-012-$stamp"
New-Item -ItemType Directory -Force $work | Out-Null
Set-Content -Encoding utf8 (Join-Path $work "util.go") "package util`n`n// Sum adds two ints.`nfunc Sum(a, b int) int { return a + b }`n"
Set-Content -Encoding utf8 (Join-Path $work "go.mod") "module probe012`n`ngo 1.25`n"

$env:MUHIYA_BENCH_JSON = "1"
$label = "P1"; if ($EffortFlip) { $label = "P3" }

Write-Host "==> $label run 1/2 (predecessor dispatch)..."
& go run ./cmd/muhiyacode --workspace $work --print "Use one general subagent to add a Product function to util.go with a doc comment. Delegate it; do not implement directly." *>&1 |
    Tee-Object (Join-Path $OutDir "$label-$stamp-run1.log") | Select-Object -Last 1 | Set-Content (Join-Path $OutDir "$label-$stamp-run1.json")

if ($EffortFlip) {
    Write-Host "==> flipping effort for run 2 (P3)..."
    & go run ./cmd/muhiyacode --workspace $work --print "/config effort high" *>&1 | Out-Null
}

Write-Host "==> $label run 2/2 (continuation dispatch)..."
& go run ./cmd/muhiyacode --workspace $work --print "Use one general subagent to continue the previous work: add a Quotient function next to Product in util.go. Delegate it." *>&1 |
    Tee-Object (Join-Path $OutDir "$label-$stamp-run2.log") | Select-Object -Last 1 | Set-Content (Join-Path $OutDir "$label-$stamp-run2.json")

Write-Host "==> Done. Inspect links[] in $label-$stamp-run2.json:"
Write-Host "    decision=continued + cache_share near 1.0  -> replay hits (end-of-output boundary favorable)"
Write-Host "    decision=continued + cache_share low/0     -> cold; record attribution before concluding"
Write-Host "    Record conclusions in $OutDir/$label-$stamp-notes.md"
