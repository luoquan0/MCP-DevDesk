param(
    [string]$CorePath = ".\\dist\\mcp-core-amd64.exe"
)

$ErrorActionPreference = "Stop"
$CorePath = (Resolve-Path -LiteralPath $CorePath).Path

function Get-FreePort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try { return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port }
    finally { $listener.Stop() }
}

function Send-McpRequest {
    param(
        [System.Net.Http.HttpClient]$Client,
        [string]$Uri,
        [string]$Session,
        [hashtable]$Payload
    )
    $json = $Payload | ConvertTo-Json -Depth 10 -Compress
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, $Uri)
    $request.Headers.TryAddWithoutValidation("Accept", "application/json") | Out-Null
    if ($Session) {
        $request.Headers.TryAddWithoutValidation("Mcp-Session-Id", $Session) | Out-Null
        $request.Headers.TryAddWithoutValidation("MCP-Protocol-Version", "2025-06-18") | Out-Null
    }
    $request.Content = [System.Net.Http.StringContent]::new($json, [System.Text.Encoding]::UTF8, "application/json")
    try {
        $response = $Client.SendAsync($request).GetAwaiter().GetResult()
        $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        if (-not $response.IsSuccessStatusCode) {
            throw "MCP HTTP $([int]$response.StatusCode): $body"
        }
        return [pscustomobject]@{ Response = $response; Body = ($body | ConvertFrom-Json) }
    } finally {
        $request.Dispose()
    }
}

function Test-Mode {
    param(
        [string]$Mode,
        [string]$WindowId,
        [int]$ExpectedCount,
        [string[]]$Required,
        [string[]]$Forbidden
    )
    $root = Join-Path $env:RUNNER_TEMP ("mcp-core-catalog-" + $Mode + "-" + [guid]::NewGuid().ToString("N"))
    $workspace = Join-Path $root "workspace"
    $dataDir = Join-Path $root "data"
    New-Item -ItemType Directory -Force -Path $workspace, $dataDir | Out-Null
    $config = Join-Path $root "config.json"
    @{
        screenCaptureEnabled = $true
        screenCaptureMode = $Mode
        screenCaptureWindowId = $WindowId
        screenCaptureWindowProcessId = $(if ($WindowId) { 1234 } else { 0 })
    } | ConvertTo-Json -Compress | Set-Content -LiteralPath $config -Encoding UTF8
    $port = Get-FreePort
    $args = @(
        "--workspace", $workspace,
        "--host", "127.0.0.1",
        "--port", "$port",
        "--permission-mode", "dangerous",
        "--tool-profile", "full",
        "--file-scope", "workspace",
        "--data-dir", $dataDir,
        "--enable-screen-capture",
        "--screen-vision-config", $config
    )
    $proc = Start-Process -FilePath $CorePath -ArgumentList $args -PassThru -WindowStyle Hidden
    $client = [System.Net.Http.HttpClient]::new()
    try {
        $uri = "http://127.0.0.1:$port/mcp"
        $session = $null
        for ($i = 0; $i -lt 60; $i++) {
            try {
                $init = Send-McpRequest -Client $client -Uri $uri -Session "" -Payload @{
                    jsonrpc = "2.0"; id = 1; method = "initialize"; params = @{
                        protocolVersion = "2025-06-18"; capabilities = @{}; clientInfo = @{ name = "catalog-smoke"; version = "1" }
                    }
                }
                $values = [System.Collections.Generic.IEnumerable[string]]$null
                if ($init.Response.Headers.TryGetValues("Mcp-Session-Id", [ref]$values)) {
                    $session = [string]($values | Select-Object -First 1)
                    if ($session) { break }
                }
            } catch {
                Start-Sleep -Milliseconds 200
            }
        }
        if (-not $session) { throw "[$Mode] core did not initialize" }
        $serverName = [string]$init.Body.result.serverInfo.name
        if ($serverName -ne "mcp-devdesk-go-core-v013-catalog2") {
            throw "[$Mode] unexpected server identity: $serverName"
        }

        $listed = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 2; method = "tools/list"; params = @{} }
        $names = @($listed.Body.result.tools | ForEach-Object { [string]$_.name } | Sort-Object)
        $info = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 3; method = "tools/call"; params = @{ name = "server_info"; arguments = @{} } }
        $toolCount = [int]$info.Body.result.structuredContent.toolCount
        $environment = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 4; method = "tools/call"; params = @{ name = "check_exec_environment"; arguments = @{} } }
        $environmentData = $environment.Body.result.structuredContent
        if ([string]$environmentData.toolCatalogGeneration -ne "v013-catalog2") { throw "[$Mode] catalog generation fallback missing" }
        if (-not [bool]$environmentData.screenCaptureProbeAdvertised) { throw "[$Mode] fallback says probe is not advertised" }
        if ([bool]$environmentData.legacyListSymbolsAdvertised) { throw "[$Mode] fallback says list_symbols is still advertised" }
        Write-Host "mode=$Mode toolsListCount=$($names.Count) serverToolCount=$toolCount catalog=$serverName"
        if ($names.Count -ne $ExpectedCount -or $toolCount -ne $ExpectedCount) {
            throw "[$Mode] catalog count $($names.Count)/$toolCount, want $ExpectedCount"
        }
        if ($names -contains "list_symbols") { throw "[$Mode] list_symbols must remain a compatibility call alias, not an advertised tool" }
        foreach ($name in @("screen_capture_probe", "ui_automation_tree") + $Required) {
            if ($names -notcontains $name) { throw "[$Mode] required tool missing: $name" }
        }
        foreach ($name in $Forbidden) {
            if ($names -contains $name) { throw "[$Mode] forbidden tool advertised: $name" }
        }
    } finally {
        $client.Dispose()
        if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
        Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Test-Mode -Mode "active" -WindowId "" -ExpectedCount 52 `
    -Required @("screen_get_active_window", "screen_capture_active_window") `
    -Forbidden @("screen_list_windows", "screen_capture_window", "screen_capture_desktop")
Test-Mode -Mode "window" -WindowId "0x10" -ExpectedCount 52 `
    -Required @("screen_list_windows", "screen_capture_window") `
    -Forbidden @("screen_get_active_window", "screen_capture_active_window", "screen_capture_desktop")
Test-Mode -Mode "desktop" -WindowId "" -ExpectedCount 55 `
    -Required @("screen_list_windows", "screen_get_active_window", "screen_capture_window", "screen_capture_active_window", "screen_capture_desktop") `
    -Forbidden @()

Write-Host "Built mcp-core Screen Vision catalog contract passed."
