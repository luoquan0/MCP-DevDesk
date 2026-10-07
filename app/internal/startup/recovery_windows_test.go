//go:build windows

package startup_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/startup"
)

func TestPrepareQuarantinesUnreadableLegacyDPAPISecretsOnly(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data", "devdesk")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}

	projects := `[{"id":"keep-me","name":"Keep Me","path":"` + filepath.ToSlash(root) + `","lastOpenedAt":"2026-10-07T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(data, "projects.json"), []byte(projects), 0o600); err != nil {
		t.Fatal(err)
	}

	legacy := map[string]any{
		"version": 2,
		"protection": "windows-dpapi-current-user",
		"data": base64.StdEncoding.EncodeToString([]byte("not-a-valid-dpapi-blob")),
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "secrets.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := startup.Prepare(root, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range report.Recovered {
		if strings.Contains(message, "DPAPI") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DPAPI recovery message, got %#v", report.Recovered)
	}

	app, err := application.New(root, data)
	if err != nil {
		t.Fatalf("application should start after legacy DPAPI recovery: %v", err)
	}
	defer app.Close()
	if len(app.Projects()) == 0 {
		t.Fatal("project data was lost while recovering secrets")
	}

	if _, err := os.Stat(filepath.Join(data, "master.key")); err != nil {
		t.Fatalf("portable master key was not created: %v", err)
	}
	recoveryDir := filepath.Join(data, "recovery")
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		t.Fatal(err)
	}
	foundBackup := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "secrets.json.") && strings.Contains(entry.Name(), "legacy-dpapi-unavailable") {
			foundBackup = true
			break
		}
	}
	if !foundBackup {
		t.Fatalf("legacy secrets backup not found in %s", recoveryDir)
	}
}
