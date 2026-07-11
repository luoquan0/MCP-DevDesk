package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestStoreNormalizesAndDeduplicatesPersistedProjects(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	older := Project{ID: "old-a", Name: "Project", Path: projectDir, AddedAt: time.Unix(10, 0), LastOpenedAt: time.Unix(20, 0)}
	newer := Project{ID: "old-b", Name: "Duplicate", Path: projectDir + string(filepath.Separator), AddedAt: time.Unix(15, 0), LastOpenedAt: time.Unix(30, 0)}
	raw, err := json.Marshal([]Project{older, newer})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "projects.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	items := store.List()
	if len(items) != 1 {
		t.Fatalf("expected duplicate paths to be merged, got %d", len(items))
	}
	if items[0].ID != projectID(projectDir) {
		t.Fatalf("expected regenerated stable ID, got %q", items[0].ID)
	}
	if !items[0].LastOpenedAt.Equal(newer.LastOpenedAt) {
		t.Fatalf("expected newest last-opened timestamp, got %v", items[0].LastOpenedAt)
	}
}
