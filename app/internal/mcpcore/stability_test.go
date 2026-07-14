package mcpcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPSessionCountIsBounded(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	var firstSession string
	for index := 0; index < maxMCPSessions+12; index++ {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
			"jsonrpc":"2.0","id":1,"method":"initialize",
			"params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"stress","version":"1"}}
		}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("initialize %d status = %d: %s", index, recorder.Code, recorder.Body.String())
		}
		if index == 0 {
			firstSession = recorder.Header().Get(SessionHeader)
			server.mu.Lock()
			server.sessions[firstSession].LastSeen = time.Now().Add(-time.Hour)
			server.mu.Unlock()
		}
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	if got := len(server.sessions); got != maxMCPSessions {
		t.Fatalf("session count = %d, want %d", got, maxMCPSessions)
	}
	if _, exists := server.sessions[firstSession]; exists {
		t.Fatal("oldest session was not evicted")
	}
}

func TestExpiredMCPSessionsAreRemoved(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	server.sessions["expired"] = &session{
		CreatedAt: time.Now().Add(-mcpSessionTTL - time.Minute),
		LastSeen:  time.Now().Add(-mcpSessionTTL - time.Minute),
	}
	server.sessions["active"] = &session{CreatedAt: time.Now(), LastSeen: time.Now()}
	server.mu.Lock()
	server.cleanupSessionsLocked(time.Now())
	server.mu.Unlock()
	if _, exists := server.sessions["expired"]; exists {
		t.Fatal("expired session remains")
	}
	if _, exists := server.sessions["active"]; !exists {
		t.Fatal("active session was removed")
	}
}

func TestSSEReplayBufferIsBoundedByCountAndBytes(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	server.sessions["session"] = &session{CreatedAt: time.Now(), LastSeen: time.Now()}
	chunk := bytes.Repeat([]byte("x"), 64*1024)
	for index := 0; index < maxSessionEvents*2; index++ {
		server.storeEvent("session", chunk)
	}
	server.mu.RLock()
	current := server.sessions["session"]
	count := len(current.Events)
	storedBytes := current.EventBytes
	server.mu.RUnlock()
	if count > maxSessionEvents {
		t.Fatalf("event count = %d, max = %d", count, maxSessionEvents)
	}
	if storedBytes > maxSessionEventBytes {
		t.Fatalf("event bytes = %d, max = %d", storedBytes, maxSessionEventBytes)
	}

	server.storeEvent("session", bytes.Repeat([]byte("y"), maxSessionEventBytes+1))
	server.mu.RLock()
	unchanged := len(server.sessions["session"].Events)
	server.mu.RUnlock()
	if unchanged != count {
		t.Fatalf("oversized event changed replay count: %d -> %d", count, unchanged)
	}
}

func TestToolConcurrencyLimitReturnsBusy(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), MaxConcurrentTools: 1})
	server.toolSlots <- struct{}{}
	defer func() { <-server.toolSlots }()
	params, err := json.Marshal(toolCallParams{Name: "server_info", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	server.handleToolCall(recorder, request, rpcRequest{ID: json.RawMessage("1"), Params: params})
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestJSONRPCRejectsTrailingObjects(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"}}}{}`,
	))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestCoreResponsesIncludeSecurityHeaders(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	for name, expected := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := recorder.Header().Get(name); got != expected {
			t.Fatalf("%s = %q, want %q", name, got, expected)
		}
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("content security policy does not block framing")
	}
}

