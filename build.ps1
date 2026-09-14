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
$V01235SmokeScript = Join-Path $Root "tools\smoke-v01235.ps1"
$PackageScript = Join-Path $Root "package-portable.ps1"

function Assert-NativeSuccess([string]$Label) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Label failed with exit code $LASTEXITCODE"
    }
}

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
            Assert-NativeSuccess "npm ci"
        }
        npm run build
        Assert-NativeSuccess "frontend build"
    } finally {
        Pop-Location
    }
}

Push-Location $AppDir
try {
    $env:GOCACHE = Join-Path $AppDir ".gocache"
    $env:GOTMPDIR = Join-Path $AppDir ".gotmp"
    if ($RunTests) {
        # The stable baseline contains unrelated legacy files that are not gofmt-clean.
        # Verify only the Go files touched by the focused v0.12.35 port so this release
        # does not reformat unrelated Tunnel, Screen Vision, web, or vendored code.
        $goFiles = @(
            "internal/mcpcore/check_tools.go",
            "internal/mcpcore/check_tools_test.go",
            "internal/mcpcore/code_navigation.go",
            "internal/mcpcore/command_platform_windows.go",
            "internal/mcpcore/command_tools.go",
            "internal/mcpcore/file_tools.go",
            "internal/mcpcore/server.go",
            "internal/mcpcore/server_test.go",
            "internal/mcpcore/v01235_features_test.go"
        )
        $badFormat = @()
        foreach ($file in $goFiles) {
            if (-not (Test-Path -LiteralPath $file)) {
                throw "gofmt target is missing: $file"
            }
            $formatted = @(gofmt -l -- $file)
            Assert-NativeSuccess "gofmt $file"
            if ($formatted.Count -gt 0) {
                $badFormat += $formatted
            }
        }
        if ($badFormat.Count -gt 0) {
            throw "gofmt verification failed: $($badFormat -join ', ')"
        }
        go test -mod=vendor ./...
        Assert-NativeSuccess "go test -mod=vendor ./..."
    }

    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    $Output = Join-Path $DistDir "MCP-DevDesk-$Arch.exe"
    $ManagerLdFlags = "-s -w -H=windowsgui"
    if (-not [string]::IsNullOrWhiteSpace($env:MCP_DEVDESK_GITHUB_REPOSITORY)) {
        $ManagerLdFlags += " -X mcp-devdesk/internal/buildinfo.Repository=$($env:MCP_DEVDESK_GITHUB_REPOSITORY)"
    }
    go build -mod=vendor -trimpath -ldflags $ManagerLdFlags -o $Output ./cmd/mcp-devdesk
    Assert-NativeSuccess "MCP DevDesk build"
    if (Test-Path -LiteralPath $ExeIconScript) {
        & $ExeIconScript -ExePath $Output -IconPath (Join-Path $AppDir "internal\desktop\assets\mcp-devdesk.ico")
    }

    $CliOutput = Join-Path $DistDir "devdeskctl-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w" -o $CliOutput ./cmd/devdeskctl
    Assert-NativeSuccess "devdeskctl build"

    $CoreOutput = Join-Path $DistDir "mcp-core-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w" -o $CoreOutput ./cmd/mcp-core
    Assert-NativeSuccess "mcp-core build"
    Copy-Item -LiteralPath $CoreOutput -Destination (Join-Path $DistDir "mcp-core.exe") -Force

    $UpdaterOutput = Join-Path $DistDir "devdesk-updater-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w -H=windowsgui" -o $UpdaterOutput ./cmd/devdesk-updater
    Assert-NativeSuccess "devdesk-updater build"

    Write-Host "Build complete: $Output" -ForegroundColor Green
    Write-Host "CLI complete:   $CliOutput" -ForegroundColor Green
    Write-Host "Go MCP core:    $CoreOutput" -ForegroundColor Green
    Write-Host "Updater:        $UpdaterOutput" -ForegroundColor Green
} finally {
    Pop-Location
}

if ($RunTests -and (Test-Path -LiteralPath $SmokeScript)) {
    & $SmokeScript -ExePath (Join-Path $DistDir "mcp-core-$Arch.exe") -Workspace $Root
    if (Test-Path -LiteralPath $V01235SmokeScript) {
        & $V01235SmokeScript -ExePath (Join-Path $DistDir "mcp-core-$Arch.exe") -Port 18768
    }

    Push-Location $AppDir
    try {
        $PreviousE2ECore = $env:MCP_DEV_DESK_E2E_CORE
        $env:MCP_DEV_DESK_E2E_CORE = Join-Path $DistDir "mcp-core-$Arch.exe"
        go test -mod=vendor ./internal/application -run TestRealMultiInstanceStart -count=1
        Assert-NativeSuccess "real multi-instance Go core test"
    } finally {
        $env:MCP_DEV_DESK_E2E_CORE = $PreviousE2ECore
        Pop-Location
    }
}

if ($RunTests -and (Test-Path -LiteralPath $PackageScript)) {
    & $PackageScript -Arch $Arch -SkipBuild
    $PackagedCore = Join-Path $DistDir "MCP-DevDesk-Portable-$Arch\mcp-core.exe"
    $PackagedUpdater = Join-Path $DistDir "MCP-DevDesk-Portable-$Arch\devdesk-updater.exe"
    if (Test-Path -LiteralPath $PackagedCore) {
        & $SmokeScript -ExePath $PackagedCore -Workspace $Root -Port 18767
    }
    if (-not (Test-Path -LiteralPath $PackagedUpdater)) {
        throw "Portable package is missing devdesk-updater.exe"
    }
}
