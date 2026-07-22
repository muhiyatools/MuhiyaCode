<#
.SYNOPSIS
  Reproducible feature-014 baseline/candidate runner.

.DESCRIPTION
  Runs a versioned task fixture against one pinned model/transport/configuration,
  preserves raw process output and provider-backed Muhiya benchmark summaries,
  executes the fixture rubric, and marks missing usage/rubrics as invalid.
  Paid execution requires -AllowPaid explicitly.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$Binary,
    [Parameter(Mandatory=$true)][string]$Model,
    [Parameter(Mandatory=$true)][string]$Transport,
    [Parameter(Mandatory=$true)][ValidateSet('low','medium','high','max')][string]$Effort,
    [Parameter(Mandatory=$true)][ValidateSet('normal','auto-accept')][string]$Permission,
    [Parameter(Mandatory=$true)][string]$Fixture,
    [Parameter(Mandatory=$true)][string]$Output,
    [ValidateRange(1,100)][int]$Repeat = 2,
    [ValidateSet('baseline','observe','balanced','aggressive')][string]$Mode = 'baseline',
    [switch]$AllowPaid,
    [switch]$KeepWorkspaces,
    [switch]$NoMCP
)

$ErrorActionPreference = 'Stop'
$RepoRoot = Split-Path -Parent $PSScriptRoot

function Resolve-RepoPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) { return [IO.Path]::GetFullPath($Path) }
    return [IO.Path]::GetFullPath((Join-Path $RepoRoot $Path))
}

function Get-SHA256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-Property($Object, [string]$Name, $Default = $null) {
    if ($null -eq $Object) { return $Default }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property -or $null -eq $property.Value) { return $Default }
    return $property.Value
}

function New-Workspace([object]$Task, [object]$Suite, [string]$Root) {
    $workspace = Join-Path $Root ([string]$Task.id)
    New-Item -ItemType Directory -Force -Path $workspace | Out-Null
    $reset = $null
    $resetName = [string](Get-Property $Task 'reset' '')
    if ($resetName) { $reset = Get-Property $Suite.reset_scripts $resetName $null }
    if ($null -eq $reset) {
        $taskWorkspace = Get-Property $Task 'workspace' $null
        if ($null -ne $taskWorkspace) { $reset = Get-Property $taskWorkspace 'reset' $null }
    }
    if ($null -eq $reset) { throw "task $($Task.id) has no reset definition" }
    $strategy = [string](Get-Property $reset 'strategy' '')
    switch ($strategy) {
        'empty' { }
        'copy' {
            $source = Resolve-RepoPath ([string]$reset.source)
            if (-not (Test-Path -LiteralPath $source -PathType Container)) { throw "reset source not found: $source" }
            Get-ChildItem -LiteralPath $source -Force | Copy-Item -Destination $workspace -Recurse -Force
        }
        'materialize' {
            foreach ($property in $reset.files.PSObject.Properties) {
                $target = Join-Path $workspace $property.Name
                New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
                [IO.File]::WriteAllText($target, [string]$property.Value, [Text.UTF8Encoding]::new($false))
            }
        }
        default { throw "unsupported reset strategy '$strategy' for $($Task.id)" }
    }
    return $workspace
}

