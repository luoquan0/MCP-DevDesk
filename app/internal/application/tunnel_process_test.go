package application

import (
	"testing"

	"mcp-devdesk/internal/model"
)

func TestTunnelIdentityAndTargetMatching(t *testing.T) {
	cfg := model.Config{
		MCPHost:    "127.0.0.1",
		MCPPort:    9876,
		TunnelID:   "abcd-1234",
		TunnelName: "mcp-devdesk",
	}
	process := model.TunnelProcess{
		TunnelID:   "ABCD-1234",
		TunnelName: "different-name",
		LocalHost:  "localhost",
		LocalPort:  9876,
	}
	if !tunnelIdentityMatches(process, cfg) {
		t.Fatal("expected tunnel identity to match by ID")
	}
	if !tunnelTargetMatches(process, cfg) {
		t.Fatal("expected loopback target to match")
	}
	process.LocalPort = 8765
	if tunnelTargetMatches(process, cfg) {
		t.Fatal("unexpected target match for stale port")
	}
}

func TestTunnelIdentityFallsBackToName(t *testing.T) {
	cfg := model.Config{TunnelName: "mcp-devdesk"}
	process := model.TunnelProcess{TunnelName: "MCP-DevDesk"}
	if !tunnelIdentityMatches(process, cfg) {
		t.Fatal("expected tunnel identity to match by name")
	}
}
