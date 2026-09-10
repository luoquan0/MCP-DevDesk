//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
)

// A window region is the Win32 fallback visibility guard used when DWM refuses
// DWMWA_CLOAK for another process (commonly HRESULT 0x80070005). An empty region
// keeps the exact HWND non-visible while it is restored and moved off-screen.
// Once the HWND is confirmed outside the virtual desktop, the original region is
// restored so GPU/Chromium/WPF renderers can resume normally there.
var (
	procGetWindowRgn  = screenUser32.NewProc("GetWindowRgn")
	procSetWindowRgn  = screenUser32.NewProc("SetWindowRgn")
	procCreateRectRgn = screenGDI32.NewProc("CreateRectRgn")
)

type screenWindowRegionGuard struct {
	originalRegion uintptr
	hadRegion      bool
}

var screenWindowRegionGuards = struct {
	sync.Mutex
	byHWND map[uintptr]screenWindowRegionGuard
}{
	byHWND: make(map[uintptr]screenWindowRegionGuard),
}

func screenWindowRegionGuardActive(hwnd uintptr) bool {
	screenWindowRegionGuards.Lock()
	defer screenWindowRegionGuards.Unlock()
	_, ok := screenWindowRegionGuards.byHWND[hwnd]
	return ok
}

func screenEnableWindowRegionGuard(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("window handle is invalid")
	}
	valid, _, _ := procIsWindow.Call(hwnd)
	if valid == 0 {
		return errors.New("window is no longer available")
	}

	screenWindowRegionGuards.Lock()
	defer screenWindowRegionGuards.Unlock()
	if _, ok := screenWindowRegionGuards.byHWND[hwnd]; ok {
		return nil
	}

	// Snapshot any custom window region. GetWindowRgn returns ERROR both when no
	// region is installed and on failure; after validating HWND we conservatively
	// treat ERROR as the normal "no custom region" case. This is how normal
	// rectangular application windows are represented by Win32.
	originalRegion, _, createErr := procCreateRectRgn.Call(0, 0, 0, 0)
	if originalRegion == 0 {
		return fmt.Errorf("CreateRectRgn for region snapshot failed: %v", createErr)
	}
	regionType, _, _ := procGetWindowRgn.Call(hwnd, originalRegion)
	hadRegion := regionType != 0
	if !hadRegion {
		procDeleteObject.Call(originalRegion)
		originalRegion = 0
	}

	emptyRegion, _, createErr := procCreateRectRgn.Call(0, 0, 0, 0)
	if emptyRegion == 0 {
		if originalRegion != 0 {
			procDeleteObject.Call(originalRegion)
		}
		return fmt.Errorf("CreateRectRgn for empty visibility guard failed: %v", createErr)
	}

	ok, _, callErr := procSetWindowRgn.Call(hwnd, emptyRegion, 0)
	if ok == 0 {
		// SetWindowRgn owns the HRGN only after success.
		procDeleteObject.Call(emptyRegion)
		if originalRegion != 0 {
			procDeleteObject.Call(originalRegion)
		}
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return fmt.Errorf("SetWindowRgn(empty) failed: %w", callErr)
		}
		return errors.New("SetWindowRgn(empty) failed")
	}

	screenWindowRegionGuards.byHWND[hwnd] = screenWindowRegionGuard{
		originalRegion: originalRegion,
		hadRegion:      hadRegion,
	}
	screenFlushDWM()
	return nil
}

func screenDisableWindowRegionGuard(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("window handle is invalid")
	}

	screenWindowRegionGuards.Lock()
	defer screenWindowRegionGuards.Unlock()
	state, ok := screenWindowRegionGuards.byHWND[hwnd]
	if !ok {
		return nil
	}

	valid, _, _ := procIsWindow.Call(hwnd)
	if valid == 0 {
		if state.originalRegion != 0 {
			procDeleteObject.Call(state.originalRegion)
		}
		delete(screenWindowRegionGuards.byHWND, hwnd)
		return errors.New("window was destroyed while the visibility region guard was active")
	}

	region := uintptr(0)
	if state.hadRegion {
		region = state.originalRegion
	}
	okValue, _, callErr := procSetWindowRgn.Call(hwnd, region, 0)
	if okValue == 0 {
		// Keep the saved region in the map so cleanup has another chance to
		// restore it. Ownership transfers only after SetWindowRgn succeeds.
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return fmt.Errorf("restore original window region failed: %w", callErr)
		}
		return errors.New("restore original window region failed")
	}

	delete(screenWindowRegionGuards.byHWND, hwnd)
	// On success Windows owns state.originalRegion when one existed.
	screenFlushDWM()
	return nil
}
