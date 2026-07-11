package config

import (
	"os"
	"path/filepath"
	"testing"

	"mcp-devdesk/internal/model"
)

func TestValidDomain(t *testing.T) {
	tests := map[string]bool{
		"mcp.example.com":        true,
		"dev-1.example.co.uk":    true,
		"localhost":              false,
		"https://example.com":    false,
		"bad_domain.example.com": false,
	}
	for input, want := range tests {
		if got := ValidDomain(input); got != want {
			t.Fatalf("ValidDomain(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestTrustedModeForcesNetwork(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	mode := "trusted"
	allow := false
	cfg, err := store.Update(model.ConfigUpdate{PermissionMode: &mode, AllowNetwork: &allow})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowNetwork {
		t.Fatal("trusted mode must force network access")
	}
}

func TestDefaultCoreModeKeepsLegacyFallback(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	goCore := filepath.Join(dist, "mcp-core.exe")
	if err := os.WriteFile(goCore, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	if cfg.CoreMode != "legacy" {
		t.Fatalf("default core mode = %q", cfg.CoreMode)
	}
	if cfg.GoCoreExecutable != goCore {
		t.Fatalf("Go core executable = %q, want %q", cfg.GoCoreExecutable, goCore)
	}
	mode := "go"
	updated, err := store.Update(model.ConfigUpdate{CoreMode: &mode})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoreMode != "go" {
		t.Fatalf("updated core mode = %q", updated.CoreMode)
	}
}

func TestInvalidCoreModeIsRejected(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	mode := "unknown"
	if _, err := store.Update(model.ConfigUpdate{CoreMode: &mode}); err == nil {
		t.Fatal("invalid core mode was accepted")
	}
}

func TestAdminHostCannotBePublic(t *testing.T) {
	cfg := model.Config{
		Version:        model.CurrentConfigVersion,
		Workspace:      t.TempDir(),
		MCPHost:        "127.0.0.1",
		MCPPort:        8765,
		AdminHost:      "0.0.0.0",
		AdminPort:      17860,
		PermissionMode: "safe",
		FileScope:      "workspace",
		ToolProfile:    "full",
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected public admin host to be rejected")
	}
}
