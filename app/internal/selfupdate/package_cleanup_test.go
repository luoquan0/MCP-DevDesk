package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSuccessfulInstallDeletesVerifiedPackage(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "cloudflared.exe", "coding-tools-mcp.exe", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packagePath := filepath.Join(t.TempDir(), "update.zip")
	createUpdateZip(t, packagePath, true)

	originalRestart := restartProcess
	restartProcess = func(_ string, _ []string) error { return nil }
	t.Cleanup(func() { restartProcess = originalRestart })

	if err := Install(Options{PackagePath: packagePath, RootDir: root, CurrentExe: filepath.Join(root, "MCP-DevDesk.exe")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packagePath); !os.IsNotExist(err) {
		t.Fatalf("successful update package should be deleted, stat err = %v", err)
	}
}

func TestFailedRestartKeepsPackageForDiagnosisOrRetry(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "cloudflared.exe", "coding-tools-mcp.exe", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packagePath := filepath.Join(t.TempDir(), "update.zip")
	createUpdateZip(t, packagePath, true)

	originalRestart := restartProcess
	calls := 0
	restartProcess = func(_ string, _ []string) error {
		calls++
		return errors.New("restart failed")
	}
	t.Cleanup(func() { restartProcess = originalRestart })

	if err := Install(Options{PackagePath: packagePath, RootDir: root, CurrentExe: filepath.Join(root, "MCP-DevDesk.exe")}); err == nil {
		t.Fatal("expected restart failure")
	}
	if calls != 2 {
		t.Fatalf("restart attempts = %d, want 2", calls)
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("failed update package should remain until startup cleanup: %v", err)
	}
}
