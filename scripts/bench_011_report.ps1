<#
.SYNOPSIS
  bench_011_report.ps1 - Comparison report generator for feature 011 benchmarks. T005 + T038 + T039.

.DESCRIPTION
  Reads benchmark run records (written by scripts/bench_011.ps1 / .sh per
  specs/011-competitive-agent-audit/contracts/benchmark-run.md) and writes
  specs/011-competitive-agent-audit/benchmarks/RESULTS.md containing:
    - the T038 variance band per aggregate: max |delta| between the baseline runs
      of the same config (contract section 2.2) - any before/after delta smaller
      than the band is reported as "noise", never as a win;
    - a before/after table per aggregate per config;
    - SC verdict rows (SC-001, SC-002, SC-003, SC-004, SC-006, SC-008).

  USAGE
    powershell -NoProfile -File scripts/bench_011_report.ps1 -Baseline <dir> [-After <dir>]

  PARAMETERS
    -Baseline  directory of baseline run records (*.json), e.g. specs/011-competitive-agent-audit/benchmarks/runs/baseline
    -After     directory of post-transformation run records (optional). Omitted =>
               baseline-only report: aggregates + variance band, SC rows PENDING.

  VIOLATION COUNTS (T005): tasks[].violations in each run record are the
  authoritative agent-emitted counters (terminal_read_when_tool_exists,
  duplicate_reads) carried through the muhiya_bench summary; this report
  aggregates them (already totalled in aggregates.violations_*_total by the
  runner). Nothing is re-derived from rendered TUI output, and absent counters
  are treated as absent - never zero-filled as data.

  SC RULES APPLIED
    SC-001  after trivial_auto_review_rate < 10%
    SC-002  after high_risk_review_retention >= 95%
    SC-003  median_small_task_tokens reduced >= 30% vs baseline (task_class in {tiny,small})
    SC-004  both segment clauses: review_overhead_median_pct_small <= 20 AND _medium <= 20
            (ceiling compliance itself is proven by the T024 conformance test; ceiling_hit
            counts are listed for reference)
    SC-006  violation totals; PASS only when after totals are 0 - a non-zero total needs the
            file-access-action denominator (not in run records) and is marked MANUAL
    SC-008  completion_rate(after) >= baseline AND cost_per_completed_task reduced beyond the
            variance band (a cost delta inside the band reports as noise, not a win)
#>
[CmdletBinding()]
param(
    [string]$Baseline,
    [string]$After
)

$ErrorActionPreference = 'Stop'

function Show-Usage {
    Write-Host "Usage: powershell -NoProfile -File scripts/bench_011_report.ps1 -Baseline <dir> [-After <dir>]"
    Write-Host "  -Baseline  Directory containing baseline run records (*.json). Required."
    Write-Host "  -After     Directory containing post-transformation run records (*.json). Optional."
    Write-Host "Writes: specs/011-competitive-agent-audit/benchmarks/RESULTS.md"
}

# --- Helpers ---------------------------------------------------------------

function Get-Prop($obj, [string]$name, $default) {
    if ($null -eq $obj) { return $default }
    if ($obj -is [System.Collections.IDictionary]) {
        if ($obj.Contains($name)) {
            $v = $obj[$name]
            if ($null -ne $v) { return $v }
        }
        return $default
    }
    $p = $obj.PSObject.Properties[$name]
    if ($null -eq $p) { return $default }
    if ($null -eq $p.Value) { return $default }
    return $p.Value
}

function Get-Agg($run, [string]$name) {
    return Get-Prop (Get-Prop $run 'aggregates' $null) $name $null
}

function Read-Runs([string]$dir) {
    $runs = @()
    $files = @(Get-ChildItem -LiteralPath $dir -Filter '*.json' -File | Sort-Object Name)
    foreach ($f in $files) {
        try {
            $r = (Get-Content -LiteralPath $f.FullName -Raw) | ConvertFrom-Json
            if ($null -eq (Get-Prop $r 'aggregates' $null)) {
                Write-Warning ("{0}: no aggregates block - skipped" -f $f.Name)
                continue
            }
            $runs += , $r
        } catch {
            Write-Warning ("{0}: unparseable run record - skipped ({1})" -f $f.Name, $_.Exception.Message)
        }
    }
    return , $runs
}

