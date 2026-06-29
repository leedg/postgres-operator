param(
    [switch]$Remove,
    [switch]$Check
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"

function Ensure-Directory {
    param([string]$Path)
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
    return (Resolve-Path -LiteralPath $Path).Path
}

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Normalize-PathSet {
    param([string[]]$Paths)
    $set = New-Object 'System.Collections.Generic.HashSet[string]' ([System.StringComparer]::OrdinalIgnoreCase)
    foreach ($path in $Paths) {
        if ([string]::IsNullOrWhiteSpace($path)) {
            continue
        }
        [void]$set.Add([System.IO.Path]::GetFullPath($path).TrimEnd('\'))
    }
    return ,$set
}

$baseDir = Join-Path $env:LOCALAPPDATA "keiailab\postgres-operator"
$goTmpDir = Ensure-Directory (Join-Path $baseDir "go-tmp")
$goCacheDir = Ensure-Directory (Join-Path $baseDir "go-cache")
$exclusionPaths = @($goTmpDir, $goCacheDir)

if (-not (Get-Command Get-MpPreference -ErrorAction SilentlyContinue) -or
    -not (Get-Command Add-MpPreference -ErrorAction SilentlyContinue)) {
    throw "Microsoft Defender PowerShell cmdlets are not available on this host."
}

$current = Normalize-PathSet -Paths @((Get-MpPreference).ExclusionPath)
$desired = Normalize-PathSet -Paths $exclusionPaths

Write-Host "Go test executable/cache paths:"
foreach ($path in $exclusionPaths) {
    $enabled = $current.Contains([System.IO.Path]::GetFullPath($path).TrimEnd('\'))
    Write-Host ("  {0}  DefenderExclusion={1}" -f $path, $enabled)
}

if ($Check) {
    exit 0
}

if (-not (Test-Administrator)) {
    throw "Administrator PowerShell is required to change Microsoft Defender exclusions."
}

foreach ($path in $exclusionPaths) {
    $normalized = [System.IO.Path]::GetFullPath($path).TrimEnd('\')
    if ($Remove) {
        if ($current.Contains($normalized)) {
            Remove-MpPreference -ExclusionPath $path
            Write-Host "removed Defender exclusion: $path"
        }
        continue
    }

    if (-not $current.Contains($normalized)) {
        Add-MpPreference -ExclusionPath $path
        Write-Host "added Defender exclusion: $path"
    } else {
        Write-Host "already allowed: $path"
    }
}
