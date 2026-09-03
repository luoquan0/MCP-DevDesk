from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def write(path: str, text: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8", newline="\n")


def replace_once(path: str, old: str, new: str) -> None:
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected exactly one match, found {count}: {old[:100]!r}")
    write(path, text.replace(old, new, 1))


# Internal tool metadata now carries minimized state so both MCP clients and the
# DevDesk picker can distinguish a minimized app from a tiny title-bar rect.
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '\tActive      bool       `json:"active"`\n}',
    '\tActive      bool       `json:"active"`\n\tMinimized   bool       `json:"minimized"`\n}',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '\t\t\tTitle:       "List Visible Windows",\n\t\t\tDescription: "List visible top-level Windows application windows. Screen Vision is explicit opt-in and this tool never starts continuous recording.",',
    '\t\t\tTitle:       "List App Windows",\n\t\t\tDescription: "List captureable top-level Windows application windows, including minimized apps. Screen Vision is explicit opt-in and this tool never starts continuous recording.",',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '\t\t\tDescription: "Capture one explicitly selected Windows application window on demand and return a PNG image to the MCP client. Nothing is saved to disk.",',
    '\t\t\tDescription: "Capture one explicitly selected Windows application window on demand, including a background or minimized target when Windows allows it, and return a PNG image to the MCP client. Minimized targets are temporarily restored without focus and returned to minimized state. Nothing is saved to disk.",',
)
text = read("app/internal/mcpcore/screen_tools.go")
if text.count("windows, err := platformListScreenWindows()") != 2:
    raise RuntimeError("screen_tools.go: expected two vision window-list call sites")
text = text.replace("windows, err := platformListScreenWindows()", "windows, err := platformListScreenWindowsForVision()")
if text.count("frame, err := platformCaptureScreenWindow(window)") != 2:
    raise RuntimeError("screen_tools.go: expected two capture call sites")
text = text.replace("frame, err := platformCaptureScreenWindow(window)", "frame, err := platformCaptureScreenWindowForVision(window)")
write("app/internal/mcpcore/screen_tools.go", text)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '\t\t\tsort.SliceStable(filtered, func(i, j int) bool {\n\t\t\t\tif filtered[i].Active != filtered[j].Active {\n\t\t\t\t\treturn filtered[i].Active\n\t\t\t\t}\n\t\t\t\treturn strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)\n\t\t\t})',
    '\t\t\tsort.SliceStable(filtered, func(i, j int) bool {\n\t\t\t\tif filtered[i].Active != filtered[j].Active {\n\t\t\t\t\treturn filtered[i].Active\n\t\t\t\t}\n\t\t\t\tif filtered[i].Minimized != filtered[j].Minimized {\n\t\t\t\t\treturn !filtered[i].Minimized\n\t\t\t\t}\n\t\t\t\treturn strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)\n\t\t\t})',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    'return screenWindow{}, fmt.Errorf("no visible window matches %q", value)',
    'return screenWindow{}, fmt.Errorf("no app window matches %q", value)',
)

# Local manager API exposes the same minimized flag and uses the broader vision
# enumeration rather than the active-window-only enumeration.
replace_once(
    "app/internal/mcpcore/screen_public.go",
    "// ListScreenWindows returns only visible top-level window metadata. It never\n// captures pixels and is used by the local DevDesk manager so the user can\n// explicitly choose a target for specified-window Screen Vision mode.",
    "// ListScreenWindows returns captureable top-level app-window metadata, including\n// minimized apps. It never captures pixels and is used by the local DevDesk manager\n// so the user can explicitly choose a target for specified-window Screen Vision mode.",
)
replace_once(
    "app/internal/mcpcore/screen_public.go",
    "windows, err := platformListScreenWindows()",
    "windows, err := platformListScreenWindowsForVision()",
)
replace_once(
    "app/internal/mcpcore/screen_public.go",
    '\t\tif windows[i].Active != windows[j].Active {\n\t\t\treturn windows[i].Active\n\t\t}\n\t\treturn strings.ToLower(windows[i].Title) < strings.ToLower(windows[j].Title)',
    '\t\tif windows[i].Active != windows[j].Active {\n\t\t\treturn windows[i].Active\n\t\t}\n\t\tif windows[i].Minimized != windows[j].Minimized {\n\t\t\treturn !windows[i].Minimized\n\t\t}\n\t\treturn strings.ToLower(windows[i].Title) < strings.ToLower(windows[j].Title)',
)
replace_once(
    "app/internal/mcpcore/screen_public.go",
    "\t\t\tActive: window.Active,\n",
    "\t\t\tActive:    window.Active,\n\t\t\tMinimized: window.Minimized,\n",
)

