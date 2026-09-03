//go:build windows

package mcpcore

import "testing"

func TestScreenBackgroundRevealRequired(t *testing.T) {
	if screenBackgroundRevealRequired(0, 0x20) {
		t.Fatal("zero HWND must never request a temporary reveal")
	}
	if screenBackgroundRevealRequired(0x10, 0) {
		t.Fatal("missing foreground window must not request a temporary reveal")
	}
	if screenBackgroundRevealRequired(0x10, 0x10) {
		t.Fatal("foreground selected window does not need a temporary reveal")
	}
	if !screenBackgroundRevealRequired(0x10, 0x20) {
		t.Fatal("background selected window should use the target-safe temporary reveal path")
	}
}

func TestScreenWindowBandInsertAfter(t *testing.T) {
	if screenWindowBandInsertAfter(true) != ^uintptr(0) {
		t.Fatal("topmost restore band must use HWND_TOPMOST")
	}
	if screenWindowBandInsertAfter(false) != ^uintptr(1) {
		t.Fatal("normal restore band must use HWND_NOTOPMOST")
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
