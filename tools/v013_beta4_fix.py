from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    actual = text.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} matches, found {actual}")
    p.write_text(text.replace(old, new), encoding="utf-8", newline="\n")


# screen_capture_probe is metadata-only and must survive the runtime policy
# narrowing performed after New(). Pixel/window capture tools remain mode-bound.
replace_exact(
    "app/internal/mcpcore/screen_policy.go",
    '''func (policy screenVisionPolicy) allows(name string) bool {\n\tswitch policy.mode {''',
    '''func (policy screenVisionPolicy) allows(name string) bool {\n\tif name == "screen_capture_probe" {\n\t\treturn true\n\t}\n\tswitch policy.mode {''',
)

# Avoid Windows PowerShell 5.1 treating Generic.List[object] unexpectedly while
# building the final PSCustomObject/JSON payload.
replace_exact(
    "app/internal/mcpcore/ui_automation_windows.go",
    '''AddNode $root 0\n[pscustomobject]@{ nodes=@($items); truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`''',
    '''AddNode $root 0\n$nodeArray = $items.ToArray()\n[pscustomobject]@{ nodes=$nodeArray; truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`''',
)

# Strengthen policy unit tests with probe availability and post-policy counts.
p = Path("app/internal/mcpcore/screen_policy_test.go")
text = p.read_text(encoding="utf-8")
text = text.replace(
    '''\t\twant      []string\n\t\tnotWanted []string\n''',
    '''\t\twant      []string\n\t\tnotWanted []string\n\t\twantCount int\n''',
    1,
)
text = text.replace(
    '''\t\t\tname:      "active",\n\t\t\tmode:      "active",\n\t\t\twant:      []string{"screen_get_active_window", "screen_capture_active_window"},\n\t\t\tnotWanted: []string{"screen_list_windows", "screen_capture_window", "screen_capture_desktop"},\n''',
    '''\t\t\tname:      "active",\n\t\t\tmode:      "active",\n\t\t\twant:      []string{"screen_capture_probe", "screen_get_active_window", "screen_capture_active_window"},\n\t\t\tnotWanted: []string{"screen_list_windows", "screen_capture_window", "screen_capture_desktop"},\n\t\t\twantCount: 52,\n''',
    1,
)
text = text.replace(
    '''\t\t\tname:      "window",\n\t\t\tmode:      "window",\n\t\t\twindowID:  "0x10",\n\t\t\twant:      []string{"screen_list_windows", "screen_capture_window"},\n\t\t\tnotWanted: []string{"screen_get_active_window", "screen_capture_active_window", "screen_capture_desktop"},\n''',
    '''\t\t\tname:      "window",\n\t\t\tmode:      "window",\n\t\t\twindowID:  "0x10",\n\t\t\twant:      []string{"screen_capture_probe", "screen_list_windows", "screen_capture_window"},\n\t\t\tnotWanted: []string{"screen_get_active_window", "screen_capture_active_window", "screen_capture_desktop"},\n\t\t\twantCount: 52,\n''',
    1,
)
text = text.replace(
    '''\t\t\tname: "desktop",\n\t\t\tmode: "desktop",\n\t\t\twant: []string{\n\t\t\t\t"screen_list_windows",''',
    '''\t\t\tname: "desktop",\n\t\t\tmode: "desktop",\n\t\t\twant: []string{\n\t\t\t\t"screen_capture_probe",\n\t\t\t\t"screen_list_windows",''',
    1,
)
text = text.replace(
    '''\t\t\t\t"screen_capture_desktop",\n\t\t\t},\n\t\t},\n''',
    '''\t\t\t\t"screen_capture_desktop",\n\t\t\t},\n\t\t\twantCount: 55,\n\t\t},\n''',
    1,
)
anchor = '''\t\t\tfor _, name := range test.notWanted {\n\t\t\t\tif containsTool(server.tools, name) {\n\t\t\t\t\tt.Fatalf("mode %s unexpectedly advertised %s", test.mode, name)\n\t\t\t\t}\n\t\t\t}\n'''
replacement = anchor + '''\t\t\tif len(server.tools) != test.wantCount {\n\t\t\t\tt.Fatalf("mode %s tool count = %d, want %d", test.mode, len(server.tools), test.wantCount)\n\t\t\t}\n'''
if text.count(anchor) != 1:
    raise SystemExit("screen_policy_test.go: policy assertion anchor missing")
