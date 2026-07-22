$ErrorActionPreference = 'Stop'
$sourceRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path $env:TEMP 'mcp-devdesk-single-instance-smoke'

Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $testRoot | Out-Null
Copy-Item (Join-Path $sourceRoot 'dist\MCP-DevDesk-amd64.exe') (Join-Path $testRoot 'MCP-DevDesk-amd64.exe')
Copy-Item (Join-Path $sourceRoot 'dist\devdeskctl-amd64.exe') (Join-Path $testRoot 'devdeskctl.exe')
Copy-Item (Join-Path $sourceRoot 'coding-tools-mcp.exe') (Join-Path $testRoot 'coding-tools-mcp.exe')
Copy-Item (Join-Path $sourceRoot 'cloudflared.exe') (Join-Path $testRoot 'cloudflared.exe')
Copy-Item (Join-Path $sourceRoot 'dist\mcp-core-amd64.exe') (Join-Path $testRoot 'mcp-core.exe')

Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class Win32SingleInstanceSmoke {
    [DllImport("user32.dll")]
    public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr parameter);
    public delegate bool EnumWindowsProc(IntPtr handle, IntPtr parameter);
    [DllImport("user32.dll")]
    public static extern uint GetWindowThreadProcessId(IntPtr handle, out uint processId);
    [DllImport("user32.dll")]
    public static extern bool IsWindowVisible(IntPtr handle);
}
'@

function Start-TestManager {
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = Join-Path $testRoot 'MCP-DevDesk-amd64.exe'
    $startInfo.Arguments = '--background'
    $startInfo.UseShellExecute = $false
    $startInfo.EnvironmentVariables['MCP_DEVDESK_ROOT'] = $testRoot
    return [System.Diagnostics.Process]::Start($startInfo)
}

function Test-VisibleWindow([int]$processId) {
    $script:visible = $false
    [Win32SingleInstanceSmoke]::EnumWindows({
        param($handle, $parameter)
        [uint32]$owner = 0
        [void][Win32SingleInstanceSmoke]::GetWindowThreadProcessId($handle, [ref]$owner)
        if ($owner -eq $processId -and [Win32SingleInstanceSmoke]::IsWindowVisible($handle)) {
            $script:visible = $true
        }
        return $true
    }, [IntPtr]::Zero) | Out-Null
    return $script:visible
}

$primary = Start-TestManager
try {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        & (Join-Path $testRoot 'devdeskctl.exe') health *> $null
        $ready = $LASTEXITCODE -eq 0
    } while (-not $ready -and (Get-Date) -lt $deadline)
    if (-not $ready) {
        throw 'primary manager did not become healthy'
    }

    $configPath = Join-Path $testRoot 'data\devdesk\config.json'
    $config = Get-Content $configPath -Raw | ConvertFrom-Json
    $config.adminPort = 17999
    $configJSON = $config | ConvertTo-Json -Depth 20
    [IO.File]::WriteAllText($configPath, $configJSON, (New-Object Text.UTF8Encoding($false)))

    $secondary = Start-TestManager
    if (-not $secondary.WaitForExit(8000)) {
        Stop-Process -Id $secondary.Id -Force -ErrorAction SilentlyContinue
        throw 'secondary manager did not exit after detecting the existing instance'
    }
    if ($secondary.ExitCode -ne 0) {
        throw "secondary manager exit code = $($secondary.ExitCode)"
    }

    $windowDeadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        $visible = Test-VisibleWindow $primary.Id
    } while (-not $visible -and (Get-Date) -lt $windowDeadline)
    if (-not $visible) {
        throw 'existing manager did not open its native window through the tray signal'
    }

    [pscustomobject]@{
        PrimaryPID = $primary.Id
        SecondaryExitCode = $secondary.ExitCode
        HTTPPortIntentionallyMismatched = 17999
        ExistingWindowOpened = $visible
    } | Format-List
}
finally {
    if ($primary -and -not $primary.HasExited) {
        Stop-Process -Id $primary.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1
    Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-single-instance-smoke*'
    } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
    Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
