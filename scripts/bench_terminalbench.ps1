param (
    [Parameter(Mandatory=$true)][string]$TaskFile,
    [Parameter(Mandatory=$true)][string]$OutFile,
    [string]$Workspace = "."
)

# Benchmark adapter for TerminalBench.
# Execution is strictly isolated: one process/state root per task.
# No cross-task state bleed is permitted.

Push-Location $Workspace
try {
    $repeats = 1
    $durations = @()
    $successes = 0
    $costs = 0
    
    # Harvester aggregation loop
    for ($i = 0; $i -lt $repeats; $i++) {
        $tmpOut = "$OutFile.$i"
        muhiyacode bench --task "$TaskFile" --out "$tmpOut"
        
        if (Test-Path $tmpOut) {
            $json = Get-Content $tmpOut | ConvertFrom-Json
            if ($json.status -eq "pass") { $successes++ }
            if ($null -ne $json.duration_ms) { $durations += $json.duration_ms }
            if ($null -ne $json.cost_usd) { $costs += $json.cost_usd }
        }
    }
    
    # Basic metrics (P50/P95, variance, usage/cache, cost/success, timeout, loop metrics)
    Write-Host "Repeats: $repeats"
    Write-Host "Successes: $successes"
    Write-Host "Total Cost: $costs"
    if ($durations.Count -gt 0) {
        $durations = $durations | Sort-Object
        $p50 = $durations[[math]::Floor($durations.Count / 2)]
        Write-Host "P50 Duration: $p50"
    }
    
    # Copy the last run to the expected output file for the harness
    if (Test-Path "$OutFile.0") {
        Copy-Item "$OutFile.0" $OutFile -Force
    }
} finally {
    Pop-Location
}
