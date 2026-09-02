package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRecoveringStoreQuarantinesMalformedConfig(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	path := filepath.Join(data, "config.json")
	if err := os.WriteFile(path, []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewRecoveringStore(root, data)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().Workspace != filepath.Clean(root) {
		t.Fatalf("workspace = %q, want %q", store.Get().Workspace, filepath.Clean(root))
	}
	entries, err := os.ReadDir(filepath.Join(data, "recovery"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovery files = %d, want 1", len(entries))
	}
}

func TestNewRecoveringStoreKeepsPortableSettingsWhenProxyCipherCannotDecrypt(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	data := t.TempDir()
	template := &Store{rootDir: root, dataDir: data, path: filepath.Join(data, "config.json")}
	cfg := template.defaults()
	cfg.Workspace = workspace
	cfg.AllowedRoots = []string{workspace}
	cfg.MCPPort = 9876
	cfg.ProxyAddress = "127.0.0.1:7890"
	cfg.ProxyUsername = "portable-user"
	cfg.ProxyPassword = protectedProxyPasswordPrefix + "%%%not-base64%%%"
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewRecoveringStore(root, data)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.Workspace != filepath.Clean(workspace) || got.MCPPort != 9876 {
		t.Fatalf("portable settings were reset: workspace=%q port=%d", got.Workspace, got.MCPPort)
	}
	if got.ProxyAddress != "127.0.0.1:7890" || got.ProxyUsername != "portable-user" {
		t.Fatalf("proxy settings were reset: %#v", got)
	}
	if got.ProxyPassword != "" {
		t.Fatalf("proxy password = %q, want empty after recovery", got.ProxyPassword)
	}
	entries, err := os.ReadDir(filepath.Join(data, "recovery"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovery files = %d, want 1", len(entries))
	}
}
