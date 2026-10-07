package tunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mcp-devdesk/internal/model"
	processmanager "mcp-devdesk/internal/process"
)

func TestReplaceTunnelWithMissingCredentialsRecreatesSameName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DEVDESK_ROOT", root)

	const staleID = "11111111-2222-3333-4444-555555555555"
	const newID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var calls [][]string
	createAttempts := 0

	client := &Client{
		runOverride: func(_ context.Context, _ model.Config, args ...string) (string, error) {
			copied := append([]string(nil), args...)
			calls = append(calls, copied)
			if reflect.DeepEqual(args, deleteTunnelArguments(staleID)) {
				return "Deleted tunnel " + staleID, nil
			}
			if len(args) == 5 && args[0] == "tunnel" && args[1] == "create" && args[2] == "--credentials-file" && args[4] == "mcp-devdesk" {
				createAttempts++
				if createAttempts == 1 {
					return "A tunnel with this name already exists", errors.New("exit status 1")
				}
				credentialsPath := args[3]
				payload := []byte(`{"AccountTag":"account","TunnelID":"` + newID + `","TunnelSecret":"replacement-secret"}`)
				if err := os.WriteFile(credentialsPath, payload, 0o600); err != nil {
					t.Fatalf("write replacement credentials: %v", err)
				}
				return "Created tunnel mcp-devdesk with id " + newID, nil
			}
			t.Fatalf("unexpected cloudflared args: %#v", args)
			return "", nil
		},
	}

	gotID, credentials, output, err := client.replaceTunnelWithMissingCredentials(context.Background(), model.Config{}, "mcp-devdesk", staleID)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != newID {
		t.Fatalf("replacement tunnel id = %q, want %q", gotID, newID)
	}
	wantCredentials := processmanager.PortableCredentialsPath(root, newID)
	if filepath.Clean(credentials) != filepath.Clean(wantCredentials) {
		t.Fatalf("credentials = %q, want %q", credentials, wantCredentials)
	}
	if _, err := os.Stat(credentials); err != nil {
		t.Fatalf("replacement credentials were not stored: %v", err)
	}
	if createAttempts != 2 {
		t.Fatalf("create attempts = %d, want 2", createAttempts)
	}
	if len(calls) != 3 {
		t.Fatalf("cloudflared calls = %#v, want delete + 2 create attempts", calls)
	}
	if output == "" {
		t.Fatal("expected combined recovery output")
	}
}

func TestTunnelNameConflictDetection(t *testing.T) {
	err := errors.New("exit status 1")
	for _, output := range []string{
		"A tunnel with this name already exists",
		"tunnel name already exists",
		"account already has a tunnel named mcp-devdesk",
	} {
		if !tunnelNameConflict(output, err) {
			t.Fatalf("expected name conflict for %q", output)
		}
	}
	if tunnelNameConflict("permission denied", err) {
		t.Fatal("permission errors must not be retried as a name conflict")
	}
}
