//go:build linux

package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxSecretProtection(t *testing.T) {
	key := filepath.Join(t.TempDir(), "master.key")
	t.Setenv("MCP_DEVDESK_KEY_FILE", key)
	plain := []byte("temporary-regression-value")
	encrypted, err := protectData(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, plain) {
		t.Fatal("plaintext leaked into envelope")
	}
	decrypted, err := unprotectData(encrypted)
	if err != nil || !bytes.Equal(decrypted, plain) {
		t.Fatalf("round trip: %v", err)
	}
	second, err := protectData(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encrypted, second) {
		t.Fatal("GCM nonce was reused")
	}
	second[len(second)-1] ^= 1
	if _, err := unprotectData(second); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("master key must have mode 0600")
	}
	if err := os.Chmod(key, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := unprotectData(encrypted); err == nil {
		t.Fatal("world-readable key accepted")
	}
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if _, err := unprotectData(encrypted); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("decryption recreated a missing key")
	}
}

func TestLinuxSecretKeyRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(real, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCP_DEVDESK_KEY_FILE", link)
	if _, err := protectData([]byte("test")); err == nil {
		t.Fatal("symlink key accepted")
	}
}
