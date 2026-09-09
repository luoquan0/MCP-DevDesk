//go:build windows

package mcpcore

import (
	"testing"
	"unsafe"
)

func TestScreenWindowPlacementMatchesWin32Layout(t *testing.T) {
	if got := unsafe.Sizeof(screenWindowPlacement{}); got != 60 {
		t.Fatalf("WINDOWPLACEMENT size = %d, want 60", got)
	}
}

func TestScreenVisionPreferredBoundsUsesNormalPlacementForDormantWindow(t *testing.T) {
	current := screenRect{X: -32000, Y: -32000, Width: 158, Height: 26}
	normal := screenRect{X: 120, Y: 90, Width: 980, Height: 720}
	got, ok := screenVisionPreferredBounds(current, true, normal, true, true)
	if !ok || got != normal {
		t.Fatalf("preferred bounds = %+v, ok=%v; want normal %+v", got, ok, normal)
	}
}

func TestScreenVisionHiddenWindowEligibilityRejectsHelpers(t *testing.T) {
	bounds := screenRect{Width: 900, Height: 600}
	if !screenVisionHiddenWindowEligible(0, 0, "v2rayN.exe", bounds) {
		t.Fatal("unowned normal-sized app main window should be eligible while hidden")
	}
	if screenVisionHiddenWindowEligible(1, 0, "v2rayN.exe", bounds) {
		t.Fatal("owned helper window must not be exposed")
	}
	if screenVisionHiddenWindowEligible(0, screenWSExToolWindow, "v2rayN.exe", bounds) {
		t.Fatal("tool window must not be exposed as hidden main window")
	}
	if screenVisionHiddenWindowEligible(0, 0, "", bounds) {
		t.Fatal("hidden window without resolvable process must not be exposed")
	}
	if screenVisionHiddenWindowEligible(0, 0, "v2rayN.exe", screenRect{Width: 158, Height: 26}) {
		t.Fatal("tiny hidden helper must not be exposed as a main window")
	}
}

func TestScreenVisionCollapseTinyHelperPrefersMainWindow(t *testing.T) {
	windows := []screenWindow{
		{ID: "0x1", ProcessID: 55, Title: "v2rayN", Bounds: screenRect{Width: 158, Height: 26}},
		{ID: "0x2", ProcessID: 55, Title: "v2rayN", Bounds: screenRect{Width: 1000, Height: 700}, Hidden: true},
		{ID: "0x3", ProcessID: 55, Title: "另一个正常窗口", Bounds: screenRect{Width: 800, Height: 500}},
	}
	got := screenVisionCollapseTinyHelpers(windows)
	if len(got) != 2 {
		t.Fatalf("candidate count = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "0x2" {
		t.Fatalf("v2rayN candidate = %s, want main window 0x2", got[0].ID)
	}
	if got[1].ID != "0x3" {
		t.Fatalf("unrelated normal window was changed: %+v", got[1])
	}
}

func TestScreenVisionBoundsNeedRepair(t *testing.T) {
	if !screenVisionBoundsNeedRepair(
		screenRect{Width: 158, Height: 26},
		screenRect{Width: 1000, Height: 700},
	) {
		t.Fatal("tiny restored bounds should be repaired to normal placement")
	}
	if screenVisionBoundsNeedRepair(
		screenRect{Width: 960, Height: 680},
		screenRect{Width: 1000, Height: 700},
	) {
		t.Fatal("already-normal restored bounds should not be moved/resized")
	}
}

func TestScreenVisionChooseRestoredCandidatePrefersReplacementForTinyOriginal(t *testing.T) {
	original := screenWindow{Handle: 0x10, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 1000, Height: 700}}
	exact := screenWindow{Handle: 0x10, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 158, Height: 26}}
	replacement := screenWindow{Handle: 0x20, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 1000, Height: 700}}
	got, err := screenVisionChooseRestoredCandidate(original, &exact, []screenWindow{replacement}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Handle != replacement.Handle {
		t.Fatalf("restored handle = 0x%X, want replacement 0x%X", got.Handle, replacement.Handle)
	}
}

