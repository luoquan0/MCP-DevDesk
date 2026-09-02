package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeriodicCleanupKeepsRecentUpdateFiles(t *testing.T) {
	data := t.TempDir()
	updates := filepath.Join(data, "updates")
	if err := os.MkdirAll(updates, 0o700); err != nil {
		t.Fatal(err)
	}
	recent := filepath.Join(updates, "active.zip.tmp")
	stale := filepath.Join(updates, "stale.zip.tmp")
	if err := os.WriteFile(recent, []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-TempMaxAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cleanup(data, false)
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent update should remain: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale update should be removed, stat err = %v", err)
	}
}
