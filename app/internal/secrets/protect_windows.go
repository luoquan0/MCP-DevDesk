//go:build windows

package secrets

import (
	"fmt"
	"syscall"
	"unsafe"
)

const cryptProtectUIForbidden = 0x1

type dataBlob struct {
	Size uint32
	Data *byte
}

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	kernel32Secrets        = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFreeSecrets   = kernel32Secrets.NewProc("LocalFree")
)

func protectData(value []byte) ([]byte, error) {
	input := blobFromBytes(value)
	var output dataBlob
	result, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, fmt.Errorf("CryptProtectData failed: %w", callErr)
	}
	defer procLocalFreeSecrets.Call(uintptr(unsafe.Pointer(output.Data)))
	return copyBlob(output), nil
}

func unprotectData(value []byte) ([]byte, error) {
	input := blobFromBytes(value)
	var output dataBlob
	result, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, fmt.Errorf("CryptUnprotectData failed: %w", callErr)
	}
	defer procLocalFreeSecrets.Call(uintptr(unsafe.Pointer(output.Data)))
	return copyBlob(output), nil
}

func blobFromBytes(value []byte) dataBlob {
	if len(value) == 0 {
		return dataBlob{}
	}
	return dataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func copyBlob(blob dataBlob) []byte {
	if blob.Size == 0 || blob.Data == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(blob.Data, int(blob.Size))...)
}

func protectionName() string    { return "windows-dpapi-current-user" }
func encryptionAvailable() bool { return true }
