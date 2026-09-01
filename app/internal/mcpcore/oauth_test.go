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

	authorizePageValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {registered.ClientID},
		"redirect_uri":          {"http://127.0.0.1:43210/callback"},
		"state":                 {"test-state"},
		"code_challenge":        {pkceChallenge(strings.Repeat("a", 43))},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
		"scope":                 {"mcp"},
	}
	authorizePageRequest := httptest.NewRequest(http.MethodGet, issuer+"/oauth/authorize?"+authorizePageValues.Encode(), nil)
	authorizePageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizePageRecorder, authorizePageRequest)
	if authorizePageRecorder.Code != http.StatusOK {
		t.Fatalf("authorize page status = %d, body = %s", authorizePageRecorder.Code, authorizePageRecorder.Body.String())
	}
	authorizeCSP := authorizePageRecorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(authorizeCSP, "form-action 'self' http://127.0.0.1:43210") {
		t.Fatalf("authorize page CSP does not allow validated redirect origin: %q", authorizeCSP)
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

func TestOAuthStaticClientSupportsMultipleChatGPTInstances(t *testing.T) {
	const (
		ownerPassword  = "owner-password-long-enough"
		clientID       = "mcp-devdesk"
		clientSecret   = "static-client-secret-value"
		firstCallback  = "https://chatgpt.com/connector/oauth/first-app"
		secondCallback = "https://chatgpt.com/connector/oauth/second-app"
	)
	tokenSecret := strings.Repeat("de", 32)

	type instance struct {
		issuer   string
		resource string
		handler  http.Handler
	}
	newInstance := func(issuer string) instance {
		resource := issuer + "/mcp"
		server := mustNewServer(t, Options{
			Workspace: t.TempDir(),
			OAuth: OAuthOptions{
				Enabled:       true,
				Issuer:        issuer,
				Resource:      resource,
				OwnerPassword: ownerPassword,
				ClientID:      clientID,
				ClientSecret:  clientSecret,
				RedirectURIs:  []string{firstCallback},
				TokenSecret:   tokenSecret,
				DataDir:       t.TempDir(),
			},
		})
		return instance{issuer: issuer, resource: resource, handler: server.Handler()}
	}

	first := newInstance("https://mcp1.example.test")
	second := newInstance("https://mcp2.example.test")

	exchange := func(target instance, callback string, withSecret bool) string {
		t.Helper()
		verifier := strings.Repeat("z", 43)
		values := url.Values{
			"response_type":         {"code"},
			"client_id":             {clientID},
			"redirect_uri":          {callback},
			"code_challenge":        {pkceChallenge(verifier)},
			"code_challenge_method": {"S256"},
			"resource":              {target.resource},
			"scope":                 {"mcp offline_access"},
		}
		page := httptest.NewRecorder()
		target.handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, target.issuer+"/oauth/authorize?"+values.Encode(), nil))
		if page.Code != http.StatusOK {
			t.Fatalf("authorize page for %s = %d %s", target.issuer, page.Code, page.Body.String())
		}

		values.Set("owner_password", ownerPassword)
		authorized := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, target.issuer+"/oauth/authorize", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		target.handler.ServeHTTP(authorized, request)
		if authorized.Code != http.StatusFound {
			t.Fatalf("authorize for %s = %d %s", target.issuer, authorized.Code, authorized.Body.String())
		}
		redirect, err := url.Parse(authorized.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		code := redirect.Query().Get("code")
		if code == "" {
			t.Fatalf("authorization code missing for %s", target.issuer)
		}

		tokenValues := url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {clientID},
			"code":          {code},
			"redirect_uri":  {callback},
			"code_verifier": {verifier},
			"resource":      {target.resource},
		}
		if withSecret {
			tokenValues.Set("client_secret", clientSecret)
		}
		tokenRecorder := httptest.NewRecorder()
		tokenRequest := httptest.NewRequest(http.MethodPost, target.issuer+"/oauth/token", strings.NewReader(tokenValues.Encode()))
		tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		target.handler.ServeHTTP(tokenRecorder, tokenRequest)
		if tokenRecorder.Code != http.StatusOK {
			t.Fatalf("token exchange for %s = %d %s", target.issuer, tokenRecorder.Code, tokenRecorder.Body.String())
		}
		var tokenResult struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Scope        string `json:"scope"`
		}
		decodeJSON(t, tokenRecorder.Body, &tokenResult)
		if tokenResult.AccessToken == "" || tokenResult.RefreshToken == "" || !containsScope(tokenResult.Scope, "offline_access") {
			t.Fatalf("incomplete token response for %s: %#v", target.issuer, tokenResult)
		}
		return tokenResult.AccessToken
	}

	firstToken := exchange(first, firstCallback, true)
	_ = exchange(second, secondCallback, false)

	initializeBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"chatgpt","version":"1"}}}`
	crossInstance := httptest.NewRecorder()
	crossRequest := httptest.NewRequest(http.MethodPost, second.resource, strings.NewReader(initializeBody))
	crossRequest.Header.Set("Content-Type", "application/json")
	crossRequest.Header.Set("Accept", "application/json, text/event-stream")
	crossRequest.Header.Set("Authorization", "Bearer "+firstToken)
	second.handler.ServeHTTP(crossInstance, crossRequest)
	if crossInstance.Code != http.StatusUnauthorized {
		t.Fatalf("token issued by first instance was accepted by second instance: %d %s", crossInstance.Code, crossInstance.Body.String())
	}
}

func TestOAuthStaticClientRejectsWrongOptionalSecret(t *testing.T) {
	const issuer = "http://127.0.0.1:18765"
	server := mustNewServer(t, Options{
		Workspace: t.TempDir(),
		OAuth: OAuthOptions{
			Enabled:       true,
			Issuer:        issuer,
			Resource:      issuer + "/mcp",
			OwnerPassword: "owner-password-long-enough",
			ClientID:      "mcp-devdesk",
			ClientSecret:  "correct-static-secret",
			TokenSecret:   strings.Repeat("ef", 32),
			DataDir:       t.TempDir(),
		},
	})
	request := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"mcp-devdesk"},
		"client_secret": {"wrong-static-secret"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), "client authentication failed") {
		t.Fatalf("wrong optional static secret = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthorizationContentSecurityPolicyUsesOnlyValidatedRedirectOrigin(t *testing.T) {
	policy := authorizationContentSecurityPolicy("https://chatgpt.com/connector/oauth/callback?state=abc")
	if !strings.Contains(policy, "form-action 'self' https://chatgpt.com") {
		t.Fatalf("validated HTTPS redirect origin missing from policy: %q", policy)
	}
	if strings.Contains(policy, "/connector/oauth/callback") || strings.Contains(policy, "state=abc") || strings.Contains(policy, "*") {
		t.Fatalf("policy contains redirect path/query or wildcard: %q", policy)
	}

	invalid := authorizationContentSecurityPolicy("javascript://example.invalid/callback")
	if invalid != "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'" {
		t.Fatalf("invalid redirect URI weakened policy: %q", invalid)
	}
}

func TestOAuthLegacyRootResourceAndDiscoveryCompatibility(t *testing.T) {
	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	const redirectURI = "http://127.0.0.1:43210/callback"
	server := mustNewServer(t, Options{
		Workspace: t.TempDir(),
		OAuth: OAuthOptions{
			Enabled:       true,
			Issuer:        issuer,
			Resource:      resource,
			OwnerPassword: "owner-password-long-enough",
			ClientID:      "mcp-devdesk",
			TokenSecret:   strings.Repeat("ac", 32),
			DataDir:       t.TempDir(),
		},
	})
	handler := server.Handler()

	rootMetadata := httptest.NewRecorder()
	handler.ServeHTTP(rootMetadata, httptest.NewRequest(http.MethodGet, issuer+"/.well-known/oauth-protected-resource", nil))
	if rootMetadata.Code != http.StatusOK || !strings.Contains(rootMetadata.Body.String(), `"resource":"`+issuer+`"`) {
		t.Fatalf("root metadata = %d %s", rootMetadata.Code, rootMetadata.Body.String())
	}
	pathMetadata := httptest.NewRecorder()
	handler.ServeHTTP(pathMetadata, httptest.NewRequest(http.MethodGet, issuer+"/.well-known/oauth-protected-resource/mcp", nil))
	if pathMetadata.Code != http.StatusOK || !strings.Contains(pathMetadata.Body.String(), `"resource":"`+resource+`"`) {
		t.Fatalf("path metadata = %d %s", pathMetadata.Code, pathMetadata.Body.String())
	}
	openid := httptest.NewRecorder()
	handler.ServeHTTP(openid, httptest.NewRequest(http.MethodGet, issuer+"/.well-known/openid-configuration", nil))
	if openid.Code != http.StatusOK {
		t.Fatalf("openid metadata status = %d", openid.Code)
	}

	registerBody := `{"client_name":"ChatGPT","redirect_uris":["http://127.0.0.1:43210/callback"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"scope":"mcp"}`
	registerRecorder := httptest.NewRecorder()
	registerRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/register", strings.NewReader(registerBody))
	registerRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(registerRecorder, registerRequest)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("registration with standard extra fields = %d %s", registerRecorder.Code, registerRecorder.Body.String())
	}

	verifier := strings.Repeat("c", 43)
	authorizeValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {"mcp-devdesk"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {issuer},
		"scope":                 {"mcp"},
		"owner_password":        {"owner-password-long-enough"},
	}
	authorizeRecorder := httptest.NewRecorder()
	authorizeRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/authorize", strings.NewReader(authorizeValues.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(authorizeRecorder, authorizeRequest)
	if authorizeRecorder.Code != http.StatusFound {
		t.Fatalf("legacy resource authorize = %d %s", authorizeRecorder.Code, authorizeRecorder.Body.String())
	}
	redirect, err := url.Parse(authorizeRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"mcp-devdesk"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {issuer},
	}
	tokenRecorder := httptest.NewRecorder()
	tokenRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(tokenValues.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(tokenRecorder, tokenRequest)
	if tokenRecorder.Code != http.StatusOK {
		t.Fatalf("legacy resource token = %d %s", tokenRecorder.Code, tokenRecorder.Body.String())
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, tokenRecorder.Body, &tokens)

	initializeBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"chatgpt","version":"1"}}}`
	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(initializeBody))
	authorizedRequest.Header.Set("Content-Type", "application/json")
	authorizedRequest.Header.Set("Accept", "application/json, text/event-stream")
	authorizedRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("legacy audience protected request = %d %s", authorized.Code, authorized.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	requestWithoutToken := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(initializeBody))
	requestWithoutToken.Header.Set("Content-Type", "application/json")
	requestWithoutToken.Header.Set("Accept", "application/json, text/event-stream")
	handler.ServeHTTP(unauthorized, requestWithoutToken)
	if !strings.Contains(unauthorized.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("resource metadata challenge = %q", unauthorized.Header().Get("WWW-Authenticate"))
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

func TestUnpinnedStaticClientAllowsValidatedHTTPSCallback(t *testing.T) {
	const issuer = "http://127.0.0.1:18765"
	const resource = issuer + "/mcp"
	const redirectURI = "https://chatgpt.com/connector/oauth/callback"
	server := mustNewServer(t, Options{
		Workspace: t.TempDir(),
		OAuth: OAuthOptions{
			Enabled:       true,
			Issuer:        issuer,
			Resource:      resource,
			OwnerPassword: "owner-password-long-enough",
			ClientID:      "mcp-devdesk",
			ClientSecret:  "static-client-secret-value",
			TokenSecret:   strings.Repeat("56", 32),
			DataDir:       t.TempDir(),
		},
	})
	handler := server.Handler()
	verifier := strings.Repeat("d", 43)
	authorizeValues := url.Values{
		"response_type":         {"code"},
		"client_id":             {"mcp-devdesk"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"resource":              {issuer},
		"scope":                 {"mcp"},
		"owner_password":        {"owner-password-long-enough"},
	}
	authorizeRecorder := httptest.NewRecorder()
	authorizeRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/authorize", strings.NewReader(authorizeValues.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(authorizeRecorder, authorizeRequest)
	if authorizeRecorder.Code != http.StatusFound {
		t.Fatalf("unpinned static authorize status = %d, body = %s", authorizeRecorder.Code, authorizeRecorder.Body.String())
	}
	redirect, err := url.Parse(authorizeRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	tokenValues := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"mcp-devdesk"},
		"client_secret": {"static-client-secret-value"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {issuer},
	}
	tokenRecorder := httptest.NewRecorder()
	tokenRequest := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(tokenValues.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(tokenRecorder, tokenRequest)
	if tokenRecorder.Code != http.StatusOK {
		t.Fatalf("unpinned static token status = %d, body = %s", tokenRecorder.Code, tokenRecorder.Body.String())
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
