# The single pre-flight gate for MuhiyaCode (Stability Overhaul T004).
#
# Nothing ships — no binary swap, no release checkpoint — unless this is green.
# Sections hard-fail in order: format, vet, build, tests, dead-code budget.
#
# Usage:
#   .\scripts\check.ps1              # full run
#   .\scripts\check.ps1 -Serial      # go test -p 1 (use if the temp disk fills)
#   .\scripts\check.ps1 -GauntletOnly # just the acceptance gauntlet (fast pre-check)
[CmdletBinding()]
param(
  [switch]$Serial,
  [switch]$GauntletOnly
)

$ErrorActionPreference = "Stop"

function Fail($msg) { Write-Error $msg; exit 1 }

if ($GauntletOnly) {
  Write-Host "==> Gauntlet only..." -ForegroundColor Cyan
  go test ./internal/orchestrator/ -run TestGauntlet -count=1
  if ($LASTEXITCODE -ne 0) { Fail "Gauntlet failed." }
  Write-Host "==> Gauntlet passed." -ForegroundColor Green
  exit 0
}

Write-Host "==> [1/5] gofmt..." -ForegroundColor Cyan
$formatCandidates = (gofmt -l internal cmd benchmarks 2>$null)
$unformatted = @()
foreach ($path in ($formatCandidates -split "`n")) {
  $path = $path.Trim()
  if ([string]::IsNullOrWhiteSpace($path)) { continue }
  # gofmt reports every CRLF file on Windows because its output is LF. Compare
  # removed/added text from the diff so line-ending-only changes do not make the
  # repository's documented Windows gate impossible to pass.
  $diff = (gofmt -d $path 2>$null)
  $removed = @($diff | Where-Object { $_.StartsWith('-') -and -not $_.StartsWith('---') } | ForEach-Object { $_.Substring(1) })
  $added = @($diff | Where-Object { $_.StartsWith('+') -and -not $_.StartsWith('+++') } | ForEach-Object { $_.Substring(1) })
  if (($removed -join "`n") -ne ($added -join "`n")) { $unformatted += $path }
}
if ($unformatted.Count -gt 0) {
  $unformatted | ForEach-Object { Write-Host $_ }
  Fail "gofmt: files above are not formatted. Run: gofmt -w <files>"
}

Write-Host "==> [2/5] go vet..." -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { Fail "go vet failed." }

Write-Host "==> [3/5] go build..." -ForegroundColor Cyan
go build ./...
if ($LASTEXITCODE -ne 0) { Fail "go build failed." }

Write-Host "==> [4/5] go test$(if ($Serial) { ' -p 1' })..." -ForegroundColor Cyan
if ($Serial) {
  go test ./... -count=1 -p 1
} else {
  go test ./... -count=1
}
if ($LASTEXITCODE -ne 0) { Fail "tests failed." }

Write-Host "==> [5/5] dead-code budget..." -ForegroundColor Cyan
# Documented irreducible cross-package test-support exports. Any NON-test
# unreachable func not on this allowlist is a NEW leak and fails the gate.
$allowed = @(
  'instructions[\\/]types\.go.*unreachable func: All',
  'workspace[\\/]permissions\.go.*unreachable func: NewMemoryTrustStore',
  'workspace[\\/]permissions\.go.*unreachable func: MemoryTrustStore\.IsTrusted',
  'workspace[\\/]permissions\.go.*unreachable func: MemoryTrustStore\.Trust'
)
$dead = (go run golang.org/x/tools/cmd/deadcode@v0.47.0 ./... 2>$null)
$violations = @()
foreach ($line in ($dead -split "`n")) {
  $t = $line.Trim()
  if ([string]::IsNullOrWhiteSpace($t)) { continue }
  if ($t -match '_test\.go') { continue }
  if ($t -match 'unreachable func: (init|main)$') { continue }
  $ok = $false
  foreach ($a in $allowed) { if ($t -match $a) { $ok = $true; break } }
  if (-not $ok) { $violations += $t }
}
if ($violations.Count -gt 0) {
  $violations | ForEach-Object { Write-Host $_ }
  Fail "dead-code budget exceeded: the lines above are NOT on the allowlist. Remove the dead code or justify + extend the allowlist."
}

Write-Host "==> ALL CHECKS PASSED." -ForegroundColor Green
