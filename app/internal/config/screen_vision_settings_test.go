package config

import (
	"testing"

	"mcp-devdesk/internal/model"
)

func TestScreenVisionSettingsPersistAndNormalize(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Get().ScreenCaptureMode; got != "active" {
		t.Fatalf("default screen capture mode = %q, want active", got)
	}

	mode := "WINDOW"
	windowID := "0x1A2B"
	processID := uint32(4242)
	title := "Project - Visual Studio Code"
	process := "Code.exe"
	updated, err := store.Update(model.ConfigUpdate{
		ScreenCaptureMode:            &mode,
		ScreenCaptureWindowID:        &windowID,
		ScreenCaptureWindowProcessID: &processID,
		ScreenCaptureWindowTitle:     &title,
		ScreenCaptureWindowProcess:   &process,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ScreenCaptureMode != "window" || updated.ScreenCaptureWindowID != windowID || updated.ScreenCaptureWindowProcessID != processID {
		t.Fatalf("unexpected Screen Vision settings: %+v", updated)
	}
}

func TestScreenVisionRejectsInvalidModeAndWindowID(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	badMode := "continuous"
	if _, err := store.Update(model.ConfigUpdate{ScreenCaptureMode: &badMode}); err == nil {
		t.Fatal("expected invalid Screen Vision mode to be rejected")
	}
	badID := "not-a-window"
	if _, err := store.Update(model.ConfigUpdate{ScreenCaptureWindowID: &badID}); err == nil {
		t.Fatal("expected invalid Screen Vision window id to be rejected")
	}
}
