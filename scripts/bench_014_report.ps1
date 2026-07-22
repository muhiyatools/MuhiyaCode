<#
.SYNOPSIS
  Compare valid feature-014 benchmark runs and emit machine JSON plus Markdown.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string[]]$Baseline,
    [Parameter(Mandatory=$true)][string[]]$Candidate,
    [Parameter(Mandatory=$true)][string]$OutputPrefix
)

$ErrorActionPreference = 'Stop'
$RepoRoot = Split-Path -Parent $PSScriptRoot

function Resolve-RepoPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) { return [IO.Path]::GetFullPath($Path) }
    return [IO.Path]::GetFullPath((Join-Path $RepoRoot $Path))
}

function Read-Run([string]$Path, [string]$Arm) {
    $resolved = Resolve-RepoPath $Path
    if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) { throw "run not found: $resolved" }
    $run = Get-Content -LiteralPath $resolved -Raw | ConvertFrom-Json
    if ([int]$run.schema_version -ne 1 -or -not $run.run_id) { throw "unsupported run schema: $resolved" }
    $records = @($run.records)
    if ($records.Count -ne [int]$run.expected_records) { throw "$($run.run_id): expected $($run.expected_records) records, found $($records.Count)" }
    foreach ($record in $records) {
        if (-not [bool]$record.valid) { throw "$($run.run_id)/$($record.task_id): invalid row: $(@($record.invalid_reasons) -join ',')" }
        if (-not [bool]$record.usage_available -or $null -eq $record.summary -or -not [bool]$record.summary.usage.reported) {
            throw "$($run.run_id)/$($record.task_id): provider usage unavailable"
        }
        if (@($record.rubric).Count -eq 0) { throw "$($run.run_id)/$($record.task_id): rubric output unavailable" }
    }
    return [pscustomobject]@{ Arm=$Arm; Path=$resolved; Run=$run }
}

function Get-Percentile([double[]]$Values, [double]$P) {
    $sorted = @($Values | Sort-Object)
    if ($sorted.Count -eq 0) { return $null }
    if ($sorted.Count -eq 1) { return [double]$sorted[0] }
    $position = ($sorted.Count - 1) * $P
    $lower = [int][Math]::Floor($position)
    $upper = [int][Math]::Ceiling($position)
    if ($lower -eq $upper) { return [double]$sorted[$lower] }
    $fraction = $position - $lower
    return [double]$sorted[$lower] + (([double]$sorted[$upper] - [double]$sorted[$lower]) * $fraction)
}

function Get-Stats([double[]]$Values) {
    $items = @($Values)
    if ($items.Count -eq 0) { return $null }
    $mean = ($items | Measure-Object -Average).Average
    $variance = (($items | ForEach-Object { ([double]$_ - $mean) * ([double]$_ - $mean) }) | Measure-Object -Sum).Sum / $items.Count
    return [ordered]@{
        count=$items.Count; median=(Get-Percentile $items 0.50); p75=(Get-Percentile $items 0.75)
        p95=(Get-Percentile $items 0.95); mean=[double]$mean; variance=[double]$variance
        minimum=[double](($items | Measure-Object -Minimum).Minimum); maximum=[double](($items | Measure-Object -Maximum).Maximum)
    }
}

