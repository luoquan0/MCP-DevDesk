//go:build windows

package desktop

import (
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"unsafe"
)

func TestOpenDashboardRequestsExistingNativeWindow(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		if request.URL.Path != "/api/ui/open" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		called <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	if err := OpenDashboard(server.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	default:
		t.Fatal("native window endpoint was not called")
	}
}

func TestTrayIconDataPreservesCallbackIdentityForRecovery(t *testing.T) {
	const (
		hwnd = uintptr(0x1234)
		icon = uintptr(0x5678)
	)
	data := newTrayIconData(hwnd, icon)
	if data.CbSize != uint32(unsafe.Sizeof(notifyIconData{})) ||
		data.HWnd != hwnd || data.UID != 1 ||
		data.UFlags != nifMessage|nifIcon|nifTip ||
		data.UCallbackMessage != wmTray || data.HIcon != icon {
		t.Fatalf("tray icon identity or callback changed: %+v", data)
	}
	if tip := syscall.UTF16ToString(data.SzTip[:]); tip != "MCP DevDesk - 本地开发网关" {
		t.Fatalf("tray icon tooltip = %q", tip)
	}
}
