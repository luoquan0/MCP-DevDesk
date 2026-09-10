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
	dwmwaCloaked             = 14
	processQueryLimitedInfo  = 0x1000
	pwRenderFullContent      = 0x00000002
	gwHwndPrev               = 3
	gwlExStyle               = 0xFFFFFFEC
	srcCopy                  = 0x00CC0020
	captureBLT               = 0x40000000
	blackness                = 0x00000042
	dibRGBColors             = 0
	biRGB                    = 0
	maxScreenCapturePixels   = 40_000_000
)

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
	procGetWindowTextLengthW     = screenUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW           = screenUser32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessID = screenUser32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow      = screenUser32.NewProc("GetForegroundWindow")
	procGetWindowRect            = screenUser32.NewProc("GetWindowRect")
	procPrintWindow              = screenUser32.NewProc("PrintWindow")
	procGetDC                    = screenUser32.NewProc("GetDC")
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
	return platformCaptureScreenWindowForVision(window)
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
	return screenCaptureIsolated(screenWorkerRequest{Mode: "desktop", Bounds: rect})
}

// captureScreenRect runs only inside the short-lived capture worker. No window
// capture reads the desktop DC or changes an application's native window state.
func captureScreenRect(rect screenRect, hwnd uintptr) (screenCaptureFrame, error) {
	if err := validateScreenRect(rect); err != nil {
		return screenCaptureFrame{}, err
	}
	if hwnd == 0 {
		return screenCaptureGDI(rect, 0)
	}
	// Prefer the compositor. PrintWindow is a compatibility path, not the first
	// operation against a Chromium/WebView2/WPF application.
	frame, wgcErr := captureScreenWindowWGC(hwnd, rect)
	if wgcErr == nil {
		return frame, nil
	}
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic != 0 {
		// PrintWindow may report success but paint only the minimized icon
		// rectangle into a normal-sized bitmap. Never return that partial surface
		// as a complete app window, and never restore the app to obtain pixels.
		return screenCaptureFrame{}, fmt.Errorf("minimized compositor surface unavailable: %w; skipped icon-clipped PrintWindow output", wgcErr)
	}
	if !screenPrintWindowAllowed(hwnd) {
		return screenCaptureFrame{}, fmt.Errorf("compositor capture failed: %w; synchronous PrintWindow is disabled for GPU/WebView/WPF or unresponsive windows", wgcErr)
	}
	frame, printErr := screenCaptureGDI(rect, hwnd)
	if printErr == nil {
		return frame, nil
	}
	return screenCaptureFrame{}, fmt.Errorf("state-neutral capture failed; WGC: %v; PrintWindow: %w; target state was not modified", wgcErr, printErr)
}

// screenCaptureGDI owns its DCs on one worker OS thread. GetDIBits requires the
// bitmap to be deselected; an unreadable bitmap is an error, never valid pixels.
func screenCaptureGDI(rect screenRect, hwnd uintptr) (screenCaptureFrame, error) {
	if err := validateScreenRect(rect); err != nil {
		return screenCaptureFrame{}, err
	}
	dc, _, callErr := procGetDC.Call(0)
	if dc == 0 {
		return screenCaptureFrame{}, fmt.Errorf("GetDC: %v", callErr)
	}
	defer procReleaseDC.Call(0, dc)
	memoryDC, _, callErr := procCreateCompatibleDC.Call(dc)
	if memoryDC == 0 {
		return screenCaptureFrame{}, fmt.Errorf("CreateCompatibleDC: %v", callErr)
	}
	defer procDeleteDC.Call(memoryDC)
	bitmap, _, callErr := procCreateCompatibleBitmap.Call(dc, uintptr(rect.Width), uintptr(rect.Height))
	if bitmap == 0 {
		return screenCaptureFrame{}, fmt.Errorf("CreateCompatibleBitmap: %v", callErr)
	}
	defer procDeleteObject.Call(bitmap)
	flags := []uintptr{pwRenderFullContent, 0}
	if hwnd == 0 {
		flags = []uintptr{0}
	}
	var lastErr error
	for _, flag := range flags {
		previous, _, selectErr := procSelectObject.Call(memoryDC, bitmap)
		if previous == 0 || previous == ^uintptr(0) {
			return screenCaptureFrame{}, fmt.Errorf("SelectObject: %v", selectErr)
		}
		screenClearCaptureBitmap(memoryDC, rect.Width, rect.Height)
		var ok uintptr
		method := "bitblt-desktop"
		if hwnd == 0 {
			ok, _, callErr = procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), dc, uintptr(int32(rect.X)), uintptr(int32(rect.Y)), srcCopy|captureBLT)
		} else {
			ok, _, callErr = procPrintWindow.Call(hwnd, memoryDC, flag)
			method = "print-window"
			if flag == pwRenderFullContent {
				method = "print-window-full"
			}
		}
		selected, _, deselectErr := procSelectObject.Call(memoryDC, previous)
		if selected == 0 || selected == ^uintptr(0) {
			return screenCaptureFrame{}, fmt.Errorf("deselect capture bitmap: %v", deselectErr)
		}
		if ok == 0 {
			lastErr = fmt.Errorf("%s failed: %v", method, callErr)
			continue
		}
		pixels, err := screenBitmapToNRGBA(dc, bitmap, rect.Width, rect.Height)
		if err != nil {
			lastErr = err
			continue
		}
		if hwnd != 0 && screenImageLikelyPrintWindowArtifact(pixels) {
			lastErr = errors.New("PrintWindow returned a blank/solid surface, not a verified application frame")
			continue
		}
		return screenCaptureFrame{Image: pixels, Bounds: rect, Method: method}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("GDI capture produced no pixels")
	}
	return screenCaptureFrame{}, lastErr
}

func screenFlushDWM() {
	if err := procDwmFlush.Find(); err == nil {
		procDwmFlush.Call()
	}
}

func screenClearCaptureBitmap(memoryDC uintptr, width, height int) {
	if memoryDC == 0 || width <= 0 || height <= 0 {
		return
	}
	procPatBlt.Call(memoryDC, 0, 0, uintptr(width), uintptr(height), blackness)
}

func screenImageLikelyBlank(capturedImage *image.NRGBA) bool {
	if capturedImage == nil {
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
		return true
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
	if int(lines) != height {
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
	if rect.Width <= 0 || rect.Height <= 0 || rect.Width > 32768 || rect.Height > 32768 {
		return errors.New("capture dimensions must be between 1 and 32768")
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
