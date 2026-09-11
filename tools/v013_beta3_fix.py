from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    actual = text.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} matches, found {actual}")
    p.write_text(text.replace(old, new), encoding="utf-8", newline="\n")


# Keep the standard LSP-shaped document_symbols tool advertised. The legacy
# list_symbols spelling remains executable in executeCodeNavigationTool, but is
# no longer a separate tools/list entry so the complete Screen Vision catalog
# stays within the 55-tool connector import budget observed in ChatGPT.
replace_exact(
    "app/internal/mcpcore/code_navigation.go",
    '''\t\t{\n\t\t\tName:        "list_symbols",\n\t\t\tTitle:       "List Source Symbols",\n\t\t\tDescription: "List declarations in one source file using the v0.13 code-navigation engine. Installed language-server binaries are reported as capabilities; this preview falls back to a bounded lexical parser when no persistent LSP session is attached.",\n\t\t\tInputSchema: codeSymbolSchema(true),\n\t\t},\n''',
    "",
)

# Harden the PowerShell UI Automation bridge against providers that expose
# scalar properties (notably FrameworkId) as Int32/IConvertible values.
p = Path("app/internal/mcpcore/ui_automation_windows.go")
text = p.read_text(encoding="utf-8")
start = text.index("const uiAutomationPowerShell = `")
end_marker = "[pscustomobject]@{ nodes=@($items); truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`"
end = text.index(end_marker, start) + len(end_marker)
replacement = r'''const uiAutomationPowerShellHelpers = `function SafeText {
  param([AllowNull()][object]$value)
  if ($null -eq $value) { return '' }
  try { $text = "$value" } catch { return '' }
  if ($null -eq $text) { return '' }
  if ($text.Length -gt 512) { return $text.Substring(0, 512) }
  return $text
}
function SafeInt {
  param([AllowNull()][object]$value)
  if ($null -eq $value) { return 0 }
  try {
    $number = [double]$value
    if ([double]::IsNaN($number) -or [double]::IsInfinity($number)) { return 0 }
    if ($number -gt [int]::MaxValue) { return [int]::MaxValue }
    if ($number -lt [int]::MinValue) { return [int]::MinValue }
    return [int]$number
  } catch { return 0 }
}`

const uiAutomationPowerShell = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
$handle = [IntPtr]([Int64]$env:MCP_UIA_HWND)
$maxDepth = [int]$env:MCP_UIA_DEPTH
$maxNodes = [int]$env:MCP_UIA_NODES
$root = [System.Windows.Automation.AutomationElement]::FromHandle($handle)
if ($null -eq $root) { throw 'UI Automation root is unavailable' }
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
$items = New-Object System.Collections.Generic.List[object]
$script:truncated = $false
` + uiAutomationPowerShellHelpers + `
function AddNode([System.Windows.Automation.AutomationElement]$element, [int]$depth) {
  if ($null -eq $element) { return }
  if ($items.Count -ge $maxNodes) { $script:truncated = $true; return }
  try { $c = $element.Current } catch { return }

  $r = $null
  try { $r = $c.BoundingRectangle } catch {}

  $isPassword = $false
  try { $isPassword = [bool]$c.IsPassword } catch {}
  $value = ''
  if (-not $isPassword) {
    try {
      $pattern = $null
      if ($element.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pattern)) {
        $value = SafeText ($pattern.Current.Value)
      }
    } catch {}
  }

  $name = ''
  $automationId = ''
  $controlType = ''
  $className = ''
  $frameworkId = ''
  try { $name = SafeText ($c.Name) } catch {}
  try { $automationId = SafeText ($c.AutomationId) } catch {}
  try {
    $programmaticName = SafeText ($c.ControlType.ProgrammaticName)
    if ($programmaticName.StartsWith('ControlType.')) {
      $controlType = $programmaticName.Substring(12)
    } else {
      $controlType = $programmaticName
    }
  } catch {}
  try { $className = SafeText ($c.ClassName) } catch {}
  try { $frameworkId = SafeText ($c.FrameworkId) } catch {}

  $enabled = $false
  $offscreen = $false
  try { $enabled = [bool]$c.IsEnabled } catch {}
  try { $offscreen = [bool]$c.IsOffscreen } catch {}

  $x = 0
  $y = 0
  $width = 0
  $height = 0
  if ($null -ne $r) {
    $x = SafeInt ($r.X)
    $y = SafeInt ($r.Y)
    $width = SafeInt ($r.Width)
    $height = SafeInt ($r.Height)
  }

  $items.Add([pscustomobject]@{
    depth = $depth
    name = $name
    automationId = $automationId
    controlType = $controlType
    className = $className
    frameworkId = $frameworkId
    enabled = $enabled
    offscreen = $offscreen
    password = $isPassword
    bounds = [pscustomobject]@{ x=$x; y=$y; width=$width; height=$height }
    value = $value
  }) | Out-Null
  if ($depth -ge $maxDepth) { return }
  try { $child = $walker.GetFirstChild($element) } catch { $child = $null }
  while ($null -ne $child) {
    AddNode $child ($depth + 1)
    if ($items.Count -ge $maxNodes) { $script:truncated = $true; break }
    try { $child = $walker.GetNextSibling($child) } catch { break }
  }
}
AddNode $root 0
[pscustomobject]@{ nodes=@($items); truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`'''
text = text[:start] + replacement + text[end:]
p.write_text(text, encoding="utf-8", newline="\n")