func TestCommandSessionRetentionAndOutputAreBounded(t *testing.T) {
	manager := &commandManager{sessions: make(map[string]*commandSession)}
	now := time.Now()
	for index := 0; index < maxRetainedCommandSessions+20; index++ {
		manager.sessions[string(rune(index+1))] = &commandSession{
			endedAt: now.Add(time.Duration(index) * time.Second),
		}
	}
	manager.cleanupLocked(now.Add(time.Minute))
	if got := len(manager.sessions); got != maxRetainedCommandSessions {
		t.Fatalf("retained command sessions = %d, want %d", got, maxRetainedCommandSessions)
	}

	output := &boundedOutput{maxBytes: 1024}
	payload := bytes.Repeat([]byte("z"), 4096)
	if _, err := output.Write(payload); err != nil {
		t.Fatal(err)
	}
	if got := len(output.data); got != 1024 {
		t.Fatalf("bounded output bytes = %d, want 1024", got)
	}
	if output.baseOffset != 3072 || output.totalBytes != 4096 {
		t.Fatalf("unexpected offsets: base=%d total=%d", output.baseOffset, output.totalBytes)
	}
}

func TestOAuthRateLimiterAndRegistrationCap(t *testing.T) {
	limiter := oauthRateLimiter{entries: make(map[string]oauthRateEntry)}
	now := time.Now()
	if !limiter.allow("client", 2, time.Minute, now) || !limiter.allow("client", 2, time.Minute, now) {
		t.Fatal("initial OAuth requests were rejected")
	}
	if limiter.allow("client", 2, time.Minute, now) {
		t.Fatal("OAuth rate limit was not enforced")
	}
	if !limiter.allow("client", 2, time.Minute, now.Add(time.Minute)) {
		t.Fatal("OAuth rate limit window did not reset")
	}

	server := &oauthServer{
		clients:       make(map[string]oauthClient),
		refreshTokens: make(map[string]refreshGrant),
		authCodes:     make(map[string]authorizationCode),
		rateLimiter:   oauthRateLimiter{entries: make(map[string]oauthRateEntry)},
	}
	for index := 0; index < maxDynamicOAuthClients; index++ {
		id := string(rune(index + 1))
		server.clients[id] = oauthClient{ClientID: id, CreatedAt: int64(index + 1)}
	}
	body := `{"client_name":"test","redirect_uris":["http://127.0.0.1:43210/callback"],"token_endpoint_auth_method":"none"}`
	request := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	server.handleDynamicRegistration(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if len(server.clients) != maxDynamicOAuthClients {
		t.Fatalf("client count = %d, want %d", len(server.clients), maxDynamicOAuthClients)
	}
	if _, exists := server.clients[string(rune(1))]; exists {
		t.Fatal("oldest unused OAuth client was not evicted")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("OAuth response is cacheable")
	}

	protected := &oauthServer{
		clients:       make(map[string]oauthClient),
		refreshTokens: make(map[string]refreshGrant),
		authCodes:     make(map[string]authorizationCode),
		rateLimiter:   oauthRateLimiter{entries: make(map[string]oauthRateEntry)},
	}
	for index := 0; index < maxDynamicOAuthClients; index++ {
		id := string(rune(index + 1))
		protected.clients[id] = oauthClient{ClientID: id, CreatedAt: int64(index + 1)}
		protected.refreshTokens[id] = refreshGrant{ClientID: id, ExpiresAt: now.Add(time.Hour)}
	}
	request = httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder = httptest.NewRecorder()
	protected.handleDynamicRegistration(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("protected registration status = %d, want %d: %s", recorder.Code, http.StatusTooManyRequests, recorder.Body.String())
	}
}

func TestOAuthStateFileSizeIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-state.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxOAuthStateFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readOAuthStateFile(path); err == nil {
		t.Fatal("oversized OAuth state file was accepted")
	}
}

func TestCanceledToolCallDoesNotLeakSlot(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), MaxConcurrentTools: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(ctx)
	params, _ := json.Marshal(toolCallParams{Name: "server_info", Arguments: map[string]any{}})
	recorder := httptest.NewRecorder()
	server.handleToolCall(recorder, request, rpcRequest{ID: json.RawMessage("1"), Params: params})
	if len(server.toolSlots) != 0 {
		t.Fatalf("tool slot leak: %d active", len(server.toolSlots))
	}
}
