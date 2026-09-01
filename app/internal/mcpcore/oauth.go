package mcpcore

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	secretstore "mcp-devdesk/internal/secrets"
)

const (
	defaultAccessTokenTTL  = time.Hour
	defaultRefreshTokenTTL = 30 * 24 * time.Hour
	defaultAuthCodeTTL     = 5 * time.Minute
	maxOAuthFormBytes      = 64 * 1024
	maxDynamicOAuthClients = 256
	maxOAuthAuthCodes      = 256
	maxOAuthRefreshTokens  = 1024
	maxOAuthStateFileBytes = 8 * 1024 * 1024
	maxOAuthRateEntries    = 2048
	oauthRegisterLimit     = 20
	oauthRegisterWindow    = 10 * time.Minute
	oauthAuthorizeLimit    = 20
	oauthAuthorizeWindow   = 5 * time.Minute
	oauthTokenLimit        = 120
	oauthTokenWindow       = time.Minute
)

type OAuthOptions struct {
	Enabled         bool
	Issuer          string
	Resource        string
	OwnerPassword   string
	ClientID        string
	ClientSecret    string
	RedirectURIs    []string
	TokenSecret     string
	DataDir         string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type oauthServer struct {
	issuer             string
	resource           string
	legacyResource     string
	ownerPassword      string
	staticClientID     string
	staticSecret       string
	staticRedirectURIs []string
	tokenSecret        []byte
	clientsPath        string
	refreshTokensPath  string
	accessTokenTTL     time.Duration
	refreshTokenTTL    time.Duration

	mu            sync.Mutex
	clients       map[string]oauthClient
	authCodes     map[string]authorizationCode
	refreshTokens map[string]refreshGrant
	rateLimiter   oauthRateLimiter
}

type oauthRateLimiter struct {
	mu      sync.Mutex
	entries map[string]oauthRateEntry
}

type oauthRateEntry struct {
	WindowStarted time.Time
	LastSeen      time.Time
	Count         int
}

type oauthClient struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	CreatedAt               int64    `json:"client_id_issued_at"`
}

type authorizationCode struct {
	ClientID      string
	RedirectURI   string
	Resource      string
	Scope         string
	CodeChallenge string
	ExpiresAt     time.Time
}

type refreshGrant struct {
	ClientID  string    `json:"clientId"`
	Resource  string    `json:"resource"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type tokenClaims struct {
	Issuer   string `json:"iss"`
	Subject  string `json:"sub"`
	Audience string `json:"aud"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	JTI      string `json:"jti"`
}

type dynamicRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type oauthClientsEnvelope struct {
	Version    int    `json:"version"`
	Protection string `json:"protection"`
	Data       string `json:"data"`
}

func newOAuthServer(options OAuthOptions) (*oauthServer, error) {
	if !options.Enabled {
		return nil, nil
	}
	issuer, err := canonicalBaseURL(options.Issuer)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth issuer: %w", err)
	}
	resource, err := canonicalResourceURL(options.Resource)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth resource: %w", err)
	}
	if len(options.OwnerPassword) < 12 {
		return nil, errors.New("OAuth owner password must contain at least 12 characters")
	}
	if strings.TrimSpace(options.ClientID) == "" {
		return nil, errors.New("OAuth client ID is required")
	}
	if len(options.RedirectURIs) > 20 {
		return nil, errors.New("OAuth static client supports at most 20 redirect URIs")
	}
	for _, redirectURI := range options.RedirectURIs {
		if err := validateRedirectURI(redirectURI); err != nil {
			return nil, fmt.Errorf("invalid OAuth static redirect URI: %w", err)
		}
	}
	secretBytes, err := hex.DecodeString(options.TokenSecret)
	if err != nil || len(secretBytes) < 32 {
		return nil, errors.New("OAuth token secret must be at least 32 bytes encoded as hexadecimal")
	}
	if options.AccessTokenTTL <= 0 {
		options.AccessTokenTTL = defaultAccessTokenTTL
	}
	if options.RefreshTokenTTL <= 0 {
		options.RefreshTokenTTL = defaultRefreshTokenTTL
	}
	server := &oauthServer{
		issuer:             issuer,
		resource:           resource,
		legacyResource:     issuer,
		ownerPassword:      options.OwnerPassword,
		staticClientID:     options.ClientID,
		staticSecret:       options.ClientSecret,
		staticRedirectURIs: append([]string(nil), options.RedirectURIs...),
		tokenSecret:        secretBytes,
		accessTokenTTL:     options.AccessTokenTTL,
		refreshTokenTTL:    options.RefreshTokenTTL,
		clients:            make(map[string]oauthClient),
		authCodes:          make(map[string]authorizationCode),
		refreshTokens:      make(map[string]refreshGrant),
		rateLimiter:        oauthRateLimiter{entries: make(map[string]oauthRateEntry)},
	}
	if strings.TrimSpace(options.DataDir) != "" {
		server.clientsPath = filepath.Join(options.DataDir, "oauth-clients.json")
		server.refreshTokensPath = filepath.Join(options.DataDir, "oauth-refresh-tokens.json")
		if err := server.loadClients(); err != nil {
			return nil, err
		}
		if err := server.loadRefreshTokens(); err != nil {
			return nil, err
		}
	}
	return server, nil
}

