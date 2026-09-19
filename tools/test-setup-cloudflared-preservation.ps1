param(
    [Parameter(Mandatory = $true)]
    [string]$SetupPath,
    [Parameter(Mandatory = $true)]
    [string]$BundledCloudflaredPath
)

$ErrorActionPreference = "Stop"

$SetupPath = (Resolve-Path -LiteralPath $SetupPath).Path
$BundledCloudflaredPath = (Resolve-Path -LiteralPath $BundledCloudflaredPath).Path
$BaseTemp = $env:RUNNER_TEMP
if ([string]::IsNullOrWhiteSpace($BaseTemp)) {
    $BaseTemp = [System.IO.Path]::GetTempPath()
}
$TestRoot = Join-Path $BaseTemp ("mcp-devdesk-setup-cloudflared-" + [Guid]::NewGuid().ToString("N"))

function Invoke-Setup([string]$Destination) {
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    $process = Start-Process -FilePath $SetupPath -ArgumentList @("/S", "/D=$Destination") -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "Setup failed for $Destination with exit code $($process.ExitCode)"
    }
}

function Invoke-Uninstall([string]$Destination) {
    $uninstaller = Join-Path $Destination "Uninstall.exe"
    if (-not (Test-Path -LiteralPath $uninstaller)) {
        return
    }
    $process = Start-Process -FilePath $uninstaller -ArgumentList @("/S") -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "Uninstall failed for $Destination with exit code $($process.ExitCode)"
    }
}

try {
    New-Item -ItemType Directory -Force -Path $TestRoot | Out-Null

    $upgradeDir = Join-Path $TestRoot "upgrade"
    New-Item -ItemType Directory -Force -Path $upgradeDir | Out-Null
    $existingCloudflared = Join-Path $upgradeDir "cloudflared.exe"
    [System.IO.File]::WriteAllText($existingCloudflared, "locally-updated-cloudflared-must-survive")
    $beforeHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $existingCloudflared).Hash

    Invoke-Setup $upgradeDir

    if (-not (Test-Path -LiteralPath (Join-Path $upgradeDir "MCP-DevDesk.exe"))) {
        throw "Upgrade install did not install MCP-DevDesk.exe"
    }
    $afterHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $existingCloudflared).Hash
    if ($afterHash -ne $beforeHash) {
        throw "Setup overwrote an existing cloudflared.exe during upgrade"
    }
    Invoke-Uninstall $upgradeDir

    $freshDir = Join-Path $TestRoot "fresh"
    Invoke-Setup $freshDir

    $freshCloudflared = Join-Path $freshDir "cloudflared.exe"
    if (-not (Test-Path -LiteralPath $freshCloudflared)) {
        throw "Fresh install did not seed cloudflared.exe"
    }
    $bundledHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $BundledCloudflaredPath).Hash
    $freshHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $freshCloudflared).Hash
    if ($freshHash -ne $bundledHash) {
        throw "Fresh install cloudflared.exe does not match the bundled runtime"
    }
    Invoke-Uninstall $freshDir

    Write-Host "NSIS cloudflared preservation smoke test passed." -ForegroundColor Green
} finally {
    Remove-Item -LiteralPath $TestRoot -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath "HKLM:\Software\MCP DevDesk" -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk" -Recurse -Force -ErrorAction SilentlyContinue
}
