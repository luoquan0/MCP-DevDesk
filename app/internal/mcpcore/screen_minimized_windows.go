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
	swHide                      = 0
	swShowNoActivate            = 4
	swMinimize                  = 6
	swShowMinNoActive           = 7
	swRestore                   = 9
	screenGWHwndOwner           = 4
	screenWSExToolWindow        = 0x00000080
	screenSWPNoZOrder           = 0x0004
	screenRestorePoll           = 20 * time.Millisecond
	screenRestoreTimeout        = 760 * time.Millisecond
	screenRebindPoll            = 35 * time.Millisecond
	screenRebindInitialTimeout  = 650 * time.Millisecond
	screenRebindFallbackTimeout = 1200 * time.Millisecond
	screenStableSamples         = 3
	screenPostStableRenderWait  = 80 * time.Millisecond
	screenRetryRenderWait       = 180 * time.Millisecond
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

func screenVisionRestoredIdentityMatches(original, candidate screenWindow) bool {
	if original.ProcessID == 0 || candidate.ProcessID != original.ProcessID {
		return false
	}
	if strings.TrimSpace(original.ProcessName) != "" && strings.TrimSpace(candidate.ProcessName) != "" && !strings.EqualFold(original.ProcessName, candidate.ProcessName) {
		return false
	}
	return true
}

func screenVisionChooseRestoredCandidate(original screenWindow, exact *screenWindow, replacements []screenWindow, allowSuspiciousExact bool) (screenWindow, error) {
	if exact != nil && !screenVisionBoundsNeedRepair(exact.Bounds, original.Bounds) {
		return *exact, nil
	}

	exactTitle := make([]screenWindow, 0, len(replacements))
	for _, candidate := range replacements {
		if strings.EqualFold(strings.TrimSpace(candidate.Title), strings.TrimSpace(original.Title)) {
			exactTitle = append(exactTitle, candidate)
		}
	}
	if len(exactTitle) == 1 {
		return exactTitle[0], nil
	}
	if len(exactTitle) > 1 {
		return screenWindow{}, fmt.Errorf("restored application exposed multiple main windows matching %q", original.Title)
	}
	if len(replacements) == 1 {
		return replacements[0], nil
	}
	if len(replacements) > 1 {
		return screenWindow{}, fmt.Errorf("restored application exposed %d possible main windows; refusing to guess", len(replacements))
	}
	if exact != nil && allowSuspiciousExact {
		return *exact, nil
	}
	return screenWindow{}, errors.New("restored application window is not ready yet")
}

func screenEnumerateRestoredWindowCandidate(original screenWindow, allowSuspiciousExact bool) (screenWindow, error) {
	var exact *screenWindow
	replacements := make([]screenWindow, 0, 4)
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if hwnd == 0 {
			return 1
		}
		valid, _, _ := procIsWindow.Call(hwnd)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		minimized, _, _ := procIsIconic.Call(hwnd)
		if valid == 0 || visible == 0 || minimized != 0 || screenWindowCloaked(hwnd) {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid != original.ProcessID {
			return 1
		}
		processName := screenProcessName(pid)
		candidate := screenWindow{
			ID:          fmt.Sprintf("0x%X", hwnd),
			Handle:      hwnd,
			Title:       strings.TrimSpace(screenWindowTitle(hwnd)),
			ProcessID:   pid,
			ProcessName: processName,
		}
		if candidate.Title == "" || !screenVisionRestoredIdentityMatches(original, candidate) {
			return 1
		}
		rect, err := screenWindowRect(hwnd)
		if err != nil {
			return 1
		}
		candidate.Bounds = rect
		if hwnd == original.Handle {
			copy := candidate
			exact = &copy
			return 1
		}
		owner, _, _ := procGetWindow.Call(hwnd, screenGWHwndOwner)
		exStyle, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
		if !screenVisionHiddenWindowEligible(owner, uint32(exStyle), processName, rect) {
			return 1
		}
		replacements = append(replacements, candidate)
		return 1
	})
	ok, _, callErr := procEnumWindows.Call(callback, 0)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return screenWindow{}, fmt.Errorf("re-enumerate restored application windows: %w", callErr)
		}
		return screenWindow{}, errors.New("re-enumerate restored application windows failed")
	}
	return screenVisionChooseRestoredCandidate(original, exact, replacements, allowSuspiciousExact)
}

func screenRectsStable(previous, current screenRect) bool {
	return screenAbs(previous.X-current.X) <= 1 &&
		screenAbs(previous.Y-current.Y) <= 1 &&
		screenAbs(previous.Width-current.Width) <= 1 &&
		screenAbs(previous.Height-current.Height) <= 1
}

