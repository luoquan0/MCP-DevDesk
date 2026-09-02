package application

import (
	"testing"

	"mcp-devdesk/internal/model"
)

func TestShouldAutoStartProjectInstanceSkipsPrimaryWorkspace(t *testing.T) {
	primary := `C:\work\main`
	cfg := model.Config{Workspace: `C:\work\main`, AutoStart: true}
	if shouldAutoStartProjectInstance(primary, cfg) {
		t.Fatal("primary workspace must not auto-start as a duplicate managed instance")
	}

	cfg.Workspace = `C:\work\other`
	if !shouldAutoStartProjectInstance(primary, cfg) {
		t.Fatal("independent auto-start project should still be restored")
	}

	cfg.AutoStart = false
	if shouldAutoStartProjectInstance(primary, cfg) {
		t.Fatal("disabled auto-start project must remain stopped")
	}
}
