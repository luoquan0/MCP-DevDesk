//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

const nativeWindowOpenTimeout = 20 * time.Second

type nativeWindowState struct {
	mu        sync.Mutex
	view      webview2.WebView
	hwnd      uintptr
	opening   bool
	openingAt time.Time
	ready     chan struct{}
	openErr   error
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
			if !nativeWindowResponsive(hwnd, 2*time.Second) {
				return errors.New("WebView2 window is not responding")
			}
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
			return nil
		}
		c.native.view = nil
		c.native.hwnd = 0
	}
	if c.native.opening {
		ready := c.native.ready
		startedAt := c.native.openingAt
		c.native.mu.Unlock()
		return c.waitForNativeOpen(ready, startedAt)
	}

	c.native.opening = true
	c.native.openingAt = time.Now()
	c.native.openErr = nil
	c.native.ready = make(chan struct{})
	ready := c.native.ready
	startedAt := c.native.openingAt
	c.native.mu.Unlock()

	go c.runNativeWindow()
	return c.waitForNativeOpen(ready, startedAt)
}

func (c *windowsController) waitForNativeOpen(ready <-chan struct{}, startedAt time.Time) error {
	remaining := nativeWindowOpenTimeout - time.Since(startedAt)
	if remaining <= 0 {
		return fmt.Errorf("WebView2 window creation exceeded %s", nativeWindowOpenTimeout)
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ready:
	case <-timer.C:
		return fmt.Errorf("WebView2 window creation exceeded %s", nativeWindowOpenTimeout)
	}
	c.native.mu.Lock()
	err := c.native.openErr
	c.native.mu.Unlock()
	return err
}

func (c *windowsController) runNativeWindow() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var view webview2.WebView
	defer func() {
		if recovered := recover(); recovered != nil {
			c.finishNativeOpen(fmt.Errorf("WebView2 window panic: %v", recovered), nil, 0)
			if view != nil {
				view.Destroy()
			}
			c.clearNativeWindow(view)
		}
	}()

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

	view = webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataPath,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "MCP DevDesk",
			Width:  1200,
			Height: 800,
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
	centerNativeWindow(hwnd)
	c.finishNativeOpen(nil, view, hwnd)
	procSetForegroundWindow.Call(hwnd)

	view.Run()
	c.clearNativeWindow(view)
}

func (c *windowsController) clearNativeWindow(view webview2.WebView) {
	c.native.mu.Lock()
	if view == nil || c.native.view == view {
		c.native.view = nil
		c.native.hwnd = 0
	}
	c.native.mu.Unlock()
}

func centerNativeWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	var windowRect rect
	if result, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&windowRect))); result == 0 {
		return
	}
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if monitor == 0 {
		return
	}
	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if result, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); result == 0 {
		return
	}

	windowWidth := windowRect.Right - windowRect.Left
	windowHeight := windowRect.Bottom - windowRect.Top
	workWidth := info.RcWork.Right - info.RcWork.Left
	workHeight := info.RcWork.Bottom - info.RcWork.Top
	x := info.RcWork.Left
	y := info.RcWork.Top
	if windowWidth < workWidth {
		x += (workWidth - windowWidth) / 2
	}
	if windowHeight < workHeight {
		y += (workHeight - windowHeight) / 2
	}

	procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(x),
		uintptr(y),
		0,
		0,
		swpNoSize|swpNoZOrder|swpNoActivate,
	)
}

func (c *windowsController) finishNativeOpen(err error, view webview2.WebView, hwnd uintptr) {
	c.native.mu.Lock()
	if !c.native.opening {
		c.native.mu.Unlock()
		return
	}
	c.native.openErr = err
	c.native.view = view
	c.native.hwnd = hwnd
	c.native.opening = false
	c.native.openingAt = time.Time{}
	ready := c.native.ready
	c.native.ready = nil
	c.native.mu.Unlock()
	if ready != nil {
		close(ready)
	}
}

func (c *windowsController) closeNativeWindow() {
	c.native.mu.Lock()
	opening := c.native.opening
	startedAt := c.native.openingAt
	ready := c.native.ready
	view := c.native.view
	c.native.mu.Unlock()

	if opening && ready != nil {
		if err := c.waitForNativeOpen(ready, startedAt); err != nil {
			return
		}
		c.native.mu.Lock()
		view = c.native.view
		c.native.mu.Unlock()
	}
	if view != nil {
		view.Destroy()
	}
}
