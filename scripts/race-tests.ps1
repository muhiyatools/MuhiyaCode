# Race detector runner for the concurrency-heavy packages (OVERHAUL_PLAN V1).
#
# The existing unit tests already cover the concurrency surface (MCP manager
# refresh/connect goroutines, orchestrator executeBatch parallel subagents,
# goal/plan mutex discipline). The -race flag adds Go's data-race detector on
# top, which requires CGO + a C compiler (gcc).
#
# This Windows development host does not have GCC installed, so -race cannot
# run here. Run this script on a CGO-capable host: Windows with MinGW-w64 or
# TDM-GCC installed, or Linux/macOS using scripts/race-tests.sh.
#
# Usage:
#   .\scripts\race-tests.ps1              # focused: mcpclient + orchestrator
#   .\scripts\race-tests.ps1 -Full        # all packages (equivalent to: make test-race)
[CmdletBinding()]
param(
  [switch]$Full
)

$ErrorActionPreference = "Stop"

Write-Host "==> Checking for C compiler..." -ForegroundColor Cyan
$gcc = Get-Command gcc -ErrorAction SilentlyContinue
$clang = Get-Command clang -ErrorAction SilentlyContinue
if (-not $gcc -and -not $clang) {
  Write-Error "No C compiler (gcc/clang) found. The race detector requires CGO.`nInstall MinGW-w64 or TDM-GCC on Windows, or run scripts/race-tests.sh on Linux/macOS."
  exit 1
}

$env:CGO_ENABLED = "1"

if ($Full) {
  Write-Host "==> Running race detector across ALL packages..." -ForegroundColor Cyan
  go test -race ./... -count=1
} else {
  Write-Host "==> Running race detector on concurrency-heavy packages (V1)..." -ForegroundColor Cyan
  go test -race ./internal/mcpclient/... ./internal/orchestrator/... -count=1
}

if ($LASTEXITCODE -eq 0) {
  Write-Host "==> Race detector passed." -ForegroundColor Green
} else {
  Write-Error "Race detector failed."
  exit 1
}
