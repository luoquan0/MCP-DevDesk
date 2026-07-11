param(
    [string]$ExePath = "",
    [string]$Workspace = "",
    [int]$Port = 18766
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not $ExePath) { $ExePath = Join-Path $Root "dist\mcp-core-amd64.exe" }
if (-not $Workspace) { $Workspace = $Root }
if (-not (Test-Path -LiteralPath $ExePath)) { throw "Go core executable not found: $ExePath" }

$DataDir = Join-Path $env:TEMP ("mcp-devdesk-smoke-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$BaseUrl = "http://127.0.0.1:$Port"

$startInfo = [System.Diagnostics.ProcessStartInfo]::new()
$startInfo.FileName = $ExePath
$startInfo.UseShellExecute = $false
$startInfo.CreateNoWindow = $true
$startInfo.RedirectStandardOutput = $true
$startInfo.RedirectStandardError = $true
$quotedWorkspace = '"' + ($Workspace -replace '"', '\"') + '"'
$quotedDataDir = '"' + ($DataDir -replace '"', '\"') + '"'
$startInfo.Arguments = "--workspace $quotedWorkspace --host 127.0.0.1 --port $Port --permission-mode safe --tool-profile full --oauth-mode --data-dir $quotedDataDir --server-url $BaseUrl"
$startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_PASSWORD"] = "smoke-owner-password"
$startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_CLIENT_ID"] = "smoke-client"
$startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET"] = "smoke-client-secret-value"
$startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET"] = (("ab" * 32) -join "")
$startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_REDIRECT_URIS"] = "http://127.0.0.1:43210/callback"

$process = [System.Diagnostics.Process]::new()
$process.StartInfo = $startInfo
if (-not $process.Start()) { throw "Failed to start Go MCP core" }

try {
    $healthy = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        Start-Sleep -Milliseconds 100
        try {
            $health = Invoke-RestMethod -Uri "$BaseUrl/healthz" -Method Get -TimeoutSec 2
            if ($health.ok) { $healthy = $true; break }
        } catch {}
        if ($process.HasExited) { break }
    }
    if (-not $healthy) {
        $stderr = $process.StandardError.ReadToEnd()
        throw "Go MCP core did not become healthy. stderr: $stderr"
    }

    $resource = Invoke-RestMethod -Uri "$BaseUrl/.well-known/oauth-protected-resource/mcp" -Method Get -TimeoutSec 5
    if ($resource.resource -ne "$BaseUrl/mcp") { throw "Unexpected protected resource metadata" }

    $authorization = Invoke-RestMethod -Uri "$BaseUrl/.well-known/oauth-authorization-server" -Method Get -TimeoutSec 5
    if ($authorization.authorization_endpoint -ne "$BaseUrl/oauth/authorize") { throw "Unexpected authorization metadata" }

    $request = [System.Net.HttpWebRequest]::Create("$BaseUrl/mcp")
    $request.Method = "POST"
    $request.ContentType = "application/json"
    $request.Accept = "application/json, text/event-stream"
    $request.Timeout = 5000
    $payload = [Text.Encoding]::UTF8.GetBytes('{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"1"}}}')
    $request.ContentLength = $payload.Length
    $stream = $request.GetRequestStream()
    $stream.Write($payload, 0, $payload.Length)
    $stream.Dispose()
    $unauthorized = $null
    try {
        $response = $request.GetResponse()
        $unauthorized = [int]$response.StatusCode
        $response.Dispose()
    } catch [System.Net.WebException] {
        if ($_.Exception.Response) { $unauthorized = [int]$_.Exception.Response.StatusCode }
    }
    if ($unauthorized -ne 401) { throw "Expected HTTP 401 from protected MCP endpoint, got $unauthorized" }

    Write-Host "Go MCP core smoke test passed: $ExePath" -ForegroundColor Green
} finally {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        $process.WaitForExit(5000) | Out-Null
    }
    Remove-Item -LiteralPath $DataDir -Recurse -Force -ErrorAction SilentlyContinue
}
