package mcpcore

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInitializeListAndCallTools(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("hello preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Name: "test-core", Version: "test", Workspace: workspace})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	initialize := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	}
	response := postRPC(t, httpServer.URL+"/mcp", "", initialize)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
	sessionID := response.Header.Get(SessionHeader)
	if sessionID == "" {
		t.Fatal("initialize did not return an MCP session ID")
	}
	var initializeResult struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	decodeJSON(t, response.Body, &initializeResult)
	if initializeResult.Result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %q", initializeResult.Result.ProtocolVersion)
	}

	listResponse := postRPC(t, httpServer.URL+"/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	defer listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, body = %s", listResponse.StatusCode, readBody(t, listResponse.Body))
	}
	var listResult struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, listResponse.Body, &listResult)
	if len(listResult.Result.Tools) != 33 {
		t.Fatalf("tool count = %d", len(listResult.Result.Tools))
	}

	callResponse := postRPC(t, httpServer.URL+"/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "server_info",
			"arguments": map[string]any{},
		},
	})
	defer callResponse.Body.Close()
	if callResponse.StatusCode != http.StatusOK {
		t.Fatalf("tools/call status = %d, body = %s", callResponse.StatusCode, readBody(t, callResponse.Body))
	}
	var callResult struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	decodeJSON(t, callResponse.Body, &callResult)
	if callResult.Result.IsError {
		t.Fatal("server_info returned an error result")
	}
	if callResult.Result.StructuredContent["coreMode"] != "go" {
		t.Fatalf("core mode = %#v", callResult.Result.StructuredContent["coreMode"])
	}

	readResponse := postRPC(t, httpServer.URL+"/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "read_file",
			"arguments": map[string]any{"path": "hello.txt"},
		},
	})
	defer readResponse.Body.Close()
	var readResult struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	decodeJSON(t, readResponse.Body, &readResult)
	if readResult.Result.IsError || readResult.Result.StructuredContent["content"] != "hello preview" {
		t.Fatalf("unexpected read_file result: %#v", readResult.Result)
	}
}

func TestSessionIsRequiredAndCanBeDeleted(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	missing := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing session status = %d", missing.StatusCode)
	}

	initialized := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": ProtocolVersion,
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})
	defer initialized.Body.Close()
	if initialized.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d", initialized.StatusCode)
	}
	sessionID := initialized.Header.Get(SessionHeader)

	request, err := http.NewRequest(http.MethodDelete, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(SessionHeader, sessionID)
	deleted, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.StatusCode)
	}

	afterDelete := postRPC(t, httpServer.URL+"/mcp", sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "ping",
	})
	defer afterDelete.Body.Close()
	if afterDelete.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted session status = %d", afterDelete.StatusCode)
	}
}

func TestProtocolNegotiationAndNotificationStatus(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	initialize := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2099-01-01",
			"clientInfo":      map[string]any{"name": "future-client", "version": "1"},
		},
	})
	defer initialize.Body.Close()
	if initialize.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, body = %s", initialize.StatusCode, readBody(t, initialize.Body))
	}
	var initialized struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	decodeJSON(t, initialize.Body, &initialized)
	if initialized.Result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("negotiated protocol = %q", initialized.Result.ProtocolVersion)
	}

	sessionID := initialize.Header.Get(SessionHeader)
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set(SessionHeader, sessionID)
	request.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status = %d, body = %s", response.StatusCode, readBody(t, response.Body))
	}
}

func TestLegacyProtocolVersionIsNegotiated(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	initialize := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": LegacyProtocolVersion,
			"clientInfo":      map[string]any{"name": "legacy-client", "version": "1"},
		},
	})
	defer initialize.Body.Close()
	var initialized struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	decodeJSON(t, initialize.Body, &initialized)
	if initialized.Result.ProtocolVersion != LegacyProtocolVersion {
		t.Fatalf("legacy protocol = %q", initialized.Result.ProtocolVersion)
	}
}

