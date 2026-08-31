package appearance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppearanceSettingsPersistAndValidate(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	theme := "dark"
	customColors := true
	primary := "#ff3366"
	secondary := "#22aa88"
	opacity := 58
	updated, err := store.Update(Update{
		Theme:               &theme,
		CustomColorsEnabled: &customColors,
		PrimaryColor:        &primary,
		SecondaryColor:      &secondary,
		BackgroundOpacity:   &opacity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Theme != theme || !updated.CustomColorsEnabled || updated.PrimaryColor != primary || updated.SecondaryColor != secondary || updated.BackgroundOpacity != opacity {
		t.Fatalf("appearance update mismatch: %+v", updated)
	}
	reloaded, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get(); got.Theme != theme || !got.CustomColorsEnabled || got.PrimaryColor != primary || got.SecondaryColor != secondary || got.BackgroundOpacity != opacity {
		t.Fatalf("appearance settings did not persist: %+v", got)
	}
	bad := "red"
	if _, err := reloaded.Update(Update{PrimaryColor: &bad}); err == nil {
		t.Fatal("invalid color was accepted")
	}
}

func TestAppearanceBackgroundPersistsAndCanBeRemoved(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveBackground([]byte("not an image")); err == nil {
		t.Fatal("non-image background was accepted")
	}
	// Minimal valid PNG signature and IHDR-sized prefix is enough for MIME detection.
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	updated, err := store.SaveBackground(png)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HasBackgroundImage || updated.BackgroundRevision == 0 {
		t.Fatalf("background state was not updated: %+v", updated)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "appearance-background.bin")); err != nil {
		t.Fatalf("background file missing: %v", err)
	}
	reloaded, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Get().HasBackgroundImage {
		t.Fatal("background presence was not restored")
	}
	removed, err := reloaded.RemoveBackground()
	if err != nil {
		t.Fatal(err)
	}
	if removed.HasBackgroundImage {
		t.Fatal("background remained enabled after removal")
	}
}
