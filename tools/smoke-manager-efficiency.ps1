param(
    [string]$ManagerPath = "",
    [string]$CorePath = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not $ManagerPath) { $ManagerPath = Join-Path $Root "dist\MCP-DevDesk-amd64.exe" }
if (-not $CorePath) { $CorePath = Join-Path $Root "dist\mcp-core-amd64.exe" }
if (-not (Test-Path -LiteralPath $ManagerPath)) { throw "Manager executable not found: $ManagerPath" }
if (-not (Test-Path -LiteralPath $CorePath)) { throw "Go core executable not found: $CorePath" }

$TestRoot = Join-Path $env:TEMP ("mcp-devdesk-manager-smoke-" + [Guid]::NewGuid().ToString("N"))
$Manager = $null

$existingManager = Get-Process -Name "MCP-DevDesk-amd64" -ErrorAction SilentlyContinue
if ($existingManager) {
    throw "Refusing to run isolated manager smoke test while MCP DevDesk is already running; stop the real manager first to avoid touching its local API."
}

function Send-Json {
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Uri,
        [object]$Body = $null
    )
    $parameters = @{
        Method = $Method
        Uri = $Uri
        UseBasicParsing = $true
        TimeoutSec = 15
    }
    if ($null -ne $Body) {
        $parameters.ContentType = "application/json"
        $parameters.Body = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }
    return Invoke-WebRequest @parameters
}

try {
    New-Item -ItemType Directory -Force -Path $TestRoot | Out-Null
    Copy-Item -LiteralPath $ManagerPath -Destination (Join-Path $TestRoot "MCP-DevDesk-amd64.exe")
    Copy-Item -LiteralPath $CorePath -Destination (Join-Path $TestRoot "mcp-core.exe")
    Copy-Item -LiteralPath (Join-Path $Root "coding-tools-mcp.exe") -Destination (Join-Path $TestRoot "coding-tools-mcp.exe")
    Copy-Item -LiteralPath (Join-Path $Root "cloudflared.exe") -Destination (Join-Path $TestRoot "cloudflared.exe")

    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = Join-Path $TestRoot "MCP-DevDesk-amd64.exe"
    $startInfo.Arguments = "--background"
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.EnvironmentVariables["MCP_DEVDESK_ROOT"] = $TestRoot
    $Manager = [System.Diagnostics.Process]::Start($startInfo)

    $BaseUrl = "http://127.0.0.1:17860"
    $ready = $false
    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        Start-Sleep -Milliseconds 100
        try {
            $health = Send-Json -Method "GET" -Uri "$BaseUrl/api/health"
            $parsed = $health.Content | ConvertFrom-Json
            if ($health.StatusCode -eq 200 -and $parsed.ok -and $parsed.version -eq "0.8.5") {
                $ready = $true
                break
            }
        } catch {}
        if ($Manager.HasExited) { break }
    }
    if (-not $ready) { throw "Manager did not become healthy" }

    $promptSettings = Send-Json -Method "PUT" -Uri "$BaseUrl/api/projects/prompt-settings" -Body @{ globalPrompt = "SMOKE_GLOBAL_PROMPT: finish the complete task before replying." }
    if ($promptSettings.StatusCode -ne 200) { throw "Global project prompt endpoint failed" }
    $projects = (Send-Json -Method "GET" -Uri "$BaseUrl/api/projects").Content | ConvertFrom-Json
    $project = @($projects)[0]
    if (-not $project.id) { throw "Manager returned no project for prompt smoke test" }
    $projectPrompt = Send-Json -Method "PATCH" -Uri "$BaseUrl/api/projects/$($project.id)" -Body @{ prompt = "SMOKE_PROJECT_PROMPT: run validation before reporting completion." }
    if ($projectPrompt.StatusCode -ne 200) { throw "Project prompt endpoint failed" }

    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $mcpPort = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
    $listener.Stop()
    $configUpdate = Send-Json -Method "PUT" -Uri "$BaseUrl/api/config" -Body @{ coreMode = "go"; mcpPort = $mcpPort }
    if ($configUpdate.StatusCode -ne 200) { throw "Failed to select Go core for prompt smoke test" }
    $startMcp = Send-Json -Method "POST" -Uri "$BaseUrl/api/services/start"
    if ($startMcp.StatusCode -ne 200) { throw "Failed to start Go core for prompt smoke test" }
    $instructionsPath = Join-Path $TestRoot "data\devdesk\project-instructions.md"
    if (-not (Test-Path -LiteralPath $instructionsPath)) { throw "Managed project instructions file was not generated" }
    $instructions = Get-Content -LiteralPath $instructionsPath -Raw
    if ($instructions -notlike "*SMOKE_GLOBAL_PROMPT*" -or $instructions -notlike "*SMOKE_PROJECT_PROMPT*") {
        throw "Managed project instructions did not compose global and project prompts"
    }
    [void](Send-Json -Method "POST" -Uri "$BaseUrl/api/services/stop")

    $primary = (Send-Json -Method "GET" -Uri "$BaseUrl/api/instances/primary").Content | ConvertFrom-Json
    $targetCore = if ($primary.coreMode -eq "go") { "legacy" } else { "go" }
    $cloneResponse = Send-Json -Method "POST" -Uri "$BaseUrl/api/instances/primary/clone" -Body @{ coreMode = $targetCore }
    if ($cloneResponse.StatusCode -ne 201) { throw "Clone endpoint returned $($cloneResponse.StatusCode)" }
    $clone = $cloneResponse.Content | ConvertFrom-Json
    if ($clone.primary -or $clone.coreMode -ne $targetCore -or $clone.domain -or $clone.mcpPort -eq $primary.mcpPort) {
        throw "Clone endpoint returned an invalid instance"
    }

    $instances = (Send-Json -Method "GET" -Uri "$BaseUrl/api/instances").Content | ConvertFrom-Json
    if (@($instances).Count -ne 2) { throw "Expected two isolated instances after cloning" }

    $diagnostics = Send-Json -Method "GET" -Uri "$BaseUrl/api/diagnostics/export"
    if ($diagnostics.StatusCode -ne 200 -or $diagnostics.Headers["Content-Disposition"] -notlike "*attachment*") {
        throw "Diagnostics export headers are invalid"
    }
    $report = $diagnostics.Content | ConvertFrom-Json
    if ($report.diagnostics.version -ne "0.8.5" -or -not $report.instances) {
        throw "Diagnostics export content is invalid"
    }

    Write-Host "Manager efficiency smoke test passed: $ManagerPath" -ForegroundColor Green
} finally {
    if ($Manager -and -not $Manager.HasExited) {
        Stop-Process -Id $Manager.Id -Force -ErrorAction SilentlyContinue
        $Manager.WaitForExit(5000) | Out-Null
    }
    Start-Sleep -Milliseconds 300
    Get-CimInstance Win32_Process | Where-Object {
        $_.ExecutablePath -like "$TestRoot*"
    } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $TestRoot -Recurse -Force -ErrorAction SilentlyContinue
}
