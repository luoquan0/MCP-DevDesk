from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text(encoding="utf-8")


def write(path: str, value: str) -> None:
    Path(path).write_text(value, encoding="utf-8", newline="\n")


def replace_once(path: str, old: str, new: str) -> None:
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected one match, got {count}: {old[:120]!r}")
    write(path, text.replace(old, new, 1))


# Windows Screen Vision: include tray-hidden main windows, use restored/normal
# placement bounds instead of minimized icon rectangles, and preserve exact
# hidden/minimized state after best-effort capture.
write("app/internal/mcpcore/screen_minimized_windows.go", r'''//go:build windows

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
	swHide                  = 0
	swShowNoActivate        = 4
	swMinimize              = 6
	swShowMinNoActive       = 7
	swRestore               = 9
	screenGWHwndOwner       = 4
	screenWSExToolWindow    = 0x00000080
	screenSWPNoZOrder       = 0x0004
	screenRestorePoll       = 20 * time.Millisecond
	screenRestoreTimeout    = 760 * time.Millisecond
	screenInitialRenderWait = 220 * time.Millisecond
	screenRetryRenderWait   = 360 * time.Millisecond
)

var (
	procShowWindowAsync    = screenUser32.NewProc("ShowWindowAsync")
	procGetWindowPlacement = screenUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = screenUser32.NewProc("SetWindowPlacement")
)

type screenPlacementPoint struct {
	X int32
	Y int32
}

type screenWindowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    screenPlacementPoint
	MaxPosition    screenPlacementPoint
	NormalPosition winRect
}

// platformListScreenWindowsForVision includes normal, minimized, and selected
// tray-hidden top-level application windows. Hidden windows are intentionally
// filtered more strictly than visible windows so internal helper/service HWNDs
// do not become Screen Vision targets merely because they have a title.
func platformListScreenWindowsForVision() ([]screenWindow, error) {
	active, _, _ := procGetForegroundWindow.Call()
	candidates := make([]screenWindow, 0, 40)
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if hwnd == 0 || len(candidates) >= maxScreenWindows {
			return 1
		}
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid == 0 || screenWindowCloaked(hwnd) {
			return 1
		}
		title := strings.TrimSpace(screenWindowTitle(hwnd))
		if title == "" {
			return 1
		}

		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		processName := screenProcessName(pid)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		minimized, _, _ := procIsIconic.Call(hwnd)
		hidden := visible == 0

		currentBounds, currentErr := screenWindowRect(hwnd)
		placement, placementOK := screenGetWindowPlacement(hwnd)
		normalBounds, normalOK := screenPlacementNormalBounds(placement, placementOK)
		bounds, boundsOK := screenVisionPreferredBounds(
			currentBounds,
			currentErr == nil,
			normalBounds,
			normalOK,
			minimized != 0 || hidden,
		)
		if !boundsOK {
			return 1
		}

		if hidden {
			owner, _, _ := procGetWindow.Call(hwnd, screenGWHwndOwner)
			exStyle, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
			if !screenVisionHiddenWindowEligible(owner, uint32(exStyle), processName, bounds) {
				return 1
			}
		}

		candidates = append(candidates, screenWindow{
			ID:          fmt.Sprintf("0x%X", hwnd),
			Handle:      hwnd,
			Title:       title,
			ProcessID:   pid,
			ProcessName: processName,
			Bounds:      bounds,
			Active:      hwnd == active,
			Minimized:   minimized != 0,
			Hidden:      hidden,
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
	return screenVisionCollapseTinyHelpers(candidates), nil
}

func screenVisionHiddenWindowEligible(owner uintptr, exStyle uint32, processName string, bounds screenRect) bool {
	if owner != 0 || exStyle&screenWSExToolWindow != 0 || strings.TrimSpace(processName) == "" {
		return false
	}
	return bounds.Width >= 160 && bounds.Height >= 90
}

func screenVisionPreferredBounds(current screenRect, currentOK bool, normal screenRect, normalOK bool, dormant bool) (screenRect, bool) {
	if dormant && normalOK {
		return normal, true
	}
	if currentOK {
		return current, true
	}
	if normalOK {
		return normal, true
	}
	return screenRect{}, false
}

// Some tray applications keep a tiny helper/title HWND while their real main
// window is hidden. If the same process and exact title expose both a tiny and a
// normal-sized candidate, keep the normal-sized one. Normal multi-window apps
// are otherwise left untouched.
func screenVisionCollapseTinyHelpers(windows []screenWindow) []screenWindow {
	result := make([]screenWindow, 0, len(windows))
	for _, candidate := range windows {
		handled := false
		for index, existing := range result {
			if existing.ProcessID == 0 || existing.ProcessID != candidate.ProcessID || !strings.EqualFold(existing.Title, candidate.Title) {
				continue
			}
			existingTiny := screenVisionRectSuspicious(existing.Bounds)
			candidateTiny := screenVisionRectSuspicious(candidate.Bounds)
			existingMain := screenVisionRectLooksMain(existing.Bounds)
			candidateMain := screenVisionRectLooksMain(candidate.Bounds)
			if existingTiny && candidateMain {
				result[index] = candidate
				handled = true
				break
			}
			if candidateTiny && existingMain {
				handled = true
				break
			}
		}
		if !handled {
			result = append(result, candidate)
		}
	}
	return result
}

func screenVisionRectSuspicious(rect screenRect) bool {
	return rect.Width < 240 || rect.Height < 80
}

func screenVisionRectLooksMain(rect screenRect) bool {
	return rect.Width >= 240 && rect.Height >= 120
}

func screenRectArea(rect screenRect) int64 {
	if rect.Width <= 0 || rect.Height <= 0 {
		return 0
	}
	return int64(rect.Width) * int64(rect.Height)
}

func screenVisionBoundsNeedRepair(current, normal screenRect) bool {
	if !screenVisionRectLooksMain(normal) {
		return false
	}
	if screenVisionRectSuspicious(current) {
		return true
	}
	currentArea := screenRectArea(current)
	normalArea := screenRectArea(normal)
	return currentArea > 0 && normalArea > currentArea*3
}

func screenGetWindowPlacement(hwnd uintptr) (screenWindowPlacement, bool) {
	placement := screenWindowPlacement{Length: uint32(unsafe.Sizeof(screenWindowPlacement{}))}
	ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&placement)))
	return placement, ok != 0
}

func screenPlacementNormalBounds(placement screenWindowPlacement, ok bool) (screenRect, bool) {
	if !ok {
		return screenRect{}, false
	}
	rect := placement.NormalPosition
	bounds := screenRect{
		X:      int(rect.Left),
		Y:      int(rect.Top),
		Width:  int(rect.Right - rect.Left),
		Height: int(rect.Bottom - rect.Top),
	}
	if validateScreenRect(bounds) != nil {
		return screenRect{}, false
	}
	return bounds, true
}

func platformCaptureScreenWindowForVision(window screenWindow) (screenCaptureFrame, error) {
	if window.Handle == 0 {
		return screenCaptureFrame{}, errors.New("window handle is invalid")
	}
	valid, _, _ := procIsWindow.Call(window.Handle)
	if valid == 0 {
		return screenCaptureFrame{}, errors.New("window is no longer available")
	}
	visible, _, _ := procIsWindowVisible.Call(window.Handle)
	minimized, _, _ := procIsIconic.Call(window.Handle)
	if visible != 0 && minimized == 0 {
		return platformCaptureScreenWindow(window)
	}
	return captureDormantScreenWindow(window, visible == 0, minimized != 0)
}

func captureDormantScreenWindow(window screenWindow, wasHidden, wasMinimized bool) (frame screenCaptureFrame, err error) {
	hwnd := window.Handle
	foreground, _, _ := procGetForegroundWindow.Call()
	originalAbove, _, _ := procGetWindow.Call(hwnd, gwHwndPrev)
	wasTopmost := screenWindowTopmost(hwnd)
	originalAboveSameBand := false
	if originalAbove != 0 {
		valid, _, _ := procIsWindow.Call(originalAbove)
		originalAboveSameBand = valid != 0 && screenWindowTopmost(originalAbove) == wasTopmost
	}
	placement, placementOK := screenGetWindowPlacement(hwnd)

	touched := false
	defer func() {
		var restoreErrors []string
		if touched {
			if restoreErr := screenRestoreDormantWindow(hwnd, placement, placementOK, wasHidden, wasMinimized); restoreErr != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("restore dormant window state: %v", restoreErr))
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
				err = fmt.Errorf("%v; cleanup after dormant capture: %w", err, cleanupErr)
			}
		}
	}()

	touched = true
	if err := screenWakeDormantWindowWithoutFocus(hwnd, foreground); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("temporarily restore hidden/minimized window: %w", err)
	}
	if err := screenRepairRestoredBounds(hwnd, placement, placementOK); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("restore normal window bounds: %w", err)
	}
	window.Minimized = false
	window.Hidden = false

	screenFlushDWM()
	time.Sleep(screenInitialRenderWait)
	frame, err = platformCaptureScreenWindow(window)
	if err != nil || screenImageLikelyBlank(frame.Image) {
		// Tray/GPU apps can need one extra show/render cycle after their real main
		// HWND is restored. Retry without ever substituting foreground pixels.
		_ = screenWakeDormantWindowWithoutFocus(hwnd, foreground)
		_ = screenRepairRestoredBounds(hwnd, placement, placementOK)
		screenFlushDWM()
		time.Sleep(screenRetryRenderWait)
		frame, err = platformCaptureScreenWindow(window)
	}
	if err != nil {
		return screenCaptureFrame{}, fmt.Errorf("capture temporarily restored hidden/minimized window: %w", err)
	}
	if screenImageLikelyBlank(frame.Image) {
		return screenCaptureFrame{}, errors.New("hidden/minimized window resumed but still returned a blank frame; the application may destroy its main surface or suspend protected/GPU rendering while in the tray")
	}
	if wasHidden {
		frame.Method = "hidden-tray-restore/" + frame.Method
	} else {
		frame.Method = "minimized-restore/" + frame.Method
	}
	return frame, nil
}

func screenWakeDormantWindowWithoutFocus(hwnd, foreground uintptr) error {
	procShowWindowAsync.Call(hwnd, swShowNoActivate)
	screenRestoreForeground(foreground)
	if screenWaitForWindowReady(hwnd, screenRestoreTimeout) {
		screenRestoreForeground(foreground)
		return nil
	}

	// Some GPU/tray applications ignore SW_SHOWNOACTIVATE while iconic. SW_RESTORE
	// is a compatibility fallback; immediately return focus to the user's original
	// foreground window before waiting for rendering/capture.
	procShowWindowAsync.Call(hwnd, swRestore)
	screenRestoreForeground(foreground)
	if screenWaitForWindowReady(hwnd, screenRestoreTimeout) {
		screenRestoreForeground(foreground)
		return nil
	}
	return errors.New("Windows/application did not expose a restorable main window in time")
}

func screenRepairRestoredBounds(hwnd uintptr, placement screenWindowPlacement, placementOK bool) error {
	normal, normalOK := screenPlacementNormalBounds(placement, placementOK)
	if !normalOK {
		return nil
	}
	current, currentErr := screenWindowRect(hwnd)
	if currentErr == nil && !screenVisionBoundsNeedRepair(current, normal) {
		return nil
	}
	flags := uintptr(screenSWPNoZOrder | swpNoActivate | swpNoOwnerZOrder | swpNoSendChanging)
	ok, _, callErr := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(int32(normal.X)),
		uintptr(int32(normal.Y)),
		uintptr(normal.Width),
		uintptr(normal.Height),
		flags,
	)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return callErr
		}
		return errors.New("SetWindowPos failed while restoring normal bounds")
	}
	screenFlushDWM()
	return nil
}

func screenRestoreDormantWindow(hwnd uintptr, placement screenWindowPlacement, placementOK, wasHidden, wasMinimized bool) error {
	var restoreErrors []string
	if placementOK {
		placement.Length = uint32(unsafe.Sizeof(screenWindowPlacement{}))
		ok, _, callErr := procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&placement)))
		if ok == 0 && callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			restoreErrors = append(restoreErrors, fmt.Sprintf("SetWindowPlacement: %v", callErr))
		}
	}
	if wasHidden {
		procShowWindowAsync.Call(hwnd, swHide)
		if !screenWaitForVisibilityState(hwnd, false, screenRestoreTimeout) {
			restoreErrors = append(restoreErrors, "Windows did not return target to hidden/tray state in time")
		}
	} else if wasMinimized {
		if !screenWaitForIconicState(hwnd, true, 120*time.Millisecond) {
			if minimizeErr := screenReturnWindowToMinimized(hwnd); minimizeErr != nil {
				restoreErrors = append(restoreErrors, minimizeErr.Error())
			}
		}
	}
	if len(restoreErrors) > 0 {
		return errors.New(strings.Join(restoreErrors, "; "))
	}
	return nil
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

func screenWaitForWindowReady(hwnd uintptr, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid == 0 {
			return false
		}
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		iconic, _, _ := procIsIconic.Call(hwnd)
		if visible != 0 && iconic == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(screenRestorePoll)
	}
}

func screenWaitForVisibilityState(hwnd uintptr, wantVisible bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid == 0 {
			return false
		}
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if (visible != 0) == wantVisible {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(screenRestorePoll)
	}
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
''')