function Expand-Rows($Runs) {
    $rows = New-Object Collections.ArrayList
    foreach ($entry in $Runs) {
        foreach ($record in @($entry.Run.records)) {
            $usage = $record.summary.usage
            $prompt = [double]$usage.prompt_tokens
            $output = [double]$usage.completion_tokens
            $cacheRead = [double]$usage.cache_read_tokens
            $cacheMiss = [double]$usage.cache_miss_tokens
            $cacheWrite = [double]$usage.cache_write_tokens
            $toolRows = @()
            foreach ($tool in @($record.tool_calls)) {
                if ($null -eq $tool) { continue }
                $toolRows += [ordered]@{
                    name=[string](if($tool.name){$tool.name}else{$tool.tool_name})
                    schema_bytes=[int](if($tool.schema_bytes){$tool.schema_bytes}else{0})
                    failed=[bool]$tool.failed
                    discovered=[bool](if($null -ne $tool.discovered){$tool.discovered}else{$tool.brokered})
                }
            }
            [void]$rows.Add([ordered]@{
                arm=$entry.Arm; run_id=[string]$entry.Run.run_id; run_path=$entry.Path; task_id=[string]$record.task_id
                category=[string]$record.category; task_class=[string](if($record.summary.task_class){$record.summary.task_class}else{$record.declared_task_class})
                repeat=[int]$record.repeat; completed=[bool]$record.summary.completed; rubric_passed=(@($record.rubric | Where-Object {-not $_.passed}).Count -eq 0)
                main_requests=[double]$record.summary.turns; prompt_tokens=$prompt; output_tokens=$output; cache_read_tokens=$cacheRead
                cache_miss_tokens=$cacheMiss; cache_write_tokens=$cacheWrite; total_tokens=($prompt+$output)
                cost_usd=if($null -eq $record.summary.cost_usd){$null}else{[double]$record.summary.cost_usd}
                tool_calls=$toolRows
            })
        }
    }
    return @($rows)
}

function Summarize-Arm([object[]]$Rows) {
    $metrics = [ordered]@{}
    foreach ($name in @('main_requests','prompt_tokens','output_tokens','cache_read_tokens','cache_miss_tokens','cache_write_tokens','total_tokens')) {
        $metrics[$name] = Get-Stats @($Rows | ForEach-Object { [double]$_.$name })
    }
    $cost = @($Rows | Where-Object { $null -ne $_.cost_usd } | ForEach-Object { [double]$_.cost_usd })
    $metrics.cost_usd = Get-Stats $cost
    return [ordered]@{
        rows=$Rows.Count
        completion_rate=(@($Rows | Where-Object {$_.completed}).Count / [double]$Rows.Count)
        rubric_pass_rate=(@($Rows | Where-Object {$_.rubric_passed}).Count / [double]$Rows.Count)
        metrics=$metrics
    }
}

function Summarize-ToolSurface([object[]]$Rows) {
    $all = @()
    foreach ($row in $Rows) {
        foreach ($tool in @($row.tool_calls)) {
            if (-not $tool.name) { continue }
            $all += [pscustomobject]@{name=$tool.name; task_class=$row.task_class; schema_bytes=$tool.schema_bytes; failed=$tool.failed; discovered=$tool.discovered}
        }
    }
    $result = New-Object Collections.ArrayList
    foreach ($group in @($all | Group-Object name | Sort-Object Name)) {
        $items = @($group.Group)
        [void]$result.Add([ordered]@{
            name=$group.Name
            calls=$items.Count
            failures=@($items | Where-Object {$_.failed}).Count
            discovered_calls=@($items | Where-Object {$_.discovered}).Count
            maximum_schema_bytes=[int](($items | Measure-Object schema_bytes -Maximum).Maximum)
            task_classes=@($items.task_class | Sort-Object -Unique)
        })
    }
    return @($result)
}

function Assert-Comparable($BaselineRuns, $CandidateRuns) {
    $reference = $BaselineRuns[0].Run
    foreach ($entry in @($BaselineRuns) + @($CandidateRuns)) {
        $run = $entry.Run
        foreach ($field in @('suite','fixture_schema_version','fixture_sha256','model','transport','effort','permission','repeat','no_mcp')) {
            if ([string]$run.$field -ne [string]$reference.$field) { throw "configuration drift in $($entry.Path): $field" }
        }
    }
    $baselineTasks = @($BaselineRuns | ForEach-Object {$_.Run.records.task_id} | Sort-Object -Unique)
    $candidateTasks = @($CandidateRuns | ForEach-Object {$_.Run.records.task_id} | Sort-Object -Unique)
    if (($baselineTasks -join "`n") -ne ($candidateTasks -join "`n")) { throw 'baseline and candidate task sets differ' }
}

$baselineRuns = @($Baseline | ForEach-Object { Read-Run $_ 'baseline' })
$candidateRuns = @($Candidate | ForEach-Object { Read-Run $_ 'candidate' })
Assert-Comparable $baselineRuns $candidateRuns
$rows = @(Expand-Rows (@($baselineRuns) + @($candidateRuns)))
$baselineRows = @($rows | Where-Object {$_.arm -eq 'baseline'})
$candidateRows = @($rows | Where-Object {$_.arm -eq 'candidate'})
$baselineSummary = Summarize-Arm $baselineRows
$candidateSummary = Summarize-Arm $candidateRows