function Group-ByMode($runs) {
    $byMode = @{}
    foreach ($r in $runs) {
        $mode = [string](Get-Prop (Get-Prop $r 'config' $null) 'mode' 'unknown')
        if (-not $byMode.ContainsKey($mode)) { $byMode[$mode] = @() }
        $byMode[$mode] += , $r
    }
    return $byMode
}

function Get-AggValues($runs, [string]$name) {
    $vals = @()
    foreach ($r in $runs) {
        $v = Get-Agg $r $name
        if ($null -ne $v) { $vals += [double]$v }
    }
    return , $vals
}

# T038: variance band = max |delta| between the baseline runs of the same config.
function Get-Band($runs, [string]$name) {
    $vals = Get-AggValues $runs $name
    if ($vals.Count -lt 2) { return $null }
    $max = 0.0
    for ($i = 0; $i -lt $vals.Count; $i++) {
        for ($j = $i + 1; $j -lt $vals.Count; $j++) {
            $d = [math]::Abs($vals[$i] - $vals[$j])
            if ($d -gt $max) { $max = $d }
        }
    }
    return $max
}

function Get-MeanAgg($runs, [string]$name) {
    $vals = Get-AggValues $runs $name
    if ($vals.Count -eq 0) { return $null }
    $sum = 0.0
    foreach ($v in $vals) { $sum += $v }
    return $sum / $vals.Count
}

function Format-Num($v) {
    if ($null -eq $v) { return 'n/a' }
    return ('{0:0.####}' -f [double]$v)
}

function Format-Pct($v) {
    if ($null -eq $v) { return 'n/a' }
    return ('{0:0.##}%' -f ([double]$v * 100.0))
}

# Delta-vs-band verdict per contract section 2.2: smaller than the band => noise.
function Get-DeltaVerdict($delta, $band) {
    if ($null -eq $delta) { return 'n/a' }
    if ([double]$delta -eq 0) { return 'no change' }
    if ($null -eq $band) { return 'no band (need 2 baseline runs)' }
    if ([math]::Abs([double]$delta) -lt [double]$band) { return 'noise' }
    return 'significant'
}

# --- Argument validation ---------------------------------------------------

if ([string]::IsNullOrEmpty($Baseline)) {
    Write-Host "error: -Baseline is required." -ForegroundColor Red
    Show-Usage
    exit 1
}

$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not [System.IO.Path]::IsPathRooted($Baseline)) { $Baseline = Join-Path $RepoRoot $Baseline }
if (-not (Test-Path -LiteralPath $Baseline -PathType Container)) {
    Write-Host "error: baseline directory not found: $Baseline" -ForegroundColor Red
    exit 1
}

$afterRuns = @()
$haveAfter = $false
if (-not [string]::IsNullOrEmpty($After)) {
    if (-not [System.IO.Path]::IsPathRooted($After)) { $After = Join-Path $RepoRoot $After }
    if (-not (Test-Path -LiteralPath $After -PathType Container)) {
        Write-Host "error: after directory not found: $After" -ForegroundColor Red
        exit 1
    }
    $haveAfter = $true
}

$baseRuns = Read-Runs $Baseline
if ($baseRuns.Count -eq 0) {
    Write-Host "error: no readable run records (*.json) in baseline dir: $Baseline" -ForegroundColor Red
    exit 1
}
if ($haveAfter) {
    $afterRuns = Read-Runs $After
    if ($afterRuns.Count -eq 0) {
        Write-Host "error: -After given but no readable run records in: $After" -ForegroundColor Red
        exit 1
    }
}

