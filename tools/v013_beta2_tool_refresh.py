from pathlib import Path


def read(path: str) -> str:
    return Path(path).read_text(encoding="utf-8")


def write(path: str, text: str) -> None:
    Path(path).write_text(text, encoding="utf-8", newline="\n")


def replace_once(path: str, old: str, new: str) -> None:
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {count}: {old[:120]!r}")
    write(path, text.replace(old, new, 1))


server = "app/internal/mcpcore/server.go"
replace_once(
    server,
    '"tools": map[string]any{"listChanged": false},',
    '"tools": map[string]any{"listChanged": true},',
)
replace_once(
    server,
    '''\tif len(request.ID) == 0 {\n\t\t// Notifications do not receive JSON-RPC responses.\n\t\tw.Header().Set(SessionHeader, sessionID)\n\t\tw.WriteHeader(http.StatusAccepted)\n\t\treturn\n\t}\n''',
    '''\tif len(request.ID) == 0 {\n\t\t// A connector can keep a cached tool catalog across DevDesk upgrades.\n\t\t// Advertise and emit the standard list-changed notification after the\n\t\t// MCP initialized notification so clients re-run tools/list and pick up\n\t\t// newly added tools without requiring the connection to be recreated.\n\t\tif request.Method == "notifications/initialized" {\n\t\t\ts.queueToolsListChanged(sessionID)\n\t\t}\n\t\t// Notifications do not receive JSON-RPC responses.\n\t\tw.Header().Set(SessionHeader, sessionID)\n\t\tw.WriteHeader(http.StatusAccepted)\n\t\treturn\n\t}\n''',
)
replace_once(
    server,
    '''\t\t"instructions": s.initializeInstructions(),\n\t})\n}\n\nfunc (s *Server) handleToolCall''',
    '''\t\t"instructions": s.initializeInstructions(),\n\t})\n}\n\nfunc (s *Server) queueToolsListChanged(sessionID string) {\n\tconst notification = `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`\n\ts.storeEvent(sessionID, []byte(notification))\n}\n\nfunc (s *Server) handleToolCall''',
)
replace_once(
    server,
    '''\tevents := s.eventsAfter(sessionID, strings.TrimSpace(r.Header.Get("Last-Event-ID")))\n\tw.Header().Set("Content-Type", "text/event-stream; charset=utf-8")\n\tw.Header().Set("Cache-Control", "no-cache, no-store")\n\tw.Header().Set("Connection", "keep-alive")\n\tw.Header().Set(SessionHeader, sessionID)\n\tw.WriteHeader(http.StatusOK)\n\t_, _ = io.WriteString(w, ": connected\\n\\n")\n\tfor _, event := range events {\n\t\twriteSSEEvent(w, event)\n\t}\n\tif flusher, ok := w.(http.Flusher); ok {\n\t\tflusher.Flush()\n\t}\n\tif strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "wait=0") {\n\t\treturn\n\t}\n\tticker := time.NewTicker(15 * time.Second)\n\tdefer ticker.Stop()\n\tfor {\n\t\tselect {\n\t\tcase <-r.Context().Done():\n\t\t\treturn\n\t\tcase <-ticker.C:\n\t\t\t_, _ = io.WriteString(w, ": keepalive "+strconv.FormatInt(time.Now().Unix(), 10)+"\\n\\n")\n\t\t\tif flusher, ok := w.(http.Flusher); ok {\n\t\t\t\tflusher.Flush()\n\t\t\t}\n\t\t}\n\t}\n''',
    '''\tlastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))\n\tevents := s.eventsAfter(sessionID, lastEventID)\n\tw.Header().Set("Content-Type", "text/event-stream; charset=utf-8")\n\tw.Header().Set("Cache-Control", "no-cache, no-store")\n\tw.Header().Set("Connection", "keep-alive")\n\tw.Header().Set(SessionHeader, sessionID)\n\tw.WriteHeader(http.StatusOK)\n\t_, _ = io.WriteString(w, ": connected\\n\\n")\n\tfor _, event := range events {\n\t\twriteSSEEvent(w, event)\n\t\tlastEventID = event.ID\n\t}\n\tif flusher, ok := w.(http.Flusher); ok {\n\t\tflusher.Flush()\n\t}\n\tif strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "wait=0") {\n\t\treturn\n\t}\n\tkeepaliveTicker := time.NewTicker(15 * time.Second)\n\tdefer keepaliveTicker.Stop()\n\teventTicker := time.NewTicker(500 * time.Millisecond)\n\tdefer eventTicker.Stop()\n\tfor {\n\t\tselect {\n\t\tcase <-r.Context().Done():\n\t\t\treturn\n\t\tcase <-eventTicker.C:\n\t\t\tpending := s.eventsAfter(sessionID, lastEventID)\n\t\t\tif len(pending) == 0 {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tfor _, event := range pending {\n\t\t\t\twriteSSEEvent(w, event)\n\t\t\t\tlastEventID = event.ID\n\t\t\t}\n\t\t\tif flusher, ok := w.(http.Flusher); ok {\n\t\t\t\tflusher.Flush()\n\t\t\t}\n\t\tcase <-keepaliveTicker.C:\n\t\t\t_, _ = io.WriteString(w, ": keepalive "+strconv.FormatInt(time.Now().Unix(), 10)+"\\n\\n")\n\t\t\tif flusher, ok := w.(http.Flusher); ok {\n\t\t\t\tflusher.Flush()\n\t\t\t}\n\t\t}\n\t}\n''',
)

