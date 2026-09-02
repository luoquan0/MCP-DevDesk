package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRemovesDownloadedUpdatePackages(t *testing.T) {
	data := t.TempDir()
	updates := filepath.Join(data, "updates")
	if err := os.MkdirAll(updates, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"v1-MCP-DevDesk-Portable-amd64.zip", "v2.zip.tmp", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(updates, name), []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	report := Cleanup(data)
	if report.FilesRemoved < 2 {
		t.Fatalf("removed files = %d, want at least 2", report.FilesRemoved)
	}
	if _, err := os.Stat(filepath.Join(updates, "v1-MCP-DevDesk-Portable-amd64.zip")); !os.IsNotExist(err) {
		t.Fatalf("zip should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(updates, "v2.zip.tmp")); !os.IsNotExist(err) {
		t.Fatalf("partial package should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(updates, "keep.txt")); err != nil {
		t.Fatalf("unrelated file should remain: %v", err)
	}
}

func TestCleanupRemovesOldLogsAndKeepsRecentLogs(t *testing.T) {
	data := t.TempDir()
	logs := filepath.Join(data, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	oldLog := filepath.Join(logs, "old.log")
	recentLog := filepath.Join(logs, "recent.log")
	if err := os.WriteFile(oldLog, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recentLog, []byte("recent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-LogMaxAge - time.Hour)
	if err := os.Chtimes(oldLog, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	Cleanup(data)
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatalf("old log should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(recentLog); err != nil {
		t.Fatalf("recent log should remain: %v", err)
	}
}

func TestCleanupBoundsRecoveryBackups(t *testing.T) {
	data := t.TempDir()
	recovery := filepath.Join(data, "recovery")
	if err := os.MkdirAll(recovery, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxRecoveryPerDir+5; i++ {
		path := filepath.Join(recovery, time.Unix(int64(i+1), 0).Format("150405")+".bak")
		if err := os.WriteFile(path, []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	Cleanup(data)
	entries, err := os.ReadDir(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > MaxRecoveryPerDir {
		t.Fatalf("recovery backups = %d, want <= %d", len(entries), MaxRecoveryPerDir)
	}
}
