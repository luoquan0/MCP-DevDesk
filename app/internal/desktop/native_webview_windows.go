//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

type nativeWindowState struct {
	mu      sync.Mutex
	view    webview2.WebView
	hwnd    uintptr
	opening bool
	ready   chan struct{}
	openErr error
}

var (
	nativeRuntimeOnce      sync.Once
	nativeRuntimeVersion   string
	nativeRuntimeAvailable bool
)

func nativeRuntimeStatus() (string, bool) {
	nativeRuntimeOnce.Do(func() {
		version, err := webviewloader.GetInstalledVersion()
		nativeRuntimeVersion = version
		nativeRuntimeAvailable = err == nil && version != ""
	})
	return nativeRuntimeVersion, nativeRuntimeAvailable
}

func (c *windowsController) openNativeWindow() error {
	c.native.mu.Lock()
	if c.native.view != nil && c.native.hwnd != 0 {
		hwnd := c.native.hwnd
		valid, _, _ := procIsWindow.Call(hwnd)
		if valid != 0 {
			c.native.mu.Unlock()
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
			return nil
		}
		c.native.view = nil
		c.native.hwnd = 0
	}
	if c.native.opening {
		ready := c.native.ready
		c.native.mu.Unlock()
		<-ready
		c.native.mu.Lock()
		err := c.native.openErr
		c.native.mu.Unlock()
		return err
	}

	c.native.opening = true
	c.native.openErr = nil
	c.native.ready = make(chan struct{})
	ready := c.native.ready
	c.native.mu.Unlock()

	go c.runNativeWindow()
	<-ready
	c.native.mu.Lock()
	err := c.native.openErr
	c.native.mu.Unlock()
	return err
}

func (c *windowsController) runNativeWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	_, available := nativeRuntimeStatus()
	if !available {
		c.finishNativeOpen(errors.New("未检测到 Microsoft Edge WebView2 Runtime，无法创建内嵌 Windows 窗口"), nil, 0)
		return
	}

	dataPath := filepath.Join(c.dataPath, "webview2")
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		c.finishNativeOpen(fmt.Errorf("创建 WebView2 数据目录失败: %w", err), nil, 0)
		return
	}

	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataPath,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "MCP DevDesk",
			Width:  1340,
			Height: 880,
			Center: true,
		},
	})
	if view == nil {
		c.finishNativeOpen(errors.New("创建 WebView2 原生窗口失败"), nil, 0)
		return
	}

	view.SetSize(960, 640, webview2.HintMin)
	view.Navigate(c.url)
	hwnd := uintptr(view.Window())
	setApplicationWindowIcon(hwnd, c.applicationIcon())
	c.finishNativeOpen(nil, view, hwnd)
	procSetForegroundWindow.Call(hwnd)

	view.Run()

	c.native.mu.Lock()
	if c.native.view == view {
		c.native.view = nil
		c.native.hwnd = 0
	}
	c.native.mu.Unlock()
}

func (c *windowsController) finishNativeOpen(err error, view webview2.WebView, hwnd uintptr) {
	c.native.mu.Lock()
	c.native.openErr = err
	c.native.view = view
	c.native.hwnd = hwnd
	c.native.opening = false
	ready := c.native.ready
	c.native.mu.Unlock()
	if ready != nil {
		close(ready)
	}
}

func (c *windowsController) closeNativeWindow() {
	c.native.mu.Lock()
	opening := c.native.opening
	ready := c.native.ready
	view := c.native.view
	c.native.mu.Unlock()

	if opening && ready != nil {
		<-ready
		c.native.mu.Lock()
		view = c.native.view
		c.native.mu.Unlock()
	}
	if view != nil {
		view.Destroy()
	}
}