# Add a non-interactive Windows regression test that runs the exact helper
# functions with Int32/Double inputs. Hosted runners can execute this even
# without an interactive desktop.
p = Path("app/internal/mcpcore/ui_automation_windows_test.go")
text = p.read_text(encoding="utf-8")
text = text.replace(
    'import (\n\t"encoding/base64"',
    'import (\n\t"context"\n\t"encoding/base64"\n\t"encoding/json"',
    1,
)
text = text.replace(
    '\t"os"\n',
    '\t"os"\n\t"os/exec"\n',
    1,
)
text = text.replace(
    '\t"testing"\n',
    '\t"testing"\n\t"time"\n',
    1,
)
anchor = 'func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }\n'
if anchor not in text:
    raise SystemExit("ui_automation_windows_test.go: unsafePointer anchor not found")
new_test = r'''func TestUIAutomationPowerShellHelpersHandleScalarValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := uiAutomationPowerShellHelpers + `
[pscustomobject]@{
  text = (SafeText ([int]42))
  number = (SafeInt ([double]12))
} | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(script))
	configureCommand(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("PowerShell helper regression test timed out")
	}
	if err != nil {
		t.Fatalf("PowerShell helper regression test failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	var payload struct {
		Text   string `json:"text"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode PowerShell helper result: %v: %s", err, string(output))
	}
	if payload.Text != "42" || payload.Number != 12 {
		t.Fatalf("unexpected PowerShell helper result: %#v", payload)
	}
}

'''
text = text.replace(anchor, new_test + anchor, 1)
p.write_text(text, encoding="utf-8", newline="\n")

# Catalog contract: 48 non-Screen tools; 55 when Screen Vision + UIA are on.
p = Path("app/internal/mcpcore/server_test.go")
text = p.read_text(encoding="utf-8")
if text.count("!= 49") != 2:
    raise SystemExit(f"server_test.go: expected two 49-count assertions, found {text.count('!= 49')}")
text = text.replace("!= 49", "!= 48")
text = text.replace(
    '\t\t"list_symbols", "document_symbols", "workspace_symbols", "find_definition", "find_references",\n',
    '\t\t"document_symbols", "workspace_symbols", "find_definition", "find_references",\n',
    1,
)
needle = '\tseen := make(map[string]bool, len(server.tools))\n\tfor _, tool := range server.tools {\n\t\tseen[tool.Name] = true\n\t}\n'
replacement2 = needle + '\tif len(server.tools) != 55 {\n\t\tt.Fatalf("Screen Vision full tool catalog = %d, want 55", len(server.tools))\n\t}\n\tif seen["list_symbols"] {\n\t\tt.Fatal("list_symbols compatibility alias should not consume a separate tools/list slot")\n\t}\n'
if text.count(needle) != 1:
    raise SystemExit("server_test.go: catalog map anchor not found exactly once")
