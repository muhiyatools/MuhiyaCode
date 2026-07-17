# Make the freshly-built binary the machine's `muhiyacode` command (Stability
# Overhaul T003, defects D3). This ends the "same error keeps showing" saga: the
# global npm launcher runs a VENDORED exe that a plain `go build` never touches,
# so fixes silently never reach the user. This script gates, builds stamped, then
# overwrites that vendored exe and prints BOTH version lines so drift is visible.
#
# Usage:
#   .\scripts\swap-global.ps1            # gate + build + swap
#   .\scripts\swap-global.ps1 -Serial     # gate uses go test -p 1
#   .\scripts\swap-global.ps1 -SkipCheck  # build + swap only (skip the gate) — not for releases
[CmdletBinding()]
param(
  [switch]$Serial,
  [switch]$SkipCheck
)

$ErrorActionPreference = "Stop"

$vendorDir = Join-Path $env:APPDATA "npm\node_modules\muhiyacode\vendor"
$dest = Join-Path $vendorDir "muhiyacode.exe"

if (-not (Test-Path $vendorDir)) {
  Write-Host "The global 'muhiyacode' npm package is not installed at:" -ForegroundColor Yellow
  Write-Host "  $vendorDir" -ForegroundColor Yellow
  Write-Host "Install it first:  npm install -g muhiyacode" -ForegroundColor Yellow
  Write-Host "…then re-run this script to overwrite it with your local build." -ForegroundColor Yellow
  exit 1
}

# 1. Gate — a failing check.ps1 calls `exit 1`, which stops this script before any swap.
if (-not $SkipCheck) {
  Write-Host "==> Gate: scripts/check.ps1..." -ForegroundColor Cyan
  if ($Serial) { & "$PSScriptRoot\check.ps1" -Serial } else { & "$PSScriptRoot\check.ps1" }
}

# 2. Stamped build.
Write-Host "==> Build: scripts/build.ps1..." -ForegroundColor Cyan
& "$PSScriptRoot\build.ps1"

# 3. Overwrite the vendored exe (handle the file-locked case gracefully).
try {
  Copy-Item -Path (Join-Path (Get-Location) "muhiyacode.exe") -Destination $dest -Force
} catch {
  Write-Host "Could not overwrite $dest" -ForegroundColor Red
  Write-Host "Close any running MuhiyaCode sessions (the exe is locked while running) and re-run." -ForegroundColor Yellow
  exit 1
}
Write-Host "==> Swapped into: $dest" -ForegroundColor Green

# 4. Show both version lines — they MUST match. A mismatch means something else is shadowing.
Write-Host "==> Repo build:   " -NoNewline -ForegroundColor Cyan; & (Join-Path (Get-Location) "muhiyacode.exe") --version
Write-Host "==> Global build: " -NoNewline -ForegroundColor Cyan; & $dest --version

Write-Host ""
Write-Host "Done. Restart any running MuhiyaCode sessions to pick up the new binary." -ForegroundColor Yellow
