from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8", newline="\n")


def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise RuntimeError(f"missing marker in {path}: {old[:120]!r}")
    write(path, text.replace(old, new, 1))

# Register the five code-navigation surfaces without replacing the mature Agent Runtime.
replace_once(
    "app/internal/mcpcore/server.go",
    "\ttools = append(tools, gitTools()...)\n\ttasks := taskTools()\n",
    "\ttools = append(tools, gitTools()...)\n\ttools = append(tools, codeNavigationTools()...)\n\ttasks := taskTools()\n",
)
replace_once(
    "app/internal/mcpcore/file_tools.go",
    "\tcase \"git_status\", \"git_diff\", \"git_log\", \"git_show\", \"git_worktrees\":\n\t\treturn s.executeGitTool(name, arguments)\n",
    "\tcase \"git_status\", \"git_diff\", \"git_log\", \"git_show\", \"git_worktrees\":\n\t\treturn s.executeGitTool(name, arguments)\n\tcase \"list_symbols\", \"document_symbols\", \"workspace_symbols\", \"find_definition\", \"find_references\":\n\t\treturn s.executeCodeNavigationTool(name, arguments)\n",
)

# Windows runners can expose the same temp directory through short/long path spellings.
# Canonicalize the relative-path base before the workspace containment check.
replace_once(
    "app/internal/mcpcore/file_tools.go",
    "\t} else {\n\t\ttarget = filepath.Join(s.currentDefaultCWD(), value)\n\t}\n\ttarget, err = filepath.Abs(target)\n",
    "\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tif absolute, absErr := filepath.Abs(base); absErr == nil {\n\t\t\tbase = filepath.Clean(absolute)\n\t\t}\n\t\tif evaluated, evalErr := filepath.EvalSymlinks(base); evalErr == nil {\n\t\t\tbase = filepath.Clean(evaluated)\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n\ttarget, err = filepath.Abs(target)\n",
)

# Screen Vision becomes instance-scoped. Every instance already owns its own config.json;
# stop redirecting additional instances to the primary config.
replace_once(
    "app/internal/process/manager.go",
    '''\t\t\t// Screen Vision is a machine-wide privacy boundary. Additional MCP\n\t\t\t// instances keep their own operational config, but every Go core must\n\t\t\t// read the primary DevDesk Screen Vision policy so a locked window\n\t\t\t// cannot silently remain broader on another connected instance.\n\t\t\t"--screen-vision-config", screenVisionConfigPath(dataDir),\n''',
    '''\t\t\t// v0.13: Screen Vision is scoped to this MCP instance. Each instance\n\t\t\t// owns an independent config.json and the Go core fails closed if that\n\t\t\t// instance policy cannot be loaded.\n\t\t\t"--screen-vision-config", screenVisionConfigPath(dataDir),\n''',
)
text = read("app/internal/process/manager.go")
text, count = re.subn(
    r'''func screenVisionConfigPath\(dataDir string\) string \{.*?\n\}\n\nfunc mcpEnvironment''',
    '''func screenVisionConfigPath(dataDir string) string {\n\treturn filepath.Join(filepath.Clean(dataDir), "config.json")\n}\n\nfunc mcpEnvironment''',
    text,
    count=1,
    flags=re.S,
)
if count != 1:
    raise RuntimeError("failed to scope Screen Vision config per instance")
write("app/internal/process/manager.go", text)

# Management API fields for per-instance Screen Vision policy. The existing config store
# already persists and validates these fields, so no second security store is introduced.
replace_once(
    "app/internal/model/types.go",
    '\tLoggingEnabled       bool          `json:"loggingEnabled"`\n\tDataDirectory',
    '\tLoggingEnabled       bool          `json:"loggingEnabled"`\n\tScreenCaptureEnabled   bool          `json:"screenCaptureEnabled"`\n\tScreenVisionMode       string        `json:"screenVisionMode,omitempty"`\n\tDataDirectory',
)
replace_once(
    "app/internal/model/types.go",
    '\tLoggingEnabled *bool  `json:"loggingEnabled"`\n}\n\ntype MCPInstanceUpdateRequest',
    '\tLoggingEnabled       *bool  `json:"loggingEnabled"`\n\tScreenCaptureEnabled   *bool  `json:"screenCaptureEnabled"`\n\tScreenVisionMode       string `json:"screenVisionMode,omitempty"`\n}\n\ntype MCPInstanceUpdateRequest',
)
replace_once(
    "app/internal/model/types.go",
    '\tLoggingEnabled    *bool   `json:"loggingEnabled"`\n\tConfirmCoreSwitch bool',
    '\tLoggingEnabled       *bool   `json:"loggingEnabled"`\n\tScreenCaptureEnabled   *bool   `json:"screenCaptureEnabled"`\n\tScreenVisionMode       *string `json:"screenVisionMode"`\n\tConfirmCoreSwitch bool',
)

