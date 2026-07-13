//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	bifReturnOnlyFSDirs = 0x0001
	bifEditBox          = 0x0010
	bifNewDialogStyle   = 0x0040
	bffmInitialized     = 1
	bffmSetSelectionW   = wmUser + 103
	coinitApartment     = 0x00000002
	rpcEChangedMode     = 0x80010106
)

var (
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	procCoUninitialize       = ole32.NewProc("CoUninitialize")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	folderBrowseCallback     = syscall.NewCallback(folderBrowseCallbackProc)
)

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	CallbackArg uintptr
	Image       int32
}

func (c *windowsController) PickFolder(initialPath, title string) (string, bool, error) {
	c.pickerMu.Lock()
	defer c.pickerMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	shouldUninitialize, err := initializeFolderPickerCOM()
	if err != nil {
		return "", false, err
	}
	if shouldUninitialize {
		defer procCoUninitialize.Call()
	}

	owner := c.folderPickerOwner()
	if owner != 0 {
		procSetForegroundWindow.Call(owner)
	}
	return browseForFolder(owner, nearestExistingFolder(initialPath), title)
}

func (c *windowsController) folderPickerOwner() uintptr {
	c.native.mu.Lock()
	nativeWindow := c.native.hwnd
	c.native.mu.Unlock()
	if nativeWindow != 0 {
		if valid, _, _ := procIsWindow.Call(nativeWindow); valid != 0 {
			return nativeWindow
		}
	}
	c.hwndMu.RLock()
	trayWindow := c.hwnd
	c.hwndMu.RUnlock()
	return trayWindow
}

func initializeFolderPickerCOM() (bool, error) {
	result, _, _ := procCoInitializeEx.Call(0, coinitApartment)
	hresult := uint32(result)
	switch hresult {
	case 0, 1:
		return true, nil
	case rpcEChangedMode:
		return false, nil
	default:
		if hresult&0x80000000 != 0 {
			return false, fmt.Errorf("initialize Windows folder picker: HRESULT 0x%08X", hresult)
		}
		return false, nil
	}
}

func browseForFolder(owner uintptr, initialPath, title string) (string, bool, error) {
	if strings.TrimSpace(title) == "" {
		title = "选择本地项目文件夹"
	}
	titleUTF16, err := syscall.UTF16FromString(title)
	if err != nil {
		return "", false, fmt.Errorf("encode folder picker title: %w", err)
	}
	displayName := make([]uint16, syscall.MAX_PATH)

	var initialUTF16 []uint16
	var initialPointer uintptr
	if strings.TrimSpace(initialPath) != "" {
		initialUTF16, err = syscall.UTF16FromString(initialPath)
		if err != nil {
			return "", false, fmt.Errorf("encode initial folder: %w", err)
		}
		initialPointer = uintptr(unsafe.Pointer(&initialUTF16[0]))
	}

	info := browseInfo{
		Owner:       owner,
		DisplayName: &displayName[0],
		Title:       &titleUTF16[0],
		Flags:       bifReturnOnlyFSDirs | bifEditBox | bifNewDialogStyle,
		Callback:    folderBrowseCallback,
		CallbackArg: initialPointer,
	}
	itemIDList, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(titleUTF16)
	runtime.KeepAlive(initialUTF16)
	if itemIDList == 0 {
		return "", true, nil
	}
	defer procCoTaskMemFree.Call(itemIDList)

	selectedPath := make([]uint16, 32768)
	ok, _, callErr := procSHGetPathFromIDListW.Call(itemIDList, uintptr(unsafe.Pointer(&selectedPath[0])))
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return "", false, fmt.Errorf("read selected folder: %w", callErr)
		}
		return "", false, errors.New("the selected item is not a local filesystem folder")
	}
	path := strings.TrimSpace(syscall.UTF16ToString(selectedPath))
	if path == "" {
		return "", false, errors.New("Windows returned an empty folder path")
	}
	return filepath.Clean(path), false, nil
}

func folderBrowseCallbackProc(hwnd, message, _, callbackArg uintptr) uintptr {
	if message == bffmInitialized && callbackArg != 0 {
		procSendMessageW.Call(hwnd, bffmSetSelectionW, 1, callbackArg)
	}
	return 0
}

func nearestExistingFolder(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	path, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return ""
	}
	for {
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}
