package mcpcore

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOAuthPKCEAndProtectedMCP(t *testing.T) {
	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	server := mustNewServer(t, Options{
		Workspace: t.TempDir(),
		OAuth: OAuthOptions{
			Enabled:       true,
			Issuer:        issuer,
			Resource:      resource,
			OwnerPassword: "owner-password-long-enough",
			ClientID:      "mcp-devdesk",
			ClientSecret:  "static-client-secret-value",
			TokenSecret:   strings.Repeat("ab", 32),
			DataDir:       t.TempDir(),
		},
	})
	handler := server.Handler()

	registerBody := map[string]any{
		"client_name":                "OAuth Test Client",
		"redirect_uris":              []string{"http://127.0.0.1:43210/callback"},
		"token_endpoint_auth_method": "none",
	}
	registerRaw, _ := json.Marshal(registerBody)
	registerRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/register", bytes.NewReader(registerRaw))
	registerRequest.Header.Set("Content-Type", "application/json")
	registerRecorder := httptest.NewRecorder()
	handler.ServeHTTP(registerRecorder, registerRequest)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var registered struct {
		ClientID string `json:"client_id"`
	}
	decodeJSON(t, registerRecorder.Body, &registered)
	if registered.ClientID == "" {
		t.Fatal("dynamic registration did not return a client ID")
	}

	verifier := strings.Repeat("a", 43)
	authorizeValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {registered.ClientID},
		"redirect_uri":          {"http://127.0.0.1:43210/callback"},
		"state":                 {"test-state"},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
		"scope":                 {"mcp"},
		"owner_password":        {"owner-password-long-enough"},
	}
	authorizeRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/authorize", strings.NewReader(authorizeValues.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorizeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizeRecorder, authorizeRequest)
	if authorizeRecorder.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, body = %s", authorizeRecorder.Code, authorizeRecorder.Body.String())
	}
	redirect, err := url.Parse(authorizeRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	if code == "" || redirect.Query().Get("state") != "test-state" {
		t.Fatalf("invalid authorization redirect: %s", redirect.String())
	}

	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {registered.ClientID},
		"code":          {code},
		"redirect_uri":  {"http://127.0.0.1:43210/callback"},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	tokenRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(tokenValues.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tokenRecorder, tokenRequest)
	if tokenRecorder.Code != http.StatusOK {
		t.Fatalf("token status = %d, body = %s", tokenRecorder.Code, tokenRecorder.Body.String())
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSON(t, tokenRecorder.Body, &tokens)
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("missing OAuth tokens: %#v", tokens)
	}

	initializeBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"oauth-test","version":"1"}}}`
	unauthorizedRequest := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(initializeBody))
	unauthorizedRequest.Header.Set("Content-Type", "application/json")
	unauthorizedRequest.Header.Set("Accept", "application/json, text/event-stream")
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorizedRequest)
	if unauthorizedRecorder.Code != http.StatusUnauthorized || unauthorizedRecorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized response = %d, headers = %#v", unauthorizedRecorder.Code, unauthorizedRecorder.Header())
	}

	authorizedRequest := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(initializeBody))
	authorizedRequest.Header.Set("Content-Type", "application/json")
	authorizedRequest.Header.Set("Accept", "application/json, text/event-stream")
	authorizedRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorizedRequest)
	if authorizedRecorder.Code != http.StatusOK || authorizedRecorder.Header().Get(SessionHeader) == "" {
		t.Fatalf("authorized initialize = %d, body = %s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}

	refreshValues := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {registered.ClientID},
		"refresh_token": {tokens.RefreshToken},
		"resource":      {resource},
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(refreshValues.Encode()))
	refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshRecorder := httptest.NewRecorder()
	handler.ServeHTTP(refreshRecorder, refreshRequest)
	if refreshRecorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshRecorder.Code, refreshRecorder.Body.String())
	}

	reuseRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(refreshValues.Encode()))
	reuseRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reuseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reuseRecorder, reuseRequest)
	if reuseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("reused refresh token status = %d", reuseRecorder.Code)
	}
}

func TestStreamableHTTPSSEAndReplay(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	initialize := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": ProtocolVersion,
			"clientInfo":      map[string]any{"name": "sse-test", "version": "1"},
		},
	})
	defer initialize.Body.Close()
	sessionID := initialize.Header.Get(SessionHeader)
	if sessionID == "" {
		t.Fatal("missing session ID")
	}

	payload := []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	sseRequest, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	sseRequest.Header.Set("Content-Type", "application/json")
	sseRequest.Header.Set("Accept", "text/event-stream")
	sseRequest.Header.Set(SessionHeader, sessionID)
	sseRequest.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	sseResponse, err := http.DefaultClient.Do(sseRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResponse.Body.Close()
	body := readBody(t, sseResponse.Body)
	if sseResponse.StatusCode != http.StatusOK || !strings.Contains(body, "id: "+sessionID+":1") || !strings.Contains(body, `"result":{}`) {
		t.Fatalf("unexpected SSE response: %d %s", sseResponse.StatusCode, body)
	}

	replayRequest, err := http.NewRequest(http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	replayRequest.Header.Set("Accept", "text/event-stream")
	replayRequest.Header.Set(SessionHeader, sessionID)
	replayRequest.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	replayRequest.Header.Set("Prefer", "wait=0")
	replayResponse, err := http.DefaultClient.Do(replayRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer replayResponse.Body.Close()
	replayBody := readBody(t, replayResponse.Body)
	if replayResponse.StatusCode != http.StatusOK || !strings.Contains(replayBody, "id: "+sessionID+":1") {
		t.Fatalf("unexpected replay response: %d %s", replayResponse.StatusCode, replayBody)
	}
}

func TestOriginValidationRejectsRemoteBrowserOrigin(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/healthz", nil)
	request.Header.Set("Origin", "https://attacker.example")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("origin validation status = %d", recorder.Code)
	}
}

func TestStaticOAuthClientUsesConfiguredRedirectURI(t *testing.T) {
	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	const redirectURI = "http://127.0.0.1:43210/static-callback"
	server := mustNewServer(t, Options{
		Workspace: t.TempDir(),
		OAuth: OAuthOptions{
			Enabled:       true,
			Issuer:        issuer,
			Resource:      resource,
			OwnerPassword: "owner-password-long-enough",
			ClientID:      "static-client",
			ClientSecret:  "static-client-secret-value",
			RedirectURIs:  []string{redirectURI},
			TokenSecret:   strings.Repeat("ef", 32),
			DataDir:       t.TempDir(),
		},
	})
	handler := server.Handler()
	verifier := strings.Repeat("b", 43)
	authorizeValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {"static-client"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
		"scope":                 {"mcp"},
		"owner_password":        {"owner-password-long-enough"},
	}
	authorizeRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/authorize", strings.NewReader(authorizeValues.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorizeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizeRecorder, authorizeRequest)
	if authorizeRecorder.Code != http.StatusFound {
		t.Fatalf("static authorize status = %d, body = %s", authorizeRecorder.Code, authorizeRecorder.Body.String())
	}
	redirect, err := url.Parse(authorizeRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	if code == "" {
		t.Fatalf("missing authorization code: %s", redirect.String())
	}
	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"static-client"},
		"client_secret": {"static-client-secret-value"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	tokenRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(tokenValues.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tokenRecorder, tokenRequest)
	if tokenRecorder.Code != http.StatusOK {
		t.Fatalf("static token status = %d, body = %s", tokenRecorder.Code, tokenRecorder.Body.String())
	}

	badValues := make(url.Values, len(authorizeValues))
	for key, values := range authorizeValues {
		badValues[key] = append([]string(nil), values...)
	}
	badValues.Set("redirect_uri", "http://127.0.0.1:43210/not-registered")
	badRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/authorize", strings.NewReader(badValues.Encode()))
	badRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unregistered static redirect status = %d", badRecorder.Code)
	}
}

func TestDynamicOAuthClientsAreEncryptedAndReloaded(t *testing.T) {
	dataDir := t.TempDir()
	options := OAuthOptions{
		Enabled:       true,
		Issuer:        "http://127.0.0.1:18765",
		Resource:      "http://127.0.0.1:18765/mcp",
		OwnerPassword: "owner-password-long-enough",
		ClientID:      "static-client",
		ClientSecret:  "static-client-secret-value",
		RedirectURIs:  []string{"http://127.0.0.1:43210/static"},
		TokenSecret:   strings.Repeat("12", 32),
		DataDir:       dataDir,
	}
	oauthServer, err := newOAuthServer(options)
	if err != nil {
		t.Fatal(err)
	}
	requestBody := `{"client_name":"Encrypted Client","redirect_uris":["http://127.0.0.1:54321/callback"],"token_endpoint_auth_method":"client_secret_post"}`
	request := httptest.NewRequest(http.MethodPost, options.Issuer+"/oauth/register", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	oauthServer.handleDynamicRegistration(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var registered struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	decodeJSON(t, recorder.Body, &registered)
	if registered.ClientID == "" || registered.ClientSecret == "" {
		t.Fatalf("missing registered credentials: %#v", registered)
	}
	storedPath := filepath.Join(dataDir, "oauth-clients.json")
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), registered.ClientID) || strings.Contains(string(stored), registered.ClientSecret) {
		t.Fatalf("OAuth client file contains plaintext credentials: %s", string(stored))
	}
	var envelope oauthClientsEnvelope
	if err := json.Unmarshal(stored, &envelope); err != nil || envelope.Version != 2 || envelope.Data == "" || envelope.Protection == "" {
		t.Fatalf("invalid encrypted OAuth client envelope: %#v, %v", envelope, err)
	}
	reloaded, err := newOAuthServer(options)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := reloaded.lookupClient(registered.ClientID)
	if !ok || client.ClientSecret != registered.ClientSecret || len(client.RedirectURIs) != 1 {
		t.Fatalf("registered client was not reloaded: %#v, %v", client, ok)
	}
}
