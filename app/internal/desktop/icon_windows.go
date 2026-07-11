//go:build windows

package desktop

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	imageIcon      = 1
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
)

//go:embed assets/mcp-devdesk.ico
var embeddedApplicationIcon []byte

func (c *windowsController) applicationIcon() uintptr {
	c.iconOnce.Do(func() {
		assetDir := filepath.Join(c.dataPath, "assets")
		iconPath := filepath.Join(assetDir, "mcp-devdesk.ico")
		if err := os.MkdirAll(assetDir, 0o700); err == nil {
			current, readErr := os.ReadFile(iconPath)
			if readErr != nil || !bytes.Equal(current, embeddedApplicationIcon) {
				_ = os.WriteFile(iconPath, embeddedApplicationIcon, 0o600)
			}
			if path, err := syscall.UTF16PtrFromString(iconPath); err == nil {
				icon, _, _ := procLoadImageW.Call(
					0,
					uintptr(unsafe.Pointer(path)),
					imageIcon,
					0,
					0,
					lrLoadFromFile|lrDefaultSize,
				)
				c.iconHandle = icon
			}
		}
		if c.iconHandle == 0 {
			c.iconHandle, _, _ = procLoadIconW.Call(0, idiApplication)
		}
	})
	return c.iconHandle
}

func setApplicationWindowIcon(hwnd, icon uintptr) {
	if hwnd == 0 || icon == 0 {
		return
	}
	procSendMessageW.Call(hwnd, wmSetIcon, iconBig, icon)
	procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, icon)
}
