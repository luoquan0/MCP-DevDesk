package mcpcore

import (
	"strings"
	"testing"
)

func TestScreenVisionDefaultInstructionsMatchMode(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		windowID  string
		want      []string
		notWanted []string
	}{
		{
			name: "desktop",
			mode: "desktop",
			want: []string{"default desktop visual inspection policy", "screen_list_windows", "screen_capture_window", "screen_capture_active_window", "process/port metadata", "after an actual Screen Vision capture attempt fails"},
		},
		{
			name:      "specified window",
			mode:      "window",
			windowID:  "123",
			want:      []string{"default desktop visual inspection policy", "screen_capture_window", "locked", "foreground", "after the capture tool itself fails"},
			notWanted: []string{"screen_capture_active_window first"},
		},
		{
			name:      "active",
			mode:      "active",
			want:      []string{"default desktop visual inspection policy", "screen_capture_active_window first", "process/port metadata", "after the Screen Vision capture itself fails"},
			notWanted: []string{"screen_list_windows to locate"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, err := New(Options{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			server.ConfigureScreenVision(tc.mode, tc.windowID, 456)
			instructions := strings.ToLower(server.initializeInstructions())
			for _, want := range tc.want {
				if !strings.Contains(instructions, strings.ToLower(want)) {
					t.Fatalf("instructions for %s missing %q:\\n%s", tc.mode, want, instructions)
				}
			}
			for _, notWanted := range tc.notWanted {
				if strings.Contains(instructions, strings.ToLower(notWanted)) {
					t.Fatalf("instructions for %s unexpectedly contain %q:\\n%s", tc.mode, notWanted, instructions)
				}
			}
		})
	}
}

func TestScreenVisionDefaultInstructionsNotAdvertisedWhenUnavailable(t *testing.T) {
	cases := []Options{
		{Workspace: t.TempDir(), PermissionMode: "trusted", ScreenCaptureEnabled: false},
		{Workspace: t.TempDir(), PermissionMode: "safe", ScreenCaptureEnabled: true},
	}
	for _, options := range cases {
		server, err := New(options)
		if err != nil {
			t.Fatal(err)
		}
		server.ConfigureScreenVision("desktop", "", 0)
		instructions := server.initializeInstructions()
		server.Close()
		if strings.Contains(instructions, "default desktop visual inspection policy") {
			t.Fatalf("unavailable Screen Vision must not be advertised:\\n%s", instructions)
		}
	}
}

func TestScreenToolDescriptionsPreferDirectGUIInspection(t *testing.T) {
	descriptions := map[string]string{}
	for _, tool := range screenTools() {
		descriptions[tool.Name] = strings.ToLower(tool.Description)
	}
	checks := map[string][]string{
		"screen_list_windows":          {"first by default", "gui", "screen_capture_window", "process, port"},
		"screen_capture_window":        {"primary gui inspection tool", "process/port metadata", "only ask the user to foreground"},
		"screen_capture_active_window": {"primary gui inspection tool", "current foreground/current window"},
		"screen_capture_desktop":       {"overview", "named background app", "screen_capture_window"},
	}
	for name, fragments := range checks {
		description := descriptions[name]
		for _, fragment := range fragments {
			if !strings.Contains(description, fragment) {
				t.Fatalf("%s description missing %q: %s", name, fragment, description)
			}
		}
	}
}