# Add hidden/tray state to tool metadata.
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '\tMinimized   bool       `json:"minimized"`\n',
    '\tMinimized   bool       `json:"minimized"`\n\tHidden      bool       `json:"hidden"`\n',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    'Description: "List captureable top-level Windows application windows, including minimized apps. Screen Vision is explicit opt-in and this tool never starts continuous recording.",',
    'Description: "List captureable top-level Windows application windows, including minimized apps and tray-hidden main windows when Windows keeps a restorable top-level surface. Screen Vision is explicit opt-in and this tool never starts continuous recording.",',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    'Description: "Capture one explicitly selected Windows application window on demand, including a background or minimized target when Windows allows it, and return a PNG image to the MCP client. Minimized targets are temporarily restored without focus and returned to minimized state. Nothing is saved to disk.",',
    'Description: "Capture one explicitly selected Windows application window on demand, including a background, minimized, or tray-hidden target when Windows keeps a restorable main surface. Dormant targets are temporarily restored without focus and returned to their prior state. Nothing is saved to disk.",',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '''\t\tsort.SliceStable(filtered, func(i, j int) bool {\n\t\t\tif filtered[i].Active != filtered[j].Active {\n\t\t\t\treturn filtered[i].Active\n\t\t\t}\n\t\t\tif filtered[i].Minimized != filtered[j].Minimized {\n\t\t\t\treturn !filtered[i].Minimized\n\t\t\t}\n\t\t\treturn strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)\n\t\t})''',
    '''\t\tsort.SliceStable(filtered, func(i, j int) bool {\n\t\t\tif filtered[i].Active != filtered[j].Active {\n\t\t\t\treturn filtered[i].Active\n\t\t\t}\n\t\t\tif filtered[i].Hidden != filtered[j].Hidden {\n\t\t\t\treturn !filtered[i].Hidden\n\t\t\t}\n\t\t\tif filtered[i].Minimized != filtered[j].Minimized {\n\t\t\t\treturn !filtered[i].Minimized\n\t\t\t}\n\t\t\treturn strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)\n\t\t})''',
)
replace_once(
    "app/internal/mcpcore/screen_tools.go",
    '''\tif window != nil {\n\t\tresult["window"] = *window\n\t}\n''',
    '''\tif window != nil {\n\t\tcapturedWindow := *window\n\t\t// Report the bounds that actually produced the image. In particular, a\n\t\t// minimized icon rectangle (for example 158x26) must not survive in the\n\t\t// response after the real window was restored and captured at full size.\n\t\tcapturedWindow.Bounds = frame.Bounds\n\t\tresult["window"] = capturedWindow\n\t}\n''',
)

