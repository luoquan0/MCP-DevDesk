//go:build windows

package mcpcore

import (
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

const windowsUTF8CodePage = 65001

var (
	commandOutputKernel32            = syscall.NewLazyDLL("kernel32.dll")
	commandOutputGetOEMCP            = commandOutputKernel32.NewProc("GetOEMCP")
	commandOutputMultiByteToWideChar = commandOutputKernel32.NewProc("MultiByteToWideChar")
)

func normalizeCommandOutput(data []byte) string {
	return normalizeCommandOutputWithCodePage(data, currentWindowsOEMCodePage())
}

func currentWindowsOEMCodePage() uint32 {
	codePage, _, _ := commandOutputGetOEMCP.Call()
	return uint32(codePage)
}

func normalizeCommandOutputWithCodePage(data []byte, codePage uint32) string {
	if len(data) == 0 {
		return ""
	}
	// Go, Node, PowerShell, and many modern tools already emit UTF-8.
	// Preserve valid UTF-8 byte-for-byte instead of applying an OEM decode.
	if utf8.Valid(data) {
		return string(data)
	}
	if codePage == 0 || codePage == windowsUTF8CodePage {
		return strings.ToValidUTF8(string(data), "\uFFFD")
	}
	decoded, err := decodeWindowsCodePage(data, codePage)
	if err != nil {
		return strings.ToValidUTF8(string(data), "\uFFFD")
	}
	return decoded
}

func decodeWindowsCodePage(data []byte, codePage uint32) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	required, _, callErr := commandOutputMultiByteToWideChar.Call(
		uintptr(codePage),
		0,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		0,
		0,
	)
	if required == 0 {
		return "", fmt.Errorf("MultiByteToWideChar sizing failed for code page %d: %v", codePage, callErr)
	}
	wide := make([]uint16, int(required))
	written, _, callErr := commandOutputMultiByteToWideChar.Call(
		uintptr(codePage),
		0,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&wide[0])),
		required,
	)
	if written == 0 {
		return "", fmt.Errorf("MultiByteToWideChar decode failed for code page %d: %v", codePage, callErr)
	}
	return string(utf16.Decode(wide[:int(written)])), nil
}
