//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	smXVirtualScreen         = 76
	smYVirtualScreen         = 77
	smCXVirtualScreen        = 78
	smCYVirtualScreen        = 79
	dwmwaExtendedFrameBounds = 9
	dwmwaCloak               = 13
	dwmwaCloaked             = 14
	processQueryLimitedInfo  = 0x1000
	pwRenderFullContent      = 0x00000002
	gwHwndPrev               = 3
	gwlExStyle               = 0xFFFFFFEC
	wsExTopmost              = 0x00000008
	swpNoSize                = 0x0001
	swpNoMove                = 0x0002
	swpNoActivate            = 0x0010
	swpNoOwnerZOrder         = 0x0200
	swpNoSendChanging        = 0x0400
	srcCopy                  = 0x00CC0020
	captureBLT               = 0x40000000
	blackness                = 0x00000042
	dibRGBColors             = 0
	biRGB                    = 0
	maxScreenCapturePixels   = 40_000_000
)

const screenRevealWindowPosFlags = swpNoSize | swpNoMove | swpNoActivate | swpNoOwnerZOrder | swpNoSendChanging

var (
	screenUser32   = windows.NewLazySystemDLL("user32.dll")
	screenGDI32    = windows.NewLazySystemDLL("gdi32.dll")
	screenDWMAPI   = windows.NewLazySystemDLL("dwmapi.dll")
	screenKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procEnumWindows              = screenUser32.NewProc("EnumWindows")
	procIsWindow                 = screenUser32.NewProc("IsWindow")
	procIsWindowVisible          = screenUser32.NewProc("IsWindowVisible")
	procIsIconic                 = screenUser32.NewProc("IsIconic")
	procGetWindow                = screenUser32.NewProc("GetWindow")
	procGetWindowLongW           = screenUser32.NewProc("GetWindowLongW")
	procSetWindowPos             = screenUser32.NewProc("SetWindowPos")
	procSetForegroundWindow      = screenUser32.NewProc("SetForegroundWindow")
	procGetWindowTextLengthW     = screenUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW           = screenUser32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessID = screenUser32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow      = screenUser32.NewProc("GetForegroundWindow")
	procGetWindowRect            = screenUser32.NewProc("GetWindowRect")
	procPrintWindow              = screenUser32.NewProc("PrintWindow")
	procGetDC                    = screenUser32.NewProc("GetDC")
	procGetWindowDC              = screenUser32.NewProc("GetWindowDC")
	procReleaseDC                = screenUser32.NewProc("ReleaseDC")
	procGetSystemMetrics         = screenUser32.NewProc("GetSystemMetrics")

	procCreateCompatibleDC     = screenGDI32.NewProc("CreateCompatibleDC")
	procDeleteDC               = screenGDI32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = screenGDI32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = screenGDI32.NewProc("SelectObject")
	procDeleteObject           = screenGDI32.NewProc("DeleteObject")
	procBitBlt                 = screenGDI32.NewProc("BitBlt")
	procPatBlt                 = screenGDI32.NewProc("PatBlt")
	procGetDIBits              = screenGDI32.NewProc("GetDIBits")

	procDwmGetWindowAttribute      = screenDWMAPI.NewProc("DwmGetWindowAttribute")
	procDwmSetWindowAttribute      = screenDWMAPI.NewProc("DwmSetWindowAttribute")
	procDwmFlush                   = screenDWMAPI.NewProc("DwmFlush")
	procOpenProcess                = screenKernel32.NewProc("OpenProcess")
	procQueryFullProcessImageNameW = screenKernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                = screenKernel32.NewProc("CloseHandle")
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

func platformListScreenWindows() ([]screenWindow, error) {
	active, _, _ := procGetForegroundWindow.Call()
	result := make([]screenWindow, 0, 32)
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if hwnd == 0 || len(result) >= maxScreenWindows {
			return 1
		}
		valid, _, _ := procIsWindow.Call(hwnd)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		minimized, _, _ := procIsIconic.Call(hwnd)
		if !screenWindowStateSelectable(valid, visible, minimized) || screenWindowCloaked(hwnd) {
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
		})
		return 1
	})
	ok, _, callErr := procEnumWindows.Call(callback, 0)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return nil, fmt.Errorf("enumerate windows: %w", callErr)
		}
		return nil, errors.New("enumerate windows failed")
	}
	return result, nil
}

