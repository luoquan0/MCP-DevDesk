from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


oauth_path = Path("app/internal/mcpcore/oauth.go")
oauth = oauth_path.read_text(encoding="utf-8")

oauth = replace_once(
    oauth,
    '''\t\t"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
\t\t"scopes_supported":                      []string{"mcp"},
''',
    '''\t\t"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
\t\t"scopes_supported":                      []string{"mcp", "offline_access"},
''',
    "authorization metadata scopes",
)

oauth = replace_once(
    oauth,
    '''\tif !containsExact(client.RedirectURIs, redirectURI) {
\t\tif client.ClientID == s.staticClientID && len(s.staticRedirectURIs) == 0 {
\t\t\t// The built-in desktop client is intentionally not pinned to one
\t\t\t// callback. MCP desktop clients commonly allocate a local callback
\t\t\t// port during startup. Security comes from HTTPS/loopback validation.
\t\t\tif err := validateRedirectURI(redirectURI); err != nil {
\t\t\t\treturn validatedAuthorizationRequest{}, errors.New("redirect_uri is not registered for this client")
\t\t\t}
\t\t} else {
\t\t\treturn validatedAuthorizationRequest{}, errors.New("redirect_uri is not registered for this client")
\t\t}
\t}
''',
    '''\tif !s.redirectURIAllowed(client, redirectURI) {
\t\treturn validatedAuthorizationRequest{}, errors.New("redirect_uri is not registered for this client")
\t}
''',
    "authorization redirect validation",
)

oauth = replace_once(
    oauth,
    '''\tscope := normalizeScope(values.Get("scope"))
\tif scope != "mcp" {
\t\treturn validatedAuthorizationRequest{}, errors.New("unsupported scope")
\t}
''',
    '''\tscope := normalizeScope(values.Get("scope"))
\tif !validOAuthScope(scope) {
\t\treturn validatedAuthorizationRequest{}, errors.New("unsupported scope")
\t}
''',
    "authorization scope validation",
)

redirect_helpers = r'''
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

'''
oauth = replace_once(
    oauth,
    '''func (s *oauthServer) handleToken(w http.ResponseWriter, r *http.Request) {
''',
    redirect_helpers + '''func (s *oauthServer) handleToken(w http.ResponseWriter, r *http.Request) {
''',
    "redirect helper insertion",
)

oauth = replace_once(
    oauth,
    '''\tclient, ok := s.lookupClient(clientID)
\tif !ok {
\t\treturn oauthClient{}, errors.New("unknown client")
\t}
\tif client.TokenEndpointAuthMethod == "none" {
''',
    '''\tclient, ok := s.lookupClient(clientID)
\tif !ok {
\t\treturn oauthClient{}, errors.New("unknown client")
\t}
\tif clientID == s.staticClientID {
\t\t// PKCE is mandatory for authorization-code grants, so the built-in
\t\t// client can safely operate as a public client. Keep the configured
\t\t// secret as an optional compatibility check: clients that send it must
\t\t// send the correct value, while clients such as ChatGPT may omit it.
\t\tif clientSecret != "" {
\t\t\tif s.staticSecret == "" || subtle.ConstantTimeCompare([]byte(s.staticSecret), []byte(clientSecret)) != 1 {
\t\t\t\treturn oauthClient{}, errors.New("client authentication failed")
\t\t\t}
\t\t}
\t\treturn client, nil
\t}
\tif client.TokenEndpointAuthMethod == "none" {
''',
    "static client token authentication",
)

oauth = replace_once(
    oauth,
    '''func containsScope(value, expected string) bool {
''',
    '''func validOAuthScope(value string) bool {
\tif !containsScope(value, "mcp") {
\t\treturn false
\t}
\tfor _, scope := range strings.Fields(value) {
\t\tif scope != "mcp" && scope != "offline_access" {
\t\t\treturn false
\t\t}
\t}
\treturn true
}

func containsScope(value, expected string) bool {
''',
    "scope helper insertion",
)

oauth_path.write_text(oauth, encoding="utf-8")


