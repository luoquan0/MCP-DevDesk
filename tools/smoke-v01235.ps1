param(
    [string]$ExePath = "",
    [int]$Port = 18768
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not $ExePath) { $ExePath = Join-Path $Root "dist\mcp-core-amd64.exe" }
if (-not (Test-Path -LiteralPath $ExePath)) { throw "Go core executable not found: $ExePath" }

$TempRoot = Join-Path $env:TEMP ("mcp-devdesk-v01235-smoke-" + [Guid]::NewGuid().ToString("N"))
$Workspace = Join-Path $TempRoot "workspace"
$DataDir = Join-Path $TempRoot "data"
New-Item -ItemType Directory -Force -Path $Workspace, $DataDir | Out-Null
Set-Content -LiteralPath (Join-Path $Workspace "go.mod") -Encoding UTF8 -Value "module example.test/v01235`n`ngo 1.23`n"
Set-Content -LiteralPath (Join-Path $Workspace "sample.go") -Encoding UTF8 -Value @'
package sample

type Widget struct{}

func BuildWidget() *Widget { return &Widget{} }

func UseWidget() *Widget { return BuildWidget() }
'@

$BaseUrl = "http://127.0.0.1:$Port"
$process = $null
$sessionId = ""
$rpcID = 0

function Send-Rpc {
    param([hashtable]$Payload, [string]$Session = "")
    $headers = @{
        Accept = "application/json, text/event-stream"
        "MCP-Protocol-Version" = "2025-06-18"
    }
    if ($Session) { $headers["Mcp-Session-Id"] = $Session }
    $json = $Payload | ConvertTo-Json -Depth 12 -Compress
    return Invoke-WebRequest -Method Post -Uri "$BaseUrl/mcp" -Headers $headers -ContentType "application/json" -Body $json -UseBasicParsing
}

function Call-Tool {
    param([string]$Name, [hashtable]$Arguments = @{})
    $script:rpcID++
    $response = Send-Rpc -Session $script:sessionId -Payload @{
        jsonrpc = "2.0"
        id = $script:rpcID
        method = "tools/call"
        params = @{ name = $Name; arguments = $Arguments }
    }
    if ($response.StatusCode -ne 200) { throw "$Name returned HTTP $($response.StatusCode): $($response.Content)" }
    $parsed = $response.Content | ConvertFrom-Json
    if ($parsed.result.isError) { throw "$Name returned MCP error: $($response.Content)" }
    return $parsed.result.structuredContent
}

