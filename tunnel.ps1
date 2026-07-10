<# =============================================
  Coding Tools MCP - 隧道控制脚本 (便携版)
  用法: .\tunnel.ps1 [start|stop|show|watchdog]
  无需 Python - coding-tools-mcp.exe 已内置所有依赖
============================================= #>

param([string]$Action = "start")
$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ConfigFile  = Join-Path $RootDir "config.json"
$DataDir     = Join-Path $RootDir "data"
$McpExe      = Join-Path $RootDir "coding-tools-mcp.exe"
$CfExe       = Join-Path $RootDir "cloudflared.exe"
$StateFile   = Join-Path $DataDir "state.json"
$OAuthPwFile = Join-Path $DataDir "oauth-password.txt"
$OAuthCliFile = Join-Path $DataDir "oauth-client.json"
$TokenSecFile = Join-Path $DataDir "oauth-token-secret.txt"
$McpOutLog   = Join-Path $DataDir "mcp-stdout.log"
$McpErrLog   = Join-Path $DataDir "mcp-stderr.log"
$CfOutLog    = Join-Path $DataDir "tunnel-stdout.log"
$CfErrLog    = Join-Path $DataDir "tunnel-stderr.log"
$WdFlag      = Join-Path $DataDir "watchdog.enabled"

function d { New-Item -ItemType Directory -Force -Path $args[0] | Out-Null }
function banner($t) { Write-Host ""; Write-Host "== $t ==" -ForegroundColor Cyan }

function cfg  {
    if (-not (Test-Path $ConfigFile)) { throw "Config not found. Run setup first." }
    Get-Content $ConfigFile -Raw -Encoding UTF8 | ConvertFrom-Json
}
function state { if (Test-Path $StateFile) { try { Get-Content $StateFile -Raw -Encoding UTF8 | ConvertFrom-Json } catch { $null } } else { $null } }
function save($s) { d $DataDir; $s | ConvertTo-Json -Depth 6 | Set-Content $StateFile -Encoding UTF8 }

function alive($procId,$exe) {
    if (-not $procId) { return $false }
    try { $p = Get-Process -Id $procId -ErrorAction Stop; return ($p.Path -eq $exe) } catch { return $false }
}

function secret {
    # MCP server expects hex-encoded 32 bytes (64 hex chars), NOT base64
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $b = New-Object byte[] 32; $rng.GetBytes($b)
    return -join ($b | ForEach-Object { '{0:X2}' -f $_ })
}

function tokenSecret {
    d $DataDir
    $existing = if (Test-Path $TokenSecFile) { (Get-Content $TokenSecFile -Raw).Trim() } else { "" }
    if ($existing -match '^[0-9A-Fa-f]{64}$') { return $existing }
    if ($existing) {
        $bak = "$TokenSecFile.invalid.$((Get-Date).ToString('yyyyMMddHHmmss'))"
        Move-Item -LiteralPath $TokenSecFile -Destination $bak -Force
        Write-Host "Invalid OAuth token secret was backed up and regenerated." -ForegroundColor Yellow
    }
    $newSecret = secret
    Set-Content -LiteralPath $TokenSecFile -Value $newSecret -Encoding ASCII
    return $newSecret
}

function oauthClient {
    if (Test-Path $OAuthCliFile) { try { $c = Get-Content $OAuthCliFile -Raw -Encoding UTF8 | ConvertFrom-Json; if ($c.clientId) { return $c } } catch {} }
    $n = [PSCustomObject]@{ clientId = "codex-mcp"; clientSecret = secret; createdAt = (Get-Date).ToString("o") }
    d $DataDir; $n | ConvertTo-Json -Depth 4 | Set-Content $OAuthCliFile -Encoding UTF8; return $n
}

function killTree($procId) {
    if (-not $procId) { return }
    Get-CimInstance Win32_Process -Filter "ParentProcessId=$procId" -ErrorAction SilentlyContinue | ForEach-Object { killTree $_.ProcessId }
    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
}

function killAll($skip) {
    Get-Process -Name "coding-tools-mcp" -ErrorAction SilentlyContinue | ForEach-Object { if ($skip -notcontains $_.Id) { killTree $_.Id } }
    Get-Process -Name "cloudflared" -ErrorAction SilentlyContinue | ForEach-Object { if ($skip -notcontains $_.Id) { killTree $_.Id } }
}

