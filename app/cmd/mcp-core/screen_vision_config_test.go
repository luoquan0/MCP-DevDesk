package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScreenVisionPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"screenCaptureMode":"window","screenCaptureWindowId":"0x1234","screenCaptureWindowProcessId":4321}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mode, windowID, processID, err := loadScreenVisionPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "window" || windowID != "0x1234" || processID != 4321 {
		t.Fatalf("policy = %q %q %d", mode, windowID, processID)
	}
}

func TestLoadScreenVisionPolicyDefaultsToActive(t *testing.T) {
	mode, windowID, processID, err := loadScreenVisionPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	if mode != "active" || windowID != "" || processID != 0 {
		t.Fatalf("default policy = %q %q %d", mode, windowID, processID)
	}
}