text = text.replace(anchor, replacement, 1)
text = text.replace(
    '''\tif !containsTool(server.tools, "screen_list_windows") {\n\t\tt.Fatal("specified-window mode should still allow metadata listing before a target is selected")\n\t}\n''',
    '''\tif !containsTool(server.tools, "screen_capture_probe") {\n\t\tt.Fatal("specified-window mode should keep the metadata-only compatibility probe before a target is selected")\n\t}\n\tif !containsTool(server.tools, "screen_list_windows") {\n\t\tt.Fatal("specified-window mode should still allow metadata listing before a target is selected")\n\t}\n''',
    1,
)
text = text.replace(
    '''\tif containsTool(server.tools, "screen_capture_window") {\n\t\tt.Fatal("capture must not be advertised before a specified target is selected")\n\t}\n''',
    '''\tif containsTool(server.tools, "screen_capture_window") {\n\t\tt.Fatal("capture must not be advertised before a specified target is selected")\n\t}\n\tif len(server.tools) != 51 {\n\t\tt.Fatalf("specified-window mode without target tool count = %d, want 51", len(server.tools))\n\t}\n''',
    1,
)
text = text.replace(
    '''\tfor _, name := range []string{\n\t\t"screen_list_windows",''',
    '''\tfor _, name := range []string{\n\t\t"screen_capture_probe",\n\t\t"screen_list_windows",''',
    1,
)
p.write_text(text, encoding="utf-8", newline="\n")

# Add an exact Windows PowerShell 5.1 regression for the final Generic.List ->
# array -> JSON payload shape used by UI Automation.
p = Path("app/internal/mcpcore/ui_automation_windows_test.go")
text = p.read_text(encoding="utf-8")
anchor = 'func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }\n'
if anchor not in text:
    raise SystemExit("ui_automation_windows_test.go: unsafePointer anchor missing")
new_test = r'''func TestUIAutomationPowerShellCollectionPayloadIsJSONSafe(t *testing.T) {
	if !strings.Contains(uiAutomationPowerShell, "$items.ToArray()") {
		t.Fatal("UI Automation script must materialize the Generic.List before JSON conversion")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := uiAutomationPowerShellHelpers + `
$items = New-Object System.Collections.Generic.List[object]
$items.Add([pscustomobject]@{ name='alpha'; frameworkId=(SafeText ([int]42)) }) | Out-Null
$items.Add([pscustomobject]@{ name='beta'; frameworkId='WebView2' }) | Out-Null
$nodeArray = $items.ToArray()
[pscustomobject]@{ nodes=$nodeArray; truncated=$false } | ConvertTo-Json -Depth 8 -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(script))
	configureCommand(cmd)
	output, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatal("PowerShell collection payload regression test timed out")
	}
	if err != nil {
		t.Fatalf("PowerShell collection payload regression test failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	var payload struct {
		Nodes []struct {
			Name        string `json:"name"`
			FrameworkID string `json:"frameworkId"`
		} `json:"nodes"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode PowerShell collection payload: %v: %s", err, string(output))
	}
	if payload.Truncated || len(payload.Nodes) != 2 || payload.Nodes[0].FrameworkID != "42" || payload.Nodes[1].FrameworkID != "WebView2" {
		t.Fatalf("unexpected PowerShell collection payload: %#v", payload)
	}
}

'''
text = text.replace(anchor, new_test + anchor, 1)
p.write_text(text, encoding="utf-8", newline="\n")

# Durable product-binary smoke test. It starts the compiled mcp-core and checks
# the catalog after production ConfigureScreenVision policy narrowing.
smoke = r'''param(
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

        $listed = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 2; method = "tools/list"; params = @{} }
        $names = @($listed.Body.result.tools | ForEach-Object { [string]$_.name } | Sort-Object)
        $info = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 3; method = "tools/call"; params = @{ name = "server_info"; arguments = @{} } }
        $toolCount = [int]$info.Body.result.structuredContent.toolCount
        Write-Host "mode=$Mode toolsListCount=$($names.Count) serverToolCount=$toolCount"
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
'''
Path("tools/smoke-mcp-core-catalog.ps1").write_text(smoke, encoding="utf-8", newline="\n")

# Make the official preview release gate exercise the compiled product binary.
p = Path(".github/workflows/v013-preview.yml")
text = p.read_text(encoding="utf-8")
anchor = '''      - name: Build NSIS setup\n        shell: pwsh\n'''
insert = '''      - name: Built core Screen Vision catalog contract\n        shell: pwsh\n        run: .\\tools\\smoke-mcp-core-catalog.ps1 -CorePath .\\dist\\mcp-core-amd64.exe\n\n'''
if text.count(anchor) != 1:
    raise SystemExit("v013-preview.yml: NSIS anchor missing")