# Public manager API mirrors hidden state and sorting.
replace_once(
    "app/internal/model/types.go",
    '\tMinimized   bool       `json:"minimized"`\n',
    '\tMinimized   bool       `json:"minimized"`\n\tHidden      bool       `json:"hidden"`\n',
)
replace_once(
    "app/internal/mcpcore/screen_public.go",
    '''\t\tif windows[i].Minimized != windows[j].Minimized {\n\t\t\treturn !windows[i].Minimized\n\t\t}\n''',
    '''\t\tif windows[i].Hidden != windows[j].Hidden {\n\t\t\treturn !windows[i].Hidden\n\t\t}\n\t\tif windows[i].Minimized != windows[j].Minimized {\n\t\t\treturn !windows[i].Minimized\n\t\t}\n''',
)
replace_once(
    "app/internal/mcpcore/screen_public.go",
    '\t\t\tMinimized: window.Minimized,\n',
    '\t\t\tMinimized: window.Minimized,\n\t\t\tHidden:    window.Hidden,\n',
)

# Replace Windows-only tests with pure policy/geometry tests that exercise the
# tray/minimized heuristics deterministically on CI.
write("app/internal/mcpcore/screen_minimized_windows_test.go", r'''//go:build windows

package mcpcore

import "testing"

func TestScreenVisionPreferredBoundsUsesNormalPlacementForDormantWindow(t *testing.T) {
	current := screenRect{X: -32000, Y: -32000, Width: 158, Height: 26}
	normal := screenRect{X: 120, Y: 90, Width: 980, Height: 720}
	got, ok := screenVisionPreferredBounds(current, true, normal, true, true)
	if !ok || got != normal {
		t.Fatalf("preferred bounds = %+v, ok=%v; want normal %+v", got, ok, normal)
	}
}

func TestScreenVisionHiddenWindowEligibilityRejectsHelpers(t *testing.T) {
	bounds := screenRect{Width: 900, Height: 600}
	if !screenVisionHiddenWindowEligible(0, 0, "v2rayN.exe", bounds) {
		t.Fatal("unowned normal-sized app main window should be eligible while hidden")
	}
	if screenVisionHiddenWindowEligible(1, 0, "v2rayN.exe", bounds) {
		t.Fatal("owned helper window must not be exposed")
	}
	if screenVisionHiddenWindowEligible(0, screenWSExToolWindow, "v2rayN.exe", bounds) {
		t.Fatal("tool window must not be exposed as hidden main window")
	}
	if screenVisionHiddenWindowEligible(0, 0, "", bounds) {
		t.Fatal("hidden window without resolvable process must not be exposed")
	}
	if screenVisionHiddenWindowEligible(0, 0, "v2rayN.exe", screenRect{Width: 158, Height: 26}) {
		t.Fatal("tiny hidden helper must not be exposed as a main window")
	}
}

func TestScreenVisionCollapseTinyHelperPrefersMainWindow(t *testing.T) {
	windows := []screenWindow{
		{ID: "0x1", ProcessID: 55, Title: "v2rayN", Bounds: screenRect{Width: 158, Height: 26}},
		{ID: "0x2", ProcessID: 55, Title: "v2rayN", Bounds: screenRect{Width: 1000, Height: 700}, Hidden: true},
		{ID: "0x3", ProcessID: 55, Title: "另一个正常窗口", Bounds: screenRect{Width: 800, Height: 500}},
	}
	got := screenVisionCollapseTinyHelpers(windows)
	if len(got) != 2 {
		t.Fatalf("candidate count = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "0x2" {
		t.Fatalf("v2rayN candidate = %s, want main window 0x2", got[0].ID)
	}
	if got[1].ID != "0x3" {
		t.Fatalf("unrelated normal window was changed: %+v", got[1])
	}
}

func TestScreenVisionBoundsNeedRepair(t *testing.T) {
	if !screenVisionBoundsNeedRepair(
		screenRect{Width: 158, Height: 26},
		screenRect{Width: 1000, Height: 700},
	) {
		t.Fatal("tiny restored bounds should be repaired to normal placement")
	}
	if screenVisionBoundsNeedRepair(
		screenRect{Width: 960, Height: 680},
		screenRect{Width: 1000, Height: 700},
	) {
		t.Fatal("already-normal restored bounds should not be moved/resized")
	}
}
''')

