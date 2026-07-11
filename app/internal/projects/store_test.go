package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistsAndProtectsActiveProject(t *testing.T) {
	dataDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.Add("Second", second)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(store.List()))
	}
	if err := store.Remove(project.ID, first); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(dataDir, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 {
		t.Fatalf("expected persisted project, got %d", len(reloaded.List()))
	}
	active := reloaded.List()[0]
	if err := reloaded.Remove(active.ID, first); err == nil {
		t.Fatal("expected active project removal to fail")
	}
}
