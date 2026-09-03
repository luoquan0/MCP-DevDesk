package process

import (
	"slices"
	"testing"

	"github.com/luoquan0/mcp-devdesk/app/internal/model"
)

func TestMCPArgumentsScreenCaptureOptIn(t *testing.T) {
	base := model.Config{
		Workspace:      t.TempDir(),
		MCPHost:        "127.0.0.1",
		MCPPort:        8765,
		CoreMode:       "go",
		ToolProfile:    "full",
		PermissionMode: "trusted",
		FileScope:      "workspace",
	}
	without := mcpArguments(base, t.TempDir(), "http://127.0.0.1:8765", "")
	if slices.Contains(without, "--enable-screen-capture") {
		t.Fatalf("screen capture flag present while disabled: %v", without)
	}
	base.ScreenCaptureEnabled = true
	with := mcpArguments(base, t.TempDir(), "http://127.0.0.1:8765", "")
	if !slices.Contains(with, "--enable-screen-capture") {
		t.Fatalf("screen capture flag missing while enabled: %v", with)
	}
}