# ===== START =====
function Start-Tunnel {
    d $DataDir
    $st = state
    if ($st -and (alive $st.mcpPid $McpExe) -and (alive $st.tunnelPid $CfExe)) {
        Write-Host "Service already running." -ForegroundColor Yellow; Show-Info; return
    }
    if ($st) { killAll @($st.mcpPid, $st.tunnelPid) } else { killAll @() }
    Remove-Item $StateFile -ErrorAction SilentlyContinue

    $c = cfg
    if (-not $c.tunnelDomain) { throw "Domain not configured!" }
    if (-not $c.tunnelId)     { throw "Tunnel UUID not configured! Run setup." }
    if (-not (Test-Path $McpExe)) { throw "coding-tools-mcp.exe not found!" }
    if (-not (Test-Path $CfExe))  { throw "cloudflared.exe not found!" }

    $creds = Join-Path $env:USERPROFILE ".cloudflared\$($c.tunnelId).json"
    if (-not (Test-Path $creds)) {
        $f = Get-ChildItem "$env:USERPROFILE\.cloudflared\*.json" -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($f) { $creds = $f.FullName } else { throw "Tunnel credentials not found! Re-run setup." }
    }

    Remove-Item $McpOutLog,$McpErrLog,$CfOutLog,$CfErrLog -ErrorAction SilentlyContinue

    # OAuth secrets
    $tokenSec = tokenSecret
    $oauthPw = secret; $oauthCli = oauthClient
    Set-Content $OAuthPwFile -Value $oauthPw -Encoding ASCII

    $base = "https://$($c.tunnelDomain)"
    $env:CODING_TOOLS_MCP_SERVER_URL          = $base
    $env:CODING_TOOLS_MCP_OAUTH_PASSWORD      = $oauthPw
    $env:CODING_TOOLS_MCP_OAUTH_CLIENT_ID     = $oauthCli.clientId
    $env:CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET = $oauthCli.clientSecret
    $env:CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET  = $tokenSec
    $env:CODING_TOOLS_MCP_TOOL_PROFILE        = $c.toolProfile

    # Proxy for cloudflared
    if ($c.proxyAddress) {
        $env:HTTPS_PROXY = $c.proxyAddress; $env:HTTP_PROXY = $c.proxyAddress
        if ($c.proxyUsername) {
            $u = [System.Uri]$c.proxyAddress
            $env:HTTPS_PROXY = "http://$($c.proxyUsername):$($c.proxyPassword)@$($u.Host):$($u.Port)"
            $env:HTTP_PROXY = $env:HTTPS_PROXY
        }
    }

    banner "Starting MCP server"
    $ws = if ($c.workspace) { $c.workspace } else { $RootDir }
    # Allow package managers and other development tools to access the network
    # while keeping the remaining exec_command permission gates enabled.
    $mcpArgs = @("--workspace", $ws, "--host", "127.0.0.1", "--port", "8765", "--tool-profile", $c.toolProfile, "--oauth-mode", "--allow-network")
    $mcp = Start-Process -FilePath $McpExe -ArgumentList $mcpArgs -WorkingDirectory $RootDir -PassThru -RedirectStandardOutput $McpOutLog -RedirectStandardError $McpErrLog -WindowStyle Hidden

    for ($i=0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 500
        if ($mcp.HasExited) {
            $mcpErr = if (Test-Path $McpErrLog) { Get-Content $McpErrLog -Raw } else { "" }
            throw "MCP exited: $mcpErr"
        }
        if (Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8765 -State Listen -ErrorAction SilentlyContinue) { break }
    }

    banner "Starting Cloudflare tunnel"
    Write-Host "Tunnel: $($c.tunnelName) -> $($c.tunnelDomain) -> 127.0.0.1:8765"
    if ($c.proxyAddress) { Write-Host "Proxy: $($c.proxyAddress)" -ForegroundColor Yellow }
    $tArgs = @("tunnel","run","--credentials-file",$creds,"--protocol","http2","--url","http://127.0.0.1:8765",$c.tunnelName)
    $tun = Start-Process -FilePath $CfExe -ArgumentList $tArgs -WorkingDirectory $RootDir -PassThru -RedirectStandardOutput $CfOutLog -RedirectStandardError $CfErrLog -WindowStyle Hidden

    Write-Host "Waiting for tunnel connection..." -ForegroundColor Yellow
    $tunOk = $false
    for ($i=0; $i -lt 180; $i++) {
        Start-Sleep -Milliseconds 1000
        if ($tun.HasExited) {
            $cfErr = if (Test-Path $CfErrLog) { Get-Content $CfErrLog -Raw } else { "" }
            throw "cloudflared exited: $cfErr"
        }
        if ((Test-Path $CfErrLog) -and ((Get-Content $CfErrLog -Raw) -match "Registered tunnel connection")) { $tunOk=$true; break }
    }
    if (-not $tunOk) { throw "Tunnel connection timeout" }

    save ([PSCustomObject]@{
        remoteUrl="$base/mcp"; localUrl="http://127.0.0.1:8765/mcp"; authMode="oauth"
        oauthPassword=$oauthPw; oauthClientId=$oauthCli.clientId; oauthClientSecret=$oauthCli.clientSecret
        mcpPid=$mcp.Id; tunnelPid=$tun.Id; port=8765; toolProfile=$c.toolProfile; workspace=$ws; startedAt=(Get-Date).ToString("o")
    })

    banner "Verifying remote endpoint"
    $null = Test-OAuth $base $c.proxyAddress $c.proxyUsername $c.proxyPassword
    Show-Info
    banner "Done"
    Write-Host "OAuth tokens persisted across restarts." -ForegroundColor Green
}