replace_once(
    "app/internal/model/types.go",
    '\tActive      bool       `json:"active"`\n}',
    '\tActive      bool       `json:"active"`\n\tMinimized   bool       `json:"minimized"`\n}',
)
replace_once(
    "frontend/src/types/api.ts",
    "  active: boolean;\n}",
    "  active: boolean;\n  minimized: boolean;\n}",
)

# Keep policy documentation/tool wording aligned with the widened window list.
replace_once(
    "app/internal/mcpcore/screen_policy.go",
    "// screenshot: the agent may enumerate visible top-level windows and read\n\t\t// them individually, which also covers windows hidden behind others.",
    "// screenshot: the agent may enumerate top-level application windows, including\n\t\t// minimized targets, and read them individually when Windows allows it.",
)
replace_once(
    "app/internal/mcpcore/screen_policy.go",
    'tool.Description = "Capture only the Windows application window selected in MCP DevDesk. Omit window to use the locked target; another window id is rejected. Nothing is saved to disk."',
    'tool.Description = "Capture only the Windows application window selected in MCP DevDesk, including when it is behind another app or minimized. Minimized targets are temporarily restored without focus and minimized again. Omit window to use the locked target; another window id is rejected. Nothing is saved to disk."',
)

# Frontend: show minimized applications, label their state, and explain the
# temporary no-focus restore behavior instead of asking users to restore them.
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '{ id: "window", title: "指定窗口", subtitle: "锁定一个目标，浏览器在前面也读取它", icon: "lock" },',
    '{ id: "window", title: "指定窗口", subtitle: "锁定一个目标，后台或最小化也尝试读取它", icon: "lock" },',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    "目标用窗口 ID + 进程 ID 锁定。它可以位于 Edge/Chrome 等窗口背后，不需要保持前台；最小化窗口暂不支持。窗口关闭或身份变化后必须重新选择。",
    "目标用窗口 ID + 进程 ID 锁定。后台和已最小化的应用窗口都会显示；读取最小化目标时会在后台无焦点恢复一帧、截图后再恢复最小化。纯服务或没有顶层窗体的进程不会显示。窗口关闭或身份变化后必须重新选择。",
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '<StatusPill :tone="selectedScreenWindow ? \'success\' : \'danger\'">{{ selectedScreenWindow ? \'已锁定\' : \'已失效\' }}</StatusPill>',
    '<StatusPill v-if="selectedScreenWindow?.minimized" tone="warning">最小化 · 已锁定</StatusPill>\n            <StatusPill v-else :tone="selectedScreenWindow ? \'success\' : \'danger\'">{{ selectedScreenWindow ? \'已锁定\' : \'已失效\' }}</StatusPill>',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    "<small>{{ window.processName || '未知进程' }} · PID {{ window.processId }} · {{ window.bounds.width }}×{{ window.bounds.height }}</small>",
    "<small>{{ window.processName || '未知进程' }} · PID {{ window.processId }} · {{ window.minimized ? '已最小化' : window.bounds.width + '×' + window.bounds.height }}</small>",
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '<StatusPill v-if="window.active" tone="info">当前前台</StatusPill>\n              <span class="screen-window-radio"><i /></span>',
    '<StatusPill v-if="window.active" tone="info">当前前台</StatusPill>\n              <StatusPill v-else-if="window.minimized" tone="warning">已最小化</StatusPill>\n              <span class="screen-window-radio"><i /></span>',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    "没有找到匹配窗口。若目标已最小化，请先恢复窗口再刷新。",
    "没有找到匹配的可截图应用窗口。纯后台服务或没有顶层窗体的进程不会出现在这里。",
)

