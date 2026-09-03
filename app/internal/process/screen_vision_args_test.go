package process

import (
	"path/filepath"
	"slices"
	"testing"

	"mcp-devdesk/internal/model"
)

func TestMCPArgumentsScreenCaptureOptIn(t *testing.T) {
	dataDir := t.TempDir()
	base := model.Config{
		Workspace:      t.TempDir(),
		MCPHost:        "127.0.0.1",
		MCPPort:        8765,
		CoreMode:       "go",
		ToolProfile:    "full",
		PermissionMode: "trusted",
		FileScope:      "workspace",
	}
	without := mcpArguments(base, dataDir, "http://127.0.0.1:8765", "")
	if slices.Contains(without, "--enable-screen-capture") {
		t.Fatalf("screen capture flag present while disabled: %v", without)
	}
	if got := argumentValue(without, "--screen-vision-config"); got != filepath.Join(dataDir, "config.json") {
		t.Fatalf("primary screen vision config = %q", got)
	}

	base.ScreenCaptureEnabled = true
	with := mcpArguments(base, dataDir, "http://127.0.0.1:8765", "")
	if !slices.Contains(with, "--enable-screen-capture") {
		t.Fatalf("screen capture flag missing while enabled: %v", with)
	}
}

func TestMCPArgumentsManagedInstanceUsesPrimaryScreenVisionConfig(t *testing.T) {
	primaryDataDir := t.TempDir()
	instanceDataDir := filepath.Join(primaryDataDir, "instances", "abc123")
	cfg := model.Config{
		Workspace:      t.TempDir(),
		MCPHost:        "127.0.0.1",
		MCPPort:        8770,
		CoreMode:       "go",
		ToolProfile:    "full",
		PermissionMode: "trusted",
		FileScope:      "workspace",
	}
	args := mcpArguments(cfg, instanceDataDir, "http://127.0.0.1:8770", "")
	if got := argumentValue(args, "--logging-config"); got != filepath.Join(instanceDataDir, "config.json") {
		t.Fatalf("instance logging config = %q", got)
	}
	if got := argumentValue(args, "--screen-vision-config"); got != filepath.Join(primaryDataDir, "config.json") {
		t.Fatalf("managed screen vision config = %q, want primary config", got)
	}
}

func argumentValue(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}
