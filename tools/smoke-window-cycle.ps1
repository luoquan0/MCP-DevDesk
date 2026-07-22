param(
    [int]$Cycles = 12,
    [int]$PauseMilliseconds = 700,
    [int]$FinalWaitSeconds = 8
)

$ErrorActionPreference = 'Stop'

$sourceRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path $env:TEMP 'mcp-devdesk-window-cycle-smoke'

Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $testRoot | Out-Null
Copy-Item (Join-Path $sourceRoot 'dist\MCP-DevDesk-amd64.exe') (Join-Path $testRoot 'MCP-DevDesk-amd64.exe')
Copy-Item (Join-Path $sourceRoot 'dist\devdeskctl-amd64.exe') (Join-Path $testRoot 'devdeskctl.exe')
Copy-Item (Join-Path $sourceRoot 'coding-tools-mcp.exe') (Join-Path $testRoot 'coding-tools-mcp.exe')
Copy-Item (Join-Path $sourceRoot 'cloudflared.exe') (Join-Path $testRoot 'cloudflared.exe')
Copy-Item (Join-Path $sourceRoot 'dist\mcp-core-amd64.exe') (Join-Path $testRoot 'mcp-core.exe')

$startInfo = New-Object System.Diagnostics.ProcessStartInfo
$startInfo.FileName = Join-Path $testRoot 'MCP-DevDesk-amd64.exe'
$startInfo.Arguments = '--background'
$startInfo.UseShellExecute = $false
$startInfo.EnvironmentVariables['MCP_DEVDESK_ROOT'] = $testRoot
$manager = [System.Diagnostics.Process]::Start($startInfo)

try {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        & (Join-Path $testRoot 'devdeskctl.exe') health *> $null
        $ready = $LASTEXITCODE -eq 0
    } while (-not $ready -and (Get-Date) -lt $deadline)
    if (-not $ready) {
        throw 'manager health did not become ready'
    }

    Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class Win32Cycle {
    [DllImport("user32.dll")]
    public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr parameter);
    public delegate bool EnumWindowsProc(IntPtr handle, IntPtr parameter);
    [DllImport("user32.dll")]
    public static extern uint GetWindowThreadProcessId(IntPtr handle, out uint processId);
    [DllImport("user32.dll")]
    public static extern bool IsWindowVisible(IntPtr handle);
    [DllImport("user32.dll")]
    public static extern bool PostMessage(IntPtr handle, uint message, IntPtr wParam, IntPtr lParam);
}
'@

    function Close-VisibleWindow([int]$processId) {
        [Win32Cycle]::EnumWindows({
            param($handle, $parameter)
            [uint32]$owner = 0
            [void][Win32Cycle]::GetWindowThreadProcessId($handle, [ref]$owner)
            if ($owner -eq $processId -and [Win32Cycle]::IsWindowVisible($handle)) {
                [void][Win32Cycle]::PostMessage($handle, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)
            }
            return $true
        }, [IntPtr]::Zero) | Out-Null
    }

    $rows = @()
    for ($cycle = 1; $cycle -le $Cycles; $cycle++) {
        & (Join-Path $testRoot 'devdeskctl.exe') open *> $null
        if ($LASTEXITCODE -ne 0) {
            throw "open failed at cycle $cycle"
        }
        Start-Sleep -Milliseconds $PauseMilliseconds
        $process = Get-Process -Id $manager.Id
        $webViews = Get-CimInstance Win32_Process | Where-Object {
            $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-window-cycle-smoke*'
        }
        $rows += [pscustomobject]@{
            Cycle = $cycle
            Phase = 'open'
            WorkingSetMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
            PrivateMB = [math]::Round($process.PrivateMemorySize64 / 1MB, 2)
            Handles = $process.HandleCount
            Threads = $process.Threads.Count
            WebViewProcesses = @($webViews).Count
        }

        Close-VisibleWindow $manager.Id
        Start-Sleep -Milliseconds $PauseMilliseconds
        $process = Get-Process -Id $manager.Id
        $webViews = Get-CimInstance Win32_Process | Where-Object {
            $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-window-cycle-smoke*'
        }
        $rows += [pscustomobject]@{
            Cycle = $cycle
            Phase = 'closed'
            WorkingSetMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
            PrivateMB = [math]::Round($process.PrivateMemorySize64 / 1MB, 2)
            Handles = $process.HandleCount
            Threads = $process.Threads.Count
            WebViewProcesses = @($webViews).Count
        }
    }

    Start-Sleep -Seconds $FinalWaitSeconds
    $process = Get-Process -Id $manager.Id
    $webViews = Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-window-cycle-smoke*'
    }
    $rows += [pscustomobject]@{
        Cycle = $Cycles
        Phase = "settled-${FinalWaitSeconds}s"
        WorkingSetMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
        PrivateMB = [math]::Round($process.PrivateMemorySize64 / 1MB, 2)
        Handles = $process.HandleCount
        Threads = $process.Threads.Count
        WebViewProcesses = @($webViews).Count
    }

    $rows | Format-Table -AutoSize

    $closedRows = @($rows | Where-Object { $_.Phase -eq 'closed' })
    $residualWebViews = @($closedRows | Where-Object { $_.WebViewProcesses -ne 0 })
    if ($residualWebViews.Count -ne 0) {
        throw "WebView2 child processes remained after $($residualWebViews.Count) window closes"
    }

    $plateauRows = @($closedRows | Select-Object -Last ([math]::Min(10, $closedRows.Count)))
    if ($Cycles -ge 30 -and $plateauRows.Count -ge 2) {
        $handleValues = @($plateauRows | ForEach-Object { [int]$_.Handles })
        $handleRange = ($handleValues | Measure-Object -Maximum).Maximum - ($handleValues | Measure-Object -Minimum).Minimum
        if ($handleRange -gt 40) {
            throw "window-cycle handles did not plateau; last-window range is $handleRange"
        }
    }

    if (@($rows | Select-Object -Last 1)[0].WebViewProcesses -ne 0) {
        throw 'WebView2 child processes remained after the final settle period'
    }
}
finally {
    if ($manager -and -not $manager.HasExited) {
        Stop-Process -Id $manager.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1
    Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-window-cycle-smoke*'
    } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
    Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
