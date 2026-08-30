package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-devdesk/internal/model"
	secretstore "mcp-devdesk/internal/secrets"
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

func TestLoggingIsEnabledByDefaultAndCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Get().LoggingEnabled {
		t.Fatal("logging must be enabled by default")
	}
	disabled := false
	cfg, err := store.Update(model.ConfigUpdate{LoggingEnabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoggingEnabled {
		t.Fatal("logging setting was not disabled")
	}
}

func TestWebControlDefaultsDisabledAndUsesDedicatedPort(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	if cfg.WebControlEnabled {
		t.Fatal("web control must be disabled by default")
	}
	if cfg.WebControlPort != 17861 {
		t.Fatalf("default web control port = %d, want 17861", cfg.WebControlPort)
	}
}

func TestWebControlRejectsAdminOrMCPPortWhenEnabled(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	cfg := store.Get()
	adminPort := cfg.AdminPort
	if _, err := store.Update(model.ConfigUpdate{WebControlEnabled: &enabled, WebControlPort: &adminPort}); err == nil {
		t.Fatal("web control accepted the admin port")
	}
	mcpPort := cfg.MCPPort
	if _, err := store.Update(model.ConfigUpdate{WebControlEnabled: &enabled, WebControlPort: &mcpPort}); err == nil {
		t.Fatal("web control accepted the MCP port")
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

func TestProxyPasswordIsEncryptedAtRest(t *testing.T) {
	if !secretstore.EncryptionAvailable() {
		t.Skip("platform encryption is unavailable")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	store, err := NewStore(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	password := "proxy-password-that-must-not-be-plaintext"
	if _, err := store.Update(model.ConfigUpdate{ProxyPassword: &password}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), password) {
		t.Fatal("proxy password was stored in plaintext")
	}
	if !strings.Contains(string(raw), protectedProxyPasswordPrefix) {
		t.Fatal("protected proxy password marker is missing")
	}
	reloaded, err := NewStore(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().ProxyPassword; got != password {
		t.Fatalf("reloaded proxy password = %q", got)
	}
}

func TestFailedConfigSaveRollsBackMemory(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	store, err := NewStore(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	previous := store.Get()
	store.path = dataDir
	newPort := previous.MCPPort + 1
	if _, err := store.Update(model.ConfigUpdate{MCPPort: &newPort}); err == nil {
		t.Fatal("expected config save to fail")
	}
	if got := store.Get().MCPPort; got != previous.MCPPort {
		t.Fatalf("in-memory port = %d after failed save, want %d", got, previous.MCPPort)
	}
}
