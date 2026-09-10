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
	swShowNoActivate        = 4
	swMinimize              = 6
	swShowMinNoActive       = 7
	swRestore               = 9
	screenRestorePoll       = 20 * time.Millisecond
	screenRestoreTimeout    = 520 * time.Millisecond
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
