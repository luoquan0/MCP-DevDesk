package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mcp-devdesk/internal/model"
)

func TestBundledExecutablePathsPersistRelative(t *testing.T) {
	root := t.TempDir()
	writeExecutablePlaceholder(t, filepath.Join(root, "coding-tools-mcp.exe"))
	writeExecutablePlaceholder(t, filepath.Join(root, "mcp-core.exe"))
	writeExecutablePlaceholder(t, filepath.Join(root, "cloudflared.exe"))

	dataDir := filepath.Join(root, "data")
	store, err := NewStore(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}

	runtimeCfg := store.Get()
	if runtimeCfg.CoreExecutable != filepath.Join(root, "coding-tools-mcp.exe") {
		t.Fatalf("runtime legacy core = %q", runtimeCfg.CoreExecutable)
	}
	if runtimeCfg.GoCoreExecutable != filepath.Join(root, "mcp-core.exe") {
		t.Fatalf("runtime Go core = %q", runtimeCfg.GoCoreExecutable)
	}
	if runtimeCfg.CloudflaredExecutable != filepath.Join(root, "cloudflared.exe") {
		t.Fatalf("runtime cloudflared = %q", runtimeCfg.CloudflaredExecutable)
	}

	persisted := readPersistedConfig(t, filepath.Join(dataDir, "config.json"))
	if persisted.CoreExecutable != "coding-tools-mcp.exe" {
		t.Fatalf("persisted legacy core = %q, want relative path", persisted.CoreExecutable)
	}
	if persisted.GoCoreExecutable != "mcp-core.exe" {
		t.Fatalf("persisted Go core = %q, want relative path", persisted.GoCoreExecutable)
	}
	if persisted.CloudflaredExecutable != "cloudflared.exe" {
		t.Fatalf("persisted cloudflared = %q, want relative path", persisted.CloudflaredExecutable)
	}

	reloaded, err := NewStore(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Get().GoCoreExecutable != filepath.Join(root, "mcp-core.exe") {
		t.Fatalf("reloaded Go core = %q", reloaded.Get().GoCoreExecutable)
	}
}

func TestMovedPortableConfigRebasesMissingBundledExecutables(t *testing.T) {
	oldRoot := filepath.Join(t.TempDir(), "old-install")
	newRoot := filepath.Join(t.TempDir(), "new-install")
	writeExecutablePlaceholder(t, filepath.Join(newRoot, "coding-tools-mcp.exe"))
	writeExecutablePlaceholder(t, filepath.Join(newRoot, "mcp-core.exe"))
	writeExecutablePlaceholder(t, filepath.Join(newRoot, "cloudflared.exe"))

	dataDir := filepath.Join(newRoot, "data")
	store, err := NewStore(newRoot, dataDir)
	if err != nil {
		t.Fatal(err)
	}

	cfg := store.Get()
	cfg.CoreExecutable = filepath.Join(oldRoot, "coding-tools-mcp.exe")
	cfg.GoCoreExecutable = filepath.Join(oldRoot, "dist", "mcp-core-amd64.exe")
	cfg.CloudflaredExecutable = filepath.Join(oldRoot, "cloudflared.exe")
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(newRoot, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Get()
	if got.CoreExecutable != filepath.Join(newRoot, "coding-tools-mcp.exe") {
		t.Fatalf("rebased legacy core = %q", got.CoreExecutable)
	}
	if got.GoCoreExecutable != filepath.Join(newRoot, "mcp-core.exe") {
		t.Fatalf("rebased Go core = %q", got.GoCoreExecutable)
	}
	if got.CloudflaredExecutable != filepath.Join(newRoot, "cloudflared.exe") {
		t.Fatalf("rebased cloudflared = %q", got.CloudflaredExecutable)
	}

	persisted := readPersistedConfig(t, filepath.Join(dataDir, "config.json"))
	if persisted.CoreExecutable != "coding-tools-mcp.exe" || persisted.GoCoreExecutable != "mcp-core.exe" || persisted.CloudflaredExecutable != "cloudflared.exe" {
		t.Fatalf("migrated paths were not persisted as relative paths: %+v", persisted)
	}
}

func TestExternalExecutablePathStaysAbsolute(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	externalCore := filepath.Join(externalRoot, "custom-core.exe")
	writeExecutablePlaceholder(t, externalCore)

	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	cfg.GoCoreExecutable = externalCore
	if _, err := store.Replace(cfg); err != nil {
		t.Fatal(err)
	}

	persisted := readPersistedConfig(t, filepath.Join(root, "data", "config.json"))
	if persisted.GoCoreExecutable != externalCore {
		t.Fatalf("external executable path = %q, want %q", persisted.GoCoreExecutable, externalCore)
	}
}

func writeExecutablePlaceholder(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readPersistedConfig(t *testing.T, path string) model.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