func (s *oauthServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.handleProtectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleAuthorizationServerMetadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.handleAuthorizationServerMetadata)
	mux.HandleFunc("POST /oauth/register", s.handleDynamicRegistration)
	mux.HandleFunc("GET /oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /oauth/token", s.handleToken)
}

func (s *oauthServer) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			s.writeUnauthorized(w, "invalid_token", "Bearer access token is required")
			return
		}
		claims, err := s.verifyAccessToken(strings.TrimSpace(header[len("Bearer "):]))
		if err != nil {
			s.writeUnauthorized(w, "invalid_token", err.Error())
			return
		}
		if !s.resourceAllowed(claims.Audience) {
			s.writeUnauthorized(w, "invalid_token", "token audience does not match this MCP resource")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *oauthServer) writeUnauthorized(w http.ResponseWriter, code, description string) {
	metadata := s.issuer + "/.well-known/oauth-protected-resource/mcp"
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q, error=%q, error_description=%q`, metadata, code, description))
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": code, "error_description": description})
}

func (s *oauthServer) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	resource := s.resource
	if r.URL.Path == "/.well-known/oauth-protected-resource" {
		resource = s.legacyResource
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{s.issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"mcp"},
	})
}

func (s *oauthServer) handleAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/oauth/authorize",
		"token_endpoint":                        s.issuer + "/oauth/token",
		"registration_endpoint":                 s.issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"mcp", "offline_access"},
	})
}

func (s *oauthServer) handleDynamicRegistration(w http.ResponseWriter, r *http.Request) {
	setOAuthNoStore(w)
	if !s.allowOAuthRequest(r, "register", oauthRegisterLimit, oauthRegisterWindow) {
		w.Header().Set("Retry-After", strconv.Itoa(int(oauthRegisterWindow.Seconds())))
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down", "too many dynamic client registration requests")
		return
	}
	var request dynamicRegistrationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOAuthFormBytes))
	if err := decoder.Decode(&request); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body must contain one JSON object")
		return
	}
	if len(request.RedirectURIs) == 0 || len(request.RedirectURIs) > 16 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "at least one redirect URI is required")
		return
	}
	for _, redirectURI := range request.RedirectURIs {
		if err := validateRedirectURI(redirectURI); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	method := strings.TrimSpace(request.TokenEndpointAuthMethod)
	if method == "" {
		method = "none"
	}
	if method != "none" && method != "client_secret_post" && method != "client_secret_basic" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported token endpoint authentication method")
		return
	}
	clientID, err := randomURLToken(24)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate client ID")
		return
	}
	client := oauthClient{
		ClientID:                "mcp-" + clientID,
		ClientName:              strings.TrimSpace(request.ClientName),
		RedirectURIs:            uniqueStrings(request.RedirectURIs),
		TokenEndpointAuthMethod: method,
		CreatedAt:               time.Now().Unix(),
	}
	if method != "none" {
		client.ClientSecret, err = randomURLToken(32)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate client secret")
			return
		}
	}
	s.mu.Lock()
	s.cleanupLocked(time.Now())
	if len(s.clients) >= maxDynamicOAuthClients {
		if !s.evictUnusedClientLocked() {
			s.mu.Unlock()
			writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "dynamic client registration limit reached")
			return
		}
	}
	s.clients[client.ClientID] = client
	err = s.saveClientsLocked()
	s.mu.Unlock()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	response := map[string]any{
		"client_id":                  client.ClientID,
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"client_id_issued_at":        client.CreatedAt,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if client.ClientSecret != "" {
		response["client_secret"] = client.ClientSecret
		response["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *oauthServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	setOAuthNoStore(w)
	if r.Method == http.MethodPost && !s.allowOAuthRequest(r, "authorize", oauthAuthorizeLimit, oauthAuthorizeWindow) {
		w.Header().Set("Retry-After", strconv.Itoa(int(oauthAuthorizeWindow.Seconds())))
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down", "too many authorization attempts")
		return
	}
	if err := parseOAuthForm(w, r); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unable to parse authorization request")
		return
	}
	request, err := s.validateAuthorizationRequest(r.Form)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", authorizationContentSecurityPolicy(request.redirectURI))
		_ = authorizationPage.Execute(w, map[string]string{
			"ClientName":          request.clientName,
			"ClientID":            request.clientID,
			"RedirectURI":         request.redirectURI,
			"State":               request.state,
			"CodeChallenge":       request.codeChallenge,
			"CodeChallengeMethod": "S256",
			"Resource":            request.resource,
			"Scope":               request.scope,
		})
		return
	}
	password := r.Form.Get("owner_password")
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.ownerPassword)) != 1 {
		writeOAuthError(w, http.StatusUnauthorized, "access_denied", "owner password is incorrect")
		return
	}
	code, err := randomURLToken(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate authorization code")
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.cleanupLocked(now)
	if len(s.authCodes) >= maxOAuthAuthCodes {
		s.mu.Unlock()
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "too many pending authorization requests")
		return
	}
	s.authCodes[code] = authorizationCode{
		ClientID:      request.clientID,
		RedirectURI:   request.redirectURI,
		Resource:      request.resource,
		Scope:         request.scope,
		CodeChallenge: request.codeChallenge,
		ExpiresAt:     now.Add(defaultAuthCodeTTL),
	}
	s.mu.Unlock()
	redirect, _ := url.Parse(request.redirectURI)
	query := redirect.Query()
	query.Set("code", code)
	if request.state != "" {
		query.Set("state", request.state)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func authorizationContentSecurityPolicy(redirectURI string) string {
	policy := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'"
	if validateRedirectURI(redirectURI) == nil {
		if origin := normalizeOrigin(redirectURI); origin != "" {
			policy += " " + origin
		}
	}
	return policy + "; frame-ancestors 'none'; base-uri 'none'"
}

type validatedAuthorizationRequest struct {
	clientID      string
	clientName    string
	redirectURI   string
	state         string
	codeChallenge string
	resource      string
	scope         string
}

func (s *oauthServer) validateAuthorizationRequest(values url.Values) (validatedAuthorizationRequest, error) {
	if values.Get("response_type") != "code" {
		return validatedAuthorizationRequest{}, errors.New("response_type must be code")
	}
	clientID := strings.TrimSpace(values.Get("client_id"))
	client, ok := s.lookupClient(clientID)
	if !ok {
		return validatedAuthorizationRequest{}, errors.New("unknown client_id")
	}
	redirectURI := strings.TrimSpace(values.Get("redirect_uri"))
	if redirectURI == "" {
		return validatedAuthorizationRequest{}, errors.New("redirect_uri is required")
	}
	// Built-in desktop client compatibility: MCP clients may use a generated
	// callback endpoint. Do not permanently bind the built-in client to an old
	// callback saved by an earlier version, but keep URI validation enabled.
	if !s.redirectURIAllowed(client, redirectURI) {
		return validatedAuthorizationRequest{}, errors.New("redirect_uri is not registered for this client")
	}
	challenge := strings.TrimSpace(values.Get("code_challenge"))
	if values.Get("code_challenge_method") != "S256" || len(challenge) < 43 || len(challenge) > 128 {
		return validatedAuthorizationRequest{}, errors.New("PKCE S256 code challenge is required")
	}
	resource, err := canonicalResourceURL(values.Get("resource"))
	if err != nil || !s.resourceAllowed(resource) {
		return validatedAuthorizationRequest{}, errors.New("resource must identify this MCP server")
	}
	scope := normalizeScope(values.Get("scope"))
	if !validOAuthScope(scope) {
		return validatedAuthorizationRequest{}, errors.New("unsupported scope")
	}
	name := client.ClientName
	if name == "" {
		name = client.ClientID
	}
	return validatedAuthorizationRequest{
		clientID: clientID, clientName: name, redirectURI: redirectURI,
		state: values.Get("state"), codeChallenge: challenge, resource: resource, scope: scope,
	}, nil
}

func (s *oauthServer) redirectURIAllowed(client oauthClient, redirectURI string) bool {
	if containsExact(client.RedirectURIs, redirectURI) {
		return true
	}
	if client.ClientID != s.staticClientID || validateRedirectURI(redirectURI) != nil {
		return false
	}
	// The built-in MCP client is intentionally not pinned when no callbacks
	// have been configured. ChatGPT also allocates a different callback path
	// for each custom app. If the user explicitly registered one ChatGPT
	// connector callback, keep the origin/path family pinned to ChatGPT while
	// allowing another generated connector callback for a second MCP instance.
	if len(s.staticRedirectURIs) == 0 {
		return true
	}
	if !isChatGPTConnectorRedirect(redirectURI) {
		return false
	}
	for _, registered := range s.staticRedirectURIs {
		if isChatGPTConnectorRedirect(registered) {
			return true
		}
	}
	return false
}

func isChatGPTConnectorRedirect(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "chatgpt.com") || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/connector/oauth/") && len(strings.TrimPrefix(parsed.Path, "/connector/oauth/")) > 0
}

func (s *oauthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	setOAuthNoStore(w)
	if !s.allowOAuthRequest(r, "token", oauthTokenLimit, oauthTokenWindow) {
		w.Header().Set("Retry-After", strconv.Itoa(int(oauthTokenWindow.Seconds())))
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down", "too many token requests")
		return
	}
	if err := parseOAuthForm(w, r); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unable to parse token request")
		return
	}
	client, err := s.authenticateClient(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="MCP DevDesk OAuth"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r, client)
	case "refresh_token":
		s.exchangeRefreshToken(w, r, client)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "supported grants are authorization_code and refresh_token")
	}
}

func (s *oauthServer) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request, client oauthClient) {
	codeValue := strings.TrimSpace(r.Form.Get("code"))
	s.mu.Lock()
	grant, ok := s.authCodes[codeValue]
	delete(s.authCodes, codeValue)
	s.cleanupLocked(time.Now())
	s.mu.Unlock()
	if !ok || time.Now().After(grant.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	if grant.ClientID != client.ClientID || grant.RedirectURI != r.Form.Get("redirect_uri") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code binding mismatch")
		return
	}
	resource, err := canonicalResourceURL(r.Form.Get("resource"))
	if err != nil || resource != grant.Resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the authorization request")
		return
	}
	verifier := r.Form.Get("code_verifier")
	if !validPKCEVerifier(verifier) || pkceChallenge(verifier) != grant.CodeChallenge {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	s.issueTokens(w, client.ClientID, grant.Resource, grant.Scope)
}

func (s *oauthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request, client oauthClient) {
	token := strings.TrimSpace(r.Form.Get("refresh_token"))
	tokenKey := refreshTokenKey(token)
	resource, err := canonicalResourceURL(r.Form.Get("resource"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the refresh token")
		return
	}
	s.mu.Lock()
	s.cleanupLocked(time.Now())
	grant, ok := s.refreshTokens[tokenKey]
	if !ok || time.Now().After(grant.ExpiresAt) || grant.ClientID != client.ClientID {
		s.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	if resource != grant.Resource {
		s.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the refresh token")
		return
	}
	delete(s.refreshTokens, tokenKey)
	persistErr := s.saveRefreshTokensLocked()
	s.mu.Unlock()
	if persistErr != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to persist refresh token rotation")
		return
	}
	s.issueTokens(w, client.ClientID, grant.Resource, grant.Scope)
}

func (s *oauthServer) issueTokens(w http.ResponseWriter, clientID, resource, scope string) {
	accessToken, err := s.signAccessToken(clientID, resource, scope)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}
	refreshToken, err := randomURLToken(48)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue refresh token")
		return
	}
	s.mu.Lock()
	s.cleanupLocked(time.Now())
	if len(s.refreshTokens) >= maxOAuthRefreshTokens {
		s.mu.Unlock()
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token limit reached")
		return
	}
	refreshKey := refreshTokenKey(refreshToken)
	s.refreshTokens[refreshKey] = refreshGrant{
		ClientID: clientID, Resource: resource, Scope: scope,
		ExpiresAt: time.Now().Add(s.refreshTokenTTL),
	}
	persistErr := s.saveRefreshTokensLocked()
	if persistErr != nil {
		delete(s.refreshTokens, refreshKey)
	}
	s.mu.Unlock()
	if persistErr != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to persist refresh token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int64(s.accessTokenTTL.Seconds()),
		"refresh_token": refreshToken,
		"scope":         scope,
	})
}

func (s *oauthServer) authenticateClient(r *http.Request) (oauthClient, error) {
	clientID := r.Form.Get("client_id")
	clientSecret := r.Form.Get("client_secret")
	if basicID, basicSecret, ok := r.BasicAuth(); ok {
		clientID, clientSecret = basicID, basicSecret
	}
	client, ok := s.lookupClient(clientID)
	if !ok {
		return oauthClient{}, errors.New("unknown client")
	}
	if clientID == s.staticClientID {
		// PKCE is mandatory for authorization-code grants, so the built-in
		// client can safely operate as a public client. Keep the configured
		// secret as an optional compatibility check: clients that send it must
		// send the correct value, while clients such as ChatGPT may omit it.
		if clientSecret != "" {
			if s.staticSecret == "" || subtle.ConstantTimeCompare([]byte(s.staticSecret), []byte(clientSecret)) != 1 {
				return oauthClient{}, errors.New("client authentication failed")
			}
		}
		return client, nil
	}
	if client.TokenEndpointAuthMethod == "none" {
		if clientSecret != "" {
			return oauthClient{}, errors.New("public client must not send a client secret")
		}
		return client, nil
	}
	if subtle.ConstantTimeCompare([]byte(client.ClientSecret), []byte(clientSecret)) != 1 {
		return oauthClient{}, errors.New("client authentication failed")
	}
	return client, nil
}

func (s *oauthServer) lookupClient(clientID string) (oauthClient, bool) {
	if clientID == s.staticClientID {
		method := "none"
		if s.staticSecret != "" {
			method = "client_secret_post"
		}
		return oauthClient{
			ClientID: clientID, ClientSecret: s.staticSecret, ClientName: "MCP DevDesk",
			RedirectURIs: append([]string(nil), s.staticRedirectURIs...), TokenEndpointAuthMethod: method,
		}, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.clients[clientID]
	return client, ok
}

func (s *oauthServer) signAccessToken(clientID, resource, scope string) (string, error) {
	jti, err := randomURLToken(18)
	if err != nil {
		return "", err
	}
	now := time.Now()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := tokenClaims{
		Issuer: s.issuer, Subject: "owner", Audience: resource, ClientID: clientID,
		Scope: scope, IssuedAt: now.Unix(), Expires: now.Add(s.accessTokenTTL).Unix(), JTI: jti,
	}
	headerRaw, _ := json.Marshal(header)
	claimsRaw, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(claimsRaw)
	mac := hmac.New(sha256.New, s.tokenSecret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *oauthServer) verifyAccessToken(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("malformed access token")
	}
	mac := hmac.New(sha256.New, s.tokenSecret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return tokenClaims{}, errors.New("access token signature is invalid")
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, errors.New("access token payload is invalid")
	}
	var claims tokenClaims
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		return tokenClaims{}, errors.New("access token claims are invalid")
	}
	now := time.Now().Unix()
	if claims.Issuer != s.issuer || claims.Expires <= now || claims.IssuedAt > now+60 || claims.ClientID == "" || claims.JTI == "" {
		return tokenClaims{}, errors.New("access token is expired or has invalid claims")
	}
	if !containsScope(claims.Scope, "mcp") {
		return tokenClaims{}, errors.New("access token does not grant the mcp scope")
	}
	return claims, nil
}

func (s *oauthServer) cleanupLocked(now time.Time) {
	for code, grant := range s.authCodes {
		if now.After(grant.ExpiresAt) {
			delete(s.authCodes, code)
		}
	}
	for token, grant := range s.refreshTokens {
		if now.After(grant.ExpiresAt) {
			delete(s.refreshTokens, token)
		}
	}
}

func (s *oauthServer) evictUnusedClientLocked() bool {
	inUse := make(map[string]struct{}, len(s.authCodes)+len(s.refreshTokens))
	for _, grant := range s.authCodes {
		inUse[grant.ClientID] = struct{}{}
	}
	for _, grant := range s.refreshTokens {
		inUse[grant.ClientID] = struct{}{}
	}
	oldestID := ""
	var oldestCreated int64
	for id, client := range s.clients {
		if _, protected := inUse[id]; protected {
			continue
		}
		if oldestID == "" || client.CreatedAt < oldestCreated {
			oldestID = id
			oldestCreated = client.CreatedAt
		}
	}
	if oldestID == "" {
		return false
	}
	delete(s.clients, oldestID)
	return true
}

func refreshTokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *oauthServer) loadRefreshTokens() error {
	if s.refreshTokensPath == "" {
		return nil
	}
	raw, err := readOAuthStateFile(s.refreshTokensPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OAuth refresh tokens: %w", err)
	}

	var tokens map[string]refreshGrant
	var envelope oauthClientsEnvelope
	migrated := false
	if json.Unmarshal(raw, &envelope) == nil && envelope.Version == 2 && envelope.Data != "" {
		ciphertext, decodeErr := base64.StdEncoding.DecodeString(envelope.Data)
		if decodeErr != nil {
			return fmt.Errorf("decode OAuth refresh token data: %w", decodeErr)
		}
		plain, unprotectErr := secretstore.UnprotectForCurrentUser(ciphertext)
		if unprotectErr != nil {
			return fmt.Errorf("decrypt OAuth refresh tokens: %w", unprotectErr)
		}
		if err := json.Unmarshal(plain, &tokens); err != nil {
			return fmt.Errorf("parse decrypted OAuth refresh tokens: %w", err)
		}
	} else {
		if err := json.Unmarshal(raw, &tokens); err != nil {
			return fmt.Errorf("parse OAuth refresh tokens: %w", err)
		}
		migrated = true
	}
	if tokens == nil {
		tokens = make(map[string]refreshGrant)
	}
	now := time.Now()
	for key, grant := range tokens {
		if key == "" || grant.ClientID == "" || grant.Resource == "" || now.After(grant.ExpiresAt) {
			delete(tokens, key)
			migrated = true
		}
	}
	if len(tokens) > maxOAuthRefreshTokens {
		type tokenRecord struct {
			key       string
			expiresAt time.Time
		}
		records := make([]tokenRecord, 0, len(tokens))
		for key, grant := range tokens {
			records = append(records, tokenRecord{key: key, expiresAt: grant.ExpiresAt})
		}
		sort.Slice(records, func(left, right int) bool {
			return records[left].expiresAt.After(records[right].expiresAt)
		})
		for _, record := range records[maxOAuthRefreshTokens:] {
			delete(tokens, record.key)
		}
		migrated = true
	}
	s.refreshTokens = tokens
	if migrated {
		return s.saveRefreshTokensLocked()
	}
	return nil
}

func (s *oauthServer) saveRefreshTokensLocked() error {
	if s.refreshTokensPath == "" {
		return nil
	}
	plain, err := json.Marshal(s.refreshTokens)
	if err != nil {
		return err
	}
	ciphertext, err := secretstore.ProtectForCurrentUser(plain)
	if err != nil {
		return fmt.Errorf("encrypt OAuth refresh tokens: %w", err)
	}
	envelope := oauthClientsEnvelope{
		Version:    2,
		Protection: secretstore.ProtectionName(),
		Data:       base64.StdEncoding.EncodeToString(ciphertext),
	}
	raw, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.refreshTokensPath), 0o700); err != nil {
		return err
	}
	tmp := s.refreshTokensPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.refreshTokensPath)
}

func (s *oauthServer) loadClients() error {
	raw, err := readOAuthStateFile(s.clientsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OAuth clients: %w", err)
	}
	var clients []oauthClient
	var envelope oauthClientsEnvelope
	if json.Unmarshal(raw, &envelope) == nil && envelope.Version == 2 && envelope.Data != "" {
		ciphertext, decodeErr := base64.StdEncoding.DecodeString(envelope.Data)
		if decodeErr != nil {
			return fmt.Errorf("decode OAuth client data: %w", decodeErr)
		}
		plain, unprotectErr := secretstore.UnprotectForCurrentUser(ciphertext)
		if unprotectErr != nil {
			return fmt.Errorf("decrypt OAuth clients: %w", unprotectErr)
		}
		if err := json.Unmarshal(plain, &clients); err != nil {
			return fmt.Errorf("parse decrypted OAuth clients: %w", err)
		}
	} else if err := json.Unmarshal(raw, &clients); err != nil {
		return fmt.Errorf("parse OAuth clients: %w", err)
	}
	migrated := envelope.Version != 2
	if len(clients) > maxDynamicOAuthClients {
		sort.Slice(clients, func(left, right int) bool {
			return clients[left].CreatedAt > clients[right].CreatedAt
		})
		clients = clients[:maxDynamicOAuthClients]
		migrated = true
	}
	for _, client := range clients {
		if client.ClientID != "" && len(client.RedirectURIs) > 0 {
			s.clients[client.ClientID] = client
		}
	}
	if migrated {
		return s.saveClientsLocked()
	}
	return nil
}

func (s *oauthServer) saveClientsLocked() error {
	if s.clientsPath == "" {
		return nil
	}
	clients := make([]oauthClient, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	plain, err := json.Marshal(clients)
	if err != nil {
		return err
	}
	ciphertext, err := secretstore.ProtectForCurrentUser(plain)
	if err != nil {
		return fmt.Errorf("encrypt OAuth clients: %w", err)
	}
	envelope := oauthClientsEnvelope{
		Version:    2,
		Protection: secretstore.ProtectionName(),
		Data:       base64.StdEncoding.EncodeToString(ciphertext),
	}
	raw, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.clientsPath), 0o700); err != nil {
		return err
	}
	tmp := s.clientsPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.clientsPath)
}

func (s *oauthServer) resourceAllowed(value string) bool {
	resource, err := canonicalResourceURL(value)
	if err != nil {
		return false
	}
	return resource == s.resource || resource == s.legacyResource
}

func canonicalBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment != "" {
		return "", errors.New("absolute URL is required")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errors.New("HTTPS is required except for loopback hosts")
	}
	parsed.RawQuery = ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}

func canonicalResourceURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", errors.New("absolute resource URL without query or fragment is required")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errors.New("HTTPS is required except for loopback resources")
	}
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	return parsed.String(), nil
}

func validateRedirectURI(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Fragment != "" {
		return errors.New("redirect URI must be an absolute URL without a fragment")
	}
	if parsed.User != nil {
		return errors.New("redirect URI must not contain user information")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("redirect URI must use HTTPS or a loopback HTTP address")
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func validPKCEVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'A' && char <= 'Z') && !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && !strings.ContainsRune("-._~", char) {
			return false
		}
	}
	return true
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func randomURLToken(bytesCount int) (string, error) {
	buffer := make([]byte, bytesCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func normalizeScope(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "mcp"
	}
	return strings.Join(uniqueStrings(fields), " ")
}

func validOAuthScope(value string) bool {
	if !containsScope(value, "mcp") {
		return false
	}
	for _, scope := range strings.Fields(value) {
		if scope != "mcp" && scope != "offline_access" {
			return false
		}
	}
	return true
}

func containsScope(value, expected string) bool {
	for _, scope := range strings.Fields(value) {
		if scope == expected {
			return true
		}
	}
	return false
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func parseOAuthForm(w http.ResponseWriter, r *http.Request) error {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxOAuthFormBytes)
	}
	return r.ParseForm()
}

func (s *oauthServer) allowOAuthRequest(r *http.Request, operation string, limit int, window time.Duration) bool {
	return s.rateLimiter.allow(operation+":"+oauthRequestClient(r), limit, window, time.Now())
}

func oauthRequestClient(r *http.Request) string {
	if forwarded := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); forwarded != nil {
		return forwarded.String()
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
		return strings.ToLower(host)
	}
	if ip := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); ip != nil {
		return ip.String()
	}
	return "unknown"
}

func (l *oauthRateLimiter) allow(key string, limit int, window time.Duration, now time.Time) bool {
	if limit <= 0 || window <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]oauthRateEntry)
	}
	entry, exists := l.entries[key]
	if !exists || now.Sub(entry.WindowStarted) >= window {
		if !exists && len(l.entries) >= maxOAuthRateEntries {
			oldestKey := ""
			var oldest time.Time
			for candidate, current := range l.entries {
				if oldestKey == "" || current.LastSeen.Before(oldest) {
					oldestKey = candidate
					oldest = current.LastSeen
				}
			}
			if oldestKey != "" {
				delete(l.entries, oldestKey)
			}
		}
		l.entries[key] = oauthRateEntry{WindowStarted: now, LastSeen: now, Count: 1}
		return true
	}
	entry.LastSeen = now
	if entry.Count >= limit {
		l.entries[key] = entry
		return false
	}
	entry.Count++
	l.entries[key] = entry
	return true
}

func readOAuthStateFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxOAuthStateFileBytes {
		return nil, fmt.Errorf("OAuth state file exceeds %d bytes", maxOAuthStateFileBytes)
	}
	return os.ReadFile(path)
}

func setOAuthNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	setOAuthNoStore(w)
	writeJSON(w, status, map[string]any{"error": code, "error_description": description})
}

var authorizationPage = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>MCP DevDesk 授权</title><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f4f5f7;color:#171719;font:14px system-ui,-apple-system,"Segoe UI",sans-serif}
main{width:min(430px,calc(100vw - 40px));padding:30px;border:1px solid #dddfe4;border-radius:22px;background:#fff;box-shadow:0 20px 70px rgba(0,0,0,.12)}
h1{margin:0 0 8px;font-size:24px}p{color:#666;line-height:1.6}code{word-break:break-all;font-size:12px}label{display:grid;gap:8px;margin-top:22px;font-weight:600}
input{height:44px;padding:0 13px;border:1px solid #cfd2d8;border-radius:12px;font:inherit}button{width:100%;height:46px;margin-top:18px;border:0;border-radius:12px;background:#111;color:#fff;font-weight:650;cursor:pointer}
</style></head><body><main><h1>授权 MCP 连接</h1><p><strong>{{.ClientName}}</strong> 请求访问 MCP DevDesk。请输入设置页中的所有者密码。</p>
<p>资源：<code>{{.Resource}}</code></p><form method="post" action="/oauth/authorize">
<input type="hidden" name="response_type" value="code"><input type="hidden" name="client_id" value="{{.ClientID}}"><input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="code_challenge" value="{{.CodeChallenge}}"><input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<input type="hidden" name="resource" value="{{.Resource}}"><input type="hidden" name="scope" value="{{.Scope}}"><label>所有者密码<input type="password" name="owner_password" required autocomplete="current-password"></label><button type="submit">允许连接</button></form></main></body></html>`))

func parseTokenTTL(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