# Frontend API/UI surfaces hidden-to-tray state.
replace_once(
    "frontend/src/types/api.ts",
    '  minimized: boolean;\n',
    '  minimized: boolean;\n  hidden: boolean;\n',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '{ id: "window", title: "指定窗口", subtitle: "锁定一个目标，后台或最小化也尝试读取它", icon: "lock" },',
    '{ id: "window", title: "指定窗口", subtitle: "锁定一个目标，后台、最小化或托盘隐藏也尝试读取它", icon: "lock" },',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '目标用窗口 ID + 进程 ID 锁定。后台和已最小化的应用窗口都会显示；读取最小化目标时会在后台无焦点恢复一帧、截图后再恢复最小化。纯服务或没有顶层窗体的进程不会显示。窗口关闭或身份变化后必须重新选择。',
    '目标用窗口 ID + 进程 ID 锁定。后台、已最小化以及仍保留主窗体的托盘应用都会显示；读取休眠目标时会在后台无焦点恢复一帧，截图后恢复原来的最小化/隐藏状态。纯服务或已经销毁主窗体的进程不会显示。窗口关闭或身份变化后必须重新选择。',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '''            <StatusPill v-if="selectedScreenWindow?.minimized" tone="warning">最小化 · 已锁定</StatusPill>\n            <StatusPill v-else :tone="selectedScreenWindow ? 'success' : 'danger'">{{ selectedScreenWindow ? '已锁定' : '已失效' }}</StatusPill>''',
    '''            <StatusPill v-if="selectedScreenWindow?.hidden" tone="warning">托盘/隐藏 · 已锁定</StatusPill>\n            <StatusPill v-else-if="selectedScreenWindow?.minimized" tone="warning">最小化 · 已锁定</StatusPill>\n            <StatusPill v-else :tone="selectedScreenWindow ? 'success' : 'danger'">{{ selectedScreenWindow ? '已锁定' : '已失效' }}</StatusPill>''',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '''                <small>{{ window.processName || '未知进程' }} · PID {{ window.processId }} · {{ window.minimized ? '已最小化' : window.bounds.width + '×' + window.bounds.height }}</small>''',
    '''                <small>{{ window.processName || '未知进程' }} · PID {{ window.processId }} · {{ window.hidden ? '托盘/隐藏' : window.minimized ? '已最小化' : window.bounds.width + '×' + window.bounds.height }} · {{ window.bounds.width }}×{{ window.bounds.height }}</small>''',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '''              <StatusPill v-if="window.active" tone="info">当前前台</StatusPill>\n              <StatusPill v-else-if="window.minimized" tone="warning">已最小化</StatusPill>''',
    '''              <StatusPill v-if="window.active" tone="info">当前前台</StatusPill>\n              <StatusPill v-else-if="window.hidden" tone="warning">托盘/隐藏</StatusPill>\n              <StatusPill v-else-if="window.minimized" tone="warning">已最小化</StatusPill>''',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '没有找到匹配的可截图应用窗口。纯后台服务或没有顶层窗体的进程不会出现在这里。',
    '没有找到匹配的可截图应用窗口。最小化和部分托盘应用会显示，但纯后台服务或已经销毁主窗体的进程不会出现在这里。',
)
replace_once(
    "frontend/src/components/settings/SecuritySettingsSection.vue",
    '当前测试功能仅由 Go MCP Core 提供。指定窗口可在后台读取，必要时会做一次不激活目标的临时合成捕获；个别程序可能出现极短的层级刷新。最小化窗口暂不支持；受保护、DRM 或部分硬件窗口仍可能无法读取。',
    '当前测试功能仅由 Go MCP Core 提供。指定窗口可在后台读取；最小化/托盘窗口会尝试临时无焦点恢复并在截图后恢复原状态。个别程序可能出现极短的层级刷新；如果应用在进托盘后销毁主窗体，或 DRM/GPU 渲染拒绝恢复，仍会明确失败而不会改抓前台窗口。',
)