$deltas = [ordered]@{}
foreach ($metric in @('main_requests','prompt_tokens','output_tokens','cache_read_tokens','cache_miss_tokens','cache_write_tokens','total_tokens','cost_usd')) {
    $before = $baselineSummary.metrics[$metric]
    $after = $candidateSummary.metrics[$metric]
    if ($null -eq $before -or $null -eq $after -or [double]$before.median -eq 0) { $deltas[$metric] = $null }
    else { $deltas[$metric] = 1.0 - ([double]$after.median / [double]$before.median) }
}

$report = [ordered]@{
    schema_version=1; generated_at=(Get-Date).ToUniversalTime().ToString('o')
    baseline_runs=@($baselineRuns.Path); candidate_runs=@($candidateRuns.Path)
    baseline=$baselineSummary; candidate=$candidateSummary; median_reduction=$deltas
    tool_surface=[ordered]@{baseline=(Summarize-ToolSurface $baselineRows); candidate=(Summarize-ToolSurface $candidateRows)}
    rows=$rows
}
$OutputPrefix = Resolve-RepoPath $OutputPrefix
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $OutputPrefix) | Out-Null
$jsonPath = "$OutputPrefix.json"
$mdPath = "$OutputPrefix.md"
[IO.File]::WriteAllText($jsonPath, ($report | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))

$lines = New-Object Collections.ArrayList
[void]$lines.Add('# Feature 014 benchmark comparison')
[void]$lines.Add('')
[void]$lines.Add("Generated: $($report.generated_at)")
[void]$lines.Add('')
[void]$lines.Add('| Metric | Baseline median | Candidate median | Reduction | Candidate p75 | Candidate p95 | Candidate variance |')
[void]$lines.Add('|---|---:|---:|---:|---:|---:|---:|')
foreach ($metric in @('main_requests','prompt_tokens','output_tokens','cache_read_tokens','cache_miss_tokens','cache_write_tokens','total_tokens','cost_usd')) {
    $before=$baselineSummary.metrics[$metric]; $after=$candidateSummary.metrics[$metric]; $reduction=$deltas[$metric]
    $reductionText=if($null -eq $reduction){'unavailable'}else{'{0:P2}' -f $reduction}
    [void]$lines.Add("| $metric | $($before.median) | $($after.median) | $reductionText | $($after.p75) | $($after.p95) | $($after.variance) |")
}
[void]$lines.Add('')
[void]$lines.Add('## Tool surface')
[void]$lines.Add('')
[void]$lines.Add('| Arm | Tool | Calls | Failures | Discovered | Max schema bytes | Task classes |')
[void]$lines.Add('|---|---|---:|---:|---:|---:|---|')
foreach ($arm in @('baseline','candidate')) {
    foreach ($tool in @($report.tool_surface[$arm])) {
        [void]$lines.Add("| $arm | $($tool.name) | $($tool.calls) | $($tool.failures) | $($tool.discovered_calls) | $($tool.maximum_schema_bytes) | $(@($tool.task_classes) -join ', ') |")
    }
}
[void]$lines.Add('')
[void]$lines.Add("Correctness: baseline completion $([string]::Format('{0:P2}',$baselineSummary.completion_rate)), candidate $([string]::Format('{0:P2}',$candidateSummary.completion_rate)); baseline rubric $([string]::Format('{0:P2}',$baselineSummary.rubric_pass_rate)), candidate $([string]::Format('{0:P2}',$candidateSummary.rubric_pass_rate)).")
[void]$lines.Add('')
[void]$lines.Add('Every raw row is retained in the adjacent JSON. Missing usage, rubric output, records, or configuration parity causes this generator to fail instead of contributing a zero.')
[IO.File]::WriteAllLines($mdPath, [string[]]$lines, [Text.UTF8Encoding]::new($false))
Write-Host "json=$jsonPath markdown=$mdPath rows=$($rows.Count)"
