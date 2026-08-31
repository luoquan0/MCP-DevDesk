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
        [object]$Body = $null,
        [Microsoft.PowerShell.Commands.WebRequestSession]$WebSession = $null
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
    if ($null -ne $WebSession) {
        $parameters.WebSession = $WebSession
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
            if ($health.StatusCode -eq 200 -and $parsed.ok -and $parsed.version -eq "0.12.5") {
                $ready = $true
                break
            }
        } catch {}
        if ($Manager.HasExited) { break }
    }
    if (-not $ready) { throw "Manager did not become healthy" }

    $webListener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $webListener.Start()
    $webPort = ([System.Net.IPEndPoint]$webListener.LocalEndpoint).Port
    $webListener.Stop()
    $webPassword = "SMOKE-WEB-CONTROL-123"
    $webControl = Send-Json -Method "PUT" -Uri "$BaseUrl/api/web-control" -Body @{ enabled = $true; port = $webPort; lanEnabled = $true; authEnabled = $true; password = $webPassword }
    if ($webControl.StatusCode -ne 200) { throw "Web control enable endpoint failed" }
    $webStatus = $webControl.Content | ConvertFrom-Json
    if (-not $webStatus.enabled -or -not $webStatus.running -or -not $webStatus.lanEnabled -or -not $webStatus.authEnabled -or -not $webStatus.passwordConfigured -or $webStatus.port -ne $webPort -or $webStatus.url -notlike "*:$webPort/#/") {
        throw "Web control did not report the expected running state"
    }
    $authStatus = Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/api/control/auth/status"
    $authParsed = $authStatus.Content | ConvertFrom-Json
    if ($authStatus.StatusCode -ne 200 -or -not $authParsed.required -or $authParsed.authenticated) {
        throw "Web control auth status did not require login"
    }
    $webSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $login = Send-Json -Method "POST" -Uri "http://127.0.0.1:$webPort/api/control/auth/login" -Body @{ password = $webPassword } -WebSession $webSession
    $loginParsed = $login.Content | ConvertFrom-Json
    if ($login.StatusCode -ne 200 -or -not $loginParsed.authenticated) { throw "Web control login failed" }
    $overview = Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/api/status" -WebSession $webSession
    $overviewParsed = $overview.Content | ConvertFrom-Json
    if ($overview.StatusCode -ne 200 -or $overviewParsed.version -ne "0.12.5") { throw "Authenticated full web UI API failed" }

    $phoneProject = Join-Path $TestRoot "phone-project"
    New-Item -ItemType Directory -Force -Path $phoneProject | Out-Null
    $encodedRoot = [Uri]::EscapeDataString($TestRoot)
    $directoryBrowse = Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/api/control/directories?path=$encodedRoot" -WebSession $webSession
    if ($directoryBrowse.StatusCode -ne 200 -or $directoryBrowse.Content -notlike "*phone-project*") { throw "Web control directory browser failed" }
    $phoneProjectAdd = Send-Json -Method "POST" -Uri "http://127.0.0.1:$webPort/api/projects" -Body @{ name = "Phone Project"; path = $phoneProject } -WebSession $webSession
    if ($phoneProjectAdd.StatusCode -ne 201 -or $phoneProjectAdd.Content -notlike "*Phone Project*") { throw "Web control project add failed" }
    $phoneProjectParsed = $phoneProjectAdd.Content | ConvertFrom-Json
    $folderAdd = Send-Json -Method "POST" -Uri "http://127.0.0.1:$webPort/api/project-folders" -Body @{ name = "Smoke Folder" } -WebSession $webSession
    if ($folderAdd.StatusCode -ne 201 -or $folderAdd.Content -notlike "*Smoke Folder*") { throw "Project folder create failed" }
    $folderAssign = Send-Json -Method "PATCH" -Uri "http://127.0.0.1:$webPort/api/project-folders" -Body @{ projectIds = @($phoneProjectParsed.id); folder = "Smoke Folder" } -WebSession $webSession
    if ($folderAssign.StatusCode -ne 200 -or $folderAssign.Content -notlike "*Smoke Folder*") { throw "Project folder assignment failed" }
    $folderDelete = Send-Json -Method "DELETE" -Uri "http://127.0.0.1:$webPort/api/project-folders" -Body @{ name = "Smoke Folder" } -WebSession $webSession
    if ($folderDelete.StatusCode -ne 200) { throw "Project folder delete failed" }
    $projectsAfterFolderDelete = (Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/api/projects" -WebSession $webSession).Content | ConvertFrom-Json
    $phoneProjectAfterDelete = @($projectsAfterFolderDelete) | Where-Object { $_.id -eq $phoneProjectParsed.id } | Select-Object -First 1
    if ($phoneProjectAfterDelete.folder) { throw "Deleting a virtual project folder did not return its projects to unfiled" }

    $switchListener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $switchListener.Start()
    $switchPort = ([System.Net.IPEndPoint]$switchListener.LocalEndpoint).Port
    $switchListener.Stop()
    $switchPortUpdate = Send-Json -Method "PUT" -Uri "http://127.0.0.1:$webPort/api/config" -Body @{ mcpPort = $switchPort } -WebSession $webSession
    if ($switchPortUpdate.StatusCode -ne 200) { throw "Failed to isolate MCP port before active project removal" }
    $activatePhoneProject = Send-Json -Method "POST" -Uri "http://127.0.0.1:$webPort/api/projects/$($phoneProjectParsed.id)/activate" -WebSession $webSession
    if ($activatePhoneProject.StatusCode -ne 200) { throw "Activating secondary project before removal failed" }
    $removeActiveProject = Send-Json -Method "DELETE" -Uri "http://127.0.0.1:$webPort/api/projects/$($phoneProjectParsed.id)" -WebSession $webSession
    if ($removeActiveProject.StatusCode -ne 204) { throw "Removing active project failed" }
    $configAfterActiveRemoval = (Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/api/config" -WebSession $webSession).Content | ConvertFrom-Json
    if ($configAfterActiveRemoval.workspace -ne $TestRoot) { throw "Removing active project did not switch to the next remaining project" }

    $webPage = Send-Json -Method "GET" -Uri "http://127.0.0.1:$webPort/"
    if ($webPage.StatusCode -ne 200 -or $webPage.Content -notlike "*MCP DevDesk*") {
        throw "Web control port did not expose the embedded frontend"
    }
    $webControlOff = Send-Json -Method "PUT" -Uri "$BaseUrl/api/web-control" -Body @{ enabled = $false; port = $webPort; lanEnabled = $true; authEnabled = $true }
    $webStatusOff = $webControlOff.Content | ConvertFrom-Json
    if ($webControlOff.StatusCode -ne 200 -or $webStatusOff.enabled -or $webStatusOff.running) {
        throw "Web control disable endpoint failed"
    }

    $promptSettings = Send-Json -Method "PUT" -Uri "$BaseUrl/api/projects/prompt-settings" -Body @{ enabled = $true; globalPrompt = "SMOKE_GLOBAL_PROMPT: finish the complete task before replying." }
    if ($promptSettings.StatusCode -ne 200) { throw "Global project prompt endpoint failed" }
    $projects = (Send-Json -Method "GET" -Uri "$BaseUrl/api/projects").Content | ConvertFrom-Json
    $project = @($projects) | Where-Object { $_.path -eq $TestRoot } | Select-Object -First 1
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
    $instructionsPath = Join-Path $TestRoot "data\devdesk\global-instructions.md"
    if (-not (Test-Path -LiteralPath $instructionsPath)) { throw "Global instructions file was not generated" }
    $instructions = Get-Content -LiteralPath $instructionsPath -Raw
    if ($instructions -notlike "*SMOKE_GLOBAL_PROMPT*" -or $instructions -like "*SMOKE_PROJECT_PROMPT*") {
        throw "Global instructions file must contain only the enabled global prompt"
    }
    $agentsPath = Join-Path $TestRoot "AGENTS.md"
    if (-not (Test-Path -LiteralPath $agentsPath)) { throw "Project prompt was not written to AGENTS.md" }
    $agents = Get-Content -LiteralPath $agentsPath -Raw
    if ($agents -notlike "*SMOKE_PROJECT_PROMPT*") {
        throw "Project AGENTS.md did not contain the project prompt"
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
    if ($report.diagnostics.version -ne "0.12.5" -or -not $report.instances) {
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
