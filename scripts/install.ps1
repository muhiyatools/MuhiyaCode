param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\MuhiyaCode\bin",
    [string]$Repository = $(if ($env:MUHIYA_REPOSITORY) { $env:MUHIYA_REPOSITORY } else { "muhiyatools/MuhiyaCode" }),
    [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"
$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
    "x64" { "amd64" }
    "arm64" { "arm64" }
    default { throw "Unsupported Windows architecture: $($_)" }
}

if ($Version -eq "latest") {
    $release = Invoke-RestMethod -Headers @{ "User-Agent" = "MuhiyaCode-Installer" } -Uri "https://api.github.com/repos/$Repository/releases/latest"
    $Version = [string]$release.tag_name
}
$cleanVersion = $Version.TrimStart("v")
$asset = "muhiyacode_${cleanVersion}_windows_${arch}.zip"
$base = "https://github.com/$Repository/releases/download/$Version"
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("muhiyacode-install-" + [Guid]::NewGuid().ToString("N"))

try {
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    $archive = Join-Path $tempRoot $asset
    $checksums = Join-Path $tempRoot "checksums.txt"
    Invoke-WebRequest -Headers @{ "User-Agent" = "MuhiyaCode-Installer" } -Uri "$base/$asset" -OutFile $archive
    Invoke-WebRequest -Headers @{ "User-Agent" = "MuhiyaCode-Installer" } -Uri "$base/checksums.txt" -OutFile $checksums
    $expectedLine = Get-Content -LiteralPath $checksums | Where-Object { $_ -match "\s+$([Regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $expectedLine) { throw "Checksum entry not found for $asset" }
    $expected = ($expectedLine -split "\s+")[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Checksum mismatch for $asset" }
    $expanded = Join-Path $tempRoot "expanded"
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded -Force
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $expanded "muhiyacode.exe") -Destination (Join-Path $InstallDir "muhiyacode.exe") -Force
    Copy-Item -LiteralPath (Join-Path $expanded "muhiyacode.exe") -Destination (Join-Path $InstallDir "muhiya.exe") -Force
    if (-not $NoPathUpdate) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $entries = @($userPath -split ";" | Where-Object { $_ })
        if ($entries -notcontains $InstallDir) {
            [Environment]::SetEnvironmentVariable("Path", (($entries + $InstallDir) -join ";"), "User")
        }
    }
    Write-Host "Installed MuhiyaCode $Version to $InstallDir"
    Write-Host "Open a new terminal, then run: muhiyacode doctor --offline"
}
finally {
    if (Test-Path -LiteralPath $tempRoot) {
        $resolved = (Resolve-Path -LiteralPath $tempRoot).Path
        $tempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
        if ($resolved.StartsWith($tempBase, [StringComparison]::OrdinalIgnoreCase)) {
            Remove-Item -LiteralPath $resolved -Recurse -Force
        }
    }
}
