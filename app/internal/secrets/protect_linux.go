//go:build linux

package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var linuxEnvelopeMagic = []byte("MCPDD-LINUX-AESGCM-1\x00")

func linuxKeyPath() (string, error) {
	if value := os.Getenv("MCP_DEVDESK_KEY_FILE"); value != "" {
		return filepath.Abs(value)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcp-devdesk", "master.key"), nil
}

func linuxMasterKey(create bool) ([]byte, error) {
	path, err := linuxKeyPath()
	if err != nil {
		return nil, err
	}
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
			temp, err := os.CreateTemp(filepath.Dir(path), ".master-key-*")
			if err != nil {
				return nil, err
			}
			tmp := temp.Name()
			defer os.Remove(tmp)
			_, writeErr := temp.Write(key)
			syncErr := temp.Sync()
			closeErr := temp.Close()
			if writeErr != nil {
				return nil, writeErr
			}
			if syncErr != nil {
				return nil, syncErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			// Link atomically publishes a fully-written key without overwriting a key
			// another manager/core created concurrently. Never create keys on decryption.
			if err := os.Link(tmp, path); err != nil && !os.IsExist(err) {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open Linux master key (restore the original key if lost): %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("Linux master key must be an owner-only regular file (chmod 600), owned by the service user")
	}
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid Linux master key length; existing key was not overwritten")
	}
	return key, nil
}

func linuxAEAD(create bool) (cipher.AEAD, error) {
	key, err := linuxMasterKey(create)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func protectData(value []byte) ([]byte, error) {
	aead, err := linuxAEAD(true)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := append([]byte(nil), linuxEnvelopeMagic...)
	result = append(result, nonce...)
	return aead.Seal(result, nonce, value, linuxEnvelopeMagic), nil
}

func unprotectData(value []byte) ([]byte, error) {
	if !bytes.HasPrefix(value, linuxEnvelopeMagic) {
		return nil, errors.New("not a Linux encrypted secret; Windows DPAPI and unprotected experimental envelopes are not portable")
	}
	aead, err := linuxAEAD(false)
	if err != nil {
		return nil, err
	}
	data := value[len(linuxEnvelopeMagic):]
	if len(data) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("truncated Linux encrypted secret")
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], linuxEnvelopeMagic)
}

func protectionName() string    { return "linux-aes256-gcm-v1" }
func encryptionAvailable() bool { return true }