func screenWindowStateSelectable(valid, visible, minimized uintptr) bool {
	return valid != 0 && visible != 0 && minimized == 0
}

func platformActiveScreenWindow() (screenWindow, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return screenWindow{}, errors.New("no foreground window is available")
	}
	windowsList, err := platformListScreenWindows()
	if err != nil {
		return screenWindow{}, err
	}
	for _, window := range windowsList {
		if window.Handle == hwnd {
			window.Active = true
			return window, nil
		}
	}
	title := screenWindowTitle(hwnd)
	rect, err := screenWindowRect(hwnd)
	if err != nil {
		return screenWindow{}, err
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return screenWindow{ID: fmt.Sprintf("0x%X", hwnd), Handle: hwnd, Title: title, ProcessID: pid, ProcessName: screenProcessName(pid), Bounds: rect, Active: true}, nil
}

func platformCaptureScreenWindow(window screenWindow) (screenCaptureFrame, error) {
	if window.Handle == 0 {
		return screenCaptureFrame{}, errors.New("window handle is invalid")
	}
	valid, _, _ := procIsWindow.Call(window.Handle)
	if valid == 0 {
		return screenCaptureFrame{}, errors.New("window is no longer available")
	}
	minimized, _, _ := procIsIconic.Call(window.Handle)
	if minimized != 0 {
		return screenCaptureFrame{}, errors.New("selected window is minimized; use the dormant Screen Vision capture path")
	}
	rect, err := screenWindowRect(window.Handle)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	return captureScreenRect(rect, window.Handle)
}

func platformCaptureScreenDesktop() (screenCaptureFrame, error) {
	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	width, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	height, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	rect := screenRect{X: int(int32(x)), Y: int(int32(y)), Width: int(int32(width)), Height: int(int32(height))}
	if err := validateScreenRect(rect); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("virtual desktop: %w", err)
	}
	return captureScreenRect(rect, 0)
}