function Get-TreeHashes([string]$Workspace) {
    $result = @{}
    Get-ChildItem -LiteralPath $Workspace -Recurse -File | ForEach-Object {
        $relative = [IO.Path]::GetRelativePath($Workspace, $_.FullName).Replace('\','/')
        $result[$relative] = Get-SHA256 $_.FullName
    }
    return $result
}

function Get-ChangedPaths([hashtable]$Before, [hashtable]$After) {
    $names = @($Before.Keys) + @($After.Keys) | Sort-Object -Unique
    return @($names | Where-Object { -not $Before.ContainsKey($_) -or -not $After.ContainsKey($_) -or $Before[$_] -ne $After[$_] })
}

function Test-GlobAbsent([string]$Workspace, [string[]]$Globs) {
    $paths = @(Get-ChildItem -LiteralPath $Workspace -Recurse -Force | ForEach-Object { [IO.Path]::GetRelativePath($Workspace, $_.FullName).Replace('\','/') })
    foreach ($glob in $Globs) {
        $pattern = $glob.Replace('**','*')
        if ($paths | Where-Object { $_ -like $pattern }) { return $false }
    }
    return $true
}

function Invoke-Rubric([object]$Rubric, [string]$Workspace, [hashtable]$Before) {
    $results = New-Object Collections.ArrayList
    foreach ($check in @($Rubric.checks)) {
        $kind = [string]$check.kind
        $passed = $false
        $detail = ''
        try {
            switch ($kind) {
                'file_exists' { $passed = Test-Path -LiteralPath (Join-Path $Workspace $check.path) -PathType Leaf }
                'file_exists_any' { $passed = @($check.paths | Where-Object { Test-Path -LiteralPath (Join-Path $Workspace $_) -PathType Leaf }).Count -gt 0 }
                'file_contains' { $passed = (Get-Content -LiteralPath (Join-Path $Workspace $check.path) -Raw).Contains([string]$check.text) }
                'file_not_contains' { $passed = -not (Get-Content -LiteralPath (Join-Path $Workspace $check.path) -Raw).Contains([string]$check.text) }
                'source_regex' {
                    $joined = @($check.paths | Where-Object { Test-Path -LiteralPath (Join-Path $Workspace $_) } | ForEach-Object { Get-Content -LiteralPath (Join-Path $Workspace $_) -Raw }) -join "`n"
                    $passed = [regex]::IsMatch($joined, [string]$check.pattern)
                }
                'glob_absent' { $passed = Test-GlobAbsent $Workspace @($check.globs) }
                'max_changed_files' {
                    $changed = Get-ChangedPaths $Before (Get-TreeHashes $Workspace)
                    $passed = $changed.Count -le [int]$check.value
                    $detail = "changed=$($changed.Count):$($changed -join ',')"
                }
                'command' {
                    $argv = @($check.argv)
                    Push-Location $Workspace
                    try { & $argv[0] $argv[1..($argv.Count-1)] *> $null; $passed = $LASTEXITCODE -eq 0 }
                    finally { Pop-Location }
                }
                'html_selector_count' {
                    $html = Get-Content -LiteralPath (Join-Path $Workspace $check.path) -Raw
                    $count = 0
                    foreach ($selector in ([string]$check.selector -split ',')) {
                        switch -Regex ($selector.Trim()) {
                            '^\[data-cell\]$' { $count += [regex]::Matches($html, '(?i)\bdata-cell\b').Count; break }
                            '^\.cell$' { $count += [regex]::Matches($html, '(?i)class\s*=\s*["''][^"'']*\bcell\b').Count; break }
                            '^button$' { $count += [regex]::Matches($html, '(?i)<button\b').Count; break }
                            '^header\.site-header$' { $count += [regex]::Matches($html, '(?i)<header\b[^>]*class\s*=\s*["''][^"'']*\bsite-header\b').Count; break }
                            default { throw "unsupported selector '$selector'" }
                        }
                    }
                    $passed = $count -ge [int]$check.minimum
                    $detail = "count=$count"
                }
                default { throw "unsupported rubric kind '$kind'" }
            }
        } catch { $detail = $_.Exception.Message; $passed = $false }
        [void]$results.Add([ordered]@{ check_id=[string](Get-Property $check 'id' $kind); kind=$kind; passed=$passed; detail=$detail })
    }
    return @($results)
}

if (-not $AllowPaid) { throw 'Live provider runs can consume credits. Re-run with -AllowPaid after confirming the configuration.' }
$Binary = Resolve-RepoPath $Binary
$Fixture = Resolve-RepoPath $Fixture
$Output = Resolve-RepoPath $Output
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) { throw "binary not found: $Binary" }
if (-not (Test-Path -LiteralPath $Fixture -PathType Leaf)) { throw "fixture not found: $Fixture" }
$suite = Get-Content -LiteralPath $Fixture -Raw | ConvertFrom-Json
$tasks = @(Get-Property $suite 'tasks' @())
if ([int]$suite.schema_version -ne 1 -or -not $suite.suite -or $tasks.Count -eq 0) { throw 'fixture must be a non-empty schema_version 1 task suite' }
foreach ($task in $tasks) { if (-not $task.id -or -not $task.prompt -or @($task.rubric.checks).Count -eq 0) { throw "invalid task fixture entry" } }

New-Item -ItemType Directory -Force -Path $Output | Out-Null
$runID = "014-$Mode-$((Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss'))"
$runDir = Join-Path $Output $runID
New-Item -ItemType Directory -Force -Path $runDir | Out-Null
$scratch = Join-Path ([IO.Path]::GetTempPath()) ("muhiya-bench-014-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$sourceHome = if ($env:MUHIYA_HOME) { $env:MUHIYA_HOME } else { Join-Path $HOME '.muhiya' }
$benchHome = Join-Path $scratch 'home'
New-Item -ItemType Directory -Force -Path $benchHome | Out-Null
if (Test-Path -LiteralPath $sourceHome) { Get-ChildItem -LiteralPath $sourceHome -Force | Copy-Item -Destination $benchHome -Recurse -Force }
$previousHome = $env:MUHIYA_HOME
$env:MUHIYA_HOME = $benchHome
$env:MUHIYA_BENCH_JSON = '1'
$env:MUHIYA_BENCH_TRANSPORT = $Transport
$env:MUHIYA_TOKEN_ECONOMY_MODE = if ($Mode -eq 'baseline') { 'off' } else { $Mode }

try {
    & $Binary config set model $Model *> $null
    if ($LASTEXITCODE -ne 0) { throw "unable to pin model '$Model' in isolated benchmark home" }
    & $Binary config set effort $Effort *> $null
    if ($LASTEXITCODE -ne 0) { throw "unable to set effort '$Effort'" }
    & $Binary config set permissionMode $Permission *> $null
    if ($LASTEXITCODE -ne 0) { throw "unable to set permission '$Permission'" }

    $records = New-Object Collections.ArrayList
    for ($repeatIndex = 1; $repeatIndex -le $Repeat; $repeatIndex++) {
        foreach ($task in $tasks) {
            $workspace = New-Workspace $task $suite (Join-Path $scratch "repeat-$repeatIndex")
            $before = Get-TreeHashes $workspace
            $stdoutPath = Join-Path $runDir "$repeatIndex-$($task.id)-stdout.txt"
            $stderrPath = Join-Path $runDir "$repeatIndex-$($task.id)-stderr.txt"
            $arguments = @('--cwd', $workspace, '--new', '--print', [string]$task.prompt)
            if ($NoMCP) { $arguments = @('--no-mcp') + $arguments }
            $started = (Get-Date).ToUniversalTime()
            $stdout = @(& $Binary @arguments 2> $stderrPath)
            $exitCode = $LASTEXITCODE
            [IO.File]::WriteAllLines($stdoutPath, [string[]]$stdout, [Text.UTF8Encoding]::new($false))
            $summary = $null
            foreach ($line in $stdout) {
                if ([string]$line -match '"muhiya_bench"') {
                    try { $summary = (ConvertFrom-Json ([string]$line)).muhiya_bench } catch { }
                }
            }
            $rubric = Invoke-Rubric $task.rubric $workspace $before
            $usage = Get-Property $summary 'usage' $null
            $usageAvailable = $null -ne $usage -and [bool](Get-Property $usage 'reported' $false)
            $invalid = New-Object Collections.ArrayList
            if ($exitCode -ne 0) { [void]$invalid.Add("agent_exit_$exitCode") }
            if ($null -eq $summary) { [void]$invalid.Add('missing_machine_summary') }
            if (-not $usageAvailable) { [void]$invalid.Add('missing_provider_usage') }
            if (@($rubric).Count -eq 0) { [void]$invalid.Add('missing_rubric') }
            if (@($rubric | Where-Object { -not $_.passed }).Count -gt 0) { [void]$invalid.Add('rubric_failed') }
            [void]$records.Add([ordered]@{
                task_id=[string]$task.id; category=[string]$task.category; declared_task_class=[string]$task.task_class
                repeat=$repeatIndex; started_at=$started.ToString('o'); duration_ms=[int64]((Get-Date).ToUniversalTime().Subtract($started).TotalMilliseconds)
                exit_code=$exitCode; summary=$summary; rubric=$rubric; changed_paths=(Get-ChangedPaths $before (Get-TreeHashes $workspace))
                valid=($invalid.Count -eq 0); invalid_reasons=@($invalid); usage_available=$usageAvailable
                stdout=(Split-Path -Leaf $stdoutPath); stderr=(Split-Path -Leaf $stderrPath)
            })
        }
    }
    $manifest = [ordered]@{
        schema_version=1; run_id=$runID; suite=[string]$suite.suite; fixture_schema_version=[int]$suite.schema_version
        fixture_path=$Fixture; fixture_sha256=(Get-SHA256 $Fixture); binary_path=$Binary; binary_sha256=(Get-SHA256 $Binary)
        git_commit=(git -C $RepoRoot rev-parse HEAD).Trim(); model=$Model; transport=$Transport; effort=$Effort; permission=$Permission
        mode=$Mode; repeat=$Repeat; no_mcp=[bool]$NoMCP; started_at=(Get-Date).ToUniversalTime().ToString('o')
        expected_records=($tasks.Count*$Repeat); records=@($records)
    }
    $manifestPath = Join-Path $runDir 'run.json'
    [IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
    $invalidCount = @($records | Where-Object { -not $_.valid }).Count
    Write-Host "run=$runID records=$($records.Count) invalid=$invalidCount output=$manifestPath"
    if ($invalidCount -gt 0) { exit 2 }
} finally {
    Remove-Item Env:MUHIYA_BENCH_JSON -ErrorAction SilentlyContinue
    Remove-Item Env:MUHIYA_BENCH_TRANSPORT -ErrorAction SilentlyContinue
    Remove-Item Env:MUHIYA_TOKEN_ECONOMY_MODE -ErrorAction SilentlyContinue
    if ($null -eq $previousHome) { Remove-Item Env:MUHIYA_HOME -ErrorAction SilentlyContinue } else { $env:MUHIYA_HOME = $previousHome }
    if (-not $KeepWorkspaces) { Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue }
}
