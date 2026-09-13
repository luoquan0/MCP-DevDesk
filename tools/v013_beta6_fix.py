from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected text not found in {path}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")


# Windows command termination: make repeated termination idempotent and never
# surface localized taskkill output through MCP errors.
Path("app/internal/mcpcore/command_platform_windows.go").write_text(r'''//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func shellCommand(commandLine string) (string, []string) {
	return "cmd.exe", []string{"/d", "/s", "/c", commandLine}
}

func terminateCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	taskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	taskkill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := taskkill.Run(); err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return fmt.Errorf("terminate process tree: taskkill failed: %v; fallback kill: %w", err, killErr)
		}
	}
	return nil
}
''', encoding="utf-8")

replace_once(
    "app/internal/mcpcore/command_tools.go",
    '''func (m *commandManager) kill(args killSessionArgs) (map[string]any, error) {
\tsession, err := m.get(args.SessionID)
\tif err != nil {
\t\treturn nil, err
\t}
\tsession.mu.RLock()
\trunning := session.running
\tcmd := session.cmd
\tcancel := session.cancel
\tsession.mu.RUnlock()
\tif !running {
\t\treturn map[string]any{"sessionId": args.SessionID, "terminated": false, "message": "session already completed"}, nil
\t}
\tif cancel != nil {
\t\tcancel()
\t}
\tif err := terminateCommand(cmd); err != nil {
\t\treturn nil, err
\t}
\treturn map[string]any{"sessionId": args.SessionID, "terminated": true}, nil
}
''',
    '''func (m *commandManager) kill(args killSessionArgs) (map[string]any, error) {
\tsession, err := m.get(args.SessionID)
\tif err != nil {
\t\treturn nil, err
\t}
\tsession.mu.RLock()
\trunning := session.running
\tcmd := session.cmd
\tcancel := session.cancel
\tsession.mu.RUnlock()
\tif !running {
\t\treturn map[string]any{"sessionId": args.SessionID, "terminated": false, "completed": true, "message": "session already completed"}, nil
\t}
\t// CommandContext invokes cmd.Cancel, which is already wired to terminateCommand.
\t// Do not call terminateCommand a second time after cancel: on Windows the
\t// second taskkill races with Wait and used to surface localized "not found"
\t// output as an MCP error even though the session had already stopped.
\tif cancel != nil {
\t\tcancel()
\t} else if err := terminateCommand(cmd); err != nil {
\t\treturn nil, err
\t}
\twaitMS := args.WaitMS
\tif waitMS <= 0 {
\t\twaitMS = 2000
\t}
\ttimer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
\tdefer timer.Stop()
\tselect {
\tcase <-session.done:
\t\treturn map[string]any{"sessionId": args.SessionID, "terminated": true, "completed": true}, nil
\tcase <-timer.C:
\t\treturn map[string]any{"sessionId": args.SessionID, "terminated": true, "completed": false}, nil
\t}
}
''',
)

# Shared no-pixel probe policy plus a compatibility surface that lives inside an
# already-stable tool schema (check_exec_environment).
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '''type screenCaptureArgs struct {
\tMaxWidth int `json:"maxWidth,omitempty"`
}

func screenTools() []Tool {
''',
    '''type screenCaptureArgs struct {
\tMaxWidth int `json:"maxWidth,omitempty"`
}

func screenCaptureProbePolicy() map[string]any {
\treturn map[string]any{
\t\t"captureActive":          false,
\t\t"policy":                 "explicit-opt-in-fail-closed",
\t\t"instanceScoped":         true,
\t\t"methods":                []string{"PrintWindow(PW_RENDERFULLCONTENT)", "PrintWindow", "WindowDC"},
\t\t"stateChangingFallbacks": false,
\t\t"windowsGraphicsCapture": "not-enabled-in-preview-until-real-machine-compatibility-validation",
\t}
}

func (s *Server) screenCaptureProbeCompatibility() map[string]any {
\tresult := screenCaptureProbePolicy()
\tresult["enabled"] = s.screenCaptureEnabled
\tresult["advertised"] = s.isToolAdvertised("screen_capture_probe")
\tresult["connectorFallback"] = "check_exec_environment.screenCaptureProbe"
\treturn result
}

func screenTools() []Tool {
''',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '''\tcase "screen_capture_probe":
\t\twindowArg, _ := arguments["window"].(string)
\t\tresult := map[string]any{
\t\t\t"captureActive":          false,
\t\t\t"policy":                 "explicit-opt-in-fail-closed",
\t\t\t"instanceScoped":         true,
\t\t\t"methods":                []string{"PrintWindow(PW_RENDERFULLCONTENT)", "PrintWindow", "WindowDC"},
\t\t\t"stateChangingFallbacks": false,
\t\t\t"windowsGraphicsCapture": "not-enabled-in-preview-until-real-machine-compatibility-validation",
\t\t}
''',
    '''\tcase "screen_capture_probe":
\t\twindowArg, _ := arguments["window"].(string)
\t\tresult := screenCaptureProbePolicy()
''',
)
replace_once(
    "app/internal/mcpcore/compatibility_tools.go",
    '''\t\t\t"toolCatalogGeneration":        "v013-catalog2",
\t\t\t"advertisedToolCount":          len(s.tools),
\t\t\t"screenCaptureProbeAdvertised": s.isToolAdvertised("screen_capture_probe"),
\t\t\t"legacyListSymbolsAdvertised":  s.isToolAdvertised("list_symbols"),
''',
    '''\t\t\t"toolCatalogGeneration":        "v013-catalog3",
\t\t\t"advertisedToolCount":          len(s.tools),
\t\t\t"screenCaptureProbeAdvertised": s.isToolAdvertised("screen_capture_probe"),
\t\t\t"screenCaptureProbe":           s.screenCaptureProbeCompatibility(),
\t\t\t"legacyListSymbolsAdvertised":  s.isToolAdvertised("list_symbols"),
''',
)
replace_once(
    "app/internal/mcpcore/compatibility_tools.go",
    'Description: "Return runtime, workspace, file-scope, permission, networking, and command-session limits.",',
    'Description: "Return runtime, workspace, file-scope, permission, networking, command-session limits, and a cache-safe Screen Vision probe fallback.",',
)

