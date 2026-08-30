package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-devdesk/internal/model"
	"mcp-devdesk/internal/secrets"
)

func TestUserHomeDirIsAvailable(t *testing.T) {
	if home := UserHomeDir(); home == "" {
		t.Fatal("expected a Windows user home directory")
	}
}

func TestMCPEnvironmentIncludesAllOAuthValues(t *testing.T) {
	cfg := model.Config{ToolProfile: "full"}
	values := secrets.Values{
		OwnerPassword: "owner-value",
		ClientID:      "client-id",
		ClientSecret:  "client-value",
		TokenSecret:   strings.Repeat("ab", 32),
	}
	env := mcpEnvironment(cfg, values, "https://example.test")
	want := map[string]string{
		"CODING_TOOLS_MCP_SERVER_URL":          "https://example.test",
		"CODING_TOOLS_MCP_OAUTH_PASSWORD":      values.OwnerPassword,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_ID":     values.ClientID,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET": values.ClientSecret,
		"CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET":  values.TokenSecret,
		"CODING_TOOLS_MCP_TOOL_PROFILE":        cfg.ToolProfile,
	}
	for key, expected := range want {
		found := false
		for _, entry := range env {
			if entry == key+"="+expected {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s from MCP environment", key)
		}
	}
}

func TestGoCoreLaunchConfiguration(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cfg := model.Config{
		CoreMode:         "go",
		CoreExecutable:   `C:\legacy.exe`,
		GoCoreExecutable: `C:\mcp-core.exe`,
		Workspace:        `C:\workspace`,
		MCPHost:          "127.0.0.1",
		MCPPort:          8765,
		ToolProfile:      "full",
		PermissionMode:   "trusted",
		FileScope:        "roots",
		AllowedRoots:     []string{`C:\workspace`, `D:\projects`},
		AllowNetwork:     true,
	}
	if selected := selectedMCPExecutable(cfg); selected != cfg.GoCoreExecutable {
		t.Fatalf("selected executable = %q", selected)
	}
	args := mcpArguments(cfg, dataDir, "https://mcp.example", `C:\data\project-instructions.md`)
	wantPairs := map[string]string{
		"--workspace":         cfg.Workspace,
		"--host":              cfg.MCPHost,
		"--port":              "8765",
		"--permission-mode":   cfg.PermissionMode,
		"--data-dir":          dataDir,
		"--server-url":        "https://mcp.example",
		"--file-scope":        cfg.FileScope,
		"--instructions-file": `C:\data\project-instructions.md`,
	}
	for flag, expected := range wantPairs {
		if !argumentPairExists(args, flag, expected) {
			t.Fatalf("missing %s %q in %#v", flag, expected, args)
		}
	}
	if !containsArgument(args, "--allow-network") {
		t.Fatal("Go core launch arguments do not include --allow-network")
	}
	if countArgument(args, "--allowed-root") != 2 {
		t.Fatalf("allowed root argument count = %d", countArgument(args, "--allowed-root"))
	}
}

func TestSyncInstructionsUpdatesAndRemovesManagedFile(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	current := "global prompt v1"
	manager := NewManager("", dataDir, nil, nil, func(gotWorkspace string) string {
		if gotWorkspace != workspace {
			t.Fatalf("workspace = %q, want %q", gotWorkspace, workspace)
		}
		return current
	})
	cfg := model.Config{CoreMode: "go", Workspace: workspace}
	path := filepath.Join(dataDir, "project-instructions.md")

	if err := manager.SyncInstructions(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "global prompt v1\n" {
		t.Fatalf("instructions = %q", raw)
	}

	current = "global prompt v2"
	if err := manager.SyncInstructions(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "global prompt v2\n" {
		t.Fatalf("updated instructions = %q", raw)
	}

	current = ""
	if err := manager.SyncInstructions(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("instructions file still exists, stat err = %v", err)
	}
	watchedPath, err := manager.syncInstructionsFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if watchedPath != path {
		t.Fatalf("empty prompt watch path = %q, want %q", watchedPath, path)
	}
}

func TestLegacyCoreRemainsSelectable(t *testing.T) {
	cfg := model.Config{
		CoreMode:       "legacy",
		CoreExecutable: `C:\legacy.exe`,
		Workspace:      `C:\workspace`,
		MCPHost:        "127.0.0.1",
		MCPPort:        8765,
		ToolProfile:    "full",
		PermissionMode: "safe",
	}
	if selected := selectedMCPExecutable(cfg); selected != cfg.CoreExecutable {
		t.Fatalf("selected executable = %q", selected)
	}
	args := mcpArguments(cfg, t.TempDir(), "http://127.0.0.1:8765", "")
	if !argumentPairExists(args, "--shell-env-inherit", "core") {
		t.Fatalf("legacy safe arguments missing shell env restriction: %#v", args)
	}
	if containsArgument(args, "--data-dir") {
		t.Fatalf("legacy arguments unexpectedly contain Go-only flags: %#v", args)
	}
}

func containsArgument(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func countArgument(values []string, expected string) int {
	count := 0
	for _, value := range values {
		if value == expected {
			count++
		}
	}
	return count
}

func argumentPairExists(values []string, key, expected string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == key && values[index+1] == expected {
			return true
		}
	}
	return false
}