try {
    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $ExePath
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.ArgumentList.Add("--workspace")
    $startInfo.ArgumentList.Add($Workspace)
    $startInfo.ArgumentList.Add("--host")
    $startInfo.ArgumentList.Add("127.0.0.1")
    $startInfo.ArgumentList.Add("--port")
    $startInfo.ArgumentList.Add([string]$Port)
    $startInfo.ArgumentList.Add("--permission-mode")
    $startInfo.ArgumentList.Add("trusted")
    $startInfo.ArgumentList.Add("--tool-profile")
    $startInfo.ArgumentList.Add("full")
    $startInfo.ArgumentList.Add("--data-dir")
    $startInfo.ArgumentList.Add($DataDir)

    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    if (-not $process.Start()) { throw "Failed to start compiled Go MCP core" }

    $healthy = $false
    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        Start-Sleep -Milliseconds 100
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$BaseUrl/healthz" -TimeoutSec 2
            if ($health.ok) { $healthy = $true; break }
        } catch {}
        if ($process.HasExited) { break }
    }
    if (-not $healthy) {
        $stderr = ""
        if ($process.HasExited) { $stderr = $process.StandardError.ReadToEnd() }
        throw "Compiled Go MCP core did not become healthy. $stderr"
    }

    $initialize = Send-Rpc -Payload @{
        jsonrpc = "2.0"
        id = 1
        method = "initialize"
        params = @{
            protocolVersion = "2025-06-18"
            capabilities = @{}
            clientInfo = @{ name = "v01235-smoke"; version = "1" }
        }
    }
    if ($initialize.StatusCode -ne 200) { throw "initialize failed: $($initialize.Content)" }
    $sessionValues = $initialize.Headers["Mcp-Session-Id"]
    $sessionId = [string]::Join("", $sessionValues)
    if (-not $sessionId) { throw "initialize returned no MCP session ID" }

    $notification = Send-Rpc -Session $sessionId -Payload @{ jsonrpc = "2.0"; method = "notifications/initialized" }
    if ($notification.StatusCode -ne 202) { throw "initialized notification failed: $($notification.StatusCode)" }

    $rpcID++
    $listedResponse = Send-Rpc -Session $sessionId -Payload @{ jsonrpc = "2.0"; id = $rpcID; method = "tools/list"; params = @{} }
    $listed = ($listedResponse.Content | ConvertFrom-Json).result.tools
    foreach ($required in @(
        "validate_project", "checks_run",
        "exec_command", "read_output", "write_stdin", "kill_session",
        "document_symbols", "workspace_symbols", "find_definition", "find_references"
    )) {
        if (-not ($listed | Where-Object { $_.name -eq $required } | Select-Object -First 1)) {
            throw "Compiled core is missing required v0.12.35 tool: $required"
        }
    }
    if ($listed | Where-Object { $_.name -like "task_*" }) {
        throw "Compiled stable core unexpectedly exposes Agent Task tools"
    }

    $document = Call-Tool -Name "document_symbols" -Arguments @{ path = "sample.go" }
    if ($document.engine -ne "lexical-fallback" -or [int]$document.count -lt 3) { throw "document_symbols smoke failed" }
    $workspaceSymbols = Call-Tool -Name "workspace_symbols" -Arguments @{ query = "Widget" }
    if ([int]$workspaceSymbols.count -lt 2) { throw "workspace_symbols smoke failed" }
    $definition = Call-Tool -Name "find_definition" -Arguments @{ symbol = "BuildWidget" }
    if ([int]$definition.count -ne 1) { throw "find_definition smoke failed" }
    $references = Call-Tool -Name "find_references" -Arguments @{ symbol = "BuildWidget" }
    if ([int]$references.count -lt 2) { throw "find_references smoke failed" }

    $check = Call-Tool -Name "checks_run" -Arguments @{ type = "test"; waitMillis = 30000 }
    if (-not $check.structured -or $check.detectedRuntime -ne "go" -or $check.running -or [int]$check.exitCode -ne 0) {
        throw "checks_run smoke failed: $($check | ConvertTo-Json -Depth 6 -Compress)"
    }
    $validation = Call-Tool -Name "validate_project" -Arguments @{ waitMillis = 30000 }
    if (-not $validation.structured -or [int]$validation.validationPhases -ne 3 -or $validation.running -or [int]$validation.exitCode -ne 0) {
        throw "validate_project smoke failed: $($validation | ConvertTo-Json -Depth 6 -Compress)"
    }

    $terminalScript = '$line = [Console]::In.ReadLine(); Write-Output ("echo:" + $line); Start-Sleep -Seconds 300'
    $started = Call-Tool -Name "exec_command" -Arguments @{
        command = "powershell.exe"
        args = @("-NoProfile", "-NonInteractive", "-Command", $terminalScript)
        waitMillis = 200
    }
    $commandSession = [string]$started.sessionId
    if (-not $commandSession -or -not $started.running) { throw "exec_command did not create a running session" }

    [void](Call-Tool -Name "write_stdin" -Arguments @{ sessionId = $commandSession; chars = "hello`n" })
    $echoSeen = $false
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        $out = Call-Tool -Name "read_output" -Arguments @{ sessionId = $commandSession; offset = 0 }
        if ([string]$out.output -like "*echo:hello*") { $echoSeen = $true; break }
        Start-Sleep -Milliseconds 100
    }
    if (-not $echoSeen) { throw "read_output/write_stdin smoke did not observe echoed input" }

    $killed = Call-Tool -Name "kill_session" -Arguments @{ sessionId = $commandSession; wait_ms = 5000 }
    if (-not $killed.terminated -or -not $killed.completed) { throw "kill_session did not stop the real process" }
    $afterKill = Call-Tool -Name "read_output" -Arguments @{ sessionId = $commandSession; offset = 0 }
    if ($afterKill.running) { throw "read_output.running remained true after kill_session" }
    $secondKill = Call-Tool -Name "kill_session" -Arguments @{ sessionId = $commandSession }
    if ($secondKill.terminated -or -not $secondKill.completed) { throw "second kill_session was not idempotent" }

    Write-Host "v0.12.35 compiled mcp-core JSON-RPC smoke passed." -ForegroundColor Green
} finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        $process.WaitForExit(5000) | Out-Null
    }
    Remove-Item -LiteralPath $TempRoot -Recurse -Force -ErrorAction SilentlyContinue
}