$aggDefs = @(
    @{ Name = 'completion_rate';                   Label = 'Completion rate';                       Pct = $true  },
    @{ Name = 'cost_per_completed_task';           Label = 'Cost per completed task (USD)';         Pct = $false },
    @{ Name = 'trivial_auto_review_rate';          Label = 'Trivial auto-review rate';              Pct = $true  },
    @{ Name = 'high_risk_review_retention';        Label = 'High-risk review retention';            Pct = $true  },
    @{ Name = 'median_small_task_tokens';          Label = 'Median small-task tokens (tiny+small)'; Pct = $false },
    @{ Name = 'review_overhead_median_pct_small';  Label = 'Review overhead median % (small)';      Pct = $false },
    @{ Name = 'review_overhead_median_pct_medium'; Label = 'Review overhead median % (medium)';     Pct = $false },
    @{ Name = 'violations_terminal_read_total';    Label = 'Violations: terminal-read-when-tool-exists (total)'; Pct = $false },
    @{ Name = 'violations_duplicate_reads_total';  Label = 'Violations: duplicate reads (total)';   Pct = $false }
)

$baseByMode = Group-ByMode $baseRuns
$afterByMode = @{}
if ($haveAfter) { $afterByMode = Group-ByMode $afterRuns }

$modes = @($baseByMode.Keys | Sort-Object)

# --- Build RESULTS.md ------------------------------------------------------

$lines = New-Object System.Collections.ArrayList
function Add-Line([string]$s) { [void]$script:lines.Add($s) }

