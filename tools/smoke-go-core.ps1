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

Add-Type -AssemblyName System.Net.Http

$DataDir = Join-Path $env:TEMP ("mcp-devdesk-smoke-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$InstructionsFile = Join-Path $DataDir "managed-instructions.md"
$BaseUrl = "http://127.0.0.1:$Port"
$RedirectUri = "http://127.0.0.1:43210/callback"
$OwnerPassword = "smoke-owner-password"
$TokenSecret = (("ab" * 32) -join "")
$process = $null
$httpHandler = [System.Net.Http.HttpClientHandler]::new()
$httpHandler.AllowAutoRedirect = $false
$http = [System.Net.Http.HttpClient]::new($httpHandler)
$http.Timeout = [TimeSpan]::FromSeconds(10)

function Send-Http {
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Uri,
        [string]$Body = "",
        [string]$ContentType = "",
        [hashtable]$Headers = @{}
    )
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::new($Method), $Uri)
    try {
        foreach ($entry in $Headers.GetEnumerator()) {
            [void]$request.Headers.TryAddWithoutValidation([string]$entry.Key, [string]$entry.Value)
        }
        if ($Body -ne "" -or $Method -in @("POST", "PUT", "PATCH")) {
            $request.Content = [System.Net.Http.StringContent]::new($Body, [Text.Encoding]::UTF8)
            if ($ContentType) {
                $request.Content.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse($ContentType)
            }
        }
        $response = $http.SendAsync($request).GetAwaiter().GetResult()
        $responseBody = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        return [pscustomobject]@{
            Status = [int]$response.StatusCode
            Body = $responseBody
            Response = $response
        }
    } finally {
        $request.Dispose()
    }
}

function Start-Core {
    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $ExePath
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $quotedWorkspace = '"' + ($Workspace -replace '"', '\"') + '"'
    $quotedDataDir = '"' + ($DataDir -replace '"', '\"') + '"'
    $quotedInstructions = '"' + ($InstructionsFile -replace '"', '\"') + '"'
    $startInfo.Arguments = "--workspace $quotedWorkspace --host 127.0.0.1 --port $Port --permission-mode safe --tool-profile full --oauth-mode --data-dir $quotedDataDir --server-url $BaseUrl --instructions-file $quotedInstructions"
    $startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_PASSWORD"] = $OwnerPassword
    $startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_CLIENT_ID"] = "smoke-static-client"
    $startInfo.EnvironmentVariables["CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET"] = $TokenSecret

    $started = [System.Diagnostics.Process]::new()
    $started.StartInfo = $startInfo
    if (-not $started.Start()) { throw "Failed to start Go MCP core" }
    $script:process = $started

    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        Start-Sleep -Milliseconds 100
        try {
            $health = Send-Http -Method "GET" -Uri "$BaseUrl/healthz"
            if ($health.Status -eq 200) {
                $parsed = $health.Body | ConvertFrom-Json
                if ($parsed.ok -and $parsed.version -eq "0.9.0") { return }
            }
        } catch {}
        if ($started.HasExited) { break }
    }
    $stderr = $started.StandardError.ReadToEnd()
    throw "Go MCP core did not become healthy. stderr: $stderr"
}

function Stop-Core {
    if ($script:process -and -not $script:process.HasExited) {
        Stop-Process -Id $script:process.Id -Force -ErrorAction SilentlyContinue
        $script:process.WaitForExit(5000) | Out-Null
    }
    $script:process = $null
}

function Form-Encode {
    param([hashtable]$Values)
    $pairs = foreach ($entry in $Values.GetEnumerator()) {
        ([Uri]::EscapeDataString([string]$entry.Key) + "=" + [Uri]::EscapeDataString([string]$entry.Value))
    }
    return [string]::Join("&", $pairs)
}

function Query-Value {
    param([string]$Uri, [string]$Name)
    $query = ([Uri]$Uri).Query.TrimStart("?")
    foreach ($pair in $query.Split("&", [System.StringSplitOptions]::RemoveEmptyEntries)) {
        $parts = $pair.Split("=", 2)
        if ([Uri]::UnescapeDataString($parts[0]) -eq $Name) {
            return [Uri]::UnescapeDataString(($parts[1] -replace '\+', ' '))
        }
    }
    return ""
}