# Create/update instance config from those API fields.
replace_once(
    "app/internal/application/instances.go",
    '''\tif request.LoggingEnabled != nil {\n\t\tcfg.LoggingEnabled = *request.LoggingEnabled\n\t}\n\tnormalizeInstanceConfig(&cfg)\n''',
    '''\tif request.LoggingEnabled != nil {\n\t\tcfg.LoggingEnabled = *request.LoggingEnabled\n\t}\n\tif request.ScreenCaptureEnabled != nil {\n\t\tcfg.ScreenCaptureEnabled = *request.ScreenCaptureEnabled\n\t}\n\tif strings.TrimSpace(request.ScreenVisionMode) != "" {\n\t\tcfg.ScreenVisionMode = strings.TrimSpace(request.ScreenVisionMode)\n\t}\n\tnormalizeInstanceConfig(&cfg)\n''',
)
replace_once(
    "app/internal/application/instances.go",
    '''\tif request.LoggingEnabled != nil {\n\t\tnewCfg.LoggingEnabled = *request.LoggingEnabled\n\t}\n\tnormalizeInstanceConfig(&newCfg)\n''',
    '''\tif request.LoggingEnabled != nil {\n\t\tnewCfg.LoggingEnabled = *request.LoggingEnabled\n\t}\n\tif request.ScreenCaptureEnabled != nil {\n\t\tnewCfg.ScreenCaptureEnabled = *request.ScreenCaptureEnabled\n\t}\n\tif request.ScreenVisionMode != nil {\n\t\tnewCfg.ScreenVisionMode = strings.TrimSpace(*request.ScreenVisionMode)\n\t}\n\tnormalizeInstanceConfig(&newCfg)\n''',
)

# Add the fields to both instance view constructors by extending every matching literal tail.
text = read("app/internal/application/instances.go")
text = text.replace(
    "LoggingEnabled: cfg.LoggingEnabled,",
    "LoggingEnabled: cfg.LoggingEnabled, ScreenCaptureEnabled: cfg.ScreenCaptureEnabled, ScreenVisionMode: cfg.ScreenVisionMode,",
)
write("app/internal/application/instances.go", text)

# A read-only Screen Vision compatibility probe makes the active safety policy observable.
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    "\treturn []Tool{\n\t\t{\n\t\t\tName:        \"screen_list_windows\"",
    "\treturn []Tool{\n\t\t{\n\t\t\tName:        \"screen_capture_probe\",\n\t\t\tTitle:       \"Screen Vision Compatibility Probe\",\n\t\t\tDescription: \"Report the current Screen Vision capture policy and target state without taking pixels or changing window state. Use this before relying on background/minimized capture.\",\n\t\t\tInputSchema: map[string]any{\"type\": \"object\", \"properties\": map[string]any{\"window\": map[string]any{\"type\": \"string\"}}, \"additionalProperties\": false},\n\t\t},\n\t\t{\n\t\t\tName:        \"screen_list_windows\"",
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    "\tswitch name {\n\tcase \"screen_list_windows\":",
    '''\tswitch name {\n\tcase "screen_capture_probe":\n\t\twindowArg, _ := arguments["window"].(string)\n\t\tresult := map[string]any{\n\t\t\t"captureActive": false,\n\t\t\t"policy": "explicit-opt-in-fail-closed",\n\t\t\t"instanceScoped": true,\n\t\t\t"methods": []string{"PrintWindow(PW_RENDERFULLCONTENT)", "PrintWindow", "WindowDC", "existing v0.12.34 compatibility path"},\n\t\t\t"windowsGraphicsCapture": "not-enabled-in-preview-until-real-machine-compatibility-validation",\n\t\t}\n\t\tif strings.TrimSpace(windowArg) != "" {\n\t\t\twindows, err := platformListScreenWindowsForVision()\n\t\t\tif err != nil { return nil, err }\n\t\t\twindow, err := resolveScreenWindow(windows, windowArg)\n\t\t\tif err != nil { return nil, err }\n\t\t\tresult["window"] = window\n\t\t\tresult["minimized"] = window.Minimized\n\t\t}\n\t\treturn result, nil\n\tcase "screen_list_windows":''',
)
replace_once(
    "app/internal/mcpcore/file_tools.go",
    '\tcase "screen_list_windows", "screen_get_active_window", "screen_capture_window", "screen_capture_active_window", "screen_capture_desktop":',
    '\tcase "screen_capture_probe", "screen_list_windows", "screen_get_active_window", "screen_capture_window", "screen_capture_active_window", "screen_capture_desktop":',
)

