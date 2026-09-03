//go:build windows

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
	DevicePosition winRect
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

	// WINDOWPLACEMENT coordinates are workspace coordinates for normal top-level
	// app windows. Restore them with SetWindowPlacement itself; Microsoft warns
	// against feeding rcNormalPosition directly into SetWindowPos.
	if placementOK {
		repairedPlacement := placement
		repairedPlacement.Length = uint32(unsafe.Sizeof(screenWindowPlacement{}))
		repairedPlacement.ShowCmd = swShowNoActivate
		ok, _, callErr := procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&repairedPlacement)))
		if ok == 0 {
			if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
				return callErr
			}
			return errors.New("SetWindowPlacement failed while restoring normal placement")
		}
		screenFlushDWM()
		time.Sleep(screenRestorePoll)
		current, currentErr = screenWindowRect(hwnd)
		if currentErr == nil && !screenVisionBoundsNeedRepair(current, normal) {
			return nil
		}
	}

	// If an application keeps an abnormal icon-sized surface even after its
	// placement is restored, repair only width/height. SWP_NOMOVE deliberately
	// avoids interpreting workspace coordinates as screen coordinates.
	flags := uintptr(swpNoMove | screenSWPNoZOrder | swpNoActivate | swpNoOwnerZOrder | swpNoSendChanging)
	ok, _, callErr := procSetWindowPos.Call(
		hwnd,
		0,
		0,
		0,
		uintptr(normal.Width),
		uintptr(normal.Height),
		flags,
	)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return callErr
		}
		return errors.New("SetWindowPos failed while restoring normal size")
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