screen = "app/internal/mcpcore/screen_tools.go"
replace_once(
    screen,
    'Description: "Capture one explicitly selected Windows application window on demand, including a background or minimized target when Windows allows it, and return a PNG image to the MCP client. Minimized targets are temporarily restored without focus and returned to minimized state. Nothing is saved to disk.",',
    'Description: "Capture one explicitly selected Windows application window on demand without changing its Z-order or restoring a minimized window. Background capture uses non-state-changing fallbacks; minimized capture fails closed in this preview until a non-invasive backend passes compatibility validation. Nothing is saved to disk.",',
)

version = "app/internal/buildinfo/version.go"
replace_once(version, 'const Version = "0.13.0-beta.1"', 'const Version = "0.13.0-beta.2"')

roadmap = "docs/ROADMAP.md"
roadmap_text = read(roadmap).replace("0.13.0-beta.1", "0.13.0-beta.2")
write(roadmap, roadmap_text)

preview = "docs/V013_PREVIEW.md"
preview_text = read(preview).replace("0.13.0-beta.1", "0.13.0-beta.2")
marker = "## Beta 2：工具目录自动刷新"
if marker not in preview_text:
    preview_text += '''\n\n## Beta 2：工具目录自动刷新\n\n- 修复从 0.12.x 原地升级到 0.13 后 ChatGPT Connector 继续缓存旧 38 个工具的问题。\n- MCP initialize 现在声明 `tools.listChanged=true`，并在客户端 `notifications/initialized` 后发送标准 `notifications/tools/list_changed`。\n- Streamable HTTP GET/SSE 现在会实时投递会话期间新增的服务端事件，而不只是连接建立瞬间的历史事件。\n- 回归测试明确验证 0.13 新工具组存在：代码导航、Agent Task、持久 Job、`checks_run` / `validate_project` 与 `screen_capture_probe`。\n- 修正 Screen Vision 工具描述，使 Schema 与 Beta 的非侵入式 / fail-closed 捕获策略一致。\n'''
write(preview, preview_text)

test_path = "app/internal/mcpcore/server_test.go"
tests = read(test_path)
addition = r'''

func TestV013ToolCatalogContainsNewGroups(t *testing.T) {
	server := mustNewServer(t, Options{
		Workspace:            t.TempDir(),
		ToolProfile:          "full",
		PermissionMode:       "dangerous",
		ScreenCaptureEnabled: true,
	})
	seen := make(map[string]bool, len(server.tools))
	for _, tool := range server.tools {
		seen[tool.Name] = true
	}
	expected := []string{
		"list_symbols", "document_symbols", "workspace_symbols", "find_definition", "find_references",
		"task_start", "task_list", "task_get", "task_update", "task_resume", "task_diff", "task_finish",
		"job_list", "job_get",
		"checks_run", "validate_project",
		"screen_capture_probe",
	}
	for _, name := range expected {
		if !seen[name] {
			t.Fatalf("v0.13 tool %q is missing from tools/list catalog", name)
		}
	}
}

func TestInitializeAdvertisesAndQueuesToolListChanged(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	initialized := postRPC(t, httpServer.URL+"/mcp", "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "refresh-client", "version": "1"},
		},
	})
	defer initialized.Body.Close()
	if initialized.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d", initialized.StatusCode)
	}
	var initResult struct {
		Result struct {
			Capabilities struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	decodeJSON(t, initialized.Body, &initResult)
	if !initResult.Result.Capabilities.Tools.ListChanged {
		t.Fatal("initialize did not advertise tools.listChanged=true")
	}
	sessionID := initialized.Header.Get(SessionHeader)
	if sessionID == "" {
		t.Fatal("initialize did not return a session ID")
	}

	notification, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	notification.Header.Set("Content-Type", "application/json")
	notification.Header.Set("Accept", "application/json, text/event-stream")
	notification.Header.Set(SessionHeader, sessionID)
	notification.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	notificationResponse, err := http.DefaultClient.Do(notification)
	if err != nil {
		t.Fatal(err)
	}
	defer notificationResponse.Body.Close()
	if notificationResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("initialized notification status = %d", notificationResponse.StatusCode)
	}

	getRequest, err := http.NewRequest(http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	getRequest.Header.Set("Accept", "text/event-stream")
	getRequest.Header.Set("Prefer", "wait=0")
	getRequest.Header.Set(SessionHeader, sessionID)
	getRequest.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer getResponse.Body.Close()
	body := readBody(t, getResponse.Body)
	if getResponse.StatusCode != http.StatusOK {
		t.Fatalf("SSE refresh status = %d, body = %s", getResponse.StatusCode, body)
	}
	if !strings.Contains(body, `"method":"notifications/tools/list_changed"`) {
		t.Fatalf("SSE stream did not contain tools/list_changed notification: %s", body)
	}
}
'''
if "func TestV013ToolCatalogContainsNewGroups" in tests:
    raise SystemExit("server_test.go already contains Beta 2 tests")
write(test_path, tests + addition)
