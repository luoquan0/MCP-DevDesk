package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/model"
)

type fakeDesktop struct {
	status         model.DesktopStatus
	opened         bool
	startup        bool
	pickedPath     string
	pickerCanceled bool
	pickerInitial  string
	pickerTitle    string
}

func (f *fakeDesktop) Open() error {
	f.opened = true
	return nil
}

func (f *fakeDesktop) Status() model.DesktopStatus {
	result := f.status
	result.StartupEnabled = f.startup
	return result
}

func (f *fakeDesktop) SetStartup(enabled bool) error {
	f.startup = enabled
	return nil
}

func (f *fakeDesktop) PickFolder(initialPath, title string) (string, bool, error) {
	f.pickerInitial = initialPath
	f.pickerTitle = title
	return f.pickedPath, f.pickerCanceled, nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"coding-tools-mcp.exe", "cloudflared.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := application.New(root, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server, err := New(app, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestHealthEndpointAllowsLoopback(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/health", nil)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestManagementAPIRejectsRemoteAddress(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil)
	request.RemoteAddr = "192.0.2.10:45678"
	request.Host = "127.0.0.1:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestStaticDashboardIsEmbedded(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	request.RemoteAddr = "[::1]:45678"
	request.Host = "localhost:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "MCP DevDesk") {
		t.Fatal("embedded dashboard marker not found")
	}
}

func TestWebControlCanEnableMoveAndDisable(t *testing.T) {
	server := newTestServer(t)
	control := NewControlServer(server.Handler())
	server.SetControlServer(control)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = control.Shutdown(ctx)
	})

	freePort := func() int {
		for {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			cfg := server.app.Config()
			if port != cfg.AdminPort && port != cfg.MCPPort {
				return port
			}
		}
	}
	firstPort := freePort()
	secondPort := freePort()
	for secondPort == firstPort {
		secondPort = freePort()
	}

	update := func(enabled bool, port int) WebControlStatus {
		t.Helper()
		body, err := json.Marshal(map[string]any{"enabled": enabled, "port": port})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/web-control", bytes.NewReader(body))
		request.RemoteAddr = "127.0.0.1:45678"
		request.Host = "127.0.0.1:17860"
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("web control update failed: %d %s", recorder.Code, recorder.Body.String())
		}
		var status WebControlStatus
		if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	reachable := func(port int) bool {
		client := &http.Client{Timeout: 250 * time.Millisecond}
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}
	waitReachable := func(port int, wanted bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if reachable(port) == wanted {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("port %d reachable=%v, want %v", port, reachable(port), wanted)
	}

	status := update(true, firstPort)
	if !status.Enabled || !status.Running || status.Port != firstPort || !strings.Contains(status.URL, fmt.Sprintf(":%d/#/control", firstPort)) {
		t.Fatalf("unexpected enabled status: %+v", status)
	}
	waitReachable(firstPort, true)

	status = update(true, secondPort)
	if !status.Enabled || !status.Running || status.Port != secondPort {
		t.Fatalf("unexpected moved status: %+v", status)
	}
	waitReachable(secondPort, true)
	waitReachable(firstPort, false)

	status = update(false, secondPort)
	if status.Enabled || status.Running || status.Port != secondPort || status.URL != "" {
		t.Fatalf("unexpected disabled status: %+v", status)
	}
	waitReachable(secondPort, false)
	if cfg := server.app.Config(); cfg.WebControlEnabled || cfg.WebControlPort != secondPort {
		t.Fatalf("web control config not persisted: %+v", cfg)
	}
}

