package instances

import (
	"fmt"
	"os"
	"testing"
)

func TestStorePersistsInstanceMetadata(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Add("API service", "project-api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.DataDir(record.ID)); err != nil {
		t.Fatalf("instance data directory: %v", err)
	}
	if _, err := store.Update(record.ID, "API service 2", "project-api-2"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(record.ID)
	if !ok || got.Name != "API service 2" || got.ProjectID != "project-api-2" {
		t.Fatalf("unexpected record: %+v, ok=%v", got, ok)
	}
	if err := reloaded.Remove(record.ID); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 {
		t.Fatal("expected empty store")
	}
}

func TestStoreRejectsDuplicateNames(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("Frontend", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("frontend", ""); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestStoreRollsBackFailedMetadataSave(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Add("Original", "project")
	if err != nil {
		t.Fatal(err)
	}
	store.path = dataDir
	if _, err := store.Update(record.ID, "Changed", "project-2"); err == nil {
		t.Fatal("expected metadata save to fail")
	}
	got, ok := store.Get(record.ID)
	if !ok || got.Name != "Original" || got.ProjectID != "project" {
		t.Fatalf("record changed after failed save: %+v, ok=%v", got, ok)
	}
}

func TestStoreLimitsAdditionalInstances(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxInstances; index++ {
		if _, err := store.Add(fmt.Sprintf("instance-%02d", index), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Add("one-too-many", ""); err == nil {
		t.Fatal("expected instance limit error")
	}
}
