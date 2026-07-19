<#
.SYNOPSIS
  bench_011.ps1 - Benchmark runner for feature 011 (competitive agent audit). T004(b).

.DESCRIPTION
  Drives the fixed fixture task matrix through the muhiyacode binary in one-shot
  (-p) mode and assembles one JSON run record per invocation, exactly per
  specs/011-competitive-agent-audit/contracts/benchmark-run.md section 1.

  USAGE
    powershell -NoProfile -File scripts/bench_011.ps1 -Config single
    powershell -NoProfile -File scripts/bench_011.ps1 -Config mixed -Binary bin/muhiyacode.exe

  PARAMETERS
    -Config    single|mixed  (required; recorded in the run record's config snapshot)
    -Binary    path to the agent binary            (default: bin/muhiyacode.exe)
    -Fixtures  path to the fixture matrix JSON     (default: specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json)
    -OutDir    directory for run records           (default: specs/011-competitive-agent-audit/benchmarks/runs)

  FIXTURE FORMAT (tasks.json): a JSON array of entries
    { "task_id": "...", "category": "trivial-docs|trivial-comment|rename|format|config|standard-logic|risky-auth|risky-billing|large-multifile|greenfield-scaffold",
      "prompt": "...", "workspace": "relative/dir under fixtures/ (optional; omitted => empty temp workspace)" }

  CONSUMED AGENT OUTPUT (emitted by the agent when MUHIYA_BENCH_JSON=1; T004(a)):
  the LAST stdout line of each one-shot run is one JSON object:
    {"muhiya_bench":{"task_class":"chat|tiny|small|standard|large|epic","completed":true,"turns":N,
      "usage":{"prompt_tokens":N,"completion_tokens":N,"cache_read_tokens":N,"cache_miss_tokens":N,"cache_write_tokens":N,"reported":true},
      "cost_usd":N,"cost_estimated":bool,
      "review":{"tier":"skip|focused|deep|none","rationale":"...","spend_tokens":N,"ceiling_hit":false},
      "violations":{"terminal_read_when_tool_exists":N,"duplicate_reads":N},
      "per_pairing":[{"model":"...","pin":":main","prompt_tokens":N,"cache_read_tokens":N,"reported":true}]}}

  RULES HONOURED (contract section 2):
    - Missing summary line => task recorded with completed:false and usage.reported:false. Nothing fabricated.
    - Class segmentation (section 2.7): "small" = task_class in {tiny,small}; "medium" = standard;
      task_class == "chat" excluded from code-task aggregates. Segmentation always uses task_class, never category.
    - trivial_auto_review_rate: trivial-* categories where review.tier is neither "skip" nor "none".
    - high_risk_review_retention: risky-* categories where a review actually ran (tier neither "skip" nor "none").
    - Variance band + noise-vs-win reporting (T038) live in scripts/bench_011_report.ps1, which reads these records.
#>
[CmdletBinding()]
param(
    [string]$Config,
    [string]$Binary = "bin/muhiyacode.exe",
    [string]$Fixtures = "specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json",
    [string]$OutDir = "specs/011-competitive-agent-audit/benchmarks/runs"
)

$ErrorActionPreference = 'Stop'

function Show-Usage {
    Write-Host "Usage: powershell -NoProfile -File scripts/bench_011.ps1 -Config <single|mixed> [-Binary <path>] [-Fixtures <tasks.json>] [-OutDir <dir>]"
    Write-Host "  -Config    Model configuration label for the run record (required: single or mixed)."
    Write-Host "  -Binary    Agent binary (default: bin/muhiyacode.exe)."
    Write-Host "  -Fixtures  Fixture task matrix JSON (default: specs/011-competitive-agent-audit/benchmarks/fixtures/tasks.json)."
    Write-Host "  -OutDir    Output directory for run records (default: specs/011-competitive-agent-audit/benchmarks/runs)."
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

function Get-Median($vals) {
    $arr = @($vals)
    if ($arr.Count -eq 0) { return $null }
    $s = @($arr | Sort-Object { [double]$_ })
    $n = $s.Count
    if (($n % 2) -eq 1) {
        return [double]$s[[int][math]::Floor(($n - 1) / 2)]
    }
    return ([double]$s[($n / 2) - 1] + [double]$s[$n / 2]) / 2.0
}

function Get-TaskTokens($t) {
    # Returns prompt+completion tokens for tasks with provider-reported usage; $null otherwise.
    $u = Get-Prop $t 'usage' $null
    if ($null -eq $u) { return $null }
    if (-not [bool](Get-Prop $u 'reported' $false)) { return $null }
    return [double](Get-Prop $u 'prompt_tokens' 0) + [double](Get-Prop $u 'completion_tokens' 0)
}

function Get-ReviewTier($t) {
    $r = Get-Prop $t 'review' $null
    return [string](Get-Prop $r 'tier' 'none')
}

# --- Argument validation ---------------------------------------------------

if ([string]::IsNullOrEmpty($Config) -or (@('single', 'mixed') -notcontains $Config)) {
    Write-Host "error: -Config is required and must be 'single' or 'mixed'." -ForegroundColor Red
    Show-Usage
    exit 1
}

$RepoRoot = Split-Path -Parent $PSScriptRoot

if (-not [System.IO.Path]::IsPathRooted($Binary))   { $Binary   = Join-Path $RepoRoot $Binary }
if (-not [System.IO.Path]::IsPathRooted($Fixtures)) { $Fixtures = Join-Path $RepoRoot $Fixtures }
if (-not [System.IO.Path]::IsPathRooted($OutDir))   { $OutDir   = Join-Path $RepoRoot $OutDir }

if (-not (Test-Path -LiteralPath $Binary)) {
    Write-Host "error: agent binary not found: $Binary (build it first, e.g. 'make build' -> bin/muhiyacode.exe)." -ForegroundColor Red
    exit 1
}
if (-not (Test-Path -LiteralPath $Fixtures)) {
    Write-Host "error: fixtures file not found: $Fixtures (T003 commits the fixture matrix there)." -ForegroundColor Red
    exit 1
}

try {
    # PS 5.1: ConvertFrom-Json writes a JSON array as ONE pipeline object; assign
    # first, then enumerate, so each fixture entry becomes its own element.
    $parsedFixtures = (Get-Content -LiteralPath $Fixtures -Raw) | ConvertFrom-Json
    $fixtureTasks = @($parsedFixtures | ForEach-Object { $_ })
} catch {
    Write-Host ("error: fixtures file is not valid JSON: {0} - {1}" -f $Fixtures, $_.Exception.Message) -ForegroundColor Red
    exit 1
}
if ($fixtureTasks.Count -eq 0) {
    Write-Host "error: fixtures file contains no tasks: $Fixtures" -ForegroundColor Red
    exit 1
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$sha = 'unknown'
try {
    $gitOut = git -C $RepoRoot rev-parse HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $gitOut) { $sha = ("$gitOut").Trim() }
} catch { }

$fixturesDir = Split-Path -Parent $Fixtures
$stampUtc = (Get-Date).ToUniversalTime()
$runId = ('011-v1_{0}_{1}' -f $Config, $stampUtc.ToString('yyyyMMdd-HHmmss'))

Write-Host ("bench_011: run_id={0} config={1} tasks={2} sha={3}" -f $runId, $Config, $fixtureTasks.Count, $sha)

# --- Drive the fixture tasks ----------------------------------------------

$taskRecords = New-Object System.Collections.ArrayList
$idx = 0
foreach ($t in $fixtureTasks) {
    $idx++
    $taskId = [string](Get-Prop $t 'task_id' '')
    $category = [string](Get-Prop $t 'category' 'uncategorized')
    $prompt = [string](Get-Prop $t 'prompt' '')
    $ws = [string](Get-Prop $t 'workspace' '')

    if ([string]::IsNullOrEmpty($taskId) -or [string]::IsNullOrEmpty($prompt)) {
        Write-Warning ("fixture entry {0} missing task_id or prompt - skipped" -f $idx)
        continue
    }

    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("bench011_" + [Guid]::NewGuid().ToString('N').Substring(0, 12))
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null

    if (-not [string]::IsNullOrEmpty($ws)) {
        $wsPath = Join-Path $fixturesDir $ws
        if (Test-Path -LiteralPath $wsPath) {
            Copy-Item -Path (Join-Path $wsPath '*') -Destination $tmp -Recurse -Force
        } else {
            Write-Warning ("workspace '{0}' not found for {1} - running in empty dir" -f $ws, $taskId)
        }
    }

    Write-Host ("[{0}/{1}] {2} ({3})" -f $idx, $fixtureTasks.Count, $taskId, $category)

    $benchLine = $null
    Push-Location $tmp
    try {
        $env:MUHIYA_BENCH_JSON = '1'
        $prevEap = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $stdout = & $Binary -p $prompt 2>$null
        $ErrorActionPreference = $prevEap
        foreach ($line in @($stdout)) {
            $s = "$line"
            if ($s -match '"muhiya_bench"') { $benchLine = $s }   # keep the LAST matching stdout line
        }
    } catch {
        Write-Warning ("run failed for {0}: {1}" -f $taskId, $_.Exception.Message)
    } finally {
        Pop-Location
        try { Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction Stop } catch { }
    }

    $summary = $null
    if ($null -ne $benchLine) {
        try {
            $parsed = $benchLine | ConvertFrom-Json
            $summary = Get-Prop $parsed 'muhiya_bench' $null
        } catch {
            Write-Warning ("unparseable muhiya_bench line for {0} - treating as absent" -f $taskId)
        }
    }

    $rec = [ordered]@{ task_id = $taskId; category = $category }
    if ($null -ne $summary) {
        $rec.task_class = Get-Prop $summary 'task_class' $null
        $rec.completed = [bool](Get-Prop $summary 'completed' $false)
        $rec.turns = [int](Get-Prop $summary 'turns' 0)
        $usage = Get-Prop $summary 'usage' $null
        if ($null -ne $usage) { $rec.usage = $usage } else { $rec.usage = [ordered]@{ reported = $false } }
        $rec.cost_usd = Get-Prop $summary 'cost_usd' $null
        $rec.cost_estimated = [bool](Get-Prop $summary 'cost_estimated' $false)
        $review = Get-Prop $summary 'review' $null
        if ($null -ne $review) { $rec.review = $review }
        else { $rec.review = [ordered]@{ tier = 'none'; rationale = 'summary carried no review block'; spend_tokens = 0; ceiling_hit = $false } }
        $rec.violations = Get-Prop $summary 'violations' $null
        $pp = Get-Prop $summary 'per_pairing' $null
        if ($null -ne $pp) { $rec.per_pairing = @($pp) }
    } else {
        # Contract section 2.3: never fabricate. No summary => not completed, usage not reported.
        Write-Warning ("no muhiya_bench summary for {0} - recording completed:false, reported:false" -f $taskId)
        $rec.task_class = $null
        $rec.completed = $false
        $rec.turns = 0
        $rec.usage = [ordered]@{ reported = $false }
        $rec.cost_usd = $null
        $rec.cost_estimated = $false
        $rec.review = [ordered]@{ tier = 'none'; rationale = 'no muhiya_bench summary line emitted'; spend_tokens = 0; ceiling_hit = $false }
        $rec.violations = $null
    }
    [void]$taskRecords.Add($rec)
}

Remove-Item Env:MUHIYA_BENCH_JSON -ErrorAction SilentlyContinue

if ($taskRecords.Count -eq 0) {
    Write-Host "error: no runnable fixture tasks - nothing to record." -ForegroundColor Red
    exit 1
}

# --- Aggregates (contract section 1 + section 2.7 class segmentation) ------

$codeTasks = @($taskRecords | Where-Object { [string](Get-Prop $_ 'task_class' '') -ne 'chat' })
$completedTasks = @($codeTasks | Where-Object { [bool](Get-Prop $_ 'completed' $false) })

$completionRate = $null
if ($codeTasks.Count -gt 0) { $completionRate = $completedTasks.Count / [double]$codeTasks.Count }

$costSum = 0.0
$haveCost = $false
foreach ($ct in $codeTasks) {
    $c = Get-Prop $ct 'cost_usd' $null
    if ($null -ne $c) { $costSum += [double]$c; $haveCost = $true }
}
$costPerCompleted = $null
if ($haveCost -and $completedTasks.Count -gt 0) { $costPerCompleted = $costSum / [double]$completedTasks.Count }

$trivialTasks = @($taskRecords | Where-Object { [string](Get-Prop $_ 'category' '') -like 'trivial-*' })
$trivialAutoRate = $null
if ($trivialTasks.Count -gt 0) {
    $auto = @($trivialTasks | Where-Object { $tier = Get-ReviewTier $_; ($tier -ne 'skip') -and ($tier -ne 'none') })
    $trivialAutoRate = $auto.Count / [double]$trivialTasks.Count
}

$riskyTasks = @($taskRecords | Where-Object { [string](Get-Prop $_ 'category' '') -like 'risky-*' })
$riskRetention = $null
if ($riskyTasks.Count -gt 0) {
    $retained = @($riskyTasks | Where-Object { $tier = Get-ReviewTier $_; ($tier -ne 'skip') -and ($tier -ne 'none') })
    $riskRetention = $retained.Count / [double]$riskyTasks.Count
}

$smallTasks = @($codeTasks | Where-Object { @('tiny', 'small') -contains [string](Get-Prop $_ 'task_class' '') })
$mediumTasks = @($codeTasks | Where-Object { [string](Get-Prop $_ 'task_class' '') -eq 'standard' })

$smallTokenVals = @()
$smallOverheadVals = @()
foreach ($st in $smallTasks) {
    $tok = Get-TaskTokens $st
    if ($null -ne $tok) {
        $smallTokenVals += $tok
        if ($tok -gt 0) {
            $spend = [double](Get-Prop (Get-Prop $st 'review' $null) 'spend_tokens' 0)
            $smallOverheadVals += ($spend * 100.0 / $tok)
        }
    }
}
$mediumOverheadVals = @()
foreach ($mt in $mediumTasks) {
    $tok = Get-TaskTokens $mt
    if ($null -ne $tok -and $tok -gt 0) {
        $spend = [double](Get-Prop (Get-Prop $mt 'review' $null) 'spend_tokens' 0)
        $mediumOverheadVals += ($spend * 100.0 / $tok)
    }
}

$violTerminal = 0
$violDup = 0
foreach ($tr in $taskRecords) {
    $v = Get-Prop $tr 'violations' $null
    if ($null -ne $v) {
        $violTerminal += [int](Get-Prop $v 'terminal_read_when_tool_exists' 0)
        $violDup += [int](Get-Prop $v 'duplicate_reads' 0)
    }
}

$aggregates = [ordered]@{
    completion_rate = $completionRate
    cost_per_completed_task = $costPerCompleted
    trivial_auto_review_rate = $trivialAutoRate
    high_risk_review_retention = $riskRetention
    median_small_task_tokens = (Get-Median $smallTokenVals)
    review_overhead_median_pct_small = (Get-Median $smallOverheadVals)
    review_overhead_median_pct_medium = (Get-Median $mediumOverheadVals)
    violations_terminal_read_total = $violTerminal
    violations_duplicate_reads_total = $violDup
}

# --- Assemble + write the run record ---------------------------------------

$record = [ordered]@{
    run_id = $runId
    suite_version = '011-v1'
    timestamp = $stampUtc.ToString('yyyy-MM-ddTHH:mm:ssZ')
    config = [ordered]@{
        mode = $Config
        main_model = $null          # not introspectable by the runner; operator-recorded if needed
        sub_model = $null
        effort = $null
        review_gating = $null
        agent_build = $sha
        binary = $Binary
        fixtures = $Fixtures
    }
    tasks = $taskRecords
    aggregates = $aggregates
}

$outPath = Join-Path $OutDir ($runId + '.json')
$json = ConvertTo-Json -InputObject $record -Depth 16
[System.IO.File]::WriteAllText($outPath, $json, (New-Object System.Text.UTF8Encoding($false)))

Write-Host ("run record written: {0}" -f $outPath)
Write-Host ("completion_rate={0}  tasks={1}  completed={2}" -f $completionRate, $codeTasks.Count, $completedTasks.Count)
exit 0
