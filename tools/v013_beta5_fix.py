from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace_once(path, old, new):
    p = ROOT / path
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"missing expected text in {path}: {old[:120]!r}")
    if text.count(old) != 1:
        raise SystemExit(f"expected one match in {path}, got {text.count(old)}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8", newline="\n")


def write(path, content):
    p = ROOT / path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content, encoding="utf-8", newline="\n")


# 1) Background Windows child processes: HideWindow alone can still flash a console.
replace_once(
    "app/internal/process/platform_windows.go",
    '''func configureChildProcess(cmd *exec.Cmd, hidden bool) {\n''',
    '''const createNoWindow = 0x08000000\n\nfunc configureChildProcess(cmd *exec.Cmd, hidden bool) {\n''',
)
replace_once(
    "app/internal/process/platform_windows.go",
    '''\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    hidden,\n\t\tCreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,\n\t}\n''',
    '''\tflags := uint32(syscall.CREATE_NEW_PROCESS_GROUP)\n\tif hidden {\n\t\tflags |= createNoWindow\n\t}\n\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    hidden,\n\t\tCreationFlags: flags,\n\t}\n''',
)
replace_once(
    "app/internal/process/platform_windows_test.go",
    '''\tif command.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {\n\t\tt.Fatal("child process group flag is missing")\n\t}\n}\n''',
    '''\tif command.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {\n\t\tt.Fatal("child process group flag is missing")\n\t}\n\tif command.SysProcAttr.CreationFlags&createNoWindow == 0 {\n\t\tt.Fatal("CREATE_NO_WINDOW is missing for hidden child process")\n\t}\n}\n\nfunc TestConfigureChildProcessVisibleModeDoesNotForceNoWindow(t *testing.T) {\n\tcommand := exec.Command("netstat.exe", "-ano")\n\tconfigureChildProcess(command, false)\n\tif command.SysProcAttr == nil {\n\t\tt.Fatal("SysProcAttr was not configured")\n\t}\n\tif command.SysProcAttr.HideWindow {\n\t\tt.Fatal("visible child process unexpectedly has HideWindow")\n\t}\n\tif command.SysProcAttr.CreationFlags&createNoWindow != 0 {\n\t\tt.Fatal("visible child process unexpectedly has CREATE_NO_WINDOW")\n\t}\n}\n''',
)
replace_once(
    "app/internal/tunnel/platform_windows.go",
    '''func configureCommand(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}\n}\n''',
    '''const createNoWindow = 0x08000000\n\nfunc configureCommand(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    true,\n\t\tCreationFlags: createNoWindow,\n\t}\n}\n''',
)
replace_once(
    "app/internal/desktop/desktop_windows.go",
    '''\tcommand.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}\n\treturn command\n}\n''',
    '''\tcommand.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    true,\n\t\tCreationFlags: 0x08000000, // CREATE_NO_WINDOW\n\t}\n\treturn command\n}\n''',
)

# Raw Task git subprocesses also need the no-console policy.
replace_once(
    "app/internal/agentstate/tasks.go",
    '''\tcommand := exec.Command("git", args...)\n\tcommand.Dir = cwd\n''',
    '''\tcommand := exec.Command("git", args...)\n\tconfigureBackgroundCommand(command)\n\tcommand.Dir = cwd\n''',
)
write(
    "app/internal/agentstate/command_platform_windows.go",
    '''//go:build windows\n\npackage agentstate\n\nimport (\n\t"os/exec"\n\t"syscall"\n)\n\nfunc configureBackgroundCommand(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    true,\n\t\tCreationFlags: 0x08000000, // CREATE_NO_WINDOW\n\t}\n}\n''',
)
write(
    "app/internal/agentstate/command_platform_other.go",
    '''//go:build !windows\n\npackage agentstate\n\nimport "os/exec"\n\nfunc configureBackgroundCommand(_ *exec.Cmd) {}\n''',
)

