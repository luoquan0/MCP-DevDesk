//go:build windows

package mcpcore

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

func TestScreenKnownGPUClassesNeverUsePrintWindow(t *testing.T) {
	for _, class := range []string{"Chrome_WidgetWin_1", "Chrome_RenderWidgetHostHWND", "HwndWrapper[v2rayN.exe;;123]", "webview", "Intermediate D3D Window", "WinUIDesktopWin32WindowClass"} {
		if !screenGPUWindowClass(class) {
			t.Errorf("GPU class incorrectly eligible for synchronous printing: %s", class)
		}
	}
	for _, class := range []string{"Notepad", "CabinetWClass", "MCPDevDeskCaptureIsolatedFixture"} {
		if screenGPUWindowClass(class) {
			t.Errorf("traditional GDI class incorrectly excluded: %s", class)
		}
	}
}

var testCOMCallbackOnce sync.Once
var testCOMCallback uintptr

func TestScreenCOMCallRetainsPointerArguments(t *testing.T) {
	source, err := os.ReadFile("screen_wgc_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "//go:uintptrescapes\nfunc screenCOMCall") {
		t.Fatal("native pointer-lifetime directive was removed")
	}
	testCOMCallbackOnce.Do(func() {
		testCOMCallback = syscall.NewCallback(func(_ uintptr, output uintptr) uintptr {
			runtime.GC()
			*(*uint64)(unsafe.Pointer(output)) = 0xFEDCBA9876543210
			return 0
		})
	})
	table := [4]uintptr{3: testCOMCallback}
	object := struct{ VTable *[4]uintptr }{VTable: &table}
	for i := 0; i < 12; i++ {
		var output uint64
		status := screenCOMCall(uintptr(unsafe.Pointer(&object)), 3, uintptr(unsafe.Pointer(&output)))
		if status != 0 || output != 0xFEDCBA9876543210 {
			t.Fatalf("COM output pointer lost under GC: status=%x output=%x", status, output)
		}
	}
	runtime.KeepAlive(object)
}
