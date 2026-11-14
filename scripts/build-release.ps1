[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version must be in X.Y.Z format (e.g. 1.1.0). Got: '$Version'"
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$sysoPath = Join-Path $repoRoot "cmd\grab\resource_windows.syso"
$issPath  = Join-Path $repoRoot "deploy\windows\grab-setup.iss"
$distDir  = Join-Path $repoRoot "dist"

if (-not (Test-Path $sysoPath)) {
    throw "Missing Windows resource file (icon/manifest): $sysoPath"
}
if (-not (Test-Path $issPath)) {
    throw "Missing Inno Setup script: $issPath"
}

$isccCmd = Get-Command iscc -ErrorAction SilentlyContinue
if ($isccCmd) {
    $iscc = $isccCmd.Source
} else {
    $candidates = @(
        (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe"),
        (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"),
        (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe")
    )
    $iscc = $candidates | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
}
if (-not $iscc) {
    throw "Inno Setup compiler (iscc / ISCC.exe) not found on PATH or standard install locations."
}

if (Test-Path $distDir) {
    Remove-Item -Path $distDir -Recurse -Force
}
New-Item -ItemType Directory -Path $distDir | Out-Null

$buildDate = [DateTime]::UtcNow.ToString("MMM dd yyyy HH:mm:ss", [System.Globalization.CultureInfo]::InvariantCulture)
$ldflags   = "-s -w -X main.Version=$Version -X 'main.BuildDate=$buildDate'"

$winOut   = Join-Path $distDir "grab-windows-amd64.exe"
$linuxOut = Join-Path $distDir "grab-linux-amd64"
$setupOut = Join-Path $distDir "GRAB-Setup-$Version.exe"
$sumsOut  = Join-Path $distDir "SHA256SUMS.txt"

Push-Location $repoRoot
try {
    $origGOOS   = $env:GOOS
    $origGOARCH = $env:GOARCH
    $origCGO    = $env:CGO_ENABLED

    Write-Host "Building GRAB $Version (built: $buildDate)..."

    $env:GOOS        = "windows"
    $env:GOARCH      = "amd64"
    $env:CGO_ENABLED = "0"
    & go build -trimpath -ldflags="$ldflags" -o $winOut ./cmd/grab
    if ($LASTEXITCODE -ne 0) {
        throw "go build for windows/amd64 failed with exit code $LASTEXITCODE"
    }

    $env:GOOS        = "linux"
    $env:GOARCH      = "amd64"
    $env:CGO_ENABLED = "0"
    & go build -trimpath -ldflags="$ldflags" -o $linuxOut ./cmd/grab
    if ($LASTEXITCODE -ne 0) {
        throw "go build for linux/amd64 failed with exit code $LASTEXITCODE"
    }

    $env:GOOS        = $origGOOS
    $env:GOARCH      = $origGOARCH
    $env:CGO_ENABLED = $origCGO

    Write-Host "Compiling Inno Setup installer ($setupOut)..."
    & $iscc "/DAppVersion=$Version" $issPath
    if ($LASTEXITCODE -ne 0) {
        throw "Inno Setup (ISCC) failed with exit code $LASTEXITCODE"
    }
    if (-not (Test-Path $setupOut)) {
        throw "Expected installer was not created: $setupOut"
    }

    $artifacts = @(
        "GRAB-Setup-$Version.exe",
        "grab-windows-amd64.exe",
        "grab-linux-amd64"
    )
    $lines = foreach ($name in $artifacts) {
        $hash = (Get-FileHash -Path (Join-Path $distDir $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $name"
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($sumsOut, (($lines -join "`n") + "`n"), $utf8NoBom)

    Write-Host "Release artifacts built cleanly in ${distDir}:"
    Get-ChildItem -Path $distDir | ForEach-Object { Write-Host ("  {0} ({1:N0} bytes)" -f $_.Name, $_.Length) }
}
finally {
    $env:GOOS        = $origGOOS
    $env:GOARCH      = $origGOARCH
    $env:CGO_ENABLED = $origCGO
    Pop-Location
}