# New Windows regression: terminateCommand must be safe to invoke again after a
# process has already been reaped.
Path("app/internal/mcpcore/command_platform_windows_test.go").write_text(r'''//go:build windows

package mcpcore

import (
	"os/exec"
	"testing"
)

func TestTerminateCommandIsIdempotentAfterWait(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "ping 127.0.0.1 -n 31 >nul")
	configureCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	if err := terminateCommand(cmd); err != nil {
		t.Fatalf("first terminate: %v", err)
	}
	_ = cmd.Wait()
	if err := terminateCommand(cmd); err != nil {
		t.Fatalf("repeated terminate after Wait must be idempotent: %v", err)
	}
}
''', encoding="utf-8")

Path("app/internal/mcpcore/beta6_compatibility_test.go").write_text(r'''package mcpcore

import (
	"runtime"
	"testing"
)

func TestCheckExecEnvironmentIncludesScreenProbeCompatibility(t *testing.T) {
	server, err := New(Options{
		Workspace:            t.TempDir(),
		PermissionMode:       "dangerous",
		ToolProfile:          "full",
		ScreenCaptureEnabled: true,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer server.Close()
	result, err := server.executeCompatibilityTool("check_exec_environment", map[string]any{})
	if err != nil {
		t.Fatalf("check_exec_environment: %v", err)
	}
	if got := result["toolCatalogGeneration"]; got != "v013-catalog3" {
		t.Fatalf("catalog generation = %v, want v013-catalog3", got)
	}
	probe, ok := result["screenCaptureProbe"].(map[string]any)
	if !ok {
		t.Fatalf("screenCaptureProbe compatibility payload missing: %#v", result["screenCaptureProbe"])
	}
	if got := probe["connectorFallback"]; got != "check_exec_environment.screenCaptureProbe" {
		t.Fatalf("connector fallback = %v", got)
	}
	if got := probe["stateChangingFallbacks"]; got != false {
		t.Fatalf("stateChangingFallbacks = %v, want false", got)
	}
	if runtime.GOOS == "windows" {
		if got := probe["advertised"]; got != true {
			t.Fatalf("probe advertised = %v, want true", got)
		}
	}
}
''', encoding="utf-8")

replace_once("app/internal/buildinfo/version.go", 'const Version = "0.13.0-beta.5"', 'const Version = "0.13.0-beta.6"')
replace_once("app/cmd/mcp-core/main.go", 'Name:                    "mcp-devdesk-go-core-v013-catalog2",', 'Name:                    "mcp-devdesk-go-core-v013-catalog3",')

