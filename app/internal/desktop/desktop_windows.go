//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"mcp-devdesk/internal/model"
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmNull          = 0x0000
	wmUser          = 0x0400
	wmTray          = wmUser + 7
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	idiApplication = 32512
	idcArrow       = 32512

	cmdOpen    = 1001
	cmdStart   = 1002
	cmdStop    = 1003
	cmdRestart = 1004
	cmdExit    = 1005

	errorAlreadyExists = 183
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	wndProcCallback         = syscall.NewCallback(windowProc)
	activeControllerMu      sync.RWMutex
	activeController        *windowsController
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          [16]byte
	HBalloonIcon      uintptr
}

type windowsController struct {
	url        string
	executable string
	edgePath   string
	callbacks  Callbacks
	done       chan struct{}
	ready      chan error
	closeOnce  sync.Once
	hwndMu     sync.RWMutex
	hwnd       uintptr
}

func New(url, executable string, callbacks Callbacks) Controller {
	controller := &windowsController{
		url:        url,
		executable: executable,
		edgePath:   findEdge(),
		callbacks:  callbacks,
		done:       make(chan struct{}),
		ready:      make(chan error, 1),
	}
	if controller.callbacks.Open == nil {
		controller.callbacks.Open = func() { _ = controller.Open() }
	}
	return controller
}

func AcquireSingleInstance() (alreadyRunning bool, release func(), err error) {
	name, _ := syscall.UTF16PtrFromString(`Local\MCPDevDesk.Manager`)
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return false, nil, fmt.Errorf("create single-instance mutex: %w", callErr)
	}
	already := callErr == syscall.Errno(errorAlreadyExists)
	return already, func() { procCloseHandle.Call(handle) }, nil
}

func OpenDashboard(url string) error {
	edge := findEdge()
	if edge != "" {
		return startEdgeApp(edge, url)
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func (c *windowsController) Start() error {
	go c.runTray()
	return <-c.ready
}

func (c *windowsController) Open() error {
	if c.edgePath != "" {
		return startEdgeApp(c.edgePath, c.url)
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", c.url).Start()
}

func (c *windowsController) Status() model.DesktopStatus {
	return model.DesktopStatus{
		Available:       true,
		AppMode:         c.edgePath != "",
		EdgePath:        c.edgePath,
		StartupEnabled:  startupEnabled(c.executable),
		TrayAvailable:   true,
		SingleInstance:  true,
		DashboardURL:    c.url,
		WindowModeLabel: windowModeLabel(c.edgePath),
	}
}

func (c *windowsController) SetStartup(enabled bool) error {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	const valueName = "MCPDevDesk"
	if enabled {
		data := `"` + c.executable + `" --background`
		output, err := hiddenCommand("reg.exe", "ADD", key, "/v", valueName, "/t", "REG_SZ", "/d", data, "/f").CombinedOutput()
		if err != nil {
			return fmt.Errorf("enable startup: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	output, err := hiddenCommand("reg.exe", "DELETE", key, "/v", valueName, "/f").CombinedOutput()
	if err != nil {
		text := strings.ToLower(string(output))
		if strings.Contains(text, "unable to find") || strings.Contains(text, "cannot find") || strings.Contains(text, "找不到") {
			return nil
		}
		return fmt.Errorf("disable startup: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *windowsController) Done() <-chan struct{} { return c.done }

func (c *windowsController) Close() error {
	c.hwndMu.RLock()
	hwnd := c.hwnd
	c.hwndMu.RUnlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
	return nil
}

func (c *windowsController) runTray() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	activeControllerMu.Lock()
	activeController = c
	activeControllerMu.Unlock()
	defer func() {
		activeControllerMu.Lock()
		if activeController == c {
			activeController = nil
		}
		activeControllerMu.Unlock()
	}()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("MCPDevDeskTrayWindow")
	title, _ := syscall.UTF16PtrFromString("MCP DevDesk")
	hIcon, _, _ := procLoadIconW.Call(0, idiApplication)
	hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	class := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   wndProcCallback,
		HInstance:     hInstance,
		HIcon:         hIcon,
		HCursor:       hCursor,
		LpszClassName: className,
		HIconSm:       hIcon,
	}
	atom, _, registerErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		c.ready <- fmt.Errorf("register tray window class: %w", registerErr)
		return
	}
	hwnd, _, createErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		0,
		0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		c.ready <- fmt.Errorf("create tray window: %w", createErr)
		return
	}
	c.hwndMu.Lock()
	c.hwnd = hwnd
	c.hwndMu.Unlock()

	iconData := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTray,
		HIcon:            hIcon,
	}
	copyUTF16(iconData.SzTip[:], "MCP DevDesk - 本地开发网关")
	result, _, notifyErr := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&iconData)))
	if result == 0 {
		procDestroyWindow.Call(hwnd)
		c.ready <- fmt.Errorf("add tray icon: %w", notifyErr)
		return
	}
	c.ready <- nil

	var message msg
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	activeControllerMu.RLock()
	controller := activeController
	activeControllerMu.RUnlock()

	switch message {
	case wmTray:
		if controller == nil {
			break
		}
		switch uint32(lParam) {
		case wmLButtonDblClk:
			go controller.invoke(controller.callbacks.Open)
			return 0
		case wmRButtonUp:
			controller.showMenu(hwnd)
			return 0
		}
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if controller != nil {
			iconData := notifyIconData{CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: hwnd, UID: 1}
			procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&iconData)))
			controller.closeOnce.Do(func() { close(controller.done) })
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (c *windowsController) showMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, mfString, cmdOpen, "打开 MCP DevDesk")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdStart, "启动全部服务")
	appendMenu(menu, mfString, cmdStop, "停止全部服务")
	appendMenu(menu, mfString, cmdRestart, "重新启动服务")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdExit, "退出 MCP DevDesk")

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(hwnd)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightButton|tpmReturnCmd|tpmBottomAlign,
		uintptr(cursor.X), uintptr(cursor.Y), 0,
		hwnd, 0,
	)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	switch command {
	case cmdOpen:
		go c.invoke(c.callbacks.Open)
	case cmdStart:
		go c.invoke(c.callbacks.Start)
	case cmdStop:
		go c.invoke(c.callbacks.Stop)
	case cmdRestart:
		go c.invoke(c.callbacks.Restart)
	case cmdExit:
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
}