# Policy must permit the probe in every enabled Screen Vision mode; it never captures pixels.
policy = read("app/internal/mcpcore/screen_policy.go")
policy = policy.replace('case "screen_list_windows":', 'case "screen_capture_probe":\n\t\treturn nil\n\tcase "screen_list_windows":', 1)
write("app/internal/mcpcore/screen_policy.go", policy)

# Tool-count invariants: 42 base Agent Runtime tools + 5 code-navigation tools.
server_test = read("app/internal/mcpcore/server_test.go")
server_test = server_test.replace("len(listResult.Result.Tools) != 42", "len(listResult.Result.Tools) != 47")
server_test = server_test.replace("len(seen) != 42", "len(seen) != 47")
write("app/internal/mcpcore/server_test.go", server_test)

# Preview docs: fix stale stable version/online-update flags and state true implementation boundaries.
roadmap = read("docs/ROADMAP.md")
roadmap = re.sub(r"当前稳定开发版本：`[^`]+`。", "当前预览开发版本：`0.13.0-beta.1`（正式稳定版仍为 `0.12.34`）。", roadmap, count=1)
roadmap = roadmap.replace("- [ ] 自动更新", "- [x] 自动更新")
if "## M6：0.13 AI Coding Workspace Preview" not in roadmap:
    roadmap += '''\n\n## M6：0.13 AI Coding Workspace Preview\n\n- [x] 持久 Agent Task / Job 与桌面任务中心\n- [x] 每任务独立 Git Worktree，人工 Accept / Reject，基础工作区变化时 fail closed\n- [x] 结构化 checks_run 自动测试 / 检查 / 格式化\n- [x] OpenAI Secure Tunnel / Cloudflare / Local 三种主实例连接模式\n- [x] 代码导航 Preview：list_symbols / document_symbols / workspace_symbols / find_definition / find_references\n- [x] 每 MCP 实例独立 Screen Vision 配置文件与管理 API 字段\n- [x] Screen Vision 兼容性探针，明确报告当前捕获策略且不主动截图\n- [x] Windows Runner 短路径 / 长路径规范化，避免合法工作区误判越界\n- [ ] Windows Graphics Capture 原生后端：需完成 VMware、Chromium、WebView2、多 DPI / 多显示器实机兼容性矩阵后再启用，不在 Preview 中伪装已完成\n- [ ] Authenticode：构建流程可产出安装/便携包，但正式签名需要代码签名证书\n- [ ] macOS / Linux：0.13 不做\n'''
write("docs/ROADMAP.md", roadmap)

write("docs/V013_PREVIEW.md", '''# MCP DevDesk v0.13.0-beta.1\n\n0.13 Preview 把 MCP DevDesk 从 MCP 管理器推进为可恢复、可审核的 Windows AI Coding Workspace。\n\n## 本次可测试内容\n\n- Agent Runtime：持久 Task ID / Job ID、隔离 Worktree、重启后状态恢复、后台命令与结构化检查。\n- 人工审核：AI 只能 `task_finish`，不能自行接受自己的修改；Accept / Reject 由本机 DevDesk UI 控制。\n- 代码导航：新增 `list_symbols`、`document_symbols`、`workspace_symbols`、`find_definition`、`find_references`。Preview 会探测已安装 language-server，但当前结果明确标记 `lexical-fallback`，不把未接入的 LSP 会话伪装成完整 LSP。\n- OpenAI Secure Tunnel：主实例可在 Cloudflare / OpenAI / Local 之间选择；OpenAI API Key 使用现有 Windows 加密 secrets，sidecar token 仅允许 loopback。\n- Screen Vision：改为每 MCP 实例读取自己的 `config.json`，新增无截图的 `screen_capture_probe` 兼容性探针。现有 0.12.34 捕获行为保留作为兼容基线；Windows Graphics Capture 原生后端暂不在未完成实机矩阵前启用。\n- 文档状态同步：稳定版仍为 0.12.34，0.13.0-beta.1 是独立 prerelease。\n\n## 已知 Preview 边界\n\n- Windows Graphics Capture 原生后端仍需要 VMware / Chromium / WebView2 / 多显示器实机验证；本版本不会宣称未验证能力已完成。\n- Authenticode 需要实际代码签名证书；本测试版可能触发 Windows SmartScreen。\n- OpenAI Secure Tunnel Preview 使用用户提供的官方 `tunnel-client.exe`，当前不捆绑第三方二进制。\n- macOS / Linux 不属于 0.13 范围。\n''')

print("v0.13 supplemental changes applied")
