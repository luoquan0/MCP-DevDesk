package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func attachTestControlServer(t *testing.T, server *Server) (*ControlServer, http.Handler) {
	t.Helper()
	control := NewControlServer(server.app)
	server.SetControlServer(control)
	handler, err := server.ControlHandler()
	if err != nil {
		t.Fatal(err)
	}
	control.SetHandler(handler)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = control.Shutdown(ctx)
	})
	return control, handler
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

func TestSuccessfulMutationPublishesStateEvent(t *testing.T) {
	server := newTestServer(t)
	before := server.events.revision.Load()
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/projects/prompt-settings", bytes.NewBufferString(`{"enabled":true,"globalPrompt":"sync test"}`))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("mutation status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if after := server.events.revision.Load(); after <= before {
		t.Fatalf("state event revision did not advance: before=%d after=%d", before, after)
	}
}

func TestReadOnlyRequestDoesNotPublishStateEvent(t *testing.T) {
	server := newTestServer(t)
	before := server.events.revision.Load()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/projects", nil)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Host = "127.0.0.1:17860"
	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if after := server.events.revision.Load(); after != before {
		t.Fatalf("read-only request unexpectedly advanced state revision: before=%d after=%d", before, after)
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
	_, _ = attachTestControlServer(t, server)

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

	update := func(enabled bool, port int, lanEnabled bool) WebControlStatus {
		t.Helper()
		body, err := json.Marshal(map[string]any{"enabled": enabled, "port": port, "lanEnabled": lanEnabled})
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
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/control/auth/status", port))
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

	status := update(true, firstPort, true)
	if !status.Enabled || !status.Running || !status.LANEnabled || status.Port != firstPort || !strings.Contains(status.URL, fmt.Sprintf(":%d/#/", firstPort)) {
		t.Fatalf("unexpected enabled status: %+v", status)
	}
	waitReachable(firstPort, true)
	status = update(true, firstPort, false)
	if !status.Enabled || !status.Running || status.LANEnabled || status.Port != firstPort {
		t.Fatalf("unexpected loopback rebind status: %+v", status)
	}
	waitReachable(firstPort, true)
	status = update(true, firstPort, true)
	if !status.Enabled || !status.Running || !status.LANEnabled || status.Port != firstPort {
		t.Fatalf("unexpected LAN rebind status: %+v", status)
	}
	waitReachable(firstPort, true)

	status = update(true, secondPort, true)
	if !status.Enabled || !status.Running || !status.LANEnabled || status.Port != secondPort {
		t.Fatalf("unexpected moved status: %+v", status)
	}
	waitReachable(secondPort, true)
	waitReachable(firstPort, false)

	status = update(false, secondPort, true)
	if status.Enabled || status.Running || status.Port != secondPort || status.URL != "" {
		t.Fatalf("unexpected disabled status: %+v", status)
	}
	waitReachable(secondPort, false)
	if cfg := server.app.Config(); cfg.WebControlEnabled || cfg.WebControlPort != secondPort {
		t.Fatalf("web control config not persisted: %+v", cfg)
	}
}

func TestWebControlPasswordProtectsLANAPI(t *testing.T) {
	server := newTestServer(t)
	control, handler := attachTestControlServer(t, server)
	if err := server.app.SetWebControlPassword("mobile-pass-123"); err != nil {
		t.Fatal(err)
	}
	authEnabled := true
	lanEnabled := true
	if _, err := server.app.UpdateConfig(model.ConfigUpdate{
		WebControlAuthEnabled: &authEnabled,
		WebControlLANEnabled:  &lanEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	control.InvalidateSessions()

	request := httptest.NewRequest(http.MethodGet, "http://192.168.1.5/api/projects", nil)
	request.RemoteAddr = "192.168.1.20:45678"
	request.Host = "192.168.1.5:17861"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated LAN request status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	wrongLogin := httptest.NewRequest(http.MethodPost, "http://192.168.1.5/api/control/auth/login", bytes.NewBufferString(`{"password":"wrong"}`))
	wrongLogin.RemoteAddr = "192.168.1.20:45678"
	wrongLogin.Host = "192.168.1.5:17861"
	wrongLogin.Header.Set("Origin", "http://192.168.1.5:17861")
	wrongLogin.Header.Set("Content-Type", "application/json")
	wrongRecorder := httptest.NewRecorder()
	handler.ServeHTTP(wrongRecorder, wrongLogin)
	if wrongRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, body = %s", wrongRecorder.Code, wrongRecorder.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "http://192.168.1.5/api/control/auth/login", bytes.NewBufferString(`{"password":"mobile-pass-123"}`))
	login.RemoteAddr = "192.168.1.20:45678"
	login.Host = "192.168.1.5:17861"
	login.Header.Set("Origin", "http://192.168.1.5:17861")
	login.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, login)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != webControlSessionCookie || !cookies[0].HttpOnly {
		t.Fatalf("login did not return HttpOnly session cookie: %+v", cookies)
	}

	authorized := httptest.NewRequest(http.MethodGet, "http://192.168.1.5/api/projects", nil)
	authorized.RemoteAddr = "192.168.1.20:45678"
	authorized.Host = "192.168.1.5:17861"
	authorized.AddCookie(cookies[0])
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusOK {
		t.Fatalf("authorized request status = %d, body = %s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}

	publicRemote := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/control/auth/status", nil)
	publicRemote.RemoteAddr = "203.0.113.10:45678"
	publicRemote.Host = "127.0.0.1:17861"
	publicRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publicRecorder, publicRemote)
	if publicRecorder.Code != http.StatusForbidden {
		t.Fatalf("public remote status = %d, want %d", publicRecorder.Code, http.StatusForbidden)
	}
}

func TestControlDirectoryListingCanAddProject(t *testing.T) {
	server := newTestServer(t)
	_, handler := attachTestControlServer(t, server)
	root := server.app.Status().RootDirectory
	projectPath := filepath.Join(root, "mobile-project")
	childPath := filepath.Join(projectPath, "src")
	if err := os.MkdirAll(childPath, 0o700); err != nil {
		t.Fatal(err)
	}

	browse := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/control/directories?path="+url.QueryEscape(root), nil)
	browse.RemoteAddr = "127.0.0.1:45678"
	browse.Host = "127.0.0.1:17861"
	browseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(browseRecorder, browse)
	if browseRecorder.Code != http.StatusOK || !strings.Contains(browseRecorder.Body.String(), "mobile-project") {
		t.Fatalf("directory browse failed: %d %s", browseRecorder.Code, browseRecorder.Body.String())
	}

	body, err := json.Marshal(map[string]string{"name": "Phone Project", "path": projectPath})
	if err != nil {
		t.Fatal(err)
	}
	add := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/projects", bytes.NewReader(body))
	add.RemoteAddr = "127.0.0.1:45678"
	add.Host = "127.0.0.1:17861"
	add.Header.Set("Origin", "http://127.0.0.1:17861")
	add.Header.Set("Content-Type", "application/json")
	addRecorder := httptest.NewRecorder()
	handler.ServeHTTP(addRecorder, add)
	if addRecorder.Code != http.StatusCreated || !strings.Contains(addRecorder.Body.String(), "Phone Project") {
		t.Fatalf("project add through control API failed: %d %s", addRecorder.Code, addRecorder.Body.String())
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

func TestAppearanceAPIUpdatesPaletteAndBackground(t *testing.T) {
	server := newTestServer(t)
	appearanceUpdate := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/appearance", bytes.NewBufferString(`{"theme":"dark","customColorsEnabled":true,"primaryColor":"#ff3366","secondaryColor":"#22aa88","backgroundOpacity":62}`))
	appearanceUpdate.RemoteAddr = "127.0.0.1:45678"
	appearanceUpdate.Host = "127.0.0.1:17860"
	appearanceUpdate.Header.Set("Content-Type", "application/json")
	appearanceRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(appearanceRecorder, appearanceUpdate)
	if appearanceRecorder.Code != http.StatusOK || !strings.Contains(appearanceRecorder.Body.String(), `"customColorsEnabled":true`) || !strings.Contains(appearanceRecorder.Body.String(), `"primaryColor":"#ff3366"`) {
		t.Fatalf("appearance update failed: %d %s", appearanceRecorder.Code, appearanceRecorder.Body.String())
	}

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	backgroundPut := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/appearance/background", bytes.NewReader(png))
	backgroundPut.RemoteAddr = "127.0.0.1:45678"
	backgroundPut.Host = "127.0.0.1:17860"
	backgroundPut.Header.Set("Content-Type", "image/png")
	backgroundPutRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(backgroundPutRecorder, backgroundPut)
	if backgroundPutRecorder.Code != http.StatusOK || !strings.Contains(backgroundPutRecorder.Body.String(), `"hasBackgroundImage":true`) {
		t.Fatalf("appearance background upload failed: %d %s", backgroundPutRecorder.Code, backgroundPutRecorder.Body.String())
	}

	backgroundGet := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/appearance/background", nil)
	backgroundGet.RemoteAddr = "127.0.0.1:45678"
	backgroundGet.Host = "127.0.0.1:17860"
	backgroundGetRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(backgroundGetRecorder, backgroundGet)
	if backgroundGetRecorder.Code != http.StatusOK || backgroundGetRecorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("appearance background read failed: %d %s", backgroundGetRecorder.Code, backgroundGetRecorder.Header().Get("Content-Type"))
	}

	backgroundDelete := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/appearance/background", nil)
	backgroundDelete.RemoteAddr = "127.0.0.1:45678"
	backgroundDelete.Host = "127.0.0.1:17860"
	backgroundDeleteRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(backgroundDeleteRecorder, backgroundDelete)
	if backgroundDeleteRecorder.Code != http.StatusOK || !strings.Contains(backgroundDeleteRecorder.Body.String(), `"hasBackgroundImage":false`) {
		t.Fatalf("appearance background delete failed: %d %s", backgroundDeleteRecorder.Code, backgroundDeleteRecorder.Body.String())
	}
}

func TestProjectFolderAPIOrganizesProjects(t *testing.T) {
	server := newTestServer(t)
	projects := server.app.Projects()
	if len(projects) == 0 {
		t.Fatal("expected initial project")
	}

	create := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/project-folders", bytes.NewBufferString(`{"name":"客户项目/2026"}`))
	create.RemoteAddr = "127.0.0.1:45678"
	create.Host = "127.0.0.1:17860"
	create.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated || !strings.Contains(createRecorder.Body.String(), "客户项目/2026") {
		t.Fatalf("create project folder failed: %d %s", createRecorder.Code, createRecorder.Body.String())
	}

	assign := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/projects/"+projects[0].ID, bytes.NewBufferString(`{"folder":"客户项目/2026"}`))
	assign.SetPathValue("id", projects[0].ID)
	assign.RemoteAddr = "127.0.0.1:45678"
	assign.Host = "127.0.0.1:17860"
	assign.Header.Set("Content-Type", "application/json")
	assignRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(assignRecorder, assign)
	if assignRecorder.Code != http.StatusOK || !strings.Contains(assignRecorder.Body.String(), `"folder":"客户项目/2026"`) {
		t.Fatalf("assign project folder failed: %d %s", assignRecorder.Code, assignRecorder.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/project-folders", nil)
	list.RemoteAddr = "127.0.0.1:45678"
	list.Host = "127.0.0.1:17860"
	listRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), "客户项目/2026") {
		t.Fatalf("list project folders failed: %d %s", listRecorder.Code, listRecorder.Body.String())
	}

	batchMove := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1/api/project-folders", bytes.NewBufferString(`{"projectIds":["`+projects[0].ID+`"],"folder":""}`))
	batchMove.RemoteAddr = "127.0.0.1:45678"
	batchMove.Host = "127.0.0.1:17860"
	batchMove.Header.Set("Content-Type", "application/json")
	batchMoveRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(batchMoveRecorder, batchMove)
	if batchMoveRecorder.Code != http.StatusOK || strings.Contains(batchMoveRecorder.Body.String(), `"folder":"客户项目/2026"`) {
		t.Fatalf("batch move project folder failed: %d %s", batchMoveRecorder.Code, batchMoveRecorder.Body.String())
	}

	deleteFolder := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/project-folders", bytes.NewBufferString(`{"name":"客户项目/2026"}`))
	deleteFolder.RemoteAddr = "127.0.0.1:45678"
	deleteFolder.Host = "127.0.0.1:17860"
	deleteFolder.Header.Set("Content-Type", "application/json")
	deleteFolderRecorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(deleteFolderRecorder, deleteFolder)
	if deleteFolderRecorder.Code != http.StatusOK {
		t.Fatalf("delete project folder failed: %d %s", deleteFolderRecorder.Code, deleteFolderRecorder.Body.String())
	}
	if len(server.app.ProjectFolders()) != 0 {
		t.Fatalf("deleted folder remained in application state: %#v", server.app.ProjectFolders())
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
