//go:build windows

package mcpcore

import "testing"

func TestScreenRegionFallbackSafe(t *testing.T) {
	if screenRegionFallbackSafe(0, 0) {
		t.Fatal("zero HWND must never allow desktop-region fallback")
	}
	if screenRegionFallbackSafe(0x10, 0x20) {
		t.Fatal("background selected window must not allow desktop-region fallback")
	}
	if !screenRegionFallbackSafe(0x10, 0x10) {
		t.Fatal("foreground selected window should allow final desktop-region fallback")
	}
}

func TestScreenWindowStateSelectable(t *testing.T) {
	if !screenWindowStateSelectable(1, 1, 0) {
		t.Fatal("normal visible window should be selectable")
	}
	if screenWindowStateSelectable(0, 1, 0) {
		t.Fatal("invalid window must not be selectable")
	}
	if screenWindowStateSelectable(1, 0, 0) {
		t.Fatal("hidden window must not be selectable")
	}
	if screenWindowStateSelectable(1, 1, 1) {
		t.Fatal("minimized window must not be selectable")
	}
}
