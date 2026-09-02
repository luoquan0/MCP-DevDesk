package startup_test

import (
	"os"
	"path/filepath"
	"testing"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/startup"
)

func TestPrepareAllowsApplicationToStartWithCorruptPortableData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data", "devdesk")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	corrupt := map[string]string{
		"config.json":          `{"broken":`,
		"appearance.json":      `{"theme":`,
		"projects.json":        `[{"path":`,
		"instances.json":       `{"version":1,"instances":`,
		"update-settings.json": `{"channel":`,
	}
	for name, content := range corrupt {
		if err := os.WriteFile(filepath.Join(data, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	report, err := startup.Prepare(root, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Recovered) == 0 {
		t.Fatal("expected at least one recovered data file")
	}

	app, err := application.New(root, data)
	if err != nil {
		t.Fatalf("application should start after recovery: %v", err)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("close recovered application: %v", err)
	}
}

func TestPrepareRepairsMissingWorkspaceAndDisablesAutostart(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data", "devdesk")
	missing := filepath.Join(root, "moved-away-project")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	configJSON := `{
  "version": 1,
  "workspace": "` + filepath.ToSlash(missing) + `",
  "allowedRoots": ["` + filepath.ToSlash(missing) + `"],
  "mcpHost": "127.0.0.1",
  "mcpPort": 8765,
  "adminHost": "127.0.0.1",
  "adminPort": 17860,
  "webControlPort": 17861,
  "permissionMode": "trusted",
  "fileScope": "workspace",
  "toolProfile": "full",
  "allowNetwork": true,
  "tunnelName": "mcp-devdesk",
  "autoStart": true,
  "watchdog": true,
  "coreMode": "legacy",
  "loggingEnabled": true
}`
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := startup.Prepare(root, data); err != nil {
		t.Fatal(err)
	}
	app, err := application.New(root, data)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	cfg := app.Config()
	if filepath.Clean(cfg.Workspace) != filepath.Clean(root) {
		t.Fatalf("workspace = %q, want fallback %q", cfg.Workspace, root)
	}
	if cfg.AutoStart {
		t.Fatal("auto-start must be disabled after workspace fallback")
	}
}
