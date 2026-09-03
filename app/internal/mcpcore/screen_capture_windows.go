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
	srcCopy                  = 0x00CC0020
	captureBLT               = 0x40000000
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
	procGetDIBits              = screenGDI32.NewProc("GetDIBits")

	procDwmGetWindowAttribute      = screenDWMAPI.NewProc("DwmGetWindowAttribute")
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
		if valid == 0 || visible == 0 || screenWindowCloaked(hwnd) {
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
		// PrintWindow is the preferred path because it asks the selected HWND to
		// render itself even when another application overlaps it. If the first
		// mode is unsupported, retry the classic PrintWindow mode before falling
		// back to the selected window's own DC.
		captured, _, _ = procPrintWindow.Call(hwnd, memoryDC, pwRenderFullContent)
		if captured != 0 {
			method = "print-window-full"
		} else {
			captured, _, _ = procPrintWindow.Call(hwnd, memoryDC, 0)
			if captured != 0 {
				method = "print-window"
			}
		}
		if captured == 0 {
			windowDC, _, _ := procGetWindowDC.Call(hwnd)
			if windowDC != 0 {
				ok, _, _ := procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), windowDC, 0, 0, srcCopy|captureBLT)
				procReleaseDC.Call(hwnd, windowDC)
				if ok != 0 {
					captured = 1
					method = "window-dc"
				}
			}
		}
		if captured == 0 {
			foreground, _, _ := procGetForegroundWindow.Call()
			if !screenRegionFallbackSafe(hwnd, foreground) {
				return screenCaptureFrame{}, errors.New("the selected window could not render its own pixels while it is in the background; refusing a desktop-region fallback because that could capture another window. Bring the selected window to the foreground and retry")
			}
			ok, _, callErr := procBitBlt.Call(memoryDC, 0, 0, uintptr(rect.Width), uintptr(rect.Height), screenDC, uintptr(int32(rect.X)), uintptr(int32(rect.Y)), srcCopy|captureBLT)
			if ok == 0 {
				return screenCaptureFrame{}, fmt.Errorf("foreground BitBlt failed: %v", callErr)
			}
			captured = 1
			method = "screen-foreground-fallback"
		}
	} else {
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

func screenRegionFallbackSafe(hwnd, foreground uintptr) bool {
	return hwnd != 0 && hwnd == foreground
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