# Product-binary smoke: keep mode-aware counts, assert the cache-safe fallback,
# and kill a real long-running command through JSON-RPC on the desktop run.
replace_once("tools/smoke-mcp-core-catalog.ps1", 'mcp-devdesk-go-core-v013-catalog2', 'mcp-devdesk-go-core-v013-catalog3')
replace_once("tools/smoke-mcp-core-catalog.ps1", 'v013-catalog2', 'v013-catalog3')
replace_once(
    "tools/smoke-mcp-core-catalog.ps1",
    '''        if (-not [bool]$environmentData.screenCaptureProbeAdvertised) { throw "[$Mode] fallback says probe is not advertised" }
        if ([bool]$environmentData.legacyListSymbolsAdvertised) { throw "[$Mode] fallback says list_symbols is still advertised" }
''',
    '''        if (-not [bool]$environmentData.screenCaptureProbeAdvertised) { throw "[$Mode] fallback says probe is not advertised" }
        if (-not $environmentData.screenCaptureProbe) { throw "[$Mode] cache-safe probe payload missing" }
        if ([string]$environmentData.screenCaptureProbe.connectorFallback -ne "check_exec_environment.screenCaptureProbe") { throw "[$Mode] cache-safe probe fallback identity missing" }
        if ([bool]$environmentData.screenCaptureProbe.stateChangingFallbacks) { throw "[$Mode] cache-safe probe unexpectedly allows state-changing fallback" }
        if ([bool]$environmentData.legacyListSymbolsAdvertised) { throw "[$Mode] fallback says list_symbols is still advertised" }
''',
)
replace_once(
    "tools/smoke-mcp-core-catalog.ps1",
    '''        foreach ($name in $Forbidden) {
            if ($names -contains $name) { throw "[$Mode] forbidden tool advertised: $name" }
        }
''',
    '''        foreach ($name in $Forbidden) {
            if ($names -contains $name) { throw "[$Mode] forbidden tool advertised: $name" }
        }
        if ($Mode -eq "desktop") {
            $execCall = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 5; method = "tools/call"; params = @{ name = "exec_command"; arguments = @{ command = "powershell.exe"; args = @("-NoLogo", "-NoProfile", "-Command", "Start-Sleep -Seconds 30"); waitMillis = 100 } } }
            if ([bool]$execCall.Body.result.isError) { throw "[desktop] exec_command failed: $($execCall.Body.result.content[0].text)" }
            $commandSession = [string]$execCall.Body.result.structuredContent.sessionId
            if (-not $commandSession) { throw "[desktop] exec_command did not return a session id" }
            $killCall = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 6; method = "tools/call"; params = @{ name = "kill_session"; arguments = @{ sessionId = $commandSession; wait_ms = 5000 } } }
            if ([bool]$killCall.Body.result.isError) { throw "[desktop] kill_session failed: $($killCall.Body.result.content[0].text)" }
            $killData = $killCall.Body.result.structuredContent
            if (-not [bool]$killData.terminated -or -not [bool]$killData.completed) { throw "[desktop] kill_session did not complete cleanly" }
            $readCall = Send-McpRequest -Client $client -Uri $uri -Session $session -Payload @{ jsonrpc = "2.0"; id = 7; method = "tools/call"; params = @{ name = "read_output"; arguments = @{ sessionId = $commandSession } } }
            if ([bool]$readCall.Body.result.isError) { throw "[desktop] read_output after kill failed" }
            if ([bool]$readCall.Body.result.structuredContent.running) { throw "[desktop] command still running after kill_session" }
        }
''',
)

# Docs/version.
replace_once("docs/ROADMAP.md", '当前预览开发版本：`0.13.0-beta.5`', '当前预览开发版本：`0.13.0-beta.6`')
replace_once("docs/V013_PREVIEW.md", '# MCP DevDesk v0.13.0-beta.5', '# MCP DevDesk v0.13.0-beta.6')
replace_once("docs/V013_PREVIEW.md", '0.13.0-beta.5 是独立 prerelease。', '0.13.0-beta.6 是独立 prerelease。')
replace_once(
    "docs/V013_PREVIEW.md",
    '## Beta 5：后台无黑框、UIA UTF-8 与 Connector 目录代际修复\n',
    '''## Beta 6：命令终止幂等与 Connector Probe 兼容入口

- 修复 Windows `kill_session` 的双重终止竞态：手动终止只触发一次 CommandContext 取消，由既有 `cmd.Cancel` 负责终止进程树，并可等待会话收敛后返回 `completed=true`。
- Windows `terminateCommand` 对已结束进程幂等；`taskkill` 非零退出时不再把本地化 stdout/stderr 拼进 MCP 错误，fallback `Process.Kill` 的 `os.ErrProcessDone` 被视为成功，从而消除乱码与虚假的 exit 128 终止错误。
- `check_exec_environment` 现在直接携带 `screenCaptureProbe` 无像素策略 payload。即使第三方 Connector 继续缓存旧 tools/list、看不到独立 `screen_capture_probe` Schema，也能通过稳定旧工具读取 fail-closed 策略、后端方法、是否广告 probe 以及兼容入口标识。
- MCP Core catalog identity 升级到 `mcp-devdesk-go-core-v013-catalog3`；最终 built-core smoke 除三种 Screen Vision 模式目录契约外，还会启动真实长命令并通过 JSON-RPC `kill_session` 验证干净终止。

## Beta 5：后台无黑框、UIA UTF-8 与 Connector 目录代际修复
''',
)
