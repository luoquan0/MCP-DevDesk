<# =============================================
  Coding Tools MCP - 域名配置向导 (便携版)
  网络环境不友好时也能完成配置
============================================= #>

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ConfigFile = Join-Path $RootDir "config.json"
$CloudflaredExe = Join-Path $RootDir "cloudflared.exe"

# ===== 检查 cloudflared =====
if (-not (Test-Path $CloudflaredExe)) {
    Write-Host "[错误] cloudflared.exe 未找到!" -ForegroundColor Red
    Read-Host "按 Enter 退出"
    exit 1
}

# ===== 检查 cloudflared 登录 =====
if (-not (Test-Path "$env:USERPROFILE\.cloudflared\cert.pem")) {
    Write-Host "[提示] cloudflared 未登录, 将打开浏览器..." -ForegroundColor Yellow
    Write-Host ""
    & $CloudflaredExe tunnel login
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[错误] 登录失败!" -ForegroundColor Red
        Read-Host "按 Enter 退出"
        exit 1
    }
    Write-Host "[成功] 登录完成!" -ForegroundColor Green
    Write-Host ""
}

# ===== 读取已有配置 =====
$existingConfig = $null
if (Test-Path $ConfigFile) {
    try { $existingConfig = Get-Content $ConfigFile -Raw -Encoding UTF8 | ConvertFrom-Json } catch {}
}

if ($existingConfig -and $existingConfig.tunnelDomain) {
    Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host "  检测到已有配置:" -ForegroundColor White
    Write-Host "    域名:      $($existingConfig.tunnelDomain)" -ForegroundColor Green
    Write-Host "    隧道名称:  $($existingConfig.tunnelName)" -ForegroundColor Green
    if ($existingConfig.workspace) { Write-Host "    工作区:    $($existingConfig.workspace)" -ForegroundColor Green }
    Write-Host "    工具配置:  $($existingConfig.toolProfile)" -ForegroundColor Green
    Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""
    $useExisting = Read-Host "沿用现有配置? (直接回车=是, 输入 N=重新配置)"
    if ($useExisting -ne "N" -and $useExisting -ne "n") {
        Write-Host "配置未改变。" -ForegroundColor Green
        Read-Host "按 Enter 退出"
        return
    }
    Write-Host ""
}

# ===== 第一步: 域名 =====
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  第一步: 配置域名" -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "  请输入要绑定的完整子域名。" -ForegroundColor Yellow
Write-Host "  要求: DNS 托管在 Cloudflare, SSL/TLS 模式 '完全'" -ForegroundColor Yellow
Write-Host "  示例: mcp.yourdomain.com" -ForegroundColor DarkGray
Write-Host ""

$domain = Read-Host "域名"
if ([string]::IsNullOrWhiteSpace($domain)) { Write-Host "[错误] 域名不能为空!" -ForegroundColor Red; Read-Host "按 Enter 退出"; exit 1 }
if ($domain -notmatch "\.") { Write-Host "[错误] 域名格式无效!" -ForegroundColor Red; Read-Host "按 Enter 退出"; exit 1 }

# ===== 第二步: 隧道名称 =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  第二步: 隧道名称" -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
$tunnelName = Read-Host "隧道名称 (直接回车默认 mcp-coding-tools)"
if ([string]::IsNullOrWhiteSpace($tunnelName)) { $tunnelName = "mcp-coding-tools" }

# ===== 第三步: 工具配置 =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  第三步: 工具配置" -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "  1) read-only - 只读 (安全, 适合远程)" -ForegroundColor Yellow
Write-Host "  2) full      - 完整 (可读写, 仅限信任)" -ForegroundColor Yellow
Write-Host ""
$profileChoice = Read-Host "选择 [1/2, 直接回车=2]"
$profile = if ($profileChoice -eq "1") { "read-only" } else { "full" }

