param(
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dist = Join-Path $Root "dist"
$PackageRoot = Join-Path $Dist "MCP-DevDesk-Portable-$Arch"
$Archive = Join-Path $Dist "MCP-DevDesk-Portable-$Arch.zip"

if (-not $SkipBuild) {
    & (Join-Path $Root "build.ps1") -Arch $Arch -RunTests
}

Remove-Item -LiteralPath $PackageRoot -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $PackageRoot | Out-Null

Copy-Item (Join-Path $Dist "MCP-DevDesk-$Arch.exe") (Join-Path $PackageRoot "MCP-DevDesk.exe")
Copy-Item (Join-Path $Dist "devdeskctl-$Arch.exe") (Join-Path $PackageRoot "devdeskctl.exe")
Copy-Item (Join-Path $Root "cloudflared.exe") $PackageRoot
Copy-Item (Join-Path $Root "coding-tools-mcp.exe") $PackageRoot
Copy-Item (Join-Path $Root "README.md") $PackageRoot

@'
@echo off
cd /d "%~dp0"
start "" "%~dp0MCP-DevDesk.exe"
'@ | Set-Content -LiteralPath (Join-Path $PackageRoot "启动 MCP DevDesk.cmd") -Encoding ASCII

if (Test-Path $Archive) {
    Remove-Item -LiteralPath $Archive -Force
}
Compress-Archive -Path (Join-Path $PackageRoot "*") -DestinationPath $Archive -CompressionLevel Optimal
Write-Host "Portable package complete: $Archive" -ForegroundColor Green

