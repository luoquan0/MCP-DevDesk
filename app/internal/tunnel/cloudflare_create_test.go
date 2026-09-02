package tunnel

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTunnelCreateArgumentsUseCredentialsFile(t *testing.T) {
	got := tunnelCreateArguments(`C:\MCP-DevDesk\data\devdesk\cloudflare\create.json`, "mcp-devdesk")
	want := []string{"tunnel", "create", "--credentials-file", `C:\MCP-DevDesk\data\devdesk\cloudflare\create.json`, "mcp-devdesk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create arguments mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCredentialsFileFlagUnsupported(t *testing.T) {
	err := errors.New("exit status 1")
	if !credentialsFileFlagUnsupported("Incorrect Usage: flag provided but not defined: -credentials-file", err) {
		t.Fatal("expected old cloudflared credentials flag failure to use legacy fallback")
	}
	if credentialsFileFlagUnsupported("failed to create tunnel: permission denied", err) {
		t.Fatal("unrelated create error must not silently fall back")
	}
}

func TestTunnelIDFromCredentialsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	const tunnelID = "11111111-2222-3333-4444-555555555555"
	if err := os.WriteFile(path, []byte(`{"AccountTag":"account","TunnelID":"`+tunnelID+`","TunnelSecret":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tunnelIDFromCredentialsFile(path); got != tunnelID {
		t.Fatalf("tunnel id = %q, want %q", got, tunnelID)
	}
}