# Documentation now treats minimized windows as first-class capture targets.
replace_once(
    "docs/SCREEN_VISION.md",
    "- `指定窗口`：用户在 DevDesk 中手动选择一个目标窗口，例如 VMware、123 云盘或独立插件窗口。之后 AI 始终只能读取这个 HWND + PID 对应的窗口；即使浏览器一直在最前面，指定窗口位于浏览器背后，也应读取指定窗口自身，而不是读取当前前台浏览器。目标关闭或身份变化后不会自动改抓别的窗口，必须重新选择。",
    "- `指定窗口`：用户在 DevDesk 中手动选择一个目标窗口，例如 VMware、123 云盘或独立插件窗口。之后 AI 始终只能读取这个 HWND + PID 对应的窗口；即使浏览器一直在最前面、目标位于浏览器背后或已经最小化，也应读取指定窗口自身，而不是读取当前前台浏览器。最小化目标会在截图时后台临时恢复，完成后重新最小化。目标关闭或身份变化后不会自动改抓别的窗口，必须重新选择。",
)
replace_once(
    "docs/SCREEN_VISION.md",
    "| `screen_list_windows` | 列出当前可读取的顶层应用窗口，可按标题或进程名筛选。 |",
    "| `screen_list_windows` | 列出当前可读取的顶层应用窗口，包括已最小化应用，可按标题或进程名筛选。 |",
)
replace_once(
    "docs/SCREEN_VISION.md",
    "当前窗口枚举仍排除已最小化窗口。Windows 对最小化窗口通常不持续维护可可靠抓取的实时像素；后续如要支持“最小化也可读取”，应单独实现和测试，而不能用可能串画面的桌面矩形回退。",
    "窗口枚举会保留仍有顶层窗体的已最小化应用，但不会把纯后台服务、无顶层窗体进程或 DWM 已隐藏/受保护的窗口伪装成可截图目标。读取最小化窗口时，DevDesk 会优先使用 `ShowWindowAsync(SW_SHOWNOACTIVATE)` 在不抢焦点的情况下临时恢复目标，等待应用重新渲染后走正常的目标安全捕获链，再使用无激活最小化方式恢复原状态；必要时才使用一次兼容性恢复并立即把原前台窗口切回。整个过程失败时直接报错，绝不改抓当前 Edge/Chrome。Windows/目标应用如果在最小化后彻底停止 GPU/DRM 渲染，仍可能返回黑屏、旧帧或明确失败。",
)
replace_once(
    "docs/SCREEN_VISION.md",
    "4. 测试“指定窗口”：把 VMware/123 云盘等目标保持正常打开，先选择并锁定它，然后把 Edge/Chrome 放到最前面继续聊天。客户端调用 `screen_capture_window` 时必须返回锁定目标自身内容或明确错误，绝不能返回前台浏览器画面。",
    "4. 测试“指定窗口”：先锁定 VMware/123 云盘等目标，再分别测试“被 Edge/Chrome 覆盖”和“最小化到任务栏”两种状态。客户端调用 `screen_capture_window` 时必须返回锁定目标自身内容或明确错误，绝不能返回前台浏览器画面；最小化测试完成后目标应自动回到最小化状态，浏览器焦点应尽量保持不变。",
)
replace_once(
    "docs/SCREEN_VISION.md",
    "建议重点反馈：指定后台窗口能否在 Edge/Chrome 覆盖时仍正确读取、是否出现闪动或抢焦点、整个桌面模式能否自主查看不同已打开软件、多实例连接下权限是否一致、VMware/硬件加速窗口是否黑屏、DPI/多显示器下截图范围是否正确、截图延迟、开启/关闭后的空闲资源占用，以及所使用的 Windows 版本和目标应用。",
    "建议重点反馈：指定后台窗口能否在 Edge/Chrome 覆盖时仍正确读取、最小化目标能否自动恢复一帧并重新最小化、是否出现闪动或抢焦点、整个桌面模式能否自主查看正常/后台/最小化的软件窗口、多实例连接下权限是否一致、VMware/硬件加速窗口是否黑屏、DPI/多显示器下截图范围是否正确、截图延迟、开启/关闭后的空闲资源占用，以及所使用的 Windows 版本和目标应用。",
)

