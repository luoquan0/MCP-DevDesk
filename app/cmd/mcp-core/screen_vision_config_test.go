package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScreenVisionPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"screenCaptureEnabled":true,"screenCaptureMode":"window","screenCaptureWindowId":"0x1234","screenCaptureWindowProcessId":4321}`), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled, mode, windowID, processID, err := loadScreenVisionPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || mode != "window" || windowID != "0x1234" || processID != 4321 {
		t.Fatalf("policy = %t %q %q %d", enabled, mode, windowID, processID)
	}
}

func TestLoadScreenVisionPolicyReadsDisabledSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"screenCaptureEnabled":false,"screenCaptureMode":"desktop"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled, mode, windowID, processID, err := loadScreenVisionPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if enabled || mode != "desktop" || windowID != "" || processID != 0 {
		t.Fatalf("disabled policy = %t %q %q %d", enabled, mode, windowID, processID)
	}
}

func TestLoadScreenVisionPolicyMissingPathFailsClosed(t *testing.T) {
	if _, _, _, _, err := loadScreenVisionPolicy(""); err == nil {
		t.Fatal("empty authoritative policy path must fail")
	}
	if _, _, _, _, err := loadScreenVisionPolicy(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing authoritative policy file must fail")
	}
}
