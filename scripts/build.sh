#!/usr/bin/env bash
# Stamped build for MuhiyaCode (Stability Overhaul T002, defect D2) — bash twin of
# build.ps1. Injects the git short-commit (+dirty) and build date via -ldflags so
# `muhiyacode --version` is self-identifying instead of "commit unknown".
#
# Usage: scripts/build.sh
set -euo pipefail

commit=$(git rev-parse --short HEAD 2>/dev/null || echo nogit)
if [ -n "$(git status --porcelain 2>/dev/null || true)" ]; then
  commit="${commit}+dirty"
fi
date=$(date +%Y-%m-%d)
pkg="github.com/muhiya/muhiyacode/internal/buildinfo"

echo "==> Building muhiyacode.exe (commit ${commit}, date ${date})..."
go build -ldflags "-X ${pkg}.Commit=${commit} -X ${pkg}.Date=${date}" -o muhiyacode.exe ./cmd/muhiyacode

echo "==> Built:"
./muhiyacode.exe --version