// captureScreenRect never changes the target HWND's visibility, placement,
// activation, or Z-order. Window captures use only HWND-owned rendering paths
// first (PrintWindow / window DC), then Windows.Graphics.Capture for compositor
// and GPU-backed surfaces. Reading desktop pixels is permitted only for the
// actual foreground window or for an explicit whole-desktop capture.
func captureScreenRect(rect screenRect, hwnd uintptr) (screenCaptureFrame, error) {
	if err := validateScreenRect(rect); err != nil {
		return screenCaptureFrame{}, err
	}
	screenDC, _, callErr := procGetDC.Call(0)
	if screenDC == 0 {
		return screenCaptureFrame{}, fmt.Errorf("GetDC failed: %v", callErr)
	}
	defer procReleaseDC.Call(0, screenDC)
	memoryDC, _, callErr := procCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return screenCaptureFrame{}, fmt.Errorf("CreateCompatibleDC failed: %v", callErr)
	}
	defer procDeleteDC.Call(memoryDC)
	bitmap, _, callErr := procCreateCompatibleBitmap.Call(screenDC, uintptr(rect.Width), uintptr(rect.Height))
	if bitmap == 0 {
		return screenCaptureFrame{}, fmt.Errorf("CreateCompatibleBitmap failed: %v", callErr)
	}
	defer procDeleteObject.Call(bitmap)
	previous, _, callErr := procSelectObject.Call(memoryDC, bitmap)
	if previous == 0 || previous == ^uintptr(0) {
		return screenCaptureFrame{}, fmt.Errorf("SelectObject failed: %v", callErr)
	}
	defer procSelectObject.Call(memoryDC, previous)

	method := "bitblt-desktop"
	captured := uintptr(0)
	if hwnd != 0 {
		foreground, _, _ := procGetForegroundWindow.Call()
		screenFlushDWM()

		// Clear the destination before every HWND-owned attempt. Some GPU or
		// Chromium windows return success from PrintWindow without writing usable
		// pixels; a known black destination makes that failure detectable.
		screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
		captured, _, _ = procPrintWindow.Call(hwnd, memoryDC, pwRenderFullContent)
		if captured != 0 && !screenCapturedBitmapLikelyArtifact(screenDC, bitmap, rect.Width, rect.Height) {
			method = "print-window-full"
		} else {
			captured = 0
		}
		if captured == 0 {
			screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
			captured, _, _ = procPrintWindow.Call(hwnd, memoryDC, 0)
			if captured != 0 && !screenCapturedBitmapLikelyArtifact(screenDC, bitmap, rect.Width, rect.Height) {
				method = "print-window"
			} else {
				captured = 0
			}
		}
		if captured == 0 {
			screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
			windowDC, _, _ := procGetWindowDC.Call(hwnd)
			if windowDC != 0 {
				ok, _, _ := procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), windowDC, 0, 0, srcCopy|captureBLT)
				procReleaseDC.Call(hwnd, windowDC)
				if ok != 0 && !screenCapturedBitmapLikelyArtifact(screenDC, bitmap, rect.Width, rect.Height) {
					captured = 1
					method = "window-dc"
				}
			}
		}

		// PrintWindow commonly returns blank client surfaces for Chromium, WPF,
		// VMware, WebView2 and other GPU/compositor-heavy applications. Switch to
		// Windows.Graphics.Capture before considering any window-state mutation.
		var wgcErr error
		if captured == 0 {
			wgcFrame, captureErr := captureScreenWindowWGC(hwnd, rect)
			if captureErr == nil {
				return wgcFrame, nil
			}
			wgcErr = captureErr
		}

		if captured == 0 {
			if foreground != 0 && hwnd == foreground {
				screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
				ok, _, foregroundErr := procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), screenDC, uintptr(int32(rect.X)), uintptr(int32(rect.Y)), srcCopy|captureBLT)
				if ok == 0 {
					return screenCaptureFrame{}, fmt.Errorf("foreground BitBlt failed after Windows Graphics Capture fallback (%v): %v", wgcErr, foregroundErr)
				}
				captured = 1
				method = "screen-foreground-fallback"
			} else if wgcErr != nil {
				return screenCaptureFrame{}, fmt.Errorf("capture selected background window without desktop mutation: %w", wgcErr)
			} else {
				return screenCaptureFrame{}, errors.New("selected background window could not be captured without mutating the desktop")
			}
		}
	} else {
		screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
		ok, _, callErr := procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), screenDC, uintptr(int32(rect.X)), uintptr(int32(rect.Y)), srcCopy|captureBLT)
		if ok == 0 {
			return screenCaptureFrame{}, fmt.Errorf("BitBlt failed: %v", callErr)
		}
		captured = 1
	}
	if captured == 0 {
		return screenCaptureFrame{}, errors.New("screen capture did not produce pixels")
	}
	capturedImage, err := screenBitmapToNRGBA(screenDC, bitmap, rect.Width, rect.Height)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	return screenCaptureFrame{Image: capturedImage, Bounds: rect, Method: method}, nil
}

// restoreBackgroundWindowAfterReveal is retained as a state-restoration helper
// for the dormant off-screen fallback. No Screen Vision capture path reveals a
// background window on the user's visible desktop anymore.
func restoreBackgroundWindowAfterReveal(hwnd, originalAbove, foreground uintptr, wasTopmost, originalAboveSameBand bool) error {
	var restoreErrors []string
	if err := screenSetWindowPosition(hwnd, screenWindowBandInsertAfter(wasTopmost)); err != nil {
		restoreErrors = append(restoreErrors, fmt.Sprintf("restore topmost state: %v", err))
	}
	if originalAboveSameBand && originalAbove != 0 {
		valid, _, _ := procIsWindow.Call(originalAbove)
		if valid != 0 && screenWindowTopmost(originalAbove) == wasTopmost {
			if err := screenSetWindowPosition(hwnd, originalAbove); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("restore Z-order: %v", err))
			}
		}
	screenFlushDWM()

	// SWP_NOACTIVATE normally preserves foreground focus. If an application
	// activates itself in reaction to a state change, restore the user's exact
	// previous foreground window as a best-effort final cleanup step.
	if foreground != 0 {
		current, _, _ := procGetForegroundWindow.Call()
		if current != foreground {
			procSetForegroundWindow.Call(foreground)
		}
	}
	if len(restoreErrors) > 0 {
		return errors.New(strings.Join(restoreErrors, "; "))
	}
	return nil
}

