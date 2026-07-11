package mcpcore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshTokensAreEncryptedAndSurviveRestart(t *testing.T) {
	dataDir := t.TempDir()
	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	options := OAuthOptions{
		Enabled:       true,
		Issuer:        issuer,
		Resource:      resource,
		OwnerPassword: "owner-password-long-enough",
		ClientID:      "static-client",
		TokenSecret:   strings.Repeat("34", 32),
		DataDir:       dataDir,
	}
	first, err := newOAuthServer(options)
	if err != nil {
		t.Fatal(err)
	}
	issued := httptest.NewRecorder()
	first.issueTokens(issued, "static-client", resource, "mcp")
	if issued.Code != http.StatusOK {
		t.Fatalf("issue tokens status = %d, body = %s", issued.Code, issued.Body.String())
	}
	var original struct {
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, issued.Body, &original)
	if original.RefreshToken == "" {
		t.Fatal("missing refresh token")
	}

	storedPath := filepath.Join(dataDir, "oauth-refresh-tokens.json")
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), original.RefreshToken) {
		t.Fatalf("refresh token file contains plaintext token: %s", string(stored))
	}
	var envelope oauthClientsEnvelope
	if err := json.Unmarshal(stored, &envelope); err != nil || envelope.Version != 2 || envelope.Data == "" || envelope.Protection == "" {
		t.Fatalf("invalid encrypted refresh token envelope: %#v, %v", envelope, err)
	}

	restarted, err := newOAuthServer(options)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := restarted.lookupClient("static-client")
	if !ok {
		t.Fatal("static client was not available after restart")
	}
	refreshValues := url.Values{
		"refresh_token": {original.RefreshToken},
		"resource":      {resource},
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(refreshValues.Encode()))
	refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := refreshRequest.ParseForm(); err != nil {
		t.Fatal(err)
	}
	refreshed := httptest.NewRecorder()
	restarted.exchangeRefreshToken(refreshed, refreshRequest, client)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh after restart status = %d, body = %s", refreshed.Code, refreshed.Body.String())
	}

	reuseRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(refreshValues.Encode()))
	reuseRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := reuseRequest.ParseForm(); err != nil {
		t.Fatal(err)
	}
	reused := httptest.NewRecorder()
	restarted.exchangeRefreshToken(reused, reuseRequest, client)
	if reused.Code != http.StatusBadRequest {
		t.Fatalf("reused refresh token status = %d, body = %s", reused.Code, reused.Body.String())
	}
}