# ===== 第四步: 工作区 =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  第四步: 工作区目录" -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "  MCP 服务将只能访问此目录下的文件。" -ForegroundColor Yellow
Write-Host "  默认: 本脚本所在目录" -ForegroundColor DarkGray
Write-Host ""
$workspace = Read-Host "工作区路径 (直接回车使用默认)"
if ([string]::IsNullOrWhiteSpace($workspace)) { $workspace = $RootDir }

# ===== 第五步: 代理 (可选) =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  第五步: 代理配置 (可选)" -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "  如果你的网络无法直连 Cloudflare API, 可设置代理。" -ForegroundColor Yellow
Write-Host "  不需要请直接回车跳过。" -ForegroundColor Yellow
Write-Host "  示例: http://192.168.2.2:27000" -ForegroundColor DarkGray
Write-Host ""
$proxyAddr = Read-Host "代理地址 (直接回车跳过)"
$proxyUser = ""; $proxyPass = ""
if (-not [string]::IsNullOrWhiteSpace($proxyAddr)) {
    $needAuth = Read-Host "需要认证? (Y=是, 直接回车=否)"
    if ($needAuth -eq "Y" -or $needAuth -eq "y") {
        $proxyUser = Read-Host "  用户名"
        $proxyPass = Read-Host "  密码"
    }
    Write-Host "[信息] 代理已配置" -ForegroundColor Green
} else {
    Write-Host "[信息] 不使用代理" -ForegroundColor Green
}

# Apply proxy env vars for cloudflared commands
if ($proxyAddr) {
    $envProxy = $proxyAddr
    if ($proxyUser) {
        $u = [System.Uri]$proxyAddr
        $envProxy = "http://${proxyUser}:${proxyPass}@$($u.Host):$($u.Port)"
    }
    $env:HTTPS_PROXY = $envProxy
    $env:HTTP_PROXY = $envProxy
    Write-Host "[代理] 已设置: $envProxy" -ForegroundColor DarkGray
}

# ===== 检查/创建隧道: 网络失败时手动输入 =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  正在配置隧道..." -ForegroundColor White
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""

$tunnelId = $null
$apiOnline = $true

# Try tunnel list
try {
    Write-Host "[1/2] 查询已有隧道..." -ForegroundColor Yellow
    $tunnelList = & $CloudflaredExe tunnel list 2>&1
    $listText = $tunnelList -join "`n"

    # Check if API call failed
    if ($listText -match "REST request failed|EOF|i/o timeout|connection refused|no such host") {
        Write-Host "[注意] 无法连接 Cloudflare API (网络不通)" -ForegroundColor Yellow
        $apiOnline = $false
    } else {
        foreach ($line in $tunnelList) {
            if ($line -match "([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})\s+$([regex]::Escape($tunnelName))") {
                $tunnelId = $matches[1]; break
            }
        }
    }
} catch {
    Write-Host "[注意] cloudflared tunnel list 失败: $_" -ForegroundColor Yellow
    $apiOnline = $false
}

if ($tunnelId) {
    # Found existing tunnel
    Write-Host "[已找到] 隧道: $tunnelName (ID: $tunnelId)" -ForegroundColor Green
    $reuse = Read-Host "复用此隧道? (直接回车=是, N=新建)"
    if ($reuse -eq "N" -or $reuse -eq "n") { $tunnelId = $null; $tunnelName = Read-Host "新隧道名称" }
}

if (-not $tunnelId -and $apiOnline) {
    # Try to create tunnel via API
    Write-Host "[2/2] 创建隧道: $tunnelName ..." -ForegroundColor Yellow
    try {
        $out = & $CloudflaredExe tunnel create $tunnelName 2>&1
        $text = $out -join "`n"
        if ($text -match "REST request failed|EOF|i/o timeout") {
            Write-Host "[失败] 无法通过 API 创建隧道 (网络不通)" -ForegroundColor Red
            $apiOnline = $false
        } elseif ($text -match "([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})") {
            $tunnelId = $matches[1]
            Write-Host "[成功] 隧道已创建: $tunnelId" -ForegroundColor Green
        } else {
            Write-Host "[失败] 创建请求未返回隧道 ID, 可能需要手动配置" -ForegroundColor Red
            $apiOnline = $false
        }
    } catch {
        Write-Host "[失败] 创建隧道异常: $_" -ForegroundColor Red
        $apiOnline = $false
    }
}