# The startup mechanism already exists (HKCU Run + --background). Rename the
# existing UI so users can find it as an explicit startup switch instead of
# adding a duplicate setting with conflicting state.
replace_once(
    "frontend/src/pages/SettingsPage.vue",
    'label="Windows 登录时后台运行"\n            description="在当前用户登录后启动托盘与本地管理服务。"',
    'label="开机自启动"\n            description="当前 Windows 用户登录后自动在后台启动 MCP DevDesk 到系统托盘，不弹出主界面；可随时关闭。"',
)
replace_once(
    "frontend/src/pages/SettingsPage.vue",
    '<div><span>系统托盘</span><strong>{{ app.desktop?.trayAvailable ? \'可用\' : \'不可用\' }}</strong></div>\n          <div><span>单实例</span><strong>{{ app.desktop?.singleInstance ? \'启用\' : \'关闭\' }}</strong></div>',
    '<div><span>系统托盘</span><strong>{{ app.desktop?.trayAvailable ? \'可用\' : \'不可用\' }}</strong></div>\n          <div><span>开机自启动</span><strong>{{ app.desktop?.startupEnabled ? \'已开启\' : \'已关闭\' }}</strong></div>\n          <div><span>单实例</span><strong>{{ app.desktop?.singleInstance ? \'启用\' : \'关闭\' }}</strong></div>',
)
replace_once(
    "frontend/src/stores/app.ts",
    'ui.toast(enabled ? "已启用登录时启动" : "已关闭登录时启动", "该设置仅影响当前 Windows 用户。", "success");',
    'ui.toast(enabled ? "开机自启动已开启" : "开机自启动已关闭", "在当前 Windows 用户登录后生效，并以后台托盘方式启动。", "success");',
)