func TestGenericConfigRejectsWebControlFields(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/config", bytes.NewBufferString(`{"webControlEnabled":true,"webControlPort":17861}`))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "/api/web-control") {
		t.Fatalf("generic config unexpectedly accepted web control fields: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestDesktopStatusAndStartupEndpoints(t *testing.T) {
	server := newTestServer(t)
	desktop := &fakeDesktop{status: model.DesktopStatus{
		Available:       true,
		AppMode:         true,
		NativeWindow:    true,
		RenderEngine:    "Microsoft Edge WebView2（内嵌）",
		RuntimeVersion:  "test-runtime",
		WindowModeLabel: "Windows 原生窗口（内嵌 WebView2）",
	}}
	server.desktop = desktop

	statusRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/system/desktop", nil)
	statusRequest.RemoteAddr = "127.0.0.1:45678"
	statusRequest.Host = "127.0.0.1:17860"
	statusRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), `"appMode":true`) {
		t.Fatalf("unexpected desktop status: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	startupRequest := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/system/startup", bytes.NewBufferString(`{"enabled":true}`))
	startupRequest.RemoteAddr = "127.0.0.1:45678"
	startupRequest.Host = "127.0.0.1:17860"
	startupRequest.Header.Set("Content-Type", "application/json")
	startupRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(startupRecorder, startupRequest)
	if startupRecorder.Code != http.StatusOK || !desktop.startup {
		t.Fatalf("startup update failed: %d %s", startupRecorder.Code, startupRecorder.Body.String())
	}

	openRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/ui/open", bytes.NewBufferString(`{}`))
	openRequest.RemoteAddr = "127.0.0.1:45678"
	openRequest.Host = "127.0.0.1:17860"
	openRequest.Header.Set("Content-Type", "application/json")
	openRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(openRecorder, openRequest)
	if openRecorder.Code != http.StatusAccepted || !desktop.opened {
		t.Fatalf("open UI failed: %d %s", openRecorder.Code, openRecorder.Body.String())
	}
}

func TestFolderPickerEndpoint(t *testing.T) {
	server := newTestServer(t)
	desktop := &fakeDesktop{pickedPath: `D:\Projects\selected-app`}
	server.desktop = desktop

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/system/pick-folder", bytes.NewBufferString(`{"initialPath":"D:\\Projects","title":"选择项目目录"}`))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("folder picker failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if desktop.pickerInitial != `D:\Projects` || desktop.pickerTitle != "选择项目目录" {
		t.Fatalf("unexpected picker request: initial=%q title=%q", desktop.pickerInitial, desktop.pickerTitle)
	}
	if !strings.Contains(recorder.Body.String(), `"path":"D:\\Projects\\selected-app"`) || !strings.Contains(recorder.Body.String(), `"canceled":false`) {
		t.Fatalf("unexpected folder picker response: %s", recorder.Body.String())
	}
}

func TestCloneInstanceEndpointCreatesIndependentCore(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/instances/primary/clone", bytes.NewBufferString(`{"coreMode":"go"}`))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("clone instance failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var instance model.MCPInstance
	if err := json.Unmarshal(recorder.Body.Bytes(), &instance); err != nil {
		t.Fatal(err)
	}
	if instance.Primary || instance.CoreMode != "go" || instance.Domain != "" || instance.MCPPort == server.app.Config().MCPPort {
		t.Fatalf("unexpected cloned instance: %+v", instance)
	}
}

func TestDiagnosticsExportRedactsSensitiveLogValues(t *testing.T) {
	server := newTestServer(t)
	dataDir := server.app.Status().DataDirectory
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "manager.log"), []byte("token=secret-token password=secret-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/diagnostics/export", nil)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("diagnostics export failed: %d %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, "secret-token") || strings.Contains(body, "secret-password") || !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("diagnostics were not redacted: %s", body)
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("missing attachment header: %q", recorder.Header().Get("Content-Disposition"))
	}
}

func TestProjectPathCanBeUpdatedFromProjectsAPI(t *testing.T) {
	server := newTestServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if _, err := server.app.UpdateConfig(model.ConfigUpdate{MCPPort: &freePort}); err != nil {
		t.Fatal(err)
	}
	projects := server.app.Projects()
	if len(projects) == 0 {
		t.Fatal("expected the active workspace to be registered as a project")
	}
	active := projects[0]
	for _, project := range projects {
		if strings.EqualFold(filepath.Clean(project.Path), filepath.Clean(server.app.Config().Workspace)) {
			active = project
			break
		}
	}
	target := filepath.Join(t.TempDir(), "updated-workspace")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"path": target})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/projects/"+active.ID, bytes.NewReader(body))
	request.SetPathValue("id", active.ID)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("project path update failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.EqualFold(filepath.Clean(server.app.Config().Workspace), filepath.Clean(target)) {
		t.Fatalf("active workspace = %q, want %q", server.app.Config().Workspace, target)
	}
	if !strings.Contains(recorder.Body.String(), `"path":`) {
		t.Fatalf("unexpected project update response: %s", recorder.Body.String())
	}
}

