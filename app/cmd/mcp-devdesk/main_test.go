package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateRootPrefersExecutableDirectoryOverWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	exeRoot := filepath.Join(base, "portable")
	cwdRoot := filepath.Join(base, "shell")
	for _, root := range []string{exeRoot, cwdRoot} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"cloudflared.exe", "mcp-core-amd64.exe"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	executable := filepath.Join(exeRoot, "MCP-DevDesk-amd64.exe")
	if err := os.WriteFile(executable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := locateRootFrom(executable, cwdRoot)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(exeRoot) {
		t.Fatalf("root = %q, want executable directory %q", got, exeRoot)
	}
}

func TestLocateRootPrefersExistingPortableDataBesideExecutable(t *testing.T) {
	base := t.TempDir()
	exeRoot := filepath.Join(base, "portable")
	cwdRoot := filepath.Join(base, "shell")
	if err := os.MkdirAll(filepath.Join(exeRoot, "data", "devdesk"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwdRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cloudflared.exe", "coding-tools-mcp.exe"} {
		if err := os.WriteFile(filepath.Join(cwdRoot, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := locateRootFrom(filepath.Join(exeRoot, "MCP-DevDesk.exe"), cwdRoot)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(exeRoot) {
		t.Fatalf("root = %q, want data directory beside executable %q", got, exeRoot)
	}
}