# ===== 离线降级: 手动输入隧道 ID =====
if (-not $tunnelId) {
    Write-Host ""
    Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host "  离线模式: 手动配置隧道" -ForegroundColor White
    Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  由于网络不通, 无法自动创建 Cloudflare 隧道。" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  你需要先在能访问 Cloudflare 的机器上创建隧道:" -ForegroundColor Yellow
    Write-Host "    1. 下载 cloudflared 并登录" -ForegroundColor DarkGray
    Write-Host "    2. 执行: cloudflared.exe tunnel create $tunnelName" -ForegroundColor DarkGray
    Write-Host "    3. 执行: cloudflared.exe tunnel route dns $tunnelName $domain" -ForegroundColor DarkGray
    Write-Host "    4. 把凭据文件 %USERPROFILE%\\.cloudflared\\<ID>.json 复制到本机同路径" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  然后在这里输入隧道 UUID (凭据文件名, 不含 .json):" -ForegroundColor Yellow
    Write-Host "  格式: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" -ForegroundColor DarkGray
    Write-Host ""

    do {
        $tunnelId = Read-Host "隧道 UUID (如已有, 直接输入; 无则回车跳过)"
        if ([string]::IsNullOrWhiteSpace($tunnelId)) {
            Write-Host ""
            Write-Host "[注意] 跳过隧道配置, 只保存域名和代理设置。" -ForegroundColor Yellow
            Write-Host "之后换到能访问 Cloudflare API 的网络再运行 配置.cmd 补全。" -ForegroundColor Yellow
            $confirm = Read-Host "确认跳过隧道? (Y=跳过/先保存, 其他=重新输入)"
            if ($confirm -eq "Y" -or $confirm -eq "y") { break }
        } elseif ($tunnelId -notmatch "^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$") {
            Write-Host "[错误] UUID 格式不对, 请重试" -ForegroundColor Red
            $tunnelId = ""
        }
    } while ([string]::IsNullOrWhiteSpace($tunnelId))

    if (-not $tunnelId) {
        Write-Host "[信息] 跳过隧道创建, 仅保存基础配置" -ForegroundColor Yellow
    } else {
        # Check if creds file exists
        $credsFile = Join-Path $env:USERPROFILE ".cloudflared\$tunnelId.json"
        if (-not (Test-Path $credsFile)) {
            Write-Host "[警告] 凭据文件不存在: $credsFile" -ForegroundColor Red
            Write-Host "请将该文件从创建隧道的机器复制过来, 否则无法启动。" -ForegroundColor Yellow
        } else {
            Write-Host "[已找到] 凭据文件: $credsFile" -ForegroundColor Green
        }
    }
}

