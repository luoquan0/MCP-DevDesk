//go:build windows

package mcpcore

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOAuthStateRecoversFromUnreadableLegacyDPAPIAfterWindowsReinstall(t *testing.T) {
	dataDir := t.TempDir()
	legacy := oauthClientsEnvelope{
		Version:    2,
		Protection: "windows-dpapi-current-user",
		Data:       base64.StdEncoding.EncodeToString([]byte("not-a-valid-dpapi-blob")),
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"oauth-clients.json", "oauth-refresh-tokens.json"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	server, err := newOAuthServer(OAuthOptions{
		Enabled:       true,
		Issuer:        issuer,
		Resource:      resource,
		OwnerPassword: "owner-password-long-enough",
		ClientID:      "static-client",
		ClientSecret:  "static-client-secret-value",
		TokenSecret:   strings.Repeat("56", 32),
		DataDir:       dataDir,
	})
	if err != nil {
		t.Fatalf("OAuth server should recover unreadable DPAPI state: %v", err)
	}

	for _, name := range []string{"oauth-clients.json", "oauth-refresh-tokens.json"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be quarantined, stat err=%v", name, err)
		}
	}

	server.clients["portable-client"] = oauthClient{
		ClientID:                "portable-client",
		ClientSecret:            "portable-client-secret",
		RedirectURIs:            []string{"http://127.0.0.1:43210/callback"},
		TokenEndpointAuthMethod: "client_secret_post",
		CreatedAt:               time.Now().Unix(),
	}
	if err := server.saveClientsLocked(); err != nil {
		t.Fatal(err)
	}
	server.refreshTokens["portable-refresh"] = refreshGrant{
		ClientID:  "static-client",
		Resource:  resource,
		Scope:     "mcp",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := server.saveRefreshTokensLocked(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"oauth-clients.json", "oauth-refresh-tokens.json"} {
		stored, err := os.ReadFile(filepath.Join(dataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var envelope oauthClientsEnvelope
		if err := json.Unmarshal(stored, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Version != 3 || envelope.Protection != "portable-aes256-gcm-v1" || envelope.Data == "" {
			t.Fatalf("%s was not rewritten as portable encrypted state: %#v", name, envelope)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "master.key")); err != nil {
		t.Fatalf("portable OAuth master key missing: %v", err)
	}

	recoveryDir := filepath.Join(dataDir, "recovery")
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		t.Fatal(err)
	}
	foundClients := false
	foundRefresh := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "oauth-clients.json.") && strings.Contains(entry.Name(), "legacy-dpapi-unavailable") {
			foundClients = true
		}
		if strings.HasPrefix(entry.Name(), "oauth-refresh-tokens.json.") && strings.Contains(entry.Name(), "legacy-dpapi-unavailable") {
			foundRefresh = true
		}
	}
	if !foundClients || !foundRefresh {
		t.Fatalf("legacy OAuth backups missing: clients=%v refresh=%v", foundClients, foundRefresh)
	}
}
