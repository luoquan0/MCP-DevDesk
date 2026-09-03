package selfupdate

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableInstallReplacesProgramsAndPreservesData(t *testing.T) {
	root := t.TempDir()
	write := func(path, value string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "cloudflared.exe", "coding-tools-mcp.exe", "README.md"} {
		write(filepath.Join(root, name), "old-"+name)
	}
	dataFile := filepath.Join(root, "data", "devdesk", "keep.json")
	write(dataFile, "keep-me")
	packagePath := filepath.Join(t.TempDir(), "update.zip")
	createUpdateZip(t, packagePath, true)

	originalRestart := restartProcess
	restartProcess = func(_ string, _ []string) error { return nil }
	t.Cleanup(func() { restartProcess = originalRestart })
	if err := Install(Options{PackagePath: packagePath, RootDir: root, CurrentExe: filepath.Join(root, "MCP-DevDesk.exe")}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "coding-tools-mcp.exe", "README.md"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "new-"+name {
			t.Fatalf("%s=%q", name, raw)
		}
	}
	cloudflared, err := os.ReadFile(filepath.Join(root, "cloudflared.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cloudflared) != "old-cloudflared.exe" {
		t.Fatalf("cloudflared was overwritten by software update: %q", cloudflared)
	}
	raw, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "keep-me" {
		t.Fatalf("data file changed: %q", raw)
	}
}

func TestPortableInstallSeedsCloudflaredWhenMissing(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "coding-tools-mcp.exe", "README.md"} {
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
	raw, err := os.ReadFile(filepath.Join(root, "cloudflared.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new-cloudflared.exe" {
		t.Fatalf("missing cloudflared was not restored from package: %q", raw)
	}
}

func TestInstallRollsBackWhenPackageIsIncomplete(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe", "devdesk-updater.exe", "cloudflared.exe", "coding-tools-mcp.exe", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packagePath := filepath.Join(t.TempDir(), "update.zip")
	createUpdateZip(t, packagePath, false)
	originalRestart := restartProcess
	restartProcess = func(_ string, _ []string) error { return nil }
	t.Cleanup(func() { restartProcess = originalRestart })
	if err := Install(Options{PackagePath: packagePath, RootDir: root, CurrentExe: filepath.Join(root, "MCP-DevDesk.exe")}); err == nil {
		t.Fatal("expected incomplete package to fail")
	}
	for _, name := range []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "old-"+name {
			t.Fatalf("rollback failed for %s: %q", name, raw)
		}
	}
}

func TestDevelopmentInstallMapsPortablePayloadToDistLayout(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	currentExe := filepath.Join(dist, "MCP-DevDesk-amd64.exe")
	goCore := filepath.Join(dist, "mcp-core.exe")
	legacyCore := filepath.Join(root, "coding-tools-mcp.exe")
	cloudflared := filepath.Join(root, "cloudflared.exe")
	updaterTarget := filepath.Join(dist, "devdesk-updater-amd64.exe")
	for _, target := range []string{
		currentExe,
		filepath.Join(dist, "devdeskctl-amd64.exe"),
		goCore,
		filepath.Join(dist, "mcp-core-amd64.exe"),
		legacyCore,
		cloudflared,
		updaterTarget,
	} {
		if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packagePath := filepath.Join(t.TempDir(), "update.zip")
	createUpdateZip(t, packagePath, true)
	originalRestart := restartProcess
	restartProcess = func(_ string, _ []string) error { return nil }
	t.Cleanup(func() { restartProcess = originalRestart })
	if err := Install(Options{
		PackagePath:       packagePath,
		RootDir:           root,
		CurrentExe:        currentExe,
		GoCoreTarget:      goCore,
		LegacyCoreTarget:  legacyCore,
		CloudflaredTarget: cloudflared,
		UpdaterTarget:     updaterTarget,
	}); err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{
		currentExe: "new-MCP-DevDesk.exe",
		filepath.Join(dist, "devdeskctl-amd64.exe"): "new-devdeskctl.exe",
		goCore: "new-mcp-core.exe",
		filepath.Join(dist, "mcp-core-amd64.exe"): "new-mcp-core.exe",
		legacyCore:    "new-coding-tools-mcp.exe",
		cloudflared:   "old",
		updaterTarget: "new-devdesk-updater.exe",
	}
	for target, want := range wants {
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != want {
			t.Fatalf("%s=%q want %q", target, raw, want)
		}
	}
}

func createUpdateZip(t *testing.T, target string, complete bool) {
	t.Helper()
	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	names := []string{"MCP-DevDesk.exe", "devdeskctl.exe", "mcp-core.exe"}
	if complete {
		names = append(names, "devdesk-updater.exe", "cloudflared.exe", "coding-tools-mcp.exe", "README.md")
	}
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("new-" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