func TestMultiInstanceLifecycleAPI(t *testing.T) {
	server := newTestServer(t)
	workspace := filepath.Join(t.TempDir(), "secondary-workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	createBody, err := json.Marshal(map[string]any{
		"name":      "secondary",
		"workspace": workspace,
		"mcpPort":   port,
		"coreMode":  "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/instances", bytes.NewReader(createBody))
	createRequest.RemoteAddr = "127.0.0.1:45678"
	createRequest.Host = "127.0.0.1:17860"
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create instance failed: %d %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created model.MCPInstance
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Primary || created.MCPPort != port || created.Workspace != workspace {
		t.Fatalf("unexpected created instance: %+v", created)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/instances", nil)
	listRequest.RemoteAddr = "127.0.0.1:45678"
	listRequest.Host = "127.0.0.1:17860"
	listRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), created.ID) || !strings.Contains(listRecorder.Body.String(), `"primary":true`) {
		t.Fatalf("unexpected instance list: %d %s", listRecorder.Code, listRecorder.Body.String())
	}

	patchRequest := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/instances/"+created.ID, bytes.NewBufferString(`{"name":"secondary-renamed","loggingEnabled":false}`))
	patchRequest.SetPathValue("id", created.ID)
	patchRequest.RemoteAddr = "127.0.0.1:45678"
	patchRequest.Host = "127.0.0.1:17860"
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(patchRecorder, patchRequest)
	if patchRecorder.Code != http.StatusOK || !strings.Contains(patchRecorder.Body.String(), `"name":"secondary-renamed"`) || !strings.Contains(patchRecorder.Body.String(), `"loggingEnabled":false`) {
		t.Fatalf("update instance failed: %d %s", patchRecorder.Code, patchRecorder.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/instances/"+created.ID, nil)
	deleteRequest.SetPathValue("id", created.ID)
	deleteRequest.RemoteAddr = "127.0.0.1:45678"
	deleteRequest.Host = "127.0.0.1:17860"
	deleteRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete instance failed: %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestProjectGitHistoryAndRollbackEndpoints(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	server := newTestServer(t)
	root := t.TempDir()
	runWebTestGit(t, root, "init")
	runWebTestGit(t, root, "config", "user.email", "web-test@example.invalid")
	runWebTestGit(t, root, "config", "user.name", "Web Test")
	file := filepath.Join(root, "state.txt")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runWebTestGit(t, root, "add", "state.txt")
	runWebTestGit(t, root, "commit", "-m", "first state")
	first := strings.TrimSpace(runWebTestGit(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(file, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runWebTestGit(t, root, "add", "state.txt")
	runWebTestGit(t, root, "commit", "-m", "second state")
	project, err := server.app.AddProject("history-test", root)
	if err != nil {
		t.Fatal(err)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/projects/"+project.ID+"/git/history?limit=20", nil)
	historyRequest.SetPathValue("id", project.ID)
	historyRequest.RemoteAddr = "127.0.0.1:45678"
	historyRequest.Host = "127.0.0.1:17860"
	historyRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(historyRecorder, historyRequest)
	if historyRecorder.Code != http.StatusOK || !strings.Contains(historyRecorder.Body.String(), `"shortHash":`) || !strings.Contains(historyRecorder.Body.String(), `"current":true`) {
		t.Fatalf("history endpoint failed: %d %s", historyRecorder.Code, historyRecorder.Body.String())
	}

	rollbackRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects/"+project.ID+"/git/rollback", bytes.NewBufferString(`{"commit":"`+first+`"}`))
	rollbackRequest.SetPathValue("id", project.ID)
	rollbackRequest.RemoteAddr = "127.0.0.1:45678"
	rollbackRequest.Host = "127.0.0.1:17860"
	rollbackRequest.Header.Set("Content-Type", "application/json")
	rollbackRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rollbackRecorder, rollbackRequest)
	if rollbackRecorder.Code != http.StatusOK || !strings.Contains(rollbackRecorder.Body.String(), `"backupBranch":`) {
		t.Fatalf("rollback endpoint failed: %d %s", rollbackRecorder.Code, rollbackRecorder.Body.String())
	}
	if strings.TrimSpace(runWebTestGit(t, root, "rev-parse", "HEAD")) != first {
		t.Fatal("rollback endpoint did not move HEAD")
	}
}

func TestConfigEndpointAppliesMCPPortChange(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := newTestServer(t)
	body := bytes.NewBufferString(fmt.Sprintf(`{"mcpPort":%d}`, port))
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/config", body)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), fmt.Sprintf(`"mcpPort":%d`, port)) {
		t.Fatalf("port was not updated: %s", recorder.Body.String())
	}
}

func TestInvalidTunnelPIDIsRejected(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/tunnels/processes/not-a-pid", nil)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSecretGenerateAndUpdateEndpoints(t *testing.T) {
	server := newTestServer(t)

	generateRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/secrets/generate", bytes.NewBufferString(`{"field":"tokenSecret"}`))
	generateRequest.RemoteAddr = "127.0.0.1:45678"
	generateRequest.Host = "127.0.0.1:17860"
	generateRequest.Header.Set("Content-Type", "application/json")
	generateRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(generateRecorder, generateRequest)
	if generateRecorder.Code != http.StatusOK || !strings.Contains(generateRecorder.Body.String(), `"tokenSecret":"`) {
		t.Fatalf("secret generation failed: %d %s", generateRecorder.Code, generateRecorder.Body.String())
	}

	tokenValue := strings.Repeat("cd", 32)
	updateBody := fmt.Sprintf(`{"ownerPassword":"owner-value-long-enough","clientId":"custom-client","clientSecret":"client-value-long-enough","tokenSecret":"%s","restart":false}`, tokenValue)
	updateRequest := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/secrets", bytes.NewBufferString(updateBody))
	updateRequest.RemoteAddr = "127.0.0.1:45678"
	updateRequest.Host = "127.0.0.1:17860"
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK || !strings.Contains(updateRecorder.Body.String(), `"clientId":"custom-client"`) {
		t.Fatalf("secret update failed: %d %s", updateRecorder.Code, updateRecorder.Body.String())
	}

	revealRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/secrets?reveal=true", nil)
	revealRequest.RemoteAddr = "127.0.0.1:45678"
	revealRequest.Host = "127.0.0.1:17860"
	revealRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(revealRecorder, revealRequest)
	if revealRecorder.Code != http.StatusOK || !strings.Contains(revealRecorder.Body.String(), tokenValue) {
		t.Fatalf("saved values were not revealed: %d %s", revealRecorder.Code, revealRecorder.Body.String())
	}
}

func TestProjectPromptSettingsAndProjectPromptEndpoints(t *testing.T) {
	server := newTestServer(t)
	projects := server.app.Projects()
	if len(projects) == 0 {
		t.Fatal("expected at least one project")
	}
	project := projects[0]

	globalRequest := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/projects/prompt-settings", bytes.NewBufferString(`{"globalPrompt":"finish the whole task before replying"}`))
	globalRequest.RemoteAddr = "127.0.0.1:45678"
	globalRequest.Host = "127.0.0.1:17860"
	globalRequest.Header.Set("Content-Type", "application/json")
	globalRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(globalRecorder, globalRequest)
	if globalRecorder.Code != http.StatusOK || !strings.Contains(globalRecorder.Body.String(), "finish the whole task before replying") {
		t.Fatalf("global prompt update failed: %d %s", globalRecorder.Code, globalRecorder.Body.String())
	}

	projectRequest := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/projects/"+project.ID, bytes.NewBufferString(`{"prompt":"run tests before reporting completion"}`))
	projectRequest.SetPathValue("id", project.ID)
	projectRequest.RemoteAddr = "127.0.0.1:45678"
	projectRequest.Host = "127.0.0.1:17860"
	projectRequest.Header.Set("Content-Type", "application/json")
	projectRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(projectRecorder, projectRequest)
	if projectRecorder.Code != http.StatusOK || !strings.Contains(projectRecorder.Body.String(), "run tests before reporting completion") {
		t.Fatalf("project prompt update failed: %d %s", projectRecorder.Code, projectRecorder.Body.String())
	}

	settingsRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/projects/prompt-settings", nil)
	settingsRequest.RemoteAddr = "127.0.0.1:45678"
	settingsRequest.Host = "127.0.0.1:17860"
	settingsRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(settingsRecorder, settingsRequest)
	if settingsRecorder.Code != http.StatusOK || !strings.Contains(settingsRecorder.Body.String(), `"maxPromptBytes":32768`) {
		t.Fatalf("prompt settings read failed: %d %s", settingsRecorder.Code, settingsRecorder.Body.String())
	}
}

func runWebTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", command...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