# Documentation: tray-hidden apps are best-effort capture targets while pure
# headless services remain excluded.
replace_once(
    "docs/SCREEN_VISION.md",
    '`screen_list_windows` | 列出当前可读取的顶层应用窗口，包括已最小化应用，可按标题或进程名筛选。',
    '`screen_list_windows` | 列出当前可读取的顶层应用窗口，包括已最小化及仍保留可恢复主窗体的托盘应用，可按标题或进程名筛选。',
)
replace_once(
    "docs/SCREEN_VISION.md",
    '窗口枚举会保留仍有顶层窗体的已最小化应用，但不会把纯后台服务、无顶层窗体进程或 DWM 已隐藏/受保护的窗口伪装成可截图目标。读取最小化窗口时，DevDesk 会优先使用 `ShowWindowAsync(SW_SHOWNOACTIVATE)` 在不抢焦点的情况下临时恢复目标，等待应用重新渲染后走正常的目标安全捕获链，再使用无激活最小化方式恢复原状态；必要时才使用一次兼容性恢复并立即把原前台窗口切回。整个过程失败时直接报错，绝不改抓当前 Edge/Chrome。Windows/目标应用如果在最小化后彻底停止 GPU/DRM 渲染，仍可能返回黑屏、旧帧或明确失败。',
    '窗口枚举会保留仍有顶层窗体的已最小化应用，并额外尝试识别“隐藏到系统托盘但仍保留主 HWND”的应用；隐藏候选会排除有 Owner 的辅助窗体、`WS_EX_TOOLWINDOW` 和尺寸过小的内部窗口，不会把纯后台服务或无顶层窗体进程伪装成可截图目标。对于 Windows 最小化后常见的 158×26 一类图标矩形，DevDesk 会优先读取 `GetWindowPlacement` 的正常窗口尺寸。读取最小化/托盘目标时，会使用 `ShowWindowAsync(SW_SHOWNOACTIVATE)` 尝试无焦点恢复；若恢复后仍停留在异常小尺寸，则按正常窗口位置修复尺寸，等待重新渲染后走目标安全捕获链，并最终恢复原来的最小化/隐藏状态和用户前台窗口。整个过程失败时直接报错，绝不改抓当前 Edge/Chrome。若目标应用进入托盘时直接销毁主 HWND，或彻底停止 GPU/DRM 渲染，Windows 本身就没有可恢复画面，此时仍会明确失败。',
)
replace_once(
    "docs/SCREEN_VISION.md",
    '建议重点反馈：指定后台窗口能否在 Edge/Chrome 覆盖时仍正确读取、最小化目标能否自动恢复一帧并重新最小化、是否出现闪动或抢焦点、整个桌面模式能否自主查看正常/后台/最小化的软件窗口、多实例连接下权限是否一致、VMware/硬件加速窗口是否黑屏、DPI/多显示器下截图范围是否正确、截图延迟、开启/关闭后的空闲资源占用，以及所使用的 Windows 版本和目标应用。',
    '建议重点反馈：指定后台窗口能否在 Edge/Chrome 覆盖时仍正确读取、最小化或托盘目标能否自动恢复完整主窗口并回到原状态、是否还出现 158×26 一类小辅助窗体、是否出现闪动或抢焦点、整个桌面模式能否自主查看正常/后台/最小化/托盘软件窗口、多实例连接下权限是否一致、VMware/硬件加速窗口是否黑屏、DPI/多显示器下截图范围是否正确、截图延迟、开启/关闭后的空闲资源占用，以及所使用的 Windows 版本和目标应用。',
)
