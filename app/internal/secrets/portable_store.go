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
)

const (
	portableSecretEnvelopeVersion = 3
	portableSecretProtection      = "portable-aes256-gcm-v1"
	portableMasterKeyName         = "master.key"
)

var (
	portableSecretMagic = []byte("MCPDD-PORTABLE-AESGCM-1\x00")

	// ErrLegacySecretsUnavailable marks an old platform-bound secret envelope
	// that can no longer be decrypted (for example after reinstalling Windows).
	// Callers may quarantine only that legacy file and create fresh credentials
	// without discarding the rest of the portable application data.
	ErrLegacySecretsUnavailable = errors.New("legacy platform-bound secrets are unavailable")
)


func PortableEnvelopeVersion() int { return portableSecretEnvelopeVersion }

func PortableProtectionName() string { return portableSecretProtection }

func ProtectPortableForDir(dataDir string, value []byte) ([]byte, error) {
	return NewStore(dataDir).protectPortable(value)
}

func UnprotectPortableForDir(dataDir string, value []byte) ([]byte, error) {
	return NewStore(dataDir).unprotectPortable(value)
}

func (s *Store) portableMasterKeyPath() string {
	return filepath.Join(filepath.Dir(s.path), portableMasterKeyName)
}

func readPortableMasterKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("portable master key must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("portable master key must contain exactly 32 bytes")
	}
	return key, nil
}

func (s *Store) portableMasterKey(create bool) ([]byte, error) {
	path := s.portableMasterKeyPath()
	key, err := readPortableMasterKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) || !create {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("portable master key is missing; restore %s together with secrets.json", path)
		}
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	generated := make([]byte, 32)
	if _, err := rand.Read(generated); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readPortableMasterKey(path)
	}
	if err != nil {
		return nil, err
	}
	writeErr := func() error {
		if _, err := file.Write(generated); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return file.Close()
	}()
	if writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, writeErr
	}
	return generated, nil
}

func portableAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Store) protectPortable(value []byte) ([]byte, error) {
	key, err := s.portableMasterKey(true)
	if err != nil {
		return nil, err
	}
	aead, err := portableAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := append([]byte(nil), portableSecretMagic...)
	result = append(result, nonce...)
	return aead.Seal(result, nonce, value, portableSecretMagic), nil
}

func (s *Store) unprotectPortable(value []byte) ([]byte, error) {
	if !bytes.HasPrefix(value, portableSecretMagic) {
		return nil, errors.New("portable secret envelope has an invalid header")
	}
	key, err := s.portableMasterKey(false)
	if err != nil {
		return nil, err
	}
	aead, err := portableAEAD(key)
	if err != nil {
		return nil, err
	}
	data := value[len(portableSecretMagic):]
	if len(data) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("portable secret envelope is truncated")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], portableSecretMagic)
	if err != nil {
		return nil, errors.New("portable secret envelope authentication failed; restore the matching master.key")
	}
	return plain, nil
}
