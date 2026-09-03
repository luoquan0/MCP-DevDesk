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
