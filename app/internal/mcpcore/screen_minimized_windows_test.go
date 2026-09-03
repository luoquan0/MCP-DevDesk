//go:build windows

package mcpcore

import "testing"

func TestScreenVisionWindowStateSelectableIncludesMinimized(t *testing.T) {
	if !screenVisionWindowStateSelectable(1, 1, 0) {
		t.Fatal("normal visible app window should be selectable")
	}
	if !screenVisionWindowStateSelectable(1, 1, 1) {
		t.Fatal("minimized visible app window should remain selectable for Screen Vision")
	}
	if screenVisionWindowStateSelectable(0, 1, 1) {
		t.Fatal("invalid window must not be selectable")
	}
	if screenVisionWindowStateSelectable(1, 0, 1) {
		t.Fatal("hidden/headless window must not be exposed as a Screen Vision target")
	}
}
