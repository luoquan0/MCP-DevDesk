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
        raise RuntimeError(f"missing marker in {path}: {old[:180]!r}")
    write(path, text.replace(old, new, 1))


# Frontend API contract for per-instance Screen Vision.
replace_once(
    "frontend/src/types/api.ts",
    '''  loggingEnabled: boolean;\n  dataDirectory: string;''',
    '''  loggingEnabled: boolean;\n  screenCaptureEnabled: boolean;\n  screenCaptureMode?: ScreenCaptureMode;\n  dataDirectory: string;''',
)
replace_once(
    "frontend/src/types/api.ts",
    '''  loggingEnabled?: boolean;\n}\n\nexport interface MCPInstanceUpdateRequest {''',
    '''  loggingEnabled?: boolean;\n  screenCaptureEnabled?: boolean;\n  screenCaptureMode?: ScreenCaptureMode;\n}\n\nexport interface MCPInstanceUpdateRequest {''',
)
replace_once(
    "frontend/src/types/api.ts",
    '''  loggingEnabled?: boolean;\n  confirmCoreSwitch?: boolean;''',
    '''  loggingEnabled?: boolean;\n  screenCaptureEnabled?: boolean;\n  screenCaptureMode?: ScreenCaptureMode;\n  confirmCoreSwitch?: boolean;''',
)

page = read("frontend/src/pages/InstancesPage.vue")
page = page.replace(
    '''  loggingEnabled: true,\n});''',
    '''  loggingEnabled: true,\n  screenCaptureEnabled: false,\n  screenCaptureMode: "active" as "active" | "desktop",\n});''',
    1,
)
page = page.replace(
    '''  loggingEnabled: true,\n});''',
    '''  loggingEnabled: true,\n  screenCaptureEnabled: false,\n  screenCaptureMode: "active" as "active" | "desktop",\n});''',
    1,
)
page = page.replace(
    '''  createForm.loggingEnabled = true;\n}''',
    '''  createForm.loggingEnabled = true;\n  createForm.screenCaptureEnabled = false;\n  createForm.screenCaptureMode = "active";\n}''',
    1,
)
page = page.replace(
    '''    loggingEnabled: createForm.loggingEnabled,\n  };''',
    '''    loggingEnabled: createForm.loggingEnabled,\n    screenCaptureEnabled: createForm.coreMode === "go" && createForm.screenCaptureEnabled,\n    screenCaptureMode: createForm.screenCaptureMode,\n  };''',
    1,
)
page = page.replace(
    '''  editForm.loggingEnabled = instance.loggingEnabled;\n}''',
    '''  editForm.loggingEnabled = instance.loggingEnabled;\n  editForm.screenCaptureEnabled = Boolean(instance.screenCaptureEnabled);\n  editForm.screenCaptureMode = instance.screenCaptureMode === "desktop" ? "desktop" : "active";\n}''',
    1,
)
page = page.replace(
    '''    loggingEnabled: editForm.loggingEnabled,\n    confirmCoreSwitch,''',
    '''    loggingEnabled: editForm.loggingEnabled,\n    screenCaptureEnabled: editForm.coreMode === "go" && editForm.screenCaptureEnabled,\n    screenCaptureMode: editForm.screenCaptureMode,\n    confirmCoreSwitch,''',
    1,
)
page = page.replace(
    '''          <label class="field"><span>工具配置</span><select v-model="createForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>\n        </div>''',
    '''          <label class="field"><span>工具配置</span><select v-model="createForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>\n          <label class="field"><span>Screen Vision 范围</span><select v-model="createForm.screenCaptureMode" :disabled="createForm.coreMode !== 'go' || !createForm.screenCaptureEnabled"><option value="active">仅当前窗口</option><option value="desktop">整个桌面 / 可读取窗口</option></select><small>指定窗口锁定仍在主安全设置中完成；此处先控制每实例是否拥有视觉能力。</small></label>\n        </div>''',
    1,
)
page = page.replace(
    '''          <ToggleSwitch v-model="createForm.allowNetwork" label="允许网络" description="允许该实例的工具访问网络。" />\n        </div>''',
    '''          <ToggleSwitch v-model="createForm.allowNetwork" label="允许网络" description="允许该实例的工具访问网络。" />\n          <ToggleSwitch v-model="createForm.screenCaptureEnabled" :disabled="createForm.coreMode !== 'go' || createForm.permissionMode === 'safe'" label="Screen Vision" description="只授权这个 MCP 实例按所选范围读取屏幕；安全模式和 Python 兼容核心不可用。" />\n        </div>''',
    1,
)
page = page.replace(
    '''          <div><span>Tunnel</span><strong>{{ instance.tunnelId ? '已配置' : '未配置' }}</strong></div>\n        </div>''',
    '''          <div><span>Tunnel</span><strong>{{ instance.tunnelId ? '已配置' : '未配置' }}</strong></div>\n          <div><span>Screen Vision</span><strong>{{ instance.screenCaptureEnabled ? (instance.screenCaptureMode === 'desktop' ? '桌面' : '当前窗口') : '关闭' }}</strong></div>\n        </div>''',
    1,
)
page = page.replace(
    '''            <label class="field"><span>工具配置</span><select v-model="editForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>\n          </div>''',
    '''            <label class="field"><span>工具配置</span><select v-model="editForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>\n            <label class="field"><span>Screen Vision 范围</span><select v-model="editForm.screenCaptureMode" :disabled="editForm.coreMode !== 'go' || !editForm.screenCaptureEnabled"><option value="active">仅当前窗口</option><option value="desktop">整个桌面 / 可读取窗口</option></select><small>配置仅属于当前 MCP 实例，不再继承其他实例的视觉权限。</small></label>\n          </div>''',
    1,
)
page = page.replace(
    '''            <ToggleSwitch v-model="editForm.allowNetwork" label="允许网络" />\n          </div>''',
    '''            <ToggleSwitch v-model="editForm.allowNetwork" label="允许网络" />\n            <ToggleSwitch v-model="editForm.screenCaptureEnabled" :disabled="editForm.coreMode !== 'go' || editForm.permissionMode === 'safe'" label="Screen Vision" description="只为这个实例授权视觉工具。" />\n          </div>''',
    1,
)
write("frontend/src/pages/InstancesPage.vue", page)