text = text.replace(needle, replacement2, 1)
p.write_text(text, encoding="utf-8", newline="\n")

# Version and docs.
replace_exact(
    "app/internal/buildinfo/version.go",
    'const Version = "0.13.0-beta.2"',
    'const Version = "0.13.0-beta.3"',
)

p = Path("docs/V013_PREVIEW.md")
text = p.read_text(encoding="utf-8")
text = text.replace("# MCP DevDesk v0.13.0-beta.2", "# MCP DevDesk v0.13.0-beta.3", 1)
text = text.replace("0.13.0-beta.2 是独立 prerelease", "0.13.0-beta.3 是独立 prerelease", 1)
text = text.replace(
    "- 代码导航：新增 `list_symbols`、`document_symbols`、`workspace_symbols`、`find_definition`、`find_references`。Preview 会探测已安装 language-server，但当前结果明确标记 `lexical-fallback`，不把未接入的 LSP 会话伪装成完整 LSP。",
    "- 代码导航：正式暴露 `document_symbols`、`workspace_symbols`、`find_definition`、`find_references`；`list_symbols` 继续作为 Beta 2 兼容调用别名，但不再单独占用 tools/list 槽位。Preview 会探测已安装 language-server，但当前结果明确标记 `lexical-fallback`，不把未接入的 LSP 会话伪装成完整 LSP。",
    1,
)
beta2_heading = "## Beta 2：工具目录自动刷新\n"
if beta2_heading not in text:
    raise SystemExit("V013_PREVIEW.md: Beta 2 heading missing")
beta3 = """## Beta 3：55 工具目录兼容与 UI Automation 修复\n\n- Windows CI 诊断确认 Go Core 在 Beta 2 源码下会生成 56 个工具，但当前 ChatGPT Connector 实测只导入 55 个，且遗漏 `screen_capture_probe`。\n- 为避免第 56 个工具静默丢失，Beta 3 不再单独广告完全重复的 `list_symbols`；标准 `document_symbols` 保留，旧客户端直接调用 `list_symbols` 仍兼容。Screen Vision 开启后的完整公开目录固定为 55 个。\n- 新增目录契约测试：必须同时包含 `screen_capture_probe` 与 `ui_automation_tree`，并明确断言总数为 55。\n- 修复 `ui_automation_tree` 在部分 Windows UIA Provider 将 `FrameworkId` 返回为 `System.Int32` 时触发 `InvalidCastIConvertible` 的问题；UIA 标量和边界值现在先安全规范化，再生成 JSON。\n- 新增 Windows 非交互回归测试，直接用 Int32/Double 验证同一套 PowerShell 安全转换函数。\n\n"""
text = text.replace(beta2_heading, beta3 + beta2_heading, 1)
p.write_text(text, encoding="utf-8", newline="\n")

p = Path("docs/ROADMAP.md")
text = p.read_text(encoding="utf-8")
text = text.replace("当前预览开发版本：`0.13.0-beta.2`", "当前预览开发版本：`0.13.0-beta.3`", 1)
text = text.replace(
    "- [x] 代码导航 Preview：list_symbols / document_symbols / workspace_symbols / find_definition / find_references",
    "- [x] 代码导航 Preview：document_symbols / workspace_symbols / find_definition / find_references；list_symbols 保留为兼容调用别名，不再单独占用工具目录槽位",
    1,
)
text += "\n- Beta 3 实机修复：ChatGPT Connector 55 工具目录兼容、screen_capture_probe 保留、Windows UI Automation Int32 FrameworkId 安全转换。\n"
p.write_text(text, encoding="utf-8", newline="\n")

print("Beta 3 source migration applied")
