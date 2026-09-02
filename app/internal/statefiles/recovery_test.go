package statefiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantineMovesFileIntoRecoveryDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := Quarantine(path, "parse error")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != filepath.Join(dir, "recovery") {
		t.Fatalf("backup dir = %q", filepath.Dir(backup))
	}
	if !strings.Contains(filepath.Base(backup), "parse-error") {
		t.Fatalf("backup name = %q", filepath.Base(backup))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source must be moved, stat err = %v", err)
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "broken" {
		t.Fatalf("backup content = %q", raw)
	}
}

func TestBackupKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := Backup(path, "decrypt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "original" {
		t.Fatalf("backup content = %q", raw)
	}
}