func TestExposedToolSchemasAreValidAndUnique(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), ToolProfile: "full"})
	namePattern := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	seen := make(map[string]struct{}, len(server.tools))
	for _, tool := range server.tools {
		if !namePattern.MatchString(tool.Name) {
			t.Fatalf("invalid tool name %q", tool.Name)
		}
		if _, exists := seen[tool.Name]; exists {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		if tool.Description == "" {
			t.Fatalf("tool %q has no description", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("tool %q input schema root type = %#v", tool.Name, tool.InputSchema["type"])
		}
		if _, err := json.Marshal(tool.InputSchema); err != nil {
			t.Fatalf("tool %q input schema is not JSON encodable: %v", tool.Name, err)
		}
	}
	if len(seen) != 33 {
		t.Fatalf("tool count = %d", len(seen))
	}
}

func TestWorkspaceFileTools(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "alpha\nBeta target\ngamma target\n"
	if err := os.WriteFile(filepath.Join(workspace, "docs", "notes.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".hidden.txt"), []byte("target hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Run go test before committing."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := mustNewServer(t, Options{Workspace: workspace})
	readResult, err := server.executeTool("read_file", map[string]any{
		"path":      "docs/notes.txt",
		"startLine": 2,
		"endLine":   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if readResult["content"] != "Beta target\ngamma target" {
		t.Fatalf("unexpected read content: %#v", readResult["content"])
	}
	if readResult["returnedBytes"] != len("Beta target\ngamma target") {
		t.Fatalf("unexpected returned byte metadata: %#v", readResult)
	}

	batchResult, err := server.executeTool("read_files", map[string]any{
		"files": []map[string]any{
			{"path": "docs/notes.txt", "startLine": 1, "endLine": 1},
			{"path": "missing.txt"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batchResult["succeeded"] != 1 || batchResult["failed"] != 1 {
		t.Fatalf("unexpected batch result: %#v", batchResult)
	}

	snapshot, err := server.executeTool("project_snapshot", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	buildFiles, ok := snapshot["buildFiles"].([]string)
	if !ok || len(buildFiles) != 1 || buildFiles[0] != "go.mod" {
		t.Fatalf("unexpected build files: %#v", snapshot["buildFiles"])
	}
	rules, ok := snapshot["instructions"].([]projectRule)
	if !ok || len(rules) != 1 || rules[0].Path != "AGENTS.md" {
		t.Fatalf("unexpected project rules: %#v", snapshot["instructions"])
	}

	listResult, err := server.executeTool("list_dir", map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := listResult["entries"].([]directoryEntry)
	if !ok {
		t.Fatalf("unexpected entries type: %T", listResult["entries"])
	}
	if len(entries) != 3 {
		t.Fatalf("hidden entries should be excluded: %#v", entries)
	}

	searchResult, err := server.executeTool("search_text", map[string]any{
		"query": "TARGET",
		"path":  ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := searchResult["matches"].([]textMatch)
	if !ok {
		t.Fatalf("unexpected matches type: %T", searchResult["matches"])
	}
	if len(matches) != 2 || matches[0].Path != "docs/notes.txt" || matches[0].Line != 2 {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestProjectRulesAreIncludedInInitializeInstructions(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Use the project test command."), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace, ManagedInstructions: "Finish all executable steps before replying to the user."})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": ProtocolVersion,
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})
	defer response.Body.Close()
	var initialized struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	decodeJSON(t, response.Body, &initialized)
	if !strings.Contains(initialized.Result.Instructions, "Finish all executable steps before replying") ||
		!strings.Contains(initialized.Result.Instructions, "AGENTS.md") ||
		!strings.Contains(initialized.Result.Instructions, "Use the project test command") {
		t.Fatalf("project instructions were not loaded: %q", initialized.Result.Instructions)
	}
	if strings.Index(initialized.Result.Instructions, "Finish all executable steps before replying") < strings.Index(initialized.Result.Instructions, "Use the project test command") {
		t.Fatalf("managed project instructions must be appended after repository guidance: %q", initialized.Result.Instructions)
	}
}

func TestManagedInstructionsReloadInvalidatesSessionsAndIsReadable(t *testing.T) {
	workspace := t.TempDir()
	instructionsPath := filepath.Join(t.TempDir(), "project-instructions.md")
	if err := os.WriteFile(instructionsPath, []byte("managed prompt v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{
		Workspace:               workspace,
		ManagedInstructions:     "managed prompt v1",
		ManagedInstructionsFile: instructionsPath,
	})
	server.mu.Lock()
	server.sessions["old-session"] = &session{Protocol: ProtocolVersion}
	server.mu.Unlock()

	if err := os.WriteFile(instructionsPath, []byte("managed prompt v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.refreshManagedInstructions(); err != nil {
		t.Fatal(err)
	}
	if got := server.currentManagedInstructions(); got != "managed prompt v2" {
		t.Fatalf("managed instructions = %q", got)
	}
	server.mu.RLock()
	sessionCount := len(server.sessions)
	server.mu.RUnlock()
	if sessionCount != 0 {
		t.Fatalf("stale MCP sessions were not invalidated: %d", sessionCount)
	}

	result, err := server.executeTool("get_instructions", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result["managedInstructions"] != "managed prompt v2" || !strings.Contains(result["instructions"].(string), "managed prompt v2") {
		t.Fatalf("get_instructions returned stale content: %#v", result)
	}
	found := false
	for _, tool := range server.tools {
		if tool.Name == "get_instructions" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("get_instructions is not exposed in tools/list")
	}

	if err := os.Remove(instructionsPath); err != nil {
		t.Fatal(err)
	}
	if err := server.refreshManagedInstructions(); err != nil {
		t.Fatal(err)
	}
	if got := server.currentManagedInstructions(); got != "" {
		t.Fatalf("managed instructions were not cleared after file removal: %q", got)
	}
}

func TestManagedInstructionsFileCanAppearAfterCoreStartup(t *testing.T) {
	workspace := t.TempDir()
	instructionsPath := filepath.Join(t.TempDir(), "project-instructions.md")
	server := mustNewServer(t, Options{
		Workspace:               workspace,
		ManagedInstructionsFile: instructionsPath,
	})
	server.mu.Lock()
	server.sessions["old-session"] = &session{Protocol: ProtocolVersion}
	server.mu.Unlock()

	if err := server.refreshManagedInstructions(); err != nil {
		t.Fatal(err)
	}
	server.mu.RLock()
	before := len(server.sessions)
	server.mu.RUnlock()
	if before != 1 {
		t.Fatalf("missing unchanged instructions file invalidated session unexpectedly: %d", before)
	}

	if err := os.WriteFile(instructionsPath, []byte("first prompt after startup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.refreshManagedInstructions(); err != nil {
		t.Fatal(err)
	}
	if got := server.currentManagedInstructions(); got != "first prompt after startup" {
		t.Fatalf("managed instructions = %q", got)
	}
	server.mu.RLock()
	after := len(server.sessions)
	server.mu.RUnlock()
	if after != 0 {
		t.Fatalf("new instructions did not invalidate the old MCP session: %d", after)
	}
}

func TestOpaqueOriginIsAllowedOnlyForOAuthAuthorizationForm(t *testing.T) {
	server := &Server{allowedOrigins: map[string]struct{}{}}
	handler := server.validateOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRequest(http.MethodPost, "https://mcp.example/oauth/authorize", strings.NewReader("owner_password=test"))
	allowed.Header.Set("Origin", "null")
	allowed.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusNoContent {
		t.Fatalf("opaque OAuth form origin status = %d, want %d; body = %s", allowedRecorder.Code, http.StatusNoContent, allowedRecorder.Body.String())
	}

	for _, test := range []struct {
		name        string
		method      string
		path        string
		contentType string
	}{
		{name: "mcp", method: http.MethodPost, path: "/mcp", contentType: "application/json"},
		{name: "token", method: http.MethodPost, path: "/oauth/token", contentType: "application/x-www-form-urlencoded"},
		{name: "register", method: http.MethodPost, path: "/oauth/register", contentType: "application/json"},
		{name: "authorize json", method: http.MethodPost, path: "/oauth/authorize", contentType: "application/json"},
		{name: "authorize get", method: http.MethodGet, path: "/oauth/authorize", contentType: "application/x-www-form-urlencoded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://mcp.example"+test.path, nil)
			request.Header.Set("Origin", "null")
			request.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "origin is invalid") {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestReadFileDoesNotAdvertiseNextLineWhenOneLineIsByteTruncated(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "long.txt"), []byte(strings.Repeat("x", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	server := mustNewServer(t, Options{Workspace: workspace})
	result, err := server.executeTool("read_file", map[string]any{"path": "long.txt", "maxBytes": 16})
	if err != nil {
		t.Fatal(err)
	}
	if result["lineTruncated"] != true || result["truncated"] != true {
		t.Fatalf("expected one-line truncation metadata: %#v", result)
	}
	if _, exists := result["nextStartLine"]; exists {
		t.Fatalf("one-line byte truncation advertised an invalid nextStartLine: %#v", result)
	}
}

func TestLargeToolTextIsCompactedWithoutDroppingStructuredResult(t *testing.T) {
	structured := map[string]any{
		"path":      "large.txt",
		"content":   strings.Repeat("x", 40*1024),
		"truncated": false,
	}
	raw, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := compactToolResultText("read_file", structured, raw)
	if len(text) >= len(raw) || strings.Contains(text, strings.Repeat("x", 1024)) {
		t.Fatalf("large tool text was not compacted: text=%d raw=%d", len(text), len(raw))
	}
	if !strings.Contains(text, "structuredContent") || !strings.Contains(text, "large.txt") {
		t.Fatalf("compacted text omitted guidance or metadata: %s", text)
	}
}

func TestWorkspaceFileToolsRejectPathEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(filepath.Dir(workspace), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	server := mustNewServer(t, Options{Workspace: workspace})
	_, err := server.executeTool("read_file", map[string]any{"path": filepath.Join("..", filepath.Base(outside))})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected workspace escape error, got %v", err)
	}
}

func TestWorkspaceFileToolsRejectSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	server := mustNewServer(t, Options{Workspace: workspace})
	_, err := server.executeTool("read_file", map[string]any{"path": filepath.Join("outside-link", "outside.txt")})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func mustNewServer(t *testing.T, options Options) *Server {
	t.Helper()
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func postRPC(t *testing.T, url, sessionID string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	if sessionID != "" {
		request.Header.Set(SessionHeader, sessionID)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, reader io.Reader) string {
	t.Helper()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