func TestScreenVisionChooseRestoredCandidateRejectsAmbiguousReplacement(t *testing.T) {
	original := screenWindow{Handle: 0x10, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 1000, Height: 700}}
	replacements := []screenWindow{
		{Handle: 0x20, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 1000, Height: 700}},
		{Handle: 0x30, Title: "v2rayN", ProcessID: 55, ProcessName: "v2rayN.exe", Bounds: screenRect{Width: 900, Height: 650}},
	}
	if _, err := screenVisionChooseRestoredCandidate(original, nil, replacements, false); err == nil {
		t.Fatal("ambiguous replacement windows must be rejected")
	}
}

func TestScreenRectsStableAllowsOnlyTinyDWMJitter(t *testing.T) {
	base := screenRect{X: 100, Y: 80, Width: 1000, Height: 700}
	if !screenRectsStable(base, screenRect{X: 101, Y: 79, Width: 1001, Height: 699}) {
		t.Fatal("one-pixel DWM jitter should still count as stable")
	}
	if screenRectsStable(base, screenRect{X: 100, Y: 80, Width: 1010, Height: 700}) {
		t.Fatal("meaningful width change must reset stability")
	}
}

func TestScreenVisionRestoredIdentityRequiresResolvedProcessName(t *testing.T) {
	original := screenWindow{ProcessID: 55, ProcessName: "v2rayN.exe"}
	if screenVisionRestoredIdentityMatches(original, screenWindow{ProcessID: 55}) {
		t.Fatal("replacement with an unresolved process name must not inherit a known target identity")
	}
	if !screenVisionRestoredIdentityMatches(original, screenWindow{ProcessID: 55, ProcessName: "V2RAYN.EXE"}) {
		t.Fatal("same PID and process name should preserve identity case-insensitively")
	}
	if screenVisionRestoredIdentityMatches(original, screenWindow{ProcessID: 56, ProcessName: "v2rayN.exe"}) {
		t.Fatal("different PID must never be rebound")
	}
}

func TestScreenOffscreenCaptureRectNeverIntersectsVirtualDesktop(t *testing.T) {
	virtualDesktop := screenRect{X: -1920, Y: -200, Width: 5760, Height: 2160}
	logical := screenRect{X: 200, Y: 150, Width: 1280, Height: 900}
	offscreen := screenOffscreenCaptureRect(logical, virtualDesktop)
	if screenRectsIntersect(offscreen, virtualDesktop) {
		t.Fatalf("off-screen restore rect %+v intersects virtual desktop %+v", offscreen, virtualDesktop)
	}
	if offscreen.Width != logical.Width || offscreen.Height != logical.Height {
		t.Fatalf("off-screen restore changed size: got %+v want %dx%d", offscreen, logical.Width, logical.Height)
	}
}

func TestScreenRectsIntersectEdgeTouchIsNotIntersection(t *testing.T) {
	left := screenRect{X: 0, Y: 0, Width: 100, Height: 100}
	right := screenRect{X: 100, Y: 0, Width: 100, Height: 100}
	if screenRectsIntersect(left, right) {
		t.Fatal("rectangles that only touch at an edge must not be treated as overlapping")
	}
	if !screenRectsIntersect(left, screenRect{X: 99, Y: 0, Width: 100, Height: 100}) {
		t.Fatal("one-pixel overlap must be detected")
	}
}

func TestScreenLogicalCaptureBoundsKeepsLogicalOrigin(t *testing.T) {
	logical := screenRect{X: 440, Y: 220, Width: 1000, Height: 700}
	got := screenLogicalCaptureBounds(logical, 1012, 712)
	if got.X != logical.X || got.Y != logical.Y {
		t.Fatalf("logical capture origin changed: got %+v want origin %d,%d", got, logical.X, logical.Y)
	}
	if got.Width != 1012 || got.Height != 712 {
		t.Fatalf("capture dimensions were not updated from actual pixels: %+v", got)
	}
}
