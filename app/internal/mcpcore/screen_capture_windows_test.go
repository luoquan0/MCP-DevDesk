//go:build windows

package mcpcore

import (
	"image"
	"image/color"
	"testing"
)

func TestScreenWindowBandInsertAfter(t *testing.T) {
	if screenWindowBandInsertAfter(true) != ^uintptr(0) {
		t.Fatal("topmost restore band must use HWND_TOPMOST")
	}
	if screenWindowBandInsertAfter(false) != ^uintptr(1) {
		t.Fatal("normal restore band must use HWND_NOTOPMOST")
	}
}

func TestScreenImageLikelyBlank(t *testing.T) {
	black := image.NewNRGBA(image.Rect(0, 0, 320, 200))
	if !screenImageLikelyBlank(black) {
		t.Fatal("near-black capture should be treated as a likely hardware blank frame")
	}

	visible := image.NewNRGBA(image.Rect(0, 0, 320, 200))
	for offset := 0; offset < len(visible.Pix); offset += 4 {
		visible.Pix[offset] = 220
		visible.Pix[offset+1] = 220
		visible.Pix[offset+2] = 220
		visible.Pix[offset+3] = 0xff
	}
	if screenImageLikelyBlank(visible) {
		t.Fatal("normal visible capture must not be treated as black")
	}
}

func TestScreenImageLikelyPrintWindowArtifact(t *testing.T) {
	white := image.NewNRGBA(image.Rect(0, 0, 320, 200))
	for offset := 0; offset < len(white.Pix); offset += 4 {
		white.Pix[offset] = 255
		white.Pix[offset+1] = 255
		white.Pix[offset+2] = 255
		white.Pix[offset+3] = 0xff
	}
	if !screenImageLikelyPrintWindowArtifact(white) {
		t.Fatal("solid-white PrintWindow output should fall through to Windows Graphics Capture")
	}

	textured := image.NewNRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			value := uint8((x*3 + y*5) % 180 + 32)
			textured.SetNRGBA(x, y, color.NRGBA{R: value, G: uint8(255 - value/2), B: uint8(64 + (x+y)%128), A: 0xff})
		}
	}
	if screenImageLikelyPrintWindowArtifact(textured) {
		t.Fatal("textured application pixels must not be classified as an empty PrintWindow surface")
	}
}

func TestScreenWGCSizeArgPacksWidthLowHeightHigh(t *testing.T) {
	got := uint64(screenWGCSizeArg(2560, 1440))
	want := uint64(2560) | uint64(1440)<<32
	if got != want {
		t.Fatalf("WGC SizeInt32 ABI argument = 0x%X, want 0x%X", got, want)
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