func screenAbs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func screenWaitForRestoredWindowStable(original screenWindow, placement screenWindowPlacement, placementOK bool, timeout time.Duration, allowSuspiciousExact bool) (screenWindow, error) {
	deadline := time.Now().Add(timeout)
	var lastHandle uintptr
	var lastBounds screenRect
	stableSamples := 0
	repairAttempted := make(map[uintptr]bool)
	var lastErr error

	for {
		candidate, err := screenEnumerateRestoredWindowCandidate(original, allowSuspiciousExact)
		if err != nil {
			lastErr = err
			stableSamples = 0
		} else {
			candidatePlacement := placement
			candidatePlacementOK := placementOK
			if candidate.Handle != original.Handle && !candidatePlacementOK {
				candidatePlacement, candidatePlacementOK = screenGetWindowPlacement(candidate.Handle)
			}
			if !repairAttempted[candidate.Handle] {
				if repairErr := screenRepairRestoredBounds(candidate.Handle, candidatePlacement, candidatePlacementOK); repairErr != nil {
					lastErr = repairErr
				} else {
					repairAttempted[candidate.Handle] = true
				}
			}

			rect, rectErr := screenWindowRect(candidate.Handle)
			if rectErr != nil {
				lastErr = rectErr
				stableSamples = 0
			} else if screenVisionBoundsNeedRepair(rect, original.Bounds) {
				lastErr = fmt.Errorf("restored window still has abnormal bounds %dx%d", rect.Width, rect.Height)
				stableSamples = 0
			} else {
				candidate.Bounds = rect
				candidate.Minimized = false
				candidate.Hidden = false
				if candidate.Handle == lastHandle && screenRectsStable(lastBounds, rect) {
					stableSamples++
				} else {
					lastHandle = candidate.Handle
					lastBounds = rect
					stableSamples = 1
				}
				if stableSamples >= screenStableSamples {
					screenFlushDWM()
					time.Sleep(screenRestorePoll)
					finalRect, finalErr := screenWindowRect(candidate.Handle)
					if finalErr == nil && screenRectsStable(rect, finalRect) {
						candidate.Bounds = finalRect
						return candidate, nil
					}
					stableSamples = 0
					if finalErr != nil {
						lastErr = finalErr
					}
				}
			}
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return screenWindow{}, lastErr
			}
			return screenWindow{}, errors.New("restored application window did not become stable in time")
		}
		time.Sleep(screenRebindPoll)
	}
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
	logicalWindow := window
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
	restoreHandle := hwnd

	touched := false
	defer func() {
		var restoreErrors []string
		if touched && restoreHandle != 0 {
			valid, _, _ := procIsWindow.Call(restoreHandle)
			if valid != 0 {
				if restoreErr := screenRestoreDormantWindow(restoreHandle, placement, placementOK, wasHidden, wasMinimized); restoreErr != nil {
					restoreErrors = append(restoreErrors, fmt.Sprintf("restore dormant window state: %v", restoreErr))
				}
			}
		}
		if restoreHandle != 0 {
			valid, _, _ := procIsWindow.Call(restoreHandle)
			if valid != 0 {
				if restoreErr := restoreBackgroundWindowAfterReveal(restoreHandle, originalAbove, foreground, wasTopmost, originalAboveSameBand); restoreErr != nil {
					restoreErrors = append(restoreErrors, fmt.Sprintf("restore selected window placement: %v", restoreErr))
				}
			}
		}
		screenRestoreForeground(foreground)
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
	screenRequestDormantWindowRestore(hwnd, foreground, swShowNoActivate)
	restored, restoreErr := screenWaitForRestoredWindowStable(logicalWindow, placement, placementOK, screenRebindInitialTimeout, false)
	if restoreErr != nil {
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid != 0 {
			screenRequestDormantWindowRestore(hwnd, foreground, swRestore)
		}
		restored, restoreErr = screenWaitForRestoredWindowStable(logicalWindow, placement, placementOK, screenRebindFallbackTimeout, false)
	}
	if restoreErr != nil {
		// Final compatibility path: if the application never creates a replacement
		// HWND, allow the original HWND to be repaired from rcNormalPosition. The
		// capture still remains locked to the original PID and never substitutes
		// foreground pixels.
		restored, restoreErr = screenWaitForRestoredWindowStable(logicalWindow, placement, placementOK, 320*time.Millisecond, true)
	}
	if restoreErr != nil {
		return screenCaptureFrame{}, fmt.Errorf("temporarily restore hidden/minimized window: %w", restoreErr)
	}

	restoreHandle = restored.Handle
	window = restored
	screenRestoreForeground(foreground)
	screenFlushDWM()
	time.Sleep(screenPostStableRenderWait)
	frame, err = platformCaptureScreenWindow(window)
	if err != nil || screenImageLikelyBlank(frame.Image) {
		// A compositor/GPU surface can settle slightly after the outer window
		// geometry. Re-enumerate again in case the app replaced the HWND during
		// rendering, require stable geometry, then retry once.
		refreshed, settleErr := screenWaitForRestoredWindowStable(logicalWindow, placement, placementOK, 520*time.Millisecond, true)
		if settleErr == nil {
			restoreHandle = refreshed.Handle
			window = refreshed
		}
		screenRestoreForeground(foreground)
		screenFlushDWM()
		time.Sleep(screenRetryRenderWait)
		frame, err = platformCaptureScreenWindow(window)
	}
	if err != nil {
		return screenCaptureFrame{}, fmt.Errorf("capture temporarily restored hidden/minimized window: %w", err)
	}
	if screenImageLikelyBlank(frame.Image) {
		return screenCaptureFrame{}, errors.New("hidden/minimized window resumed and stabilized but still returned a blank frame; the application may destroy its main surface or suspend protected/GPU rendering while in the tray")
	}
	if wasHidden {
		frame.Method = "hidden-tray-rebind/" + frame.Method
	} else {
		frame.Method = "minimized-rebind/" + frame.Method
	}
	return frame, nil
}

func screenRequestDormantWindowRestore(hwnd, foreground uintptr, command uint32) {
	if hwnd == 0 {
		return
	}
	procShowWindowAsync.Call(hwnd, uintptr(command))
	screenRestoreForeground(foreground)
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