text = text.replace(anchor, insert + anchor, 1)
p.write_text(text, encoding="utf-8", newline="\n")

# Version and release notes.
replace_exact(
    "app/internal/buildinfo/version.go",
    'const Version = "0.13.0-beta.3"',
    'const Version = "0.13.0-beta.4"',
)

p = Path("docs/V013_PREVIEW.md")
text = p.read_text(encoding="utf-8")
text = text.replace("# MCP DevDesk v0.13.0-beta.3", "# MCP DevDesk v0.13.0-beta.4", 1)
text = text.replace("0.13.0-beta.3 是独立 prerelease", "0.13.0-beta.4 是独立 prerelease", 1)
heading = "## Beta 3：55 工具目录兼容与 UI Automation 修复\n"
if heading not in text:
    raise SystemExit("V013_PREVIEW.md: Beta 3 heading missing")
beta4 = """## Beta 4：生产运行时工具目录与 UIA 集合修复\n\n- Beta 3 实机复测确认：Go Core 在 `New()` 后会先建立完整工具目录，但生产入口随后执行 `ConfigureScreenVision` 按 active / window / desktop 模式做最小权限裁剪；此前 CI 只覆盖前者，因此错误地把“55 个”当成所有运行模式的固定数量。\n- `screen_capture_probe` 是不读取像素的策略/兼容性诊断工具，Beta 4 将它从模式裁剪中豁免：只要 Screen Vision 已启用且权限为 trusted/dangerous，active、指定窗口、desktop 三种模式都保留 probe。\n- full profile + UI Automation 下的生产目录契约现在按模式验证：active 52 个、已锁定 window 52 个、desktop 55 个；指定窗口但尚未选择目标时为 51 个。截图能力本身仍严格按模式最小授权，不为凑数量放宽。\n- 新增真正的产品二进制验收：Windows 发布流程在生成 `dist/mcp-core-amd64.exe` 后，会启动该 EXE 并分别完成 MCP initialize / tools/list / server_info 的 active、window、desktop 三模式契约检查，避免再次出现“库测试通过但最终运行时目录不同”的盲区。\n- 修复 Windows PowerShell 5.1 UI Automation 最终 JSON 组装：`System.Collections.Generic.List[object]` 先显式 `ToArray()`，再写入节点 payload，避免真实 WebView2/DevDesk 窗口在最终集合转换处抛 `ArgumentException`。\n- 新增不依赖交互桌面的 Windows PowerShell 5.1 Generic.List → JSON 回归测试，与生产 UIA 的最终数据形状一致。\n\n"""
text = text.replace(heading, beta4 + heading, 1)
# Correct the historical Beta 3 wording without rewriting history.
text = text.replace(
    "- 为避免第 56 个工具静默丢失，Beta 3 不再单独广告完全重复的 `list_symbols`；标准 `document_symbols` 保留，旧客户端直接调用 `list_symbols` 仍兼容。Screen Vision 开启后的完整公开目录固定为 55 个。",
    "- 为减少重复目录槽位，Beta 3 不再单独广告完全重复的 `list_symbols`；标准 `document_symbols` 保留，旧客户端直接调用 `list_symbols` 仍兼容。后续 Beta 4 实机复测确认，生产运行时还会按 Screen Vision 模式继续做最小权限目录裁剪，因此 55 只对应 desktop 模式而不是所有模式。",
    1,
)
text = text.replace(
    "- 新增目录契约测试：必须同时包含 `screen_capture_probe` 与 `ui_automation_tree`，并明确断言总数为 55。",
    "- Beta 3 新增了构造阶段目录契约测试；Beta 4 将其补强为生产二进制、模式感知的端到端目录契约测试。",
    1,
)
p.write_text(text, encoding="utf-8", newline="\n")

p = Path("docs/ROADMAP.md")
text = p.read_text(encoding="utf-8")
text = text.replace("当前预览开发版本：`0.13.0-beta.3`", "当前预览开发版本：`0.13.0-beta.4`", 1)
text += "\n- Beta 4 实机修复：Screen Vision runtime policy 始终保留 metadata-only `screen_capture_probe`；发布门禁新增最终 mcp-core active/window/desktop 工具目录 E2E；UI Automation Generic.List 最终 JSON 使用 ToArray()。\n"
p.write_text(text, encoding="utf-8", newline="\n")

print("Beta 4 source migration applied")
