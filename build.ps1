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

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $AppDir ".gocache") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $AppDir ".gotmp") | Out-Null

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

    $CliOutput = Join-Path $DistDir "devdeskctl-$Arch.exe"
    go build -mod=vendor -trimpath -ldflags "-s -w" -o $CliOutput ./cmd/devdeskctl

    Write-Host "Build complete: $Output" -ForegroundColor Green
    Write-Host "CLI complete:   $CliOutput" -ForegroundColor Green
} finally {
    Pop-Location
}


