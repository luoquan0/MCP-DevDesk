package mcpcore

import (
	"strings"
	"testing"
)

func TestConfigureScreenVisionFiltersToolsByMode(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		windowID  string
		want      []string
		notWanted []string
		wantCount int
	}{
		{
			name:      "active",
			mode:      "active",
			want:      []string{"screen_capture_probe", "screen_get_active_window", "screen_capture_active_window"},
			notWanted: []string{"screen_list_windows", "screen_capture_window", "screen_capture_desktop"},
			wantCount: 52,
		},
		{
			name:      "window",
			mode:      "window",
			windowID:  "0x10",
			want:      []string{"screen_capture_probe", "screen_list_windows", "screen_capture_window"},
			notWanted: []string{"screen_get_active_window", "screen_capture_active_window", "screen_capture_desktop"},
			wantCount: 52,
		},
		{
			name: "desktop",
			mode: "desktop",
			want: []string{
				"screen_capture_probe",
				"screen_list_windows",
				"screen_get_active_window",
				"screen_capture_window",
				"screen_capture_active_window",
				"screen_capture_desktop",
			},
			wantCount: 55,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			server.ConfigureScreenVision(test.mode, test.windowID, 1234)
			for _, name := range test.want {
				if !containsTool(server.tools, name) {
					t.Fatalf("mode %s missing tool %s: %#v", test.mode, name, server.tools)
				}
			}
			for _, name := range test.notWanted {
				if containsTool(server.tools, name) {
					t.Fatalf("mode %s unexpectedly advertised %s", test.mode, name)
				}
			}
			if len(server.tools) != test.wantCount {
				t.Fatalf("mode %s tool count = %d, want %d", test.mode, len(server.tools), test.wantCount)
			}
		})
	}
}

func TestSpecifiedWindowModeLocksTarget(t *testing.T) {
	server, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.ConfigureScreenVision("window", "0x20", 222)

	if got, err := server.screenVisionWindowArgument(""); err != nil || got != "0x20" {
		t.Fatalf("empty target = %q, %v", got, err)
	}
	if _, err := server.screenVisionWindowArgument("0x30"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("different target should be rejected, got %v", err)
	}
	if err := server.validateScreenVisionWindow(screenWindow{ID: "0x20", ProcessID: 333}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("reused window handle should be rejected, got %v", err)
	}
	if err := server.enforceScreenVisionToolPolicy("screen_capture_desktop"); err == nil {
		t.Fatal("desktop capture should be blocked while specified-window mode is active")
	}
}

func TestSpecifiedWindowModeWithoutTargetDoesNotAdvertiseCapture(t *testing.T) {
	server, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.ConfigureScreenVision("window", "", 0)
	if !containsTool(server.tools, "screen_capture_probe") {
		t.Fatal("specified-window mode should keep the metadata-only compatibility probe before a target is selected")
	}
	if !containsTool(server.tools, "screen_list_windows") {
		t.Fatal("specified-window mode should still allow metadata listing before a target is selected")
	}
	if containsTool(server.tools, "screen_capture_window") {
		t.Fatal("capture must not be advertised before a specified target is selected")
	}
	if len(server.tools) != 51 {
		t.Fatalf("specified-window mode without target tool count = %d, want 51", len(server.tools))
	}
}

func TestDesktopModeAllowsWindowInspection(t *testing.T) {
	server, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.ConfigureScreenVision("desktop", "", 0)

	for _, name := range []string{
		"screen_capture_probe",
		"screen_list_windows",
		"screen_get_active_window",
		"screen_capture_window",
		"screen_capture_active_window",
		"screen_capture_desktop",
	} {
		if err := server.enforceScreenVisionToolPolicy(name); err != nil {
			t.Fatalf("desktop mode should allow %s: %v", name, err)
		}
	}
	if got, err := server.screenVisionWindowArgument("0x99"); err != nil || got != "0x99" {
		t.Fatalf("desktop mode must not lock arbitrary visible window selection: got %q, %v", got, err)
	}
}

func containsTool(tools []Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