func (c *windowsController) invoke(callback func()) {
	if callback != nil {
		callback()
	}
}

func appendMenu(menu uintptr, flags uint32, id uintptr, label string) {
	var labelPtr uintptr
	if label != "" {
		text, _ := syscall.UTF16PtrFromString(label)
		labelPtr = uintptr(unsafe.Pointer(text))
	}
	procAppendMenuW.Call(menu, uintptr(flags), id, labelPtr)
}

func copyUTF16(destination []uint16, value string) {
	encoded, _ := syscall.UTF16FromString(value)
	copy(destination, encoded)
}

func findEdge() string {
	candidates := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	if drive := strings.TrimSpace(os.Getenv("SystemDrive")); validDriveLetter(drive) {
		candidates = append(candidates,
			filepath.Join(drive+string(os.PathSeparator), "Program Files (x86)", "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(drive+string(os.PathSeparator), "Program Files", "Microsoft", "Edge", "Application", "msedge.exe"),
		)
	}
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	if path, err := exec.LookPath("msedge.exe"); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			absolute, _ := filepath.Abs(candidate)
			return absolute
		}
	}
	return ""
}

func startEdgeApp(edgePath, url string) error {
	args := []string{
		"--app=" + url,
		"--start-maximized",
		"--no-first-run",
		"--disable-features=msEdgeSidebarV2",
	}
	command := exec.Command(edgePath, args...)
	command.Env = normalizedWindowsEnvironment()
	return command.Start()
}

func startupEnabled(executable string) bool {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	output, err := hiddenCommand("reg.exe", "QUERY", key, "/v", "MCPDevDesk").CombinedOutput()
	if err != nil {
		return false
	}
	text := strings.ToLower(string(output))
	wanted := strings.ToLower(filepath.Clean(executable))
	return strings.Contains(text, strings.ToLower("MCPDevDesk")) && strings.Contains(text, wanted)
}

func windowModeLabel(edgePath string) string {
	if edgePath != "" {
		return "Windows 独立应用窗口（Edge App 模式）"
	}
	return "系统默认浏览器"
}

func hiddenCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command
}

func validDriveLetter(value string) bool {
	if len(value) != 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return (letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')
}

func normalizedWindowsEnvironment() []string {
	drive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if !validDriveLetter(drive) {
		if volume := filepath.VolumeName(os.Getenv("SystemRoot")); validDriveLetter(volume) {
			drive = volume
		} else {
			drive = "C:"
		}
	}

	overrides := map[string]string{
		"SystemDrive":        drive,
		"ProgramData":        filepath.Join(drive+string(os.PathSeparator), "ProgramData"),
		"ProgramFiles":       filepath.Join(drive+string(os.PathSeparator), "Program Files"),
		"ProgramFiles(x86)":  filepath.Join(drive+string(os.PathSeparator), "Program Files (x86)"),
		"CommonProgramFiles": filepath.Join(drive+string(os.PathSeparator), "Program Files", "Common Files"),
	}
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		home := filepath.Clean(current.HomeDir)
		volume := filepath.VolumeName(home)
		overrides["USERPROFILE"] = home
		overrides["HOME"] = home
		overrides["HOMEDRIVE"] = volume
		overrides["HOMEPATH"] = strings.TrimPrefix(home, volume)
		overrides["LOCALAPPDATA"] = filepath.Join(home, "AppData", "Local")
		overrides["APPDATA"] = filepath.Join(home, "AppData", "Roaming")
	}

	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		replaced := false
		for overrideKey := range overrides {
			if strings.EqualFold(key, overrideKey) {
				replaced = true
				break
			}
		}
		if !replaced {
			entry = strings.ReplaceAll(entry, "%SystemDrive%", drive)
			entry = strings.ReplaceAll(entry, "%SYSTEMDRIVE%", drive)
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}