func screenSetWindowPosition(hwnd, insertAfter uintptr) error {
	ok, _, callErr := procSetWindowPos.Call(hwnd, insertAfter, 0, 0, 0, 0, uintptr(screenRevealWindowPosFlags))
	if ok != 0 {
		return nil
	}
	if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
		return callErr
	}
	return errors.New("SetWindowPos failed")
}

func screenWindowBandInsertAfter(topmost bool) uintptr {
	if topmost {
		return ^uintptr(0) // HWND_TOPMOST (-1)
	}
	return ^uintptr(1) // HWND_NOTOPMOST (-2)
}

func screenWindowTopmost(hwnd uintptr) bool {
	style, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
	return uint32(style)&wsExTopmost != 0
}

func screenFlushDWM() {
	if err := procDwmFlush.Find(); err == nil {
		procDwmFlush.Call()
	}
}

// screenSetWindowCloak uses the documented DWMWA_CLOAK guard. A cloaked HWND
// remains composed by DWM but is not visible to the user, which lets the dormant
// fallback restore/move a window without exposing any intermediate on-screen
// placement. Callers must always uncloak during cleanup after the original
// hidden/minimized state has been restored.
func screenSetWindowCloak(hwnd uintptr, cloaked bool) error {
	if hwnd == 0 {
		return errors.New("window handle is invalid")
	}
	if err := procDwmSetWindowAttribute.Find(); err != nil {
		return fmt.Errorf("DwmSetWindowAttribute unavailable: %w", err)
	}
	var value uint32
	if cloaked {
		value = 1
	}
	result, _, callErr := procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaCloak,
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	if int32(result) != 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return fmt.Errorf("DwmSetWindowAttribute(DWMWA_CLOAK=%t) failed: HRESULT 0x%08X: %w", cloaked, uint32(result), callErr)
		}
		return fmt.Errorf("DwmSetWindowAttribute(DWMWA_CLOAK=%t) failed: HRESULT 0x%08X", cloaked, uint32(result))
	}
	screenFlushDWM()
	return nil
}

func screenClearCaptureBitmap(memoryDC uintptr, width, height int) {
	if memoryDC == 0 || width <= 0 || height <= 0 {
		return
	}
	procPatBlt.Call(memoryDC, 0, 0, uintptr(width), uintptr(height), blackness)
}

func screenCapturedBitmapLikelyBlank(dc, bitmap uintptr, width, height int) bool {
	capturedImage, err := screenBitmapToNRGBA(dc, bitmap, width, height)
	if err != nil {
		return false
	}
	return screenImageLikelyBlank(capturedImage)
}

func screenCapturedBitmapLikelyArtifact(dc, bitmap uintptr, width, height int) bool {
	capturedImage, err := screenBitmapToNRGBA(dc, bitmap, width, height)
	if err != nil {
		return false
	}
	return screenImageLikelyPrintWindowArtifact(capturedImage)
}

func screenImageLikelyBlank(capturedImage *image.NRGBA) bool {
	if capturedImage == nil {
		return false
	}
	bounds := capturedImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width < 32 || height < 32 {
		return false
	}

	left := bounds.Min.X + width/8
	right := bounds.Max.X - width/8
	top := bounds.Min.Y + height/5
	bottom := bounds.Max.Y - height/10
	stepX := width / 32
	stepY := height / 24
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}

	samples := 0
	nearBlack := 0
	for y := top; y < bottom; y += stepY {
		for x := left; x < right; x += stepX {
			color := capturedImage.NRGBAAt(x, y)
			samples++
			if color.R <= 8 && color.G <= 8 && color.B <= 8 {
				nearBlack++
			}
		}
	}
	return samples >= 64 && nearBlack*100 >= samples*98
}