# ===== DNS 路由 =====
if ($tunnelId -and $apiOnline) {
    Write-Host ""
    Write-Host "[信息] 添加 DNS 路由: $domain -> $tunnelName" -ForegroundColor Yellow
    # cloudflared writes INF logs to stderr; capture both streams, never throw on stderr text
    $routeText = ""
    $routeExit = 0
    try {
        $proc = Start-Process -FilePath $CloudflaredExe -ArgumentList @("tunnel","route","dns",$tunnelName,$domain) -PassThru -Wait -NoNewWindow -RedirectStandardOutput "$DataDir.route.stdout.tmp" -RedirectStandardError "$DataDir.route.stderr.tmp"
        $routeExit = $proc.ExitCode
        if (Test-Path "$DataDir.route.stdout.tmp") { $routeText += (Get-Content "$DataDir.route.stdout.tmp" -Raw) }
        if (Test-Path "$DataDir.route.stderr.tmp") { $routeText += (Get-Content "$DataDir.route.stderr.tmp" -Raw) }
        Remove-Item "$DataDir.route.stdout.tmp","$DataDir.route.stderr.tmp" -ErrorAction SilentlyContinue
    } catch {
        $routeText = $_.Exception.Message
    }

    Write-Host $routeText

    # Treat as failure ONLY on real API errors. "already configured" and INF logs are success.
    $apiFailed = $false
    if ($routeExit -ne 0) {
        # Non-zero exit, but check if it's actually a success message on stderr
        if ($routeText -match "already configured|Added CNAME|will route to your tunnel") {
            $apiFailed = $false
        } elseif ($routeText -match "REST request failed|EOF|i/o timeout|connection refused|no such host|Could not") {
            $apiFailed = $true
        }
    }
    # Also detect failure text even with exit 0
    if ($routeText -match "REST request failed|EOF|i/o timeout|connection refused|no such host") {
        $apiFailed = $true
    }

    if ($apiFailed) {
        Write-Host ""
        Write-Host "[注意] DNS 路由 API 不通, 请手动在 Cloudflare 控制台添加:" -ForegroundColor Red
        Write-Host "  类型: CNAME  名称: $domain 的子域名部分" -ForegroundColor Yellow
        Write-Host "  目标: $tunnelId.cfargotunnel.com" -ForegroundColor Yellow
        Write-Host "  代理: 开启 (橙色云朵)" -ForegroundColor Yellow
    } else {
        Write-Host "[成功] DNS 路由已添加!" -ForegroundColor Green
    }
}

Write-Host ""
Write-Host "[重要] 请在 Cloudflare 控制台确认:" -ForegroundColor Yellow
Write-Host "  1. DNS 记录代理状态: 橙色云朵" -ForegroundColor Yellow
Write-Host "  2. SSL/TLS 加密模式: '完全'" -ForegroundColor Yellow
if (-not $tunnelId -or -not $apiOnline) {
    Write-Host "  3. DNS CNAME: $domain 的子域名 -> <隧道UUID>.cfargotunnel.com" -ForegroundColor Yellow
}

# ===== 保存配置 =====
$config = [PSCustomObject]@{
    tunnelName    = $tunnelName
    tunnelDomain  = $domain
    tunnelId      = if ($tunnelId) { $tunnelId } else { "" }
    port          = 8765
    toolProfile   = $profile
    workspace     = $workspace
    proxyAddress  = $proxyAddr
    proxyUsername = $proxyUser
    proxyPassword = $proxyPass
}
$config | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $ConfigFile -Encoding UTF8
Write-Host ""
Write-Host "[成功] 配置已保存!" -ForegroundColor Green

# ===== 完成 =====
Write-Host ""
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  配置完成!" -ForegroundColor Green
Write-Host "════════════════════════════════════════" -ForegroundColor Cyan
Write-Host ""
Write-Host "  域名:      $domain" -ForegroundColor White
if ($tunnelId) {
    Write-Host "  隧道 UUID: $tunnelId" -ForegroundColor White
    Write-Host "  远程 URL:  https://$domain/mcp" -ForegroundColor Green
    Write-Host "  授权页面:  https://$domain/oauth/authorize" -ForegroundColor Cyan
} else {
    Write-Host "  隧道 UUID: (待补全)" -ForegroundColor Yellow
    Write-Host "  换网络后重新运行 配置.cmd 补全隧道信息。" -ForegroundColor Yellow
}
Write-Host ""
if ($tunnelId) {
    Write-Host "  回到主菜单选择 [1] 启动服务。" -ForegroundColor Yellow
}
Write-Host ""
Read-Host "按 Enter 返回"