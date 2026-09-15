package mcpcore

import "testing"

func TestV01235ValidationToolsRespectToolProfiles(t *testing.T) {
	readOnly := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted", ToolProfile: "read-only"})
	defer readOnly.Close()
	for _, tool := range readOnly.tools {
		if tool.Name == "checks_run" || tool.Name == "validate_project" {
			t.Fatalf("read-only profile exposed project validation tool %s", tool.Name)
		}
	}

	full := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted", ToolProfile: "full"})
	defer full.Close()
	found := map[string]bool{"checks_run": false, "validate_project": false}
	for _, tool := range full.tools {
		if _, ok := found[tool.Name]; ok {
			found[tool.Name] = true
		}
	}
	for name, ok := range found {
		if !ok {
			t.Fatalf("full profile is missing project validation tool %s", name)
		}
	}
}