# Updater launch and restart are background launches and should not open a console.
replace_once(
    "app/internal/application/app.go",
    '''\tcommand := exec.Command(tempUpdater, args...)\n\tcommand.Dir = a.rootDir\n''',
    '''\tcommand := exec.Command(tempUpdater, args...)\n\tconfigureDetachedProcess(command)\n\tcommand.Dir = a.rootDir\n''',
)
write(
    "app/internal/application/process_launch_windows.go",
    '''//go:build windows\n\npackage application\n\nimport (\n\t"os/exec"\n\t"syscall"\n)\n\nfunc configureDetachedProcess(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    true,\n\t\tCreationFlags: 0x08000000, // CREATE_NO_WINDOW\n\t}\n}\n''',
)
write(
    "app/internal/application/process_launch_other.go",
    '''//go:build !windows\n\npackage application\n\nimport "os/exec"\n\nfunc configureDetachedProcess(_ *exec.Cmd) {}\n''',
)
replace_once(
    "app/internal/selfupdate/install.go",
    '''\tcommand := exec.Command(executable, args...)\n\tcommand.Dir = filepath.Dir(executable)\n''',
    '''\tcommand := exec.Command(executable, args...)\n\tconfigureRestartCommand(command)\n\tcommand.Dir = filepath.Dir(executable)\n''',
)
write(
    "app/internal/selfupdate/process_launch_windows.go",
    '''//go:build windows\n\npackage selfupdate\n\nimport (\n\t"os/exec"\n\t"syscall"\n)\n\nfunc configureRestartCommand(cmd *exec.Cmd) {\n\tcmd.SysProcAttr = &syscall.SysProcAttr{\n\t\tHideWindow:    true,\n\t\tCreationFlags: 0x08000000, // CREATE_NO_WINDOW\n\t}\n}\n''',
)
write(
    "app/internal/selfupdate/process_launch_other.go",
    '''//go:build !windows\n\npackage selfupdate\n\nimport "os/exec"\n\nfunc configureRestartCommand(_ *exec.Cmd) {}\n''',
)

# 2) PowerShell 5.1 UI Automation: force redirected stdout to UTF-8/no-BOM.
replace_once(
    "app/internal/mcpcore/ui_automation_windows.go",
    '''const uiAutomationPowerShell = `$ErrorActionPreference = 'Stop'\nAdd-Type -AssemblyName UIAutomationClient\n''',
    '''const uiAutomationPowerShellEncoding = `$utf8NoBom = New-Object System.Text.UTF8Encoding($false)\n[Console]::OutputEncoding = $utf8NoBom\n$OutputEncoding = $utf8NoBom\n`\n\nconst uiAutomationPowerShell = `$ErrorActionPreference = 'Stop'\n` + uiAutomationPowerShellEncoding + `Add-Type -AssemblyName UIAutomationClient\n''',
)
replace_once(
    "app/internal/mcpcore/ui_automation_windows_test.go",
    '''func TestUIAutomationPowerShellCollectionPayloadIsJSONSafe(t *testing.T) {\n''',
    '''func TestUIAutomationPowerShellUnicodeJSONIsUTF8(t *testing.T) {\n\tctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)\n\tdefer cancel()\n\tscript := uiAutomationPowerShellEncoding + `[pscustomobject]@{ title='微信情报'; minimize='最小化'; close='关闭' } | ConvertTo-Json -Compress`\n\tcmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(script))\n\tconfigureCommand(cmd)\n\toutput, err := cmd.Output()\n\tif ctx.Err() != nil {\n\t\tt.Fatal("PowerShell Unicode regression test timed out")\n\t}\n\tif err != nil {\n\t\tt.Fatalf("PowerShell Unicode regression test failed: %v: %s", err, strings.TrimSpace(string(output)))\n\t}\n\tvar payload struct {\n\t\tTitle    string `json:"title"`\n\t\tMinimize string `json:"minimize"`\n\t\tClose    string `json:"close"`\n\t}\n\tif err := json.Unmarshal(output, &payload); err != nil {\n\t\tt.Fatalf("decode PowerShell Unicode result: %v: %q", err, string(output))\n\t}\n\tif payload.Title != "微信情报" || payload.Minimize != "最小化" || payload.Close != "关闭" {\n\t\tt.Fatalf("PowerShell Unicode output was corrupted: %#v, raw=%q", payload, string(output))\n\t}\n}\n\nfunc TestUIAutomationPowerShellCollectionPayloadIsJSONSafe(t *testing.T) {\n''',
)

# 3) Catalog generation: one-time server identity bump plus fallback diagnostics on a stable tool.
replace_once(
    "app/cmd/mcp-core/main.go",
    '''\t\tName:                    "mcp-devdesk-go-core",\n''',
    '''\t\tName:                    "mcp-devdesk-go-core-v013-catalog2",\n''',
)
replace_once(
    "app/internal/mcpcore/compatibility_tools.go",
    '''\t\t\t"sseReplaySupported":  true,\n\t\t}, nil\n''',
    '''\t\t\t"sseReplaySupported":         true,\n\t\t\t"toolCatalogGeneration":        "v013-catalog2",\n\t\t\t"advertisedToolCount":          len(s.tools),\n\t\t\t"screenCaptureProbeAdvertised": s.isToolAdvertised("screen_capture_probe"),\n\t\t\t"legacyListSymbolsAdvertised":  s.isToolAdvertised("list_symbols"),\n\t\t}, nil\n''',
)
replace_once(
    "app/internal/mcpcore/compatibility_tools.go",
    '''func (s *Server) currentDefaultCWD() string {\n''',
    '''func (s *Server) isToolAdvertised(name string) bool {\n\tfor _, tool := range s.tools {\n\t\tif tool.Name == name {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n\nfunc (s *Server) currentDefaultCWD() string {\n''',
)

