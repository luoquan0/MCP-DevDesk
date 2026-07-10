<# =============================================
  Coding Tools MCP - 固定域名隧道管理菜单 (便携版)
  双击 启动.cmd 运行
============================================= #>

$ErrorActionPreference = "Continue"
$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ConfigFile = Join-Path $RootDir "config.json"
$StateFile = Join-Path $RootDir "data\state.json"
$TunnelScript = Join-Path $RootDir "tunnel.ps1"
$SetupScript = Join-Path $RootDir "setup.ps1"
$CloudflaredExe = Join-Path $RootDir "cloudflared.exe"
$McpExeFile = Join-Path $RootDir "coding-tools-mcp.exe"
$DataDir = Join-Path $RootDir "data"
$WatchdogFlag = Join-Path $DataDir "watchdog.enabled"
$OAuthPasswordFile = Join-Path $DataDir "oauth-password.txt"
$OAuthClientFile = Join-Path $DataDir "oauth-client.json"

function Ensure-Dir($Path) {
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Read-Config {
    if (-not (Test-Path $ConfigFile)) { return $null }
    try { return Get-Content $ConfigFile -Raw -Encoding UTF8 | ConvertFrom-Json } catch { return $null }
}

function Read-State {
    if (-not (Test-Path $StateFile)) { return $null }
    try { return Get-Content $StateFile -Raw -Encoding UTF8 | ConvertFrom-Json } catch { return $null }
}

function Test-ProcessByPid($ProcessId, $ExePath) {
    if (-not $ProcessId) { return $false }
    try {
        $proc = Get-Process -Id $ProcessId -ErrorAction Stop
        if ($ExePath) { return ($proc.Path -eq $ExePath) }
        return $true
    } catch { return $false }
}

function Show-Banner {
    Clear-Host
    Write-Host ""
    Write-Host "=============================================" -ForegroundColor Cyan
    Write-Host "  Coding Tools MCP - 固定域名隧道管理 (便携版)" -ForegroundColor White
    Write-Host "=============================================" -ForegroundColor Cyan
    Write-Host ""

    $config = Read-Config
    if ($config -and $config.tunnelDomain) {
        Write-Host "  域名:     " -NoNewline
        Write-Host $config.tunnelDomain -ForegroundColor Green
        if ($config.tunnelId) {
            Write-Host "  远程 URL: " -NoNewline
            Write-Host "https://$($config.tunnelDomain)/mcp" -ForegroundColor Green
        }
        if ($config.workspace) { Write-Host "  工作区:   $($config.workspace)" }
    } else {
        Write-Host "  域名未配置 - 请先执行 [4] 配置域名" -ForegroundColor Yellow
    }

    Write-Host ""

    # Accurate status: read state.json and check PIDs
    $state = Read-State
    $mcpAlive = $false; $tunnelAlive = $false
    if ($state) {
        $mcpAlive = Test-ProcessByPid $state.mcpPid $McpExeFile
        $tunnelAlive = Test-ProcessByPid $state.tunnelPid $CloudflaredExe
    }

    Write-Host "  MCP 服务:  " -NoNewline
    if ($mcpAlive) {
        Write-Host "[运行中 PID:$($state.mcpPid)]" -ForegroundColor Green
    } else {
        Write-Host "[已停止]" -ForegroundColor Red
    }

    Write-Host "  隧道连接:  " -NoNewline
    if ($tunnelAlive) {
        Write-Host "[运行中 PID:$($state.tunnelPid)]" -ForegroundColor Green
    } else {
        Write-Host "[已停止]" -ForegroundColor Red
    }

    if (Test-Path $WatchdogFlag) {
        Write-Host "  自动守护:  [已开启 - 掉线自动重启]" -ForegroundColor Cyan
    } else {
        Write-Host "  自动守护:  [未开启]" -ForegroundColor DarkGray
    }

    if (-not (Test-Path $CloudflaredExe)) {
        Write-Host "  [警告] cloudflared.exe 未找到!" -ForegroundColor Red
    }

    Write-Host ""
}

# Watchdog launcher
function Start-Watchdog {
    Ensure-Dir $DataDir
    Set-Content -LiteralPath $WatchdogFlag -Value "1" -Encoding ASCII
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = "powershell.exe"
    $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$TunnelScript`" -Action watchdog"
    $psi.WorkingDirectory = $RootDir
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    [System.Diagnostics.Process]::Start($psi) | Out-Null
    Write-Host "  [已开启] 自动守护已启动, 掉线后 30 秒内自动重启。" -ForegroundColor Green
}

function Stop-Watchdog {
    Remove-Item -LiteralPath $WatchdogFlag -ErrorAction SilentlyContinue
    Write-Host "  [已关闭] 自动守护已停止。" -ForegroundColor Green
}

do {
    Show-Banner

    Write-Host "  ════════════════════════════════════════" -ForegroundColor DarkGray
    Write-Host "  [1] 启动 MCP 隧道服务"
    Write-Host "  [2] 停止 MCP 隧道服务"
    Write-Host "  [3] 查看连接信息 / OAuth 凭证"
    Write-Host "  [4] 配置 / 修改固定域名"
    Write-Host ""
    Write-Host "  [6] 开启自动守护 (掉线自动重启)"
    Write-Host "  [7] 关闭自动守护"
    Write-Host ""
    Write-Host "  [5] 退出"
    Write-Host "  ════════════════════════════════════════" -ForegroundColor DarkGray
    Write-Host ""

    $choice = Read-Host "  输入选项 [1-7]"

    switch ($choice) {
        "1" {
            $config = Read-Config
            if (-not (Test-Path $CloudflaredExe)) {
                Write-Host "  [错误] cloudflared.exe 缺失!" -ForegroundColor Red
            } elseif (-not $config -or -not $config.tunnelDomain -or -not $config.tunnelId) {
                Write-Host "  [错误] 域名或隧道 UUID 未配置, 请先执行 [4]!" -ForegroundColor Red
            } else {
                & $TunnelScript -Action start
            }
        }
        "2" {
            Stop-Watchdog
            & $TunnelScript -Action stop
        }
        "3" { & $TunnelScript -Action show }
        "4" { & $SetupScript }
        "6" { Start-Watchdog }
        "7" { Stop-Watchdog }
        "5" { Write-Host "  再见!" -ForegroundColor Green; return }
        default { Write-Host "  无效选项" -ForegroundColor Red; Start-Sleep 1 }
    }

    if ($choice -ne "5") {
        Write-Host ""
        Read-Host "  按 Enter 返回菜单"
    }
} while ($true)