# ===== STOP =====
function Stop-Tunnel {
    param([switch]$Quiet)
    $st = state
    if (-not $st) { killAll @(); if (-not $Quiet) { Write-Host "No running service." }; return }
    killTree $st.tunnelPid; killTree $st.mcpPid; killAll @($st.tunnelPid, $st.mcpPid)
    Remove-Item $StateFile -ErrorAction SilentlyContinue
    Remove-Item $WdFlag -ErrorAction SilentlyContinue
    if (-not $Quiet) { Write-Host "Service stopped." -ForegroundColor Green }
}

# ===== SHOW =====
function Show-Info {
    $st = state; $c = try { cfg } catch { $null }
    $domain = if ($c -and $c.tunnelDomain) { $c.tunnelDomain } else { "(not configured)" }
    $pw = if (Test-Path $OAuthPwFile) { (Get-Content $OAuthPwFile -Raw).Trim() } else { $null }
    $cli = try { oauthClient } catch { $null }
    Write-Host ""
    Write-Host "==== Connection Info ====" -ForegroundColor Cyan
    Write-Host "  Domain:       $domain"
    if ($c -and $c.tunnelId) { Write-Host "  Remote URL:   https://$domain/mcp" }
    Write-Host ""
    if ($st) {
        $ma = alive $st.mcpPid $McpExe; $ta = alive $st.tunnelPid $CfExe
        if ($ma) { $mcpStatus = "[PID $($st.mcpPid)] running" } else { $mcpStatus = "[PID $($st.mcpPid)] stopped" }
        if ($ta) { $tunStatus = "[PID $($st.tunnelPid)] running" } else { $tunStatus = "[PID $($st.tunnelPid)] stopped" }
        Write-Host "  MCP:         $mcpStatus"
        Write-Host "  Tunnel:      $tunStatus"
        Write-Host ""
    }
    Write-Host "==== OAuth Credentials ====" -ForegroundColor Cyan
    Write-Host "  Authorize:    https://$domain/oauth/authorize"
    if ($pw)  { Write-Host "  Password:     $pw" }
    if ($cli) { Write-Host "  Client ID:    $($cli.clientId)"; Write-Host "  Client Secret: $($cli.clientSecret)" }
    Write-Host "  Data dir:     $DataDir"
}

function Test-OAuth($base,$pxy,$pxyU,$pxyP) {
    $url = "$base/.well-known/oauth-authorization-server"
    Write-Host "Checking OAuth: $url" -ForegroundColor Yellow
    for ($i=0; $i -lt 15; $i++) {
        try {
            $p = @{ Method="GET"; Uri=$url; UseBasicParsing=$true; TimeoutSec=10 }
            if ($pxy) {
                $u=[System.Uri]$pxy
                if ($pxyU) {
                    $proxyUrl = "http://${pxyU}:${pxyP}@$($u.Host):$($u.Port)"
                } else {
                    $proxyUrl = "http://$($u.Host):$($u.Port)"
                }
                $p["Proxy"] = $proxyUrl
            }
            if ((Invoke-WebRequest @p).StatusCode -eq 200) { Write-Host "OAuth OK!" -ForegroundColor Green; return $true }
        } catch { Start-Sleep -Seconds 2 }
    }
    Write-Host "OAuth unresponsive (network limited, service OK)" -ForegroundColor Yellow; return $false
}