# Strengthen the built-product catalog smoke: a fresh initialize must advertise the new identity,
# probe must be present, and retired list_symbols must stay out of tools/list.
replace_once(
    "tools/smoke-mcp-core-catalog.ps1",
    '''        if (-not $session) { throw "[$Mode] core did not initialize" }\n\n        $listed = Send-McpRequest''',
    '''        if (-not $session) { throw "[$Mode] core did not initialize" }\n        $serverName = [string]$init.Body.result.serverInfo.name\n        if ($serverName -ne "mcp-devdesk-go-core-v013-catalog2") {\n            throw "[$Mode] unexpected server identity: $serverName"\n        }\n\n        $listed = Send-McpRequest''',
)
replace_once(
    "tools/smoke-mcp-core-catalog.ps1",
    '''        $toolCount = [int]$info.Body.result.structuredContent.toolCount\n        Write-Host "mode=$Mode toolsListCount=$($names.Count) serverToolCount=$toolCount"\n''',
    '''        $toolCount = [int]$info.Body.result.structuredContent.toolCount\n        $environment = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 4; method = "tools/call"; params = @{ name = "check_exec_environment"; arguments = @{} } }\n        $environmentData = $environment.Body.result.structuredContent\n        if ([string]$environmentData.toolCatalogGeneration -ne "v013-catalog2") { throw "[$Mode] catalog generation fallback missing" }\n        if (-not [bool]$environmentData.screenCaptureProbeAdvertised) { throw "[$Mode] fallback says probe is not advertised" }\n        if ([bool]$environmentData.legacyListSymbolsAdvertised) { throw "[$Mode] fallback says list_symbols is still advertised" }\n        Write-Host "mode=$Mode toolsListCount=$($names.Count) serverToolCount=$toolCount catalog=$serverName"\n''',
)

# Version and release notes.
replace_once(
    "app/internal/buildinfo/version.go",
    'const Version = "0.13.0-beta.4"',
    'const Version = "0.13.0-beta.5"',
)
for doc in ("docs/V013_PREVIEW.md", "docs/ROADMAP.md"):
    p = ROOT / doc
    text = p.read_text(encoding="utf-8")
    text = text.replace("0.13.0-beta.4", "0.13.0-beta.5")
    p.write_text(text, encoding="utf-8", newline="\n")

preview = ROOT / "docs/V013_PREVIEW.md"
text = preview.read_text(encoding="utf-8")
marker = "## Beta 4：生产运行时工具目录与 UIA 集合修复\n"
notes = '''## Beta 5：后台无黑框、UIA UTF-8 与 Connector 目录代际修复\n\n- Windows 后台子进程统一补齐 `CREATE_NO_WINDOW`：覆盖 MCP Core/Cloudflare/OpenAI tunnel 进程、端口/进程探测、Task Git、Updater、自动重启和桌面启动检查；继续保留显式可见模式，不影响需要用户交互的流程。\n- UI Automation PowerShell 5.1 stdout 固定为 UTF-8 no-BOM，并新增真实 Unicode JSON 回归，避免中文窗口标题和“最小化/关闭”等控件名出现 mojibake。\n- MCP Core serverInfo 做一次性 catalog identity 升级为 `mcp-devdesk-go-core-v013-catalog2`，帮助会缓存旧 tools/list 的 Connector 建立新的工具目录代际。\n- `check_exec_environment` 增加目录诊断兜底：公开 catalog generation、实际广告工具数、`screen_capture_probe` 是否被服务端广告、旧 `list_symbols` 是否仍在 tools/list；即使第三方 Connector 自身缓存异常也可以判定问题所在层。\n- 正式 built-core smoke 继续要求 active 52 / window 52 / desktop 55，并额外断言新 server identity、probe 存在、`list_symbols` 不再广告以及 fallback 目录状态一致。\n\n'''
if marker not in text:
    raise SystemExit("Beta 4 marker missing from V013_PREVIEW.md")
text = text.replace(marker, notes + marker, 1)
preview.write_text(text, encoding="utf-8", newline="\n")

print("Beta 5 source migration applied")
