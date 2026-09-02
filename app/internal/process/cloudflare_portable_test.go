package process

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePortableCredentialsMigratesLegacyFile(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("USERPROFILE", home)

	tunnelID := "11111111-2222-3333-4444-555555555555"
	legacy := filepath.Join(home, ".cloudflared", tunnelID+".json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	const content = `{"TunnelID":"portable-test"}`
	if err := os.WriteFile(legacy, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := EnsurePortableCredentials(root, tunnelID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "data", "devdesk", "cloudflare", tunnelID+".json")
	if got != want {
		t.Fatalf("portable path = %q, want %q", got, want)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != content {
		t.Fatalf("portable content = %q", raw)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy file should be retained for rollback: %v", err)
	}
}

func TestStorePortableCredentialsUsesTunnelUUIDFilename(t *testing.T) {
	root := t.TempDir()
	tunnelID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	source := filepath.Join(t.TempDir(), "created.json")
	if err := os.WriteFile(source, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := StorePortableCredentials(root, tunnelID, source)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "data", "devdesk", "cloudflare", tunnelID+".json")
	if got != want {
		t.Fatalf("portable path = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal(err)
	}
}
