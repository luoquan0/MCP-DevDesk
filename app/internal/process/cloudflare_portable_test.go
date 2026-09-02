package process

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPreparePortableCredentialsCreatePathUsesPortableDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DEVDESK_ROOT", root)

	path, err := PreparePortableCredentialsCreatePath()
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, "data", "devdesk", "cloudflare")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("create path dir = %q, want %q", filepath.Dir(path), wantDir)
	}
	if !strings.HasPrefix(filepath.Base(path), ".tunnel-create-") || filepath.Ext(path) != ".json" {
		t.Fatalf("unexpected create path %q", path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("reserved path must not exist before cloudflared writes it, stat err = %v", err)
	}
}

func TestFinalizePortableCredentialsMovesTemporaryFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DEVDESK_ROOT", root)
	tunnelID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	source, err := PreparePortableCredentialsCreatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("portable-created"), 0o600); err != nil {
		t.Fatal(err)
	}
	// cloudflared writes tunnel credentials as 0400. On Windows this can map to
	// a read-only file attribute, so finalization must make the temporary file removable.
	if err := os.Chmod(source, 0o400); err != nil {
		t.Fatal(err)
	}

	got, err := FinalizePortableCredentials(tunnelID, source)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "data", "devdesk", "cloudflare", tunnelID+".json")
	if got != want {
		t.Fatalf("final path = %q, want %q", got, want)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("temporary credentials should be removed, stat err = %v", err)
	}
}
