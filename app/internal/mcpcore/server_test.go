package mcpcore

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if len(listResult.Result.Tools) != 30 {
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
	if afterDelete.StatusCode != http.StatusBadRequest {
		t.Fatalf("deleted session status = %d", afterDelete.StatusCode)
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

	listResult, err := server.executeTool("list_dir", map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := listResult["entries"].([]directoryEntry)
	if !ok {
		t.Fatalf("unexpected entries type: %T", listResult["entries"])
	}
	if len(entries) != 1 || entries[0].Name != "docs" {
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
