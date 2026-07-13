package web

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