# New platform wrappers keep the existing foreground-only helpers untouched,
# while Screen Vision gets a broader list plus minimized restore/capture logic.
write(
    "app/internal/mcpcore/screen_minimized_windows.go",
    r'''//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	swShowNoActivate       = 4
	swMinimize             = 6
	swShowMinNoActive      = 7
	swRestore              = 9
	screenRestorePoll      = 20 * time.Millisecond
	screenRestoreTimeout   = 520 * time.Millisecond
	screenInitialRenderWait = 180 * time.Millisecond
	screenRetryRenderWait   = 260 * time.Millisecond
)

var procShowWindowAsync = screenUser32.NewProc("ShowWindowAsync")

// platformListScreenWindowsForVision includes minimized top-level app windows.
// Pure services/headless processes still have no visual surface and are not
// candidates for Screen Vision.
func platformListScreenWindowsForVision() ([]screenWindow, error) {
	active, _, _ := procGetForegroundWindow.Call()
	result := make([]screenWindow, 0, 32)
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if hwnd == 0 || len(result) >= maxScreenWindows {
			return 1
		}
		valid, _, _ := procIsWindow.Call(hwnd)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		minimized, _, _ := procIsIconic.Call(hwnd)
		if !screenVisionWindowStateSelectable(valid, visible, minimized) || screenWindowCloaked(hwnd) {
			return 1
		}
		title := screenWindowTitle(hwnd)
		if strings.TrimSpace(title) == "" {
			return 1
		}
		rect, err := screenWindowRect(hwnd)
		if err != nil || rect.Width <= 0 || rect.Height <= 0 {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		result = append(result, screenWindow{
			ID:          fmt.Sprintf("0x%X", hwnd),
			Handle:      hwnd,
			Title:       title,
			ProcessID:   pid,
			ProcessName: screenProcessName(pid),
			Bounds:      rect,
			Active:      hwnd == active,
			Minimized:   minimized != 0,
		})
		return 1
	})
	ok, _, callErr := procEnumWindows.Call(callback, 0)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return nil, fmt.Errorf("enumerate Screen Vision windows: %w", callErr)
		}
		return nil, errors.New("enumerate Screen Vision windows failed")
	}
	return result, nil
}

func screenVisionWindowStateSelectable(valid, visible, _ uintptr) bool {
	return valid != 0 && visible != 0
}

func platformCaptureScreenWindowForVision(window screenWindow) (screenCaptureFrame, error) {
	if window.Handle == 0 {
		return screenCaptureFrame{}, errors.New("window handle is invalid")
	}
	valid, _, _ := procIsWindow.Call(window.Handle)
	if valid == 0 {
		return screenCaptureFrame{}, errors.New("window is no longer available")
	}
	minimized, _, _ := procIsIconic.Call(window.Handle)
	if minimized == 0 {
		return platformCaptureScreenWindow(window)
	}
	return captureMinimizedScreenWindow(window)
}

func captureMinimizedScreenWindow(window screenWindow) (frame screenCaptureFrame, err error) {
	hwnd := window.Handle
	foreground, _, _ := procGetForegroundWindow.Call()
	originalAbove, _, _ := procGetWindow.Call(hwnd, gwHwndPrev)
	wasTopmost := screenWindowTopmost(hwnd)
	originalAboveSameBand := false
	if originalAbove != 0 {
		valid, _, _ := procIsWindow.Call(originalAbove)
		originalAboveSameBand = valid != 0 && screenWindowTopmost(originalAbove) == wasTopmost
	}

	restored := false
	defer func() {
		var restoreErrors []string
		if restored {
			if minimizeErr := screenReturnWindowToMinimized(hwnd); minimizeErr != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("re-minimize selected window: %v", minimizeErr))
			}
		}
		if restoreErr := restoreBackgroundWindowAfterReveal(hwnd, originalAbove, foreground, wasTopmost, originalAboveSameBand); restoreErr != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("restore selected window placement: %v", restoreErr))
		}
		if len(restoreErrors) > 0 {
			cleanupErr := errors.New(strings.Join(restoreErrors, "; "))
			if err == nil {
				err = cleanupErr
			} else {
				err = fmt.Errorf("%v; cleanup after minimized capture: %w", err, cleanupErr)
			}
		}
	}()

	if err := screenRestoreWindowWithoutFocus(hwnd, foreground); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("temporarily restore minimized window: %w", err)
	}
	restored = true
	window.Minimized = false

	screenFlushDWM()
	time.Sleep(screenInitialRenderWait)
	frame, err = platformCaptureScreenWindow(window)
	if err != nil || screenImageLikelyBlank(frame.Image) {
		screenFlushDWM()
		time.Sleep(screenRetryRenderWait)
		frame, err = platformCaptureScreenWindow(window)
	}
	if err != nil {
		return screenCaptureFrame{}, fmt.Errorf("capture temporarily restored minimized window: %w", err)
	}
	if screenImageLikelyBlank(frame.Image) {
		return screenCaptureFrame{}, errors.New("minimized window resumed but still returned a blank frame; the application may suspend protected or GPU rendering while minimized")
	}
	frame.Method = "minimized-restore/" + frame.Method
	return frame, nil
}

func screenRestoreWindowWithoutFocus(hwnd, foreground uintptr) error {
	procShowWindowAsync.Call(hwnd, swShowNoActivate)
	if screenWaitForIconicState(hwnd, false, screenRestoreTimeout) {
		screenRestoreForeground(foreground)
		return nil
	}

	// Some GPU applications ignore SW_SHOWNOACTIVATE when iconic. SW_RESTORE is
	// a compatibility fallback; immediately return focus to the user's original
	// foreground window before waiting for rendering/capture.
	procShowWindowAsync.Call(hwnd, swRestore)
	screenRestoreForeground(foreground)
	if screenWaitForIconicState(hwnd, false, screenRestoreTimeout) {
		screenRestoreForeground(foreground)
		return nil
	}
	return errors.New("Windows did not restore the minimized target in time")
}

func screenReturnWindowToMinimized(hwnd uintptr) error {
	procShowWindowAsync.Call(hwnd, swShowMinNoActive)
	if screenWaitForIconicState(hwnd, true, screenRestoreTimeout) {
		return nil
	}
	procShowWindowAsync.Call(hwnd, swMinimize)
	if screenWaitForIconicState(hwnd, true, screenRestoreTimeout) {
		return nil
	}
	return errors.New("Windows did not return the target to minimized state in time")
}

func screenWaitForIconicState(hwnd uintptr, wantMinimized bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid == 0 {
			return false
		}
		iconic, _, _ := procIsIconic.Call(hwnd)
		if (iconic != 0) == wantMinimized {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(screenRestorePoll)
	}
}

func screenRestoreForeground(foreground uintptr) {
	if foreground == 0 {
		return
	}
	valid, _, _ := procIsWindow.Call(foreground)
	if valid == 0 {
		return
	}
	current, _, _ := procGetForegroundWindow.Call()
	if current != foreground {
		procSetForegroundWindow.Call(foreground)
	}
}
''',
)

write(
    "app/internal/mcpcore/screen_minimized_other.go",
    r'''//go:build !windows

package mcpcore

func platformListScreenWindowsForVision() ([]screenWindow, error) {
	return platformListScreenWindows()
}

func platformCaptureScreenWindowForVision(window screenWindow) (screenCaptureFrame, error) {
	return platformCaptureScreenWindow(window)
}
''',
)

write(
    "app/internal/mcpcore/screen_minimized_windows_test.go",
    r'''//go:build windows

package mcpcore

import "testing"

func TestScreenVisionWindowStateSelectableIncludesMinimized(t *testing.T) {
	if !screenVisionWindowStateSelectable(1, 1, 0) {
		t.Fatal("normal visible app window should be selectable")
	}
	if !screenVisionWindowStateSelectable(1, 1, 1) {
		t.Fatal("minimized visible app window should remain selectable for Screen Vision")
	}
	if screenVisionWindowStateSelectable(0, 1, 1) {
		t.Fatal("invalid window must not be selectable")
	}
	if screenVisionWindowStateSelectable(1, 0, 1) {
		t.Fatal("hidden/headless window must not be exposed as a Screen Vision target")
	}
}
''',
)

print("minimized Screen Vision patch applied")
