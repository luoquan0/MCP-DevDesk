package process

import (
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