// screenImageLikelyPrintWindowArtifact detects the common "successful but
// empty" PrintWindow outcomes. It is deliberately broader than the final blank
// test so a suspicious solid client surface is retried with WGC instead of being
// trusted as authoritative pixels.
func screenImageLikelyPrintWindowArtifact(capturedImage *image.NRGBA) bool {
	if capturedImage == nil {
		return false
	}
	if screenImageLikelyBlank(capturedImage) {
		return true
	}
	bounds := capturedImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width < 32 || height < 32 {
		return false
	}

	left := bounds.Min.X + width/8
	right := bounds.Max.X - width/8
	top := bounds.Min.Y + height/5
	bottom := bounds.Max.Y - height/10
	stepX := width / 32
	stepY := height / 24
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}
	samples := 0
	nearWhite := 0
	for y := top; y < bottom; y += stepY {
		for x := left; x < right; x += stepX {
			color := capturedImage.NRGBAAt(x, y)
			samples++
			if color.R >= 247 && color.G >= 247 && color.B >= 247 {
				nearWhite++
			}
		}
	}
	if samples >= 64 && nearWhite*100 >= samples*99 {
		return true
	}

	stepX = width / 40
	stepY = height / 30
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}
	minR, minG, minB := uint8(255), uint8(255), uint8(255)
	maxR, maxG, maxB := uint8(0), uint8(0), uint8(0)
	samples = 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			color := capturedImage.NRGBAAt(x, y)
			samples++
			if color.R < minR {
				minR = color.R
			}
			if color.G < minG {
				minG = color.G
			}
			if color.B < minB {
				minB = color.B
			}
			if color.R > maxR {
				maxR = color.R
			}
			if color.G > maxG {
				maxG = color.G
			}
			if color.B > maxB {
				maxB = color.B
			}
		}
	}
	return samples >= 64 && int(maxR)-int(minR) <= 2 && int(maxG)-int(minG) <= 2 && int(maxB)-int(minB) <= 2
}

func screenBitmapToNRGBA(dc, bitmap uintptr, width, height int) (*image.NRGBA, error) {
	pixels := make([]byte, width*height*4)
	info := bitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(width)
	info.Header.Height = -int32(height)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = biRGB
	lines, _, callErr := procGetDIBits.Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), dibRGBColors)
	if lines == 0 {
		return nil, fmt.Errorf("GetDIBits failed: %v", callErr)
	}
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for offset := 0; offset < len(pixels); offset += 4 {
		result.Pix[offset] = pixels[offset+2]
		result.Pix[offset+1] = pixels[offset+1]
		result.Pix[offset+2] = pixels[offset]
		result.Pix[offset+3] = 0xff
	}
	return result, nil
}

func screenWindowRect(hwnd uintptr) (screenRect, error) {
	var rect winRect
	if err := procDwmGetWindowAttribute.Find(); err == nil {
		result, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&rect)), unsafe.Sizeof(rect))
		if int32(result) != 0 {
			rect = winRect{}
		}
	}
	if rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		ok, _, callErr := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		if ok == 0 {
			return screenRect{}, fmt.Errorf("GetWindowRect failed: %v", callErr)
		}
	}
	result := screenRect{X: int(rect.Left), Y: int(rect.Top), Width: int(rect.Right - rect.Left), Height: int(rect.Bottom - rect.Top)}
	if err := validateScreenRect(result); err != nil {
		return screenRect{}, err
	}
	return result, nil
}

func validateScreenRect(rect screenRect) error {
	if rect.Width <= 0 || rect.Height <= 0 {
		return errors.New("capture area has no visible size")
	}
	pixels := int64(rect.Width) * int64(rect.Height)
	if pixels <= 0 || pixels > maxScreenCapturePixels {
		return fmt.Errorf("capture area is too large: %dx%d", rect.Width, rect.Height)
	}
	return nil
}

func screenWindowTitle(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, int(length)+1)
	written, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if written == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

func screenWindowCloaked(hwnd uintptr) bool {
	if err := procDwmGetWindowAttribute.Find(); err != nil {
		return false
	}
	var cloaked uint32
	result, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	return int32(result) == 0 && cloaked != 0
}

func screenProcessName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(pid))
	if handle == 0 {
		return ""
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := procQueryFullProcessImageNameW.Call(handle, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 || size == 0 || int(size) > len(buffer) {
		return ""
	}
	path := windows.UTF16ToString(buffer[:size])
	return filepath.Base(path)
}