function Base64Url-Sha256 {
    param([string]$Value)
    $sha = [Security.Cryptography.SHA256]::Create()
    try {
        $hash = $sha.ComputeHash([Text.Encoding]::ASCII.GetBytes($Value))
        return [Convert]::ToBase64String($hash).TrimEnd("=").Replace("+", "-").Replace("/", "_")
    } finally {
        $sha.Dispose()
    }
}

try {
    Start-Core

    $rootMetadata = Send-Http -Method "GET" -Uri "$BaseUrl/.well-known/oauth-protected-resource"
    if ($rootMetadata.Status -ne 200 -or (($rootMetadata.Body | ConvertFrom-Json).resource -ne $BaseUrl)) {
        throw "Unexpected root protected resource metadata: $($rootMetadata.Status) $($rootMetadata.Body)"
    }
    $pathMetadata = Send-Http -Method "GET" -Uri "$BaseUrl/.well-known/oauth-protected-resource/mcp"
    if ($pathMetadata.Status -ne 200 -or (($pathMetadata.Body | ConvertFrom-Json).resource -ne "$BaseUrl/mcp")) {
        throw "Unexpected /mcp protected resource metadata: $($pathMetadata.Status) $($pathMetadata.Body)"
    }
    $authorization = Send-Http -Method "GET" -Uri "$BaseUrl/.well-known/oauth-authorization-server"
    if ($authorization.Status -ne 200 -or (($authorization.Body | ConvertFrom-Json).authorization_endpoint -ne "$BaseUrl/oauth/authorize")) {
        throw "Unexpected authorization metadata"
    }
    $openid = Send-Http -Method "GET" -Uri "$BaseUrl/.well-known/openid-configuration"
    if ($openid.Status -ne 200) { throw "OpenID discovery compatibility endpoint failed" }

    $unauthorized = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers @{ Accept = "application/json, text/event-stream" } -Body '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"1"}}}'
    if ($unauthorized.Status -ne 401) { throw "Expected HTTP 401 from protected MCP endpoint, got $($unauthorized.Status)" }
    $challenge = [string]::Join(",", $unauthorized.Response.Headers.GetValues("WWW-Authenticate"))
    if ($challenge -notlike "*/.well-known/oauth-protected-resource/mcp*") { throw "Incorrect resource metadata challenge: $challenge" }

    $registerBody = @{
        client_name = "MCP DevDesk Smoke"
        redirect_uris = @($RedirectUri)
        token_endpoint_auth_method = "none"
        grant_types = @("authorization_code", "refresh_token")
        response_types = @("code")
        scope = "mcp"
    } | ConvertTo-Json -Depth 5 -Compress
    $registration = Send-Http -Method "POST" -Uri "$BaseUrl/oauth/register" -ContentType "application/json" -Body $registerBody
    if ($registration.Status -ne 201) { throw "Dynamic registration failed: $($registration.Status) $($registration.Body)" }
    $registered = $registration.Body | ConvertFrom-Json
    if (-not $registered.client_id) { throw "Dynamic registration returned no client_id" }

    $verifier = "s" * 43
    $authorizeQuery = Form-Encode @{
        response_type = "code"
        client_id = $registered.client_id
        redirect_uri = $RedirectUri
        code_challenge = (Base64Url-Sha256 $verifier)
        code_challenge_method = "S256"
        resource = $BaseUrl
        scope = "mcp"
        state = "smoke-state"
    }
    $authorizePage = Send-Http -Method "GET" -Uri "$BaseUrl/oauth/authorize?$authorizeQuery"
    if ($authorizePage.Status -ne 200) { throw "Authorization page failed: $($authorizePage.Status) $($authorizePage.Body)" }
    $authorizeCsp = [string]::Join(",", $authorizePage.Response.Headers.GetValues("Content-Security-Policy"))
    if ($authorizeCsp -notlike "*form-action 'self' http://127.0.0.1:43210*") {
        throw "Authorization page CSP does not allow the validated callback origin: $authorizeCsp"
    }

    $authorizeBody = Form-Encode @{
        response_type = "code"
        client_id = $registered.client_id
        redirect_uri = $RedirectUri
        code_challenge = (Base64Url-Sha256 $verifier)
        code_challenge_method = "S256"
        resource = $BaseUrl
        scope = "mcp"
        state = "smoke-state"
        owner_password = $OwnerPassword
    }
    $authorize = Send-Http -Method "POST" -Uri "$BaseUrl/oauth/authorize" -ContentType "application/x-www-form-urlencoded" -Headers @{ Origin = "null" } -Body $authorizeBody
    if ($authorize.Status -ne 302) { throw "Authorization failed: $($authorize.Status) $($authorize.Body)" }
    $location = [string]$authorize.Response.Headers.Location
    $code = Query-Value -Uri $location -Name "code"
    if (-not $code -or (Query-Value -Uri $location -Name "state") -ne "smoke-state") { throw "Authorization redirect is invalid: $location" }

    $tokenBody = Form-Encode @{
        grant_type = "authorization_code"
        client_id = $registered.client_id
        code = $code
        redirect_uri = $RedirectUri
        code_verifier = $verifier
        resource = $BaseUrl
    }
    $tokenResponse = Send-Http -Method "POST" -Uri "$BaseUrl/oauth/token" -ContentType "application/x-www-form-urlencoded" -Body $tokenBody
    if ($tokenResponse.Status -ne 200) { throw "Token exchange failed: $($tokenResponse.Status) $($tokenResponse.Body)" }
    $tokens = $tokenResponse.Body | ConvertFrom-Json
    if (-not $tokens.access_token -or -not $tokens.refresh_token) { throw "OAuth token response is incomplete" }

    $mcpHeaders = @{
        Accept = "application/json, text/event-stream"
        Authorization = "Bearer $($tokens.access_token)"
    }
    $initialize = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}'
    if ($initialize.Status -ne 200) { throw "MCP initialize failed: $($initialize.Status) $($initialize.Body)" }
    $initialized = $initialize.Body | ConvertFrom-Json
    if ($initialized.result.protocolVersion -ne "2025-06-18") { throw "MCP protocol negotiation failed" }
    if ([string]$initialized.result.instructions -like "*SMOKE_MANAGED_PROJECT_PROMPT*") { throw "Managed project prompt unexpectedly existed before it was saved" }
    $sessionId = [string]::Join("", $initialize.Response.Headers.GetValues("Mcp-Session-Id"))
    if (-not $sessionId) { throw "MCP initialize returned no session ID" }
    $mcpHeaders["Mcp-Session-Id"] = $sessionId
    $mcpHeaders["MCP-Protocol-Version"] = "2025-06-18"

    $notification = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    if ($notification.Status -ne 202 -or $notification.Body) { throw "Initialized notification response is invalid: $($notification.Status) $($notification.Body)" }

    $tools = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
    if ($tools.Status -ne 200) { throw "tools/list failed: $($tools.Status) $($tools.Body)" }
    $toolList = ($tools.Body | ConvertFrom-Json).result.tools
    if ($toolList.Count -lt 33) { throw "tools/list returned too few tools: $($toolList.Count)" }
    foreach ($requiredTool in @("read_files", "project_snapshot", "get_instructions")) {
        if (-not ($toolList | Where-Object { $_.name -eq $requiredTool } | Select-Object -First 1)) {
            throw "$requiredTool tool is missing"
        }
    }
    $imageTool = $toolList | Where-Object { $_.name -eq "save_chatgpt_image" } | Select-Object -First 1
    if (-not $imageTool) { throw "save_chatgpt_image tool is missing" }
    $fileParams = @($imageTool._meta.'openai/fileParams')
    if (-not $fileParams -or $fileParams.Count -ne 1 -or $fileParams[0] -ne "source_image") {
        throw "save_chatgpt_image does not declare the OpenAI image file parameter"
    }
    $fileSchema = $imageTool.inputSchema.'$defs'.OpenAIFile
    if (-not $fileSchema.properties.download_url -or -not $fileSchema.properties.file_id -or
        -not $fileSchema.properties.mime_type -or -not $fileSchema.properties.file_name) {
        throw "save_chatgpt_image file schema is incomplete"
    }
    $imageProperties = $imageTool.inputSchema.properties
    if (-not $imageProperties.source_image -or $imageProperties.data -or $imageProperties.dataUrl -or
        $imageProperties.image -or $imageProperties.mimeType -or $imageProperties.createParents) {
        throw "save_chatgpt_image must expose only the source_image file-transfer path"
    }
    if (-not $imageProperties.create_parents -or -not $imageProperties.max_bytes) {
        throw "save_chatgpt_image transfer controls are incomplete"
    }
    $requiredImageFields = @($imageTool.inputSchema.required)
    if ($requiredImageFields -notcontains "path" -or $requiredImageFields -notcontains "source_image") {
        throw "save_chatgpt_image must require path and source_image"
    }
    $writeImageTool = $toolList | Where-Object { $_.name -eq "write_image" } | Select-Object -First 1
    if (-not $writeImageTool -or -not $writeImageTool.inputSchema.properties.data -or
        -not $writeImageTool.inputSchema.properties.dataUrl) {
        throw "write_image must retain the legacy base64/data-URL path"
    }

    $call = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"server_info","arguments":{}}}'
    if ($call.Status -ne 200 -or ($call.Body | ConvertFrom-Json).result.isError) { throw "tools/call failed: $($call.Status) $($call.Body)" }

    $instructionsCall = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"get_instructions","arguments":{}}}'
    $instructionsResult = ($instructionsCall.Body | ConvertFrom-Json).result
    if ($instructionsCall.Status -ne 200 -or $instructionsResult.isError -or
        $instructionsResult.structuredContent.managedInstructionsActive) {
        throw "get_instructions unexpectedly reported a managed prompt before save: $($instructionsCall.Status) $($instructionsCall.Body)"
    }

    Set-Content -LiteralPath $InstructionsFile -Encoding UTF8 -NoNewline -Value "SMOKE_MANAGED_PROJECT_PROMPT: first runtime prompt save works."
    $staleAfterPromptChange = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":301,"method":"ping"}'
    if ($staleAfterPromptChange.Status -ne 404) {
        throw "Prompt change must invalidate the old MCP session, got $($staleAfterPromptChange.Status): $($staleAfterPromptChange.Body)"
    }
    $mcpHeaders.Remove("Mcp-Session-Id")
    $mcpHeaders.Remove("MCP-Protocol-Version")
    $reinitialize = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":302,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}'
    if ($reinitialize.Status -ne 200) { throw "MCP reinitialize after prompt change failed: $($reinitialize.Status) $($reinitialize.Body)" }
    $reinitialized = $reinitialize.Body | ConvertFrom-Json
    if ([string]$reinitialized.result.instructions -notlike "*SMOKE_MANAGED_PROJECT_PROMPT*") {
        throw "Updated managed prompt was not included after automatic MCP reinitialize"
    }
    $sessionId = [string]::Join("", $reinitialize.Response.Headers.GetValues("Mcp-Session-Id"))
    if (-not $sessionId) { throw "MCP reinitialize returned no session ID" }
    $mcpHeaders["Mcp-Session-Id"] = $sessionId
    $mcpHeaders["MCP-Protocol-Version"] = "2025-06-18"
    $renotification = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    if ($renotification.Status -ne 202) { throw "Reinitialized notification response is invalid: $($renotification.Status) $($renotification.Body)" }

    $updatedInstructionsCall = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":303,"method":"tools/call","params":{"name":"get_instructions","arguments":{}}}'
    $updatedInstructionsResult = ($updatedInstructionsCall.Body | ConvertFrom-Json).result
    if ($updatedInstructionsCall.Status -ne 200 -or $updatedInstructionsResult.isError -or
        [string]$updatedInstructionsResult.structuredContent.managedInstructions -notlike "*SMOKE_MANAGED_PROJECT_PROMPT*") {
        throw "get_instructions did not return the updated runtime prompt: $($updatedInstructionsCall.Status) $($updatedInstructionsCall.Body)"
    }
    if (@($updatedInstructionsResult.content).Count -lt 2 -or
        [string]$updatedInstructionsResult.content[0].text -notlike "*SMOKE_MANAGED_PROJECT_PROMPT*") {
        throw "First tool call in the new MCP session did not deliver the effective instructions into tool content: $($updatedInstructionsCall.Body)"
    }

    $batchRequest = @{
        jsonrpc = "2.0"
        id = 31
        method = "tools/call"
        params = @{
            name = "read_files"
            arguments = @{
                files = @(
                    @{ path = "README.md"; maxBytes = 4096 },
                    @{ path = "app/internal/buildinfo/version.go"; maxBytes = 4096 }
                )
            }
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $batchCall = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body $batchRequest
    if ($batchCall.Status -ne 200) { throw "read_files call failed: $($batchCall.Status) $($batchCall.Body)" }
    $batchResult = ($batchCall.Body | ConvertFrom-Json).result
    if ($batchResult.isError -or $batchResult.structuredContent.succeeded -ne 2 -or
        $batchResult.structuredContent.files[1].content -notlike '*0.9.0*') {
        throw "read_files returned an unexpected result: $($batchCall.Body)"
    }

    $snapshotRequest = @{
        jsonrpc = "2.0"
        id = 32
        method = "tools/call"
        params = @{
            name = "project_snapshot"
            arguments = @{ includeInstructions = $false }
        }
    } | ConvertTo-Json -Depth 6 -Compress
    $snapshotCall = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body $snapshotRequest
    if ($snapshotCall.Status -ne 200) { throw "project_snapshot call failed: $($snapshotCall.Status) $($snapshotCall.Body)" }
    $snapshotResult = ($snapshotCall.Body | ConvertFrom-Json).result
    if ($snapshotResult.isError -or $snapshotResult.structuredContent.topLevelCount -lt 1 -or
        -not $snapshotResult.structuredContent.git) {
        throw "project_snapshot returned an unexpected result: $($snapshotCall.Body)"
    }

    $sseHeaders = @{
        Accept = "text/event-stream"
        Authorization = "Bearer $($tokens.access_token)"
        "Mcp-Session-Id" = $sessionId
        "MCP-Protocol-Version" = "2025-06-18"
        Prefer = "wait=0"
    }
    $sse = Send-Http -Method "GET" -Uri "$BaseUrl/mcp" -Headers $sseHeaders
    if ($sse.Status -ne 200 -or $sse.Response.Content.Headers.ContentType.MediaType -ne "text/event-stream") { throw "SSE connection failed" }

    $deleted = Send-Http -Method "DELETE" -Uri "$BaseUrl/mcp" -Headers $mcpHeaders
    if ($deleted.Status -ne 204) { throw "Session delete failed: $($deleted.Status)" }
    $stale = Send-Http -Method "POST" -Uri "$BaseUrl/mcp" -ContentType "application/json" -Headers $mcpHeaders -Body '{"jsonrpc":"2.0","id":4,"method":"ping"}'
    if ($stale.Status -ne 404) { throw "Unknown session must return 404, got $($stale.Status)" }

    Stop-Core
    Start-Sleep -Milliseconds 300
    Start-Core

    $refreshBody = Form-Encode @{
        grant_type = "refresh_token"
        client_id = $registered.client_id
        refresh_token = $tokens.refresh_token
        resource = $BaseUrl
    }
    $refresh = Send-Http -Method "POST" -Uri "$BaseUrl/oauth/token" -ContentType "application/x-www-form-urlencoded" -Body $refreshBody
    if ($refresh.Status -ne 200) { throw "Refresh token did not survive restart: $($refresh.Status) $($refresh.Body)" }
    $refreshedTokens = $refresh.Body | ConvertFrom-Json
    if (-not $refreshedTokens.access_token -or -not $refreshedTokens.refresh_token) { throw "Refreshed OAuth token response is incomplete" }
    $reused = Send-Http -Method "POST" -Uri "$BaseUrl/oauth/token" -ContentType "application/x-www-form-urlencoded" -Body $refreshBody
    if ($reused.Status -ne 400) { throw "Refresh token rotation failed; reused token status = $($reused.Status)" }

    Write-Host "Go MCP core end-to-end smoke test passed: $ExePath" -ForegroundColor Green
} finally {
    Stop-Core
    $http.Dispose()
    $httpHandler.Dispose()
    Remove-Item -LiteralPath $DataDir -Recurse -Force -ErrorAction SilentlyContinue
}
