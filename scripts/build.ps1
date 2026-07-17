# Stamped build for MuhiyaCode (Stability Overhaul T002, defect D2).
#
# A plain `go build` leaves buildinfo.Commit / buildinfo.Date at "unknown", so
# `muhiyacode --version` cannot reveal WHICH build is running — the root cause of
# the recurring "the same error keeps showing" saga (a stale binary shadowing the
# rebuild). This script injects the git short-commit (+dirty when the tree has
# uncommitted changes) and the build date via -ldflags, then prints the version
# so every build is self-identifying.
#
# Usage:
#   .\scripts\build.ps1            # builds ./muhiyacode.exe, stamped, and prints --version
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$commit = (git rev-parse --short HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) { $commit = "nogit" }
$porcelain = (git status --porcelain 2>$null)
if (-not [string]::IsNullOrWhiteSpace($porcelain)) { $commit = "$commit+dirty" }
$date = Get-Date -Format "yyyy-MM-dd"
$pkg = "github.com/muhiya/muhiyacode/internal/buildinfo"

Write-Host "==> Building muhiyacode.exe (commit $commit, date $date)..." -ForegroundColor Cyan
go build -ldflags "-X $pkg.Commit=$commit -X $pkg.Date=$date" -o muhiyacode.exe ./cmd/muhiyacode
if ($LASTEXITCODE -ne 0) {
  Write-Error "Build failed."
  exit 1
}

Write-Host "==> Built:" -ForegroundColor Green
& .\muhiyacode.exe --version