Add-Line '# Feature 011 Benchmark Results'
Add-Line ''
Add-Line ('Generated: {0} by `scripts/bench_011_report.ps1` (contract: `contracts/benchmark-run.md`)' -f (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ'))
Add-Line ''
Add-Line ('- Baseline dir: `{0}` ({1} run record(s))' -f $Baseline, $baseRuns.Count)
if ($haveAfter) {
    Add-Line ('- After dir: `{0}` ({1} run record(s))' -f $After, $afterRuns.Count)
} else {
    Add-Line '- After dir: none - baseline-only report; SC verdicts are PENDING.'
}
Add-Line ''
Add-Line 'Variance band rule (contract 2.2 / T038): per aggregate, band = max |delta| between the baseline runs of the same config; any before/after delta smaller than the band is reported as **noise**, never as a win (SC-008).'
Add-Line ''

Add-Line '## Run inventory'
Add-Line ''
Add-Line '| Set | run_id | Config | Timestamp | Tasks | Completion |'
Add-Line '|---|---|---|---|---|---|'
foreach ($r in $baseRuns) {
    $mode = Get-Prop (Get-Prop $r 'config' $null) 'mode' 'unknown'
    Add-Line ('| baseline | {0} | {1} | {2} | {3} | {4} |' -f (Get-Prop $r 'run_id' '?'), $mode, (Get-Prop $r 'timestamp' '?'), (@(Get-Prop $r 'tasks' @()).Count), (Format-Pct (Get-Agg $r 'completion_rate')))
}
foreach ($r in $afterRuns) {
    $mode = Get-Prop (Get-Prop $r 'config' $null) 'mode' 'unknown'
    Add-Line ('| after | {0} | {1} | {2} | {3} | {4} |' -f (Get-Prop $r 'run_id' '?'), $mode, (Get-Prop $r 'timestamp' '?'), (@(Get-Prop $r 'tasks' @()).Count), (Format-Pct (Get-Agg $r 'completion_rate')))
}
Add-Line ''

foreach ($mode in $modes) {
    $bRuns = $baseByMode[$mode]
    $aRuns = @()
    if ($afterByMode.ContainsKey($mode)) { $aRuns = $afterByMode[$mode] }
    $modeHasAfter = ($aRuns.Count -gt 0)

    Add-Line ('## Config: {0}' -f $mode)
    Add-Line ''
    if ($bRuns.Count -lt 2) {
        Add-Line ('> WARNING: only {0} baseline run(s) for this config - the variance band needs a double run (contract 2.2); band shown as n/a.' -f $bRuns.Count)
        Add-Line ''
    }

    # Per-aggregate before/after table with the band (T038 + T039).
    Add-Line '### Aggregates: before/after vs variance band'
    Add-Line ''
    if ($modeHasAfter) {
        Add-Line '| Aggregate | Baseline (mean) | After (mean) | Delta | Band | Verdict |'
        Add-Line '|---|---|---|---|---|---|'
    } else {
        Add-Line '| Aggregate | Baseline (mean) | Band |'
        Add-Line '|---|---|---|'
    }

    foreach ($def in $aggDefs) {
        $name = $def.Name
        $bVal = Get-MeanAgg $bRuns $name
        $band = Get-Band $bRuns $name
        $fmtB = ''
        if ($def.Pct) { $fmtB = Format-Pct $bVal } else { $fmtB = Format-Num $bVal }
        $fmtBand = Format-Num $band
        if ($null -ne $band -and $def.Pct) { $fmtBand = Format-Pct $band }

        if ($modeHasAfter) {
            $aVal = Get-MeanAgg $aRuns $name
            $delta = $null
            if ($null -ne $bVal -and $null -ne $aVal) { $delta = [double]$aVal - [double]$bVal }
            $fmtA = ''
            if ($def.Pct) { $fmtA = Format-Pct $aVal } else { $fmtA = Format-Num $aVal }
            $fmtD = ''
            if ($def.Pct) { $fmtD = Format-Pct $delta } else { $fmtD = Format-Num $delta }
            $verdict = Get-DeltaVerdict $delta $band
            Add-Line ('| {0} | {1} | {2} | {3} | {4} | {5} |' -f $def.Label, $fmtB, $fmtA, $fmtD, $fmtBand, $verdict)
        } else {
            Add-Line ('| {0} | {1} | {2} |' -f $def.Label, $fmtB, $fmtBand)
        }
    }
    Add-Line ''

    # Ceiling-hit reference counts (SC-004 clause ii context).
    $bCeil = 0
    foreach ($r in $bRuns) {
        foreach ($t in @(Get-Prop $r 'tasks' @())) {
            if ([bool](Get-Prop (Get-Prop $t 'review' $null) 'ceiling_hit' $false)) { $bCeil++ }
        }
    }
    $aCeil = 0
    foreach ($r in $aRuns) {
        foreach ($t in @(Get-Prop $r 'tasks' @())) {
            if ([bool](Get-Prop (Get-Prop $t 'review' $null) 'ceiling_hit' $false)) { $aCeil++ }
        }
    }
    Add-Line ('Ceiling-hit tasks (reference for SC-004 clause ii; compliance proven by the T024 conformance test): baseline {0}, after {1}.' -f $bCeil, $aCeil)
    Add-Line ''

    # SC verdict rows.
    Add-Line '### Success-criteria verdicts'
    Add-Line ''
    Add-Line '| SC | Rule | Baseline | After | Verdict |'
    Add-Line '|---|---|---|---|---|'

    $bTriv = Get-MeanAgg $bRuns 'trivial_auto_review_rate'
    $bRisk = Get-MeanAgg $bRuns 'high_risk_review_retention'
    $bTok  = Get-MeanAgg $bRuns 'median_small_task_tokens'
    $bOvS  = Get-MeanAgg $bRuns 'review_overhead_median_pct_small'
    $bOvM  = Get-MeanAgg $bRuns 'review_overhead_median_pct_medium'
    $bVio  = $null
    $bV1 = Get-MeanAgg $bRuns 'violations_terminal_read_total'
    $bV2 = Get-MeanAgg $bRuns 'violations_duplicate_reads_total'
    if ($null -ne $bV1 -or $null -ne $bV2) {
        $bVio = 0.0
        if ($null -ne $bV1) { $bVio += [double]$bV1 }
        if ($null -ne $bV2) { $bVio += [double]$bV2 }
    }
    $bComp = Get-MeanAgg $bRuns 'completion_rate'
    $bCost = Get-MeanAgg $bRuns 'cost_per_completed_task'

    if (-not $modeHasAfter) {
        Add-Line ('| SC-001 | trivial auto-review rate < 10% | {0} | - | PENDING (no after runs) |' -f (Format-Pct $bTriv))
        Add-Line ('| SC-002 | high-risk review retention >= 95% | {0} | - | PENDING (no after runs) |' -f (Format-Pct $bRisk))
        Add-Line ('| SC-003 | median small-task tokens reduced >= 30% | {0} | - | PENDING (no after runs) |' -f (Format-Num $bTok))
        Add-Line ('| SC-004 | review overhead median <= 20% (small AND medium) | small {0} / medium {1} | - | PENDING (no after runs) |' -f (Format-Num $bOvS), (Format-Num $bOvM))
        Add-Line ('| SC-006 | violations < 2% of file-access actions | total {0} | - | PENDING (no after runs) |' -f (Format-Num $bVio))
        Add-Line ('| SC-008 | completion >= baseline at lower cost/completed (beyond band) | comp {0} / cost {1} | - | PENDING (no after runs) |' -f (Format-Pct $bComp), (Format-Num $bCost))
        Add-Line ''
        continue
    }

    $aTriv = Get-MeanAgg $aRuns 'trivial_auto_review_rate'
    $aRisk = Get-MeanAgg $aRuns 'high_risk_review_retention'
    $aTok  = Get-MeanAgg $aRuns 'median_small_task_tokens'
    $aOvS  = Get-MeanAgg $aRuns 'review_overhead_median_pct_small'
    $aOvM  = Get-MeanAgg $aRuns 'review_overhead_median_pct_medium'
    $aVio  = $null
    $aV1 = Get-MeanAgg $aRuns 'violations_terminal_read_total'
    $aV2 = Get-MeanAgg $aRuns 'violations_duplicate_reads_total'
    if ($null -ne $aV1 -or $null -ne $aV2) {
        $aVio = 0.0
        if ($null -ne $aV1) { $aVio += [double]$aV1 }
        if ($null -ne $aV2) { $aVio += [double]$aV2 }
    }
    $aComp = Get-MeanAgg $aRuns 'completion_rate'
    $aCost = Get-MeanAgg $aRuns 'cost_per_completed_task'

    # SC-001
    $v = 'NO DATA'
    if ($null -ne $aTriv) {
        if ([double]$aTriv -lt 0.10) { $v = 'PASS' } else { $v = 'FAIL' }
    }
    Add-Line ('| SC-001 | trivial auto-review rate < 10% | {0} | {1} | {2} |' -f (Format-Pct $bTriv), (Format-Pct $aTriv), $v)

    # SC-002
    $v = 'NO DATA'
    if ($null -ne $aRisk) {
        if ([double]$aRisk -ge 0.95) { $v = 'PASS' } else { $v = 'FAIL' }
    }
    Add-Line ('| SC-002 | high-risk review retention >= 95% | {0} | {1} | {2} |' -f (Format-Pct $bRisk), (Format-Pct $aRisk), $v)

    # SC-003 (reduction >= 30%, and a delta inside the band is only noise)
    $v = 'NO DATA'
    if ($null -ne $bTok -and $null -ne $aTok -and [double]$bTok -gt 0) {
        $tokBand = Get-Band $bRuns 'median_small_task_tokens'
        $reduction = ([double]$bTok - [double]$aTok) / [double]$bTok
        if ($null -ne $tokBand -and [math]::Abs([double]$aTok - [double]$bTok) -lt [double]$tokBand) {
            $v = ('NOISE (delta inside band {0})' -f (Format-Num $tokBand))
        } elseif ($reduction -ge 0.30) {
            $v = ('PASS ({0} reduction)' -f (Format-Pct $reduction))
        } else {
            $v = ('FAIL ({0} reduction)' -f (Format-Pct $reduction))
        }
    }
    Add-Line ('| SC-003 | median small-task tokens reduced >= 30% | {0} | {1} | {2} |' -f (Format-Num $bTok), (Format-Num $aTok), $v)

    # SC-004 (both segment clauses)
    $v = 'NO DATA'
    if ($null -ne $aOvS -and $null -ne $aOvM) {
        if ([double]$aOvS -le 20.0 -and [double]$aOvM -le 20.0) { $v = 'PASS' } else { $v = 'FAIL' }
    } elseif ($null -ne $aOvS -or $null -ne $aOvM) {
        $v = 'PARTIAL DATA (one segment missing)'
    }
    Add-Line ('| SC-004 | review overhead median <= 20% (small AND medium) | small {0} / medium {1} | small {2} / medium {3} | {4} |' -f (Format-Num $bOvS), (Format-Num $bOvM), (Format-Num $aOvS), (Format-Num $aOvM), $v)

    # SC-006 (denominator = file-access actions, not carried in run records)
    $v = 'NO DATA'
    if ($null -ne $aVio) {
        if ([double]$aVio -eq 0) { $v = 'PASS (zero violations)' }
        else { $v = 'MANUAL (non-zero total; file-access-action denominator not in run records)' }
    }
    Add-Line ('| SC-006 | violations < 2% of file-access actions | total {0} | total {1} | {2} |' -f (Format-Num $bVio), (Format-Num $aVio), $v)

    # SC-008 (completion maintained AND cost reduced beyond the band)
    $v = 'NO DATA'
    if ($null -ne $bComp -and $null -ne $aComp -and $null -ne $bCost -and $null -ne $aCost) {
        $compBand = Get-Band $bRuns 'completion_rate'
        $costBand = Get-Band $bRuns 'cost_per_completed_task'
        $compOk = ([double]$aComp -ge [double]$bComp)
        if (-not $compOk -and $null -ne $compBand -and [math]::Abs([double]$aComp - [double]$bComp) -lt [double]$compBand) {
            $compOk = $true   # completion dip inside the band => maintained (noise)
        }
        $costDrop = [double]$bCost - [double]$aCost
        if (-not $compOk) {
            $v = 'FAIL (completion regressed beyond band)'
        } elseif ($costDrop -le 0) {
            $v = 'FAIL (cost per completed task did not drop)'
        } elseif ($null -ne $costBand -and $costDrop -lt [double]$costBand) {
            $v = ('NOISE (cost drop {0} inside band {1})' -f (Format-Num $costDrop), (Format-Num $costBand))
        } else {
            $v = ('PASS (cost drop {0} beyond band)' -f (Format-Num $costDrop))
        }
    }
    Add-Line ('| SC-008 | completion >= baseline at lower cost/completed (beyond band) | comp {0} / cost {1} | comp {2} / cost {3} | {4} |' -f (Format-Pct $bComp), (Format-Num $bCost), (Format-Pct $aComp), (Format-Num $aCost), $v)
    Add-Line ''
}

# After-only modes with no baseline counterpart.
if ($haveAfter) {
    foreach ($mode in @($afterByMode.Keys | Sort-Object)) {
        if (-not $baseByMode.ContainsKey($mode)) {
            Add-Line ('## Config: {0}' -f $mode)
            Add-Line ''
            Add-Line '> WARNING: after runs exist for this config but no baseline runs - no comparison possible (contract 2.1: baseline first).'
            Add-Line ''
        }
    }
}

$resultsPath = Join-Path $RepoRoot 'specs\011-competitive-agent-audit\benchmarks\RESULTS.md'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $resultsPath) | Out-Null
$content = ($lines -join "`n") + "`n"
[System.IO.File]::WriteAllText($resultsPath, $content, (New-Object System.Text.UTF8Encoding($false)))

Write-Host ("results written: {0}" -f $resultsPath)
exit 0