# ===== WATCHDOG =====
function Watchdog-Loop {
    d $DataDir
    if (-not (Test-Path $WdFlag)) { Set-Content $WdFlag -Value "1" -Encoding ASCII }
    $log = Join-Path $DataDir "watchdog.log"
    "$(now) Watchdog started" | Out-File $log -Encoding UTF8 -Append
    while (Test-Path $WdFlag) {
        Start-Sleep -Seconds 30
        if (-not (Test-Path $WdFlag)) { break }
        $st = state; if (-not $st) { continue }
        $ma = alive $st.mcpPid $McpExe; $ta = alive $st.tunnelPid $CfExe
        if ($ma -and $ta) { continue }
        if (-not $ma) { "$(now) MCP dead, restarting..." | Out-File $log -Encoding UTF8 -Append }
        if (-not $ta) { "$(now) Tunnel dead, restarting..." | Out-File $log -Encoding UTF8 -Append }
        try {
            killAll @()
            Remove-Item $StateFile -ErrorAction SilentlyContinue
            $c = cfg
            if (-not $c.tunnelDomain -or -not $c.tunnelId) { "$(now) Config incomplete" | Out-File $log -Encoding UTF8 -Append; continue }
            $creds = Join-Path $env:USERPROFILE ".cloudflared\$($c.tunnelId).json"
            if (-not (Test-Path $creds)) { $f=Get-ChildItem "$env:USERPROFILE\.cloudflared\*.json" -ErrorAction SilentlyContinue|Select-Object -First 1; if($f){$creds=$f.FullName}else{continue} }
            Remove-Item $McpOutLog,$McpErrLog,$CfOutLog,$CfErrLog -ErrorAction SilentlyContinue
            $tokenSec=tokenSecret; $oauthPw=secret; $oauthCli=oauthClient
            Set-Content $OAuthPwFile -Value $oauthPw -Encoding ASCII
            $base = "https://$($c.tunnelDomain)"
            $env:CODING_TOOLS_MCP_SERVER_URL=$base; $env:CODING_TOOLS_MCP_OAUTH_PASSWORD=$oauthPw
            $env:CODING_TOOLS_MCP_OAUTH_CLIENT_ID=$oauthCli.clientId; $env:CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET=$oauthCli.clientSecret
            $env:CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET=$tokenSec; $env:CODING_TOOLS_MCP_TOOL_PROFILE=$c.toolProfile
            if ($c.proxyAddress) { $env:HTTPS_PROXY=$c.proxyAddress; $env:HTTP_PROXY=$c.proxyAddress; if($c.proxyUsername){$u=[System.Uri]$c.proxyAddress;$env:HTTPS_PROXY="http://$($c.proxyUsername):$($c.proxyPassword)@$($u.Host):$($u.Port)";$env:HTTP_PROXY=$env:HTTPS_PROXY} }
            if ($c.workspace) { $ws=$c.workspace } else { $ws=$RootDir }
            $mcp=Start-Process -FilePath $McpExe -ArgumentList @("--workspace",$ws,"--host","127.0.0.1","--port","8765","--tool-profile",$c.toolProfile,"--oauth-mode","--allow-network") -WorkingDirectory $RootDir -PassThru -RedirectStandardOutput $McpOutLog -RedirectStandardError $McpErrLog -WindowStyle Hidden
            for($i=0;$i -lt 30;$i++){Start-Sleep -Milliseconds 500;if($mcp.HasExited){throw "MCP exit"};if(Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8765 -State Listen -ErrorAction SilentlyContinue){break}}
            $tun=Start-Process -FilePath $CfExe -ArgumentList @("tunnel","run","--credentials-file",$creds,"--protocol","http2","--url","http://127.0.0.1:8765",$c.tunnelName) -WorkingDirectory $RootDir -PassThru -RedirectStandardOutput $CfOutLog -RedirectStandardError $CfErrLog -WindowStyle Hidden
            for($i=0;$i -lt 120;$i++){Start-Sleep -Milliseconds 1000;if($tun.HasExited){throw "tunnel exit"};if((Test-Path $CfErrLog)-and((Get-Content $CfErrLog -Raw)-match"Registered tunnel connection")){break}}
            save ([PSCustomObject]@{remoteUrl="$base/mcp";localUrl="http://127.0.0.1:8765/mcp";authMode="oauth";oauthPassword=$oauthPw;oauthClientId=$oauthCli.clientId;oauthClientSecret=$oauthCli.clientSecret;mcpPid=$mcp.Id;tunnelPid=$tun.Id;port=8765;toolProfile=$c.toolProfile;workspace=$ws;startedAt=(Get-Date).ToString("o")})
            "$(now) Auto-restarted OK (MCP=$($mcp.Id),tunnel=$($tun.Id))" | Out-File $log -Encoding UTF8 -Append
        } catch { "$(now) Restart FAILED: $_" | Out-File $log -Encoding UTF8 -Append }
    }
    "$(now) Watchdog stopped" | Out-File $log -Encoding UTF8 -Append
}

function now { (Get-Date).ToString("yyyy-MM-dd HH:mm:ss") }

switch ($Action) {
    "start"    { Start-Tunnel }
    "stop"     { Stop-Tunnel }
    "show"     { Show-Info }
    "watchdog" { Watchdog-Loop }
    default    { Write-Host "Usage: .\tunnel.ps1 [start|stop|show|watchdog]"; exit 1 }
}