test_path = Path("app/internal/mcpcore/oauth_test.go")
tests = test_path.read_text(encoding="utf-8")
new_tests = r'''
func TestOAuthStaticClientSupportsMultipleChatGPTInstances(t *testing.T) {
	const (
		ownerPassword = "owner-password-long-enough"
		clientID = "mcp-devdesk"
		clientSecret = "static-client-secret-value"
		firstCallback = "https://chatgpt.com/connector/oauth/first-app"
		secondCallback = "https://chatgpt.com/connector/oauth/second-app"
	)
	tokenSecret := strings.Repeat("de", 32)

	type instance struct {
		issuer string
		resource string
		handler http.Handler
	}
	newInstance := func(issuer string) instance {
		resource := issuer + "/mcp"
		server := mustNewServer(t, Options{
			Workspace: t.TempDir(),
			OAuth: OAuthOptions{
				Enabled: true,
				Issuer: issuer,
				Resource: resource,
				OwnerPassword: ownerPassword,
				ClientID: clientID,
				ClientSecret: clientSecret,
				RedirectURIs: []string{firstCallback},
				TokenSecret: tokenSecret,
				DataDir: t.TempDir(),
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
			"response_type": {"code"},
			"client_id": {clientID},
			"redirect_uri": {callback},
			"code_challenge": {pkceChallenge(verifier)},
			"code_challenge_method": {"S256"},
			"resource": {target.resource},
			"scope": {"mcp offline_access"},
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
			"grant_type": {"authorization_code"},
			"client_id": {clientID},
			"code": {code},
			"redirect_uri": {callback},
			"code_verifier": {verifier},
			"resource": {target.resource},
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
			AccessToken string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Scope string `json:"scope"`
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
			Enabled: true,
			Issuer: issuer,
			Resource: issuer + "/mcp",
			OwnerPassword: "owner-password-long-enough",
			ClientID: "mcp-devdesk",
			ClientSecret: "correct-static-secret",
			TokenSecret: strings.Repeat("ef", 32),
			DataDir: t.TempDir(),
		},
	})
	request := httptest.NewRequest(http.MethodPost, issuer+"/oauth/token", strings.NewReader(url.Values{
		"grant_type": {"authorization_code"},
		"client_id": {"mcp-devdesk"},
		"client_secret": {"wrong-static-secret"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), "client authentication failed") {
		t.Fatalf("wrong optional static secret = %d %s", recorder.Code, recorder.Body.String())
	}
}

'''
tests = replace_once(
    tests,
    'func TestAuthorizationContentSecurityPolicyUsesOnlyValidatedRedirectOrigin(t *testing.T) {\n',
    new_tests + 'func TestAuthorizationContentSecurityPolicyUsesOnlyValidatedRedirectOrigin(t *testing.T) {\n',
    "multi-instance OAuth tests insertion",
)
test_path.write_text(tests, encoding="utf-8")


security_path = Path("docs/SECURITY.md")
security = security_path.read_text(encoding="utf-8")
security = replace_once(
    security,
    '''- 授权码流程必须使用 PKCE S256。\n- OAuth Token 绑定到精确的 MCP `resource` 受众。\n''',
    '''- 授权码流程必须使用 PKCE S256。\n- 内置静态客户端允许基于 PKCE 的 public-client Token 交换；客户端如果主动提交 Client Secret，则仍必须与本机保存值一致。\n- ChatGPT 为不同自定义 MCP 应用生成不同 `/connector/oauth/<id>` 回调路径；当用户已经登记过一个 ChatGPT 回调时，仅在同一 `https://chatgpt.com/connector/oauth/` 家族内允许新的生成式回调，其他已配置回调仍保持精确匹配。\n- 授权服务器声明并接受 `offline_access`，继续为远程 MCP 会话签发可轮换 Refresh Token。\n- OAuth Token 绑定到精确的 MCP `resource` 受众。\n''',
    "security OAuth documentation",
)
security_path.write_text(security, encoding="utf-8")


readme_path = Path("README.md")
readme = readme_path.read_text(encoding="utf-8")
needle = "- 每个实例可配置独立 Cloudflare Tunnel 名称与域名，并自动检查端口、域名和 Tunnel 名称冲突\n"
addition = needle + "- 多实例 OAuth 兼容 ChatGPT 为不同自定义应用生成的独立回调路径；PKCE 静态客户端可按 public-client 方式换取 Token，同时保持 Client Secret 可选校验和跨实例 issuer/resource 隔离\n"
if addition not in readme:
    readme = replace_once(readme, needle, addition, "README multi-instance OAuth note")
readme_path.write_text(readme, encoding="utf-8")