# v0.13 beta safety policy: no temporary Z-order reveal for background windows.
capture = read("app/internal/mcpcore/screen_capture_windows.go")
capture = capture.replace("\tvar backgroundRevealErr error\n", "")
pattern = re.compile(r'''\n\t\t// VMware and other compositor-heavy windows commonly report successful.*?\n\t\tif captured == 0 && screenBackgroundRevealRequired\(hwnd, foreground\) \{.*?\n\t\t\}\n''', re.S)
capture, count = pattern.subn(
    '''\n\t\t// v0.13 beta intentionally refuses the old temporary Z-order reveal\n\t\t// fallback. Background capture may use only HWND-owned non-activating\n\t\t// methods until the native Windows Graphics Capture backend has passed\n\t\t// the real-machine compatibility matrix.\n''',
    capture,
    count=1,
)
if count != 1:
    raise RuntimeError("failed to remove temporary background reveal fallback")
capture = capture.replace(
    '''\t\t\t} else if backgroundRevealErr != nil {\n\t\t\t\treturn screenCaptureFrame{}, fmt.Errorf("capture selected background window: %w", backgroundRevealErr)\n\t\t\t} else {''',
    '''\t\t\t} else {''',
    1,
)
write("app/internal/mcpcore/screen_capture_windows.go", capture)

# Minimized windows stay enumerable for diagnostics/selection but beta capture fails
# closed instead of restoring/minimizing the user's window behind their back.
replace_once(
    "app/internal/mcpcore/screen_minimized_windows.go",
    '''\tminimized, _, _ := procIsIconic.Call(window.Handle)\n\tif minimized == 0 {\n\t\treturn platformCaptureScreenWindow(window)\n\t}\n\treturn captureMinimizedScreenWindow(window)\n}''',
    '''\tminimized, _, _ := procIsIconic.Call(window.Handle)\n\tif minimized == 0 {\n\t\treturn platformCaptureScreenWindow(window)\n\t}\n\treturn screenCaptureFrame{}, errors.New("minimized capture is disabled in v0.13 beta until a non-intrusive Windows Graphics Capture backend passes compatibility validation")\n}''',
)

# Align probe text and preview docs with the actual fail-closed policy.
screen_tools = read("app/internal/mcpcore/screen_tools.go")
screen_tools = screen_tools.replace(
    '"methods": []string{"PrintWindow(PW_RENDERFULLCONTENT)", "PrintWindow", "WindowDC", "existing v0.12.34 compatibility path"},',
    '"methods": []string{"PrintWindow(PW_RENDERFULLCONTENT)", "PrintWindow", "WindowDC"},\n\t\t\t"stateChangingFallbacks": false,',
)
write("app/internal/mcpcore/screen_tools.go", screen_tools)

# Windows Git may materialize accepted worktree text as CRLF. This assertion checks
# task isolation/accept semantics, not line-ending policy.
task_test = read("app/internal/mcpcore/task_tools_test.go")
task_test = task_test.replace(
    '''\tif raw, err := os.ReadFile(filepath.Join(repo, "task.txt")); err != nil || string(raw) != "inside task\\n" {\n\t\tt.Fatalf("accepted task content = %q err=%v", raw, err)\n\t}\n''',
    '''\tif raw, err := os.ReadFile(filepath.Join(repo, "task.txt")); err != nil || strings.ReplaceAll(string(raw), "\\r\\n", "\\n") != "inside task\\n" {\n\t\tt.Fatalf("accepted task content = %q err=%v", raw, err)\n\t}\n''',
)
write("app/internal/mcpcore/task_tools_test.go", task_test)

preview = read("docs/V013_PREVIEW.md")
preview = preview.replace(
    "现有 0.12.34 捕获行为保留作为兼容基线；Windows Graphics Capture 原生后端暂不在未完成实机矩阵前启用。",
    "后台窗口仅尝试 PrintWindow / WindowDC 等不改 Z-order 的 HWND 自有路径；最小化窗口直接 fail closed，不再临时恢复。Windows Graphics Capture 原生后端暂不在未完成实机矩阵前启用。",
)
write("docs/V013_PREVIEW.md", preview)

roadmap = read("docs/ROADMAP.md")
roadmap = roadmap.replace(
    "- [x] Screen Vision 兼容性探针，明确报告当前捕获策略且不主动截图",
    "- [x] Screen Vision 兼容性探针；后台/最小化捕获禁止改变 Z-order 或窗口状态，失败时 fail closed",
)
write("docs/ROADMAP.md", roadmap)

print("v0.13 fix4 prepared")
