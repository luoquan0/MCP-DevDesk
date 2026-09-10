//go:build windows

package mcpcore

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	screenGetClassName = screenUser32.NewProc("GetClassNameW")
	screenEnumChildren = screenUser32.NewProc("EnumChildWindows")
	screenIsHungWindow = screenUser32.NewProc("IsHungAppWindow")
	// Register once. syscall.NewCallback allocates a thunk that is never freed.
	screenPrintChildCallback = syscall.NewCallback(screenInspectPrintChild)
)

type screenPrintClassScan struct {
	Count int
	GPU   bool
}

func screenGPUWindowClass(class string) bool {
	value := strings.ToLower(strings.TrimSpace(class))
	return strings.HasPrefix(value, "chrome_") ||
		strings.HasPrefix(value, "hwndwrapper[") ||
		strings.Contains(value, "webview") ||
		strings.Contains(value, "d3d") ||
		strings.HasPrefix(value, "windows.ui.") ||
		strings.HasPrefix(value, "winuidesktopwin32windowclass")
}

func screenWindowClassName(hwnd uintptr) string {
	var buffer [256]uint16
	n, _, _ := screenGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer[:])
}

func screenInspectPrintChild(hwnd, parameter uintptr) uintptr {
	scan := (*screenPrintClassScan)(unsafe.Pointer(parameter))
	scan.Count++
	if screenGPUWindowClass(screenWindowClassName(hwnd)) {
		scan.GPU = true
		return 0
	}
	// An incomplete scan is not permission to synchronously paint a complex UI.
	if scan.Count >= 512 {
		scan.GPU = true
		return 0
	}
	return 1
}

// PrintWindow asks the target GUI thread to paint synchronously. For known
// GPU/WebView2/WPF hosts, require WGC rather than falling back to that GUI-thread
// request after compositor capture failed. No window state is changed here.
func screenPrintWindowAllowed(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	if screenIsHungWindow.Find() == nil {
		if hung, _, _ := screenIsHungWindow.Call(hwnd); hung != 0 {
			return false
		}
	}
	class := screenWindowClassName(hwnd)
	if class == "" || screenGPUWindowClass(class) {
		return false
	}
	scan := screenPrintClassScan{}
	screenEnumChildren.Call(hwnd, screenPrintChildCallback, uintptr(unsafe.Pointer(&scan)))
	return !scan.GPU
}
