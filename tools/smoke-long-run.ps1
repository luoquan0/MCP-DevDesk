param(
    [int]$Requests = 600,
    [ValidateSet('health', 'status')]
    [string]$Command = 'status',
    [int]$HealthCheckEvery = 50,
    [int]$Rounds = 1
)

$ErrorActionPreference = 'Stop'
$sourceRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path $env:TEMP 'mcp-devdesk-long-run-smoke'

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

function Invoke-DevDesk([string]$command) {
    $output = & (Join-Path $testRoot 'devdeskctl.exe') $command 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "devdeskctl $command failed: $output"
    }
    return ($output -join "`n")
}

function Read-Snapshot([string]$phase) {
    $diagnostics = (Invoke-DevDesk 'diagnostics') | ConvertFrom-Json
    $process = Get-Process -Id $manager.Id
    return [pscustomobject]@{
        Phase = $phase
        WorkingSetMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
        PrivateMB = [math]::Round($process.PrivateMemorySize64 / 1MB, 2)
        Handles = $process.HandleCount
        Threads = $process.Threads.Count
        Goroutines = [int]$diagnostics.goGoroutines
        HeapAllocMB = [math]::Round(([double]$diagnostics.goHeapAllocBytes) / 1MB, 2)
        HeapInUseMB = [math]::Round(([double]$diagnostics.goHeapInUseBytes) / 1MB, 2)
    }
}

try {
    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 250
        try {
            Invoke-DevDesk 'health' *> $null
            $ready = $true
        }
        catch {
            $ready = $false
        }
    } while (-not $ready -and (Get-Date) -lt $deadline)
    if (-not $ready) {
        throw 'manager health did not become ready'
    }

    $snapshots = @()
    $before = Read-Snapshot 'before'
    $snapshots += $before
    $stopwatch = [Diagnostics.Stopwatch]::StartNew()
    for ($round = 1; $round -le $Rounds; $round++) {
        for ($index = 1; $index -le $Requests; $index++) {
            Invoke-DevDesk $Command *> $null
            if (($index % $HealthCheckEvery) -eq 0) {
                Invoke-DevDesk 'health' *> $null
                if ($manager.HasExited) {
                    throw "manager exited during round $round after $index requests"
                }
            }
        }
        Start-Sleep -Seconds 3
        $snapshots += Read-Snapshot "round-$round"
    }
    $stopwatch.Stop()
    Start-Sleep -Seconds 5
    $after = Read-Snapshot 'after'
    $snapshots += $after

    $snapshots | Format-Table -AutoSize
    [pscustomobject]@{
        Command = $Command
        RequestsPerRound = $Requests
        Rounds = $Rounds
        TotalRequests = $Requests * $Rounds
        ElapsedSeconds = [math]::Round($stopwatch.Elapsed.TotalSeconds, 2)
        HandleGrowth = $after.Handles - $before.Handles
        ThreadGrowth = $after.Threads - $before.Threads
        GoroutineGrowth = $after.Goroutines - $before.Goroutines
        PrivateMemoryGrowthMB = [math]::Round($after.PrivateMB - $before.PrivateMB, 2)
        HeapGrowthMB = [math]::Round($after.HeapAllocMB - $before.HeapAllocMB, 2)
    } | Format-List

    if (($after.Goroutines - $before.Goroutines) -gt 10) {
        throw 'goroutine growth exceeded the smoke-test limit'
    }
    if (($after.Handles - $before.Handles) -gt 200) {
        throw 'handle growth exceeded the smoke-test limit'
    }
    if (($after.PrivateMB - $before.PrivateMB) -gt 64) {
        throw 'private memory growth exceeded the smoke-test limit'
    }
}
finally {
    if ($manager -and -not $manager.HasExited) {
        Stop-Process -Id $manager.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1
    Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -like '*mcp-devdesk-long-run-smoke*'
    } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
    Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
