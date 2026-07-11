//go:build windows

package mcpcore

import (
	"fmt"
	"syscall"
	"unsafe"
)

const moveFileReplaceExisting = 0x1

var (
	kernel32FileOps = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW = kernel32FileOps.NewProc("MoveFileExW")
)

func replaceFile(source, target string) error {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		moveFileReplaceExisting,
	)
	if result == 0 {
		return fmt.Errorf("MoveFileExW failed: %w", callErr)
	}
	return nil
}
