param(
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",
    [switch]$RunTests
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$AppDir = Join-Path $Root "app"
$FrontendDir = Join-Path $Root "frontend"
$DistDir = Join-Path $Root "dist"
$BrandAssetScript = Join-Path $Root "tools\generate-brand-assets.ps1"
$ExeIconScript = Join-Path $Root "tools\set-exe-icon.ps1"
$SmokeScript = Join-Path $Root "tools\smoke-go-core.ps1"
$PackageScript = Join-Path $Root "package-portable.ps1"

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $AppDir ".gocache") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $AppDir ".gotmp") | Out-Null

if (Test-Path -LiteralPath $BrandAssetScript) {
    & $BrandAssetScript
}

if (Test-Path (Join-Path $FrontendDir "package.json")) {
    Push-Location $FrontendDir
    try {
        if (-not (Test-Path (Join-Path $FrontendDir "node_modules"))) {
            npm ci
        }
        npm run build
    } finally {
        Pop-Location
    }
}

Push-Location $AppDir
try {
    $env:GOCACHE = Join-Path $AppDir ".gocache"
    $env:GOTMPDIR = Join-Path $AppDir ".gotmp"
    if ($RunTests) {
        go test -mod=vendor ./...
    }

    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    $Output = Join-Path $DistDir "MCP-DevDesk-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w -H=windowsgui" -o $Output ./cmd/mcp-devdesk
    if (Test-Path -LiteralPath $ExeIconScript) {
        & $ExeIconScript -ExePath $Output -IconPath (Join-Path $AppDir "internal\desktop\assets\mcp-devdesk.ico")
    }

    $CliOutput = Join-Path $DistDir "devdeskctl-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w" -o $CliOutput ./cmd/devdeskctl

    $CoreOutput = Join-Path $DistDir "mcp-core-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w" -o $CoreOutput ./cmd/mcp-core
    Copy-Item -LiteralPath $CoreOutput -Destination (Join-Path $DistDir "mcp-core.exe") -Force

    Write-Host "Build complete: $Output" -ForegroundColor Green
    Write-Host "CLI complete:   $CliOutput" -ForegroundColor Green
    Write-Host "Go MCP core:    $CoreOutput" -ForegroundColor Green
} finally {
    Pop-Location
}

if ($RunTests -and (Test-Path -LiteralPath $SmokeScript)) {
    & $SmokeScript -ExePath (Join-Path $DistDir "mcp-core-$Arch.exe") -Workspace $Root

    Push-Location $AppDir
    try {
        $PreviousE2ECore = $env:MCP_DEV_DESK_E2E_CORE
        $env:MCP_DEV_DESK_E2E_CORE = Join-Path $DistDir "mcp-core-$Arch.exe"
        go test -mod=vendor ./internal/application -run TestRealMultiInstanceStart -count=1
    } finally {
        $env:MCP_DEV_DESK_E2E_CORE = $PreviousE2ECore
        Pop-Location
    }
}

if ($RunTests -and (Test-Path -LiteralPath $PackageScript)) {
    & $PackageScript -Arch $Arch -SkipBuild
    $PackagedCore = Join-Path $DistDir "MCP-DevDesk-Portable-$Arch\mcp-core.exe"
    if (Test-Path -LiteralPath $PackagedCore) {
        & $SmokeScript -ExePath $PackagedCore -Workspace $Root -Port 18767
    }
}


