package mcpcore

import (
	"runtime"
	"testing"
)

func TestCheckExecEnvironmentIncludesScreenProbeCompatibility(t *testing.T) {
	server, err := New(Options{
		Workspace:            t.TempDir(),
		PermissionMode:       "dangerous",
		ToolProfile:          "full",
		ScreenCaptureEnabled: true,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer server.Close()
	result, err := server.executeCompatibilityTool("check_exec_environment", map[string]any{})
	if err != nil {
		t.Fatalf("check_exec_environment: %v", err)
	}
	if got := result["toolCatalogGeneration"]; got != "v013-catalog3" {
		t.Fatalf("catalog generation = %v, want v013-catalog3", got)
	}
	probe, ok := result["screenCaptureProbe"].(map[string]any)
	if !ok {
		t.Fatalf("screenCaptureProbe compatibility payload missing: %#v", result["screenCaptureProbe"])
	}
	if got := probe["connectorFallback"]; got != "check_exec_environment.screenCaptureProbe" {
		t.Fatalf("connector fallback = %v", got)
	}
	if got := probe["stateChangingFallbacks"]; got != false {
		t.Fatalf("stateChangingFallbacks = %v, want false", got)
	}
	if runtime.GOOS == "windows" {
		if got := probe["advertised"]; got != true {
			t.Fatalf("probe advertised = %v, want true", got)
		}
	}
}
