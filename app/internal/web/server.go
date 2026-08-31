package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/model"
)

//go:embed static
var staticFiles embed.FS

type Server struct {
	app     *application.App
	desktop DesktopController
	server  *http.Server
	handler http.Handler
	appMux  http.Handler
	control *ControlServer
	events  *stateEventHub
}

type DesktopController interface {
	Open() error
	PickFolder(initialPath, title string) (path string, canceled bool, err error)
	Status() model.DesktopStatus
	SetStartup(enabled bool) error
}

func New(app *application.App, address string) (*Server, error) {
	return NewWithDesktop(app, address, nil)
}

func NewWithDesktop(app *application.App, address string, desktop DesktopController) (*Server, error) {
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}

	s := &Server{app: app, desktop: desktop, events: newStateEventHub()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.handleStateEvents)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("GET /api/project-folders", s.handleListProjectFolders)
	mux.HandleFunc("POST /api/project-folders", s.handleAddProjectFolder)
	mux.HandleFunc("PATCH /api/project-folders", s.handleMoveProjectsToFolder)
	mux.HandleFunc("DELETE /api/project-folders", s.handleDeleteProjectFolder)
	mux.HandleFunc("GET /api/projects/prompt-settings", s.handleProjectPromptSettings)
	mux.HandleFunc("PUT /api/projects/prompt-settings", s.handleUpdateProjectPromptSettings)
	mux.HandleFunc("POST /api/projects", s.handleAddProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.handleUpdateProject)
	mux.HandleFunc("POST /api/projects/{id}/activate", s.handleActivateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleRemoveProject)
	mux.HandleFunc("GET /api/projects/{id}/details", s.handleProjectDetails)
	mux.HandleFunc("GET /api/projects/{id}/diff", s.handleProjectDiff)
	mux.HandleFunc("GET /api/projects/{id}/git/history", s.handleProjectHistory)
	mux.HandleFunc("POST /api/projects/{id}/git/rollback", s.handleProjectRollback)
	mux.HandleFunc("POST /api/projects/{id}/worktrees", s.handleCreateWorktree)
	mux.HandleFunc("DELETE /api/projects/{id}/worktrees", s.handleRemoveWorktree)
	mux.HandleFunc("GET /api/instances", s.handleListInstances)
	mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	mux.HandleFunc("GET /api/instances/{id}", s.handleGetInstance)
	mux.HandleFunc("PATCH /api/instances/{id}", s.handleUpdateInstance)
	mux.HandleFunc("POST /api/instances/{id}/clone", s.handleCloneInstance)
	mux.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)
	mux.HandleFunc("POST /api/instances/{id}/start", s.handleStartInstance)
	mux.HandleFunc("POST /api/instances/{id}/stop", s.handleStopInstance)
	mux.HandleFunc("POST /api/instances/{id}/restart", s.handleRestartInstance)
	mux.HandleFunc("POST /api/instances/{id}/cloudflare/configure", s.handleConfigureInstanceTunnel)
	mux.HandleFunc("GET /api/instances/{id}/logs", s.handleInstanceLogs)
	mux.HandleFunc("POST /api/services/start", s.handleStartServices)
	mux.HandleFunc("POST /api/services/stop", s.handleStopServices)
	mux.HandleFunc("POST /api/services/restart", s.handleRestartServices)
	mux.HandleFunc("POST /api/services/takeover", s.handleTakeoverServices)
	mux.HandleFunc("POST /api/services/change-port", s.handleChangeMCPPort)
	mux.HandleFunc("POST /api/services/change-workspace", s.handleChangeWorkspace)
	mux.HandleFunc("POST /api/cloudflare/login", s.handleCloudflareLogin)
	mux.HandleFunc("POST /api/cloudflare/configure", s.handleCloudflareConfigure)
	mux.HandleFunc("GET /api/tunnels/processes", s.handleTunnelProcesses)
	mux.HandleFunc("DELETE /api/tunnels/processes/{pid}", s.handleStopTunnelProcess)
	mux.HandleFunc("POST /api/tunnels/sync-port", s.handleSyncTunnelPort)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("GET /api/secrets", s.handleSecrets)
	mux.HandleFunc("PUT /api/secrets", s.handleUpdateSecrets)
	mux.HandleFunc("POST /api/secrets/generate", s.handleGenerateSecret)
	mux.HandleFunc("GET /api/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("GET /api/diagnostics/export", s.handleExportDiagnostics)
	mux.HandleFunc("GET /api/system/desktop", s.handleDesktopStatus)
	mux.HandleFunc("POST /api/system/pick-folder", s.handlePickFolder)
	mux.HandleFunc("PUT /api/system/startup", s.handleStartup)
	mux.HandleFunc("POST /api/ui/open", s.handleOpenUI)
	mux.HandleFunc("GET /api/web-control", s.handleWebControlStatus)
	mux.HandleFunc("PUT /api/web-control", s.handleUpdateWebControl)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.Handle("/", noCache(http.FileServer(http.FS(assets))))

	sharedHandler := s.publishSuccessfulMutations(mux)
	s.appMux = sharedHandler
	s.handler = s.securityHeaders(s.localOnly(sharedHandler))
	s.server = &http.Server{
		Addr:              address,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) SetControlServer(control *ControlServer) { s.control = control }

func (s *Server) ListenAndServe() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Config())
}

func (s *Server) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Projects())
}

func (s *Server) handleListProjectFolders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ProjectFolders())
}

func (s *Server) handleAddProjectFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	folder, err := s.app.AddProjectFolder(request.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": folder})
}

func (s *Server) handleMoveProjectsToFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProjectIDs []string `json:"projectIds"`
		Folder     string   `json:"folder"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	projects, err := s.app.UpdateProjectsFolder(request.ProjectIDs, request.Folder)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleDeleteProjectFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.RemoveProjectFolder(request.Name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "name": request.Name})
}

func (s *Server) handleProjectPromptSettings(w http.ResponseWriter, _ *http.Request) {
	settings := s.app.ProjectPromptSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        settings.Enabled,
		"globalPrompt":   settings.GlobalPrompt,
		"maxPromptBytes": 32 * 1024,
	})
}

func (s *Server) handleUpdateProjectPromptSettings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled      bool   `json:"enabled"`
		GlobalPrompt string `json:"globalPrompt"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settings, err := s.app.UpdateGlobalProjectPrompt(request.Enabled, request.GlobalPrompt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        settings.Enabled,
		"globalPrompt":   settings.GlobalPrompt,
		"maxPromptBytes": 32 * 1024,
	})
}

func (s *Server) handleAddProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := s.app.AddProject(request.Name, request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path   *string `json:"path"`
		Folder *string `json:"folder"`
		Prompt *string `json:"prompt"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Path == nil && request.Folder == nil && request.Prompt == nil {
		writeError(w, http.StatusBadRequest, errors.New("path, folder or prompt is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	projectID := r.PathValue("id")
	var project any
	if request.Path != nil {
		updated, err := s.app.UpdateProjectPath(ctx, projectID, *request.Path)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		projectID = updated.ID
		project = updated
	}
	if request.Folder != nil {
		updated, err := s.app.UpdateProjectFolder(projectID, *request.Folder)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		project = updated
	}
	if request.Prompt != nil {
		updated, err := s.app.UpdateProjectPrompt(projectID, *request.Prompt)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		project = updated
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleActivateProject(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := s.app.SwitchProject(ctx, r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleChangeWorkspace(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := s.app.SwitchWorkspace(ctx, request.Path); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleRemoveProject(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := s.app.RemoveProject(ctx, r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProjectDetails(w http.ResponseWriter, r *http.Request) {
	details, err := s.app.ProjectDetails(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) handleProjectDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := s.app.ProjectDiff(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

func (s *Server) handleProjectHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	history, err := s.app.ProjectHistory(r.PathValue("id"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleProjectRollback(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Commit string `json:"commit"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.app.RollbackProject(r.PathValue("id"), request.Commit)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCreateWorktree(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
		Base   string `json:"base"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.CreateWorktree(r.PathValue("id"), request.Path, request.Branch, request.Base); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	details, _ := s.app.ProjectDetails(r.PathValue("id"))
	writeJSON(w, http.StatusCreated, details)
}

func (s *Server) handleRemoveWorktree(w http.ResponseWriter, r *http.Request) {
	if err := s.app.RemoveWorktree(r.PathValue("id"), r.URL.Query().Get("path")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListInstances(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Instances())
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var request model.MCPInstanceCreateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	instance, err := s.app.CreateInstance(ctx, request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, instance)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	instance, err := s.app.Instance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	var request model.MCPInstanceUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()
	instance, err := s.app.UpdateInstance(ctx, r.PathValue("id"), request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleCloneInstance(w http.ResponseWriter, r *http.Request) {
	var request model.MCPInstanceCloneRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	instance, err := s.app.CloneInstance(ctx, r.PathValue("id"), request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, instance)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteInstance(r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartInstance(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	instance, err := s.app.StartInstance(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleStopInstance(w http.ResponseWriter, r *http.Request) {
	instance, err := s.app.StopInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleRestartInstance(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	instance, err := s.app.RestartInstance(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (s *Server) handleConfigureInstanceTunnel(w http.ResponseWriter, r *http.Request) {
	var request model.ConfigureTunnelRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.app.ConfigureInstanceTunnel(ctx, r.PathValue("id"), request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleInstanceLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.app.InstanceLogs(r.PathValue("id"), r.URL.Query().Get("name"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var update model.ConfigUpdate
	if err := decodeJSON(r, &update); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if update.WebControlEnabled != nil || update.WebControlPort != nil || update.WebControlLANEnabled != nil || update.WebControlAuthEnabled != nil {
		writeError(w, http.StatusBadRequest, errors.New("web control settings must be updated through /api/web-control"))
		return
	}
	requestedPort := update.MCPPort
	update.MCPPort = nil
	cfg, err := s.app.UpdateConfig(update)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if requestedPort != nil && *requestedPort != cfg.MCPPort {
		ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
		defer cancel()
		if err := s.app.ChangeMCPPort(ctx, *requestedPort); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		cfg = s.app.Config()
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleStartServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := s.app.StartServices(ctx); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleStopServices(w http.ResponseWriter, _ *http.Request) {
	if err := s.app.StopServices(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleRestartServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.app.RestartServices(ctx); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleTakeoverServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := s.app.TakeoverAndStart(ctx); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleChangeMCPPort(w http.ResponseWriter, r *http.Request) {
	var request model.ChangeMCPPortRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()
	if err := s.app.ChangeMCPPort(ctx, request.Port); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleCloudflareLogin(w http.ResponseWriter, _ *http.Request) {
	if err := s.app.StartCloudflareLogin(); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"message": "浏览器授权已启动，请在 Cloudflare 页面完成登录",
	})
}

func (s *Server) handleCloudflareConfigure(w http.ResponseWriter, r *http.Request) {
	var request model.ConfigureTunnelRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.app.ConfigureTunnel(ctx, request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTunnelProcesses(w http.ResponseWriter, _ *http.Request) {
	inventory, err := s.app.TunnelInventory()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, inventory)
}

func (s *Server) handleStopTunnelProcess(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil || pid <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid tunnel PID"))
		return
	}
	inventory, err := s.app.StopTunnelProcess(pid)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, inventory)
}

func (s *Server) handleSyncTunnelPort(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := s.app.SyncTunnelPort(ctx); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.app.Logs(name, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSecrets(w http.ResponseWriter, r *http.Request) {
	reveal := strings.EqualFold(r.URL.Query().Get("reveal"), "true")
	result, err := s.app.Secrets(reveal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGenerateSecret(w http.ResponseWriter, r *http.Request) {
	var request model.SecretGenerateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.app.GenerateSecret(request.Field)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpdateSecrets(w http.ResponseWriter, r *http.Request) {
	var request model.SecretUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	result, err := s.app.UpdateSecrets(ctx, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Diagnostics())
}

var diagnosticSecretPattern = regexp.MustCompile(`(?i)(authorization\s*["']?\s*[:=]\s*["']?bearer\s+|(?:client[_-]?secret|owner[_-]?password|proxy[_-]?password|token[_-]?secret|access[_-]?token|refresh[_-]?token|password|token)\s*["']?\s*[:=]\s*["']?)([^"'\s&;,}]+)`)

func (s *Server) handleExportDiagnostics(w http.ResponseWriter, _ *http.Request) {
	report := map[string]any{
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"diagnostics": s.app.Diagnostics(),
		"status":      s.app.Status(),
		"instances":   s.app.Instances(),
	}
	if s.desktop != nil {
		report["desktop"] = s.desktop.Status()
	}
	if logs, err := s.app.Logs("manager", 50); err == nil {
		redacted := make([]string, 0, len(logs.Lines))
		for _, line := range logs.Lines {
			redacted = append(redacted, redactDiagnosticLine(line))
		}
		report["managerLog"] = map[string]any{
			"lines": redacted, "truncated": logs.Truncated,
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mcp-devdesk-diagnostics-%s.json"`, time.Now().Format("20060102-150405")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func redactDiagnosticLine(line string) string {
	return diagnosticSecretPattern.ReplaceAllString(line, "${1}[REDACTED]")
}

func (s *Server) handleDesktopStatus(w http.ResponseWriter, _ *http.Request) {
	if s.desktop == nil {
		writeJSON(w, http.StatusOK, model.DesktopStatus{Available: false, WindowModeLabel: "桌面集成不可用"})
		return
	}
	writeJSON(w, http.StatusOK, s.desktop.Status())
}

func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	if s.desktop == nil {
		writeError(w, http.StatusNotImplemented, errors.New("desktop folder selection is unavailable"))
		return
	}
	var request struct {
		InitialPath string `json:"initialPath"`
		Title       string `json:"title"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path, canceled, err := s.desktop.PickFolder(request.InitialPath, request.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "canceled": canceled})
}

func (s *Server) handleStartup(w http.ResponseWriter, r *http.Request) {
	if s.desktop == nil {
		writeError(w, http.StatusNotImplemented, errors.New("desktop integration is unavailable"))
		return
	}
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.desktop.SetStartup(request.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.desktop.Status())
}

func (s *Server) handleOpenUI(w http.ResponseWriter, _ *http.Request) {
	if s.desktop == nil {
		writeError(w, http.StatusNotImplemented, errors.New("desktop integration is unavailable"))
		return
	}
	if err := s.desktop.Open(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "desktop window opened"})
}

func (s *Server) handleWebControlStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.app.Config()
	if s.control == nil {
		writeJSON(w, http.StatusOK, WebControlStatus{
			Enabled:            cfg.WebControlEnabled,
			Port:               cfg.WebControlPort,
			LANEnabled:         cfg.WebControlLANEnabled,
			AuthEnabled:        cfg.WebControlAuthEnabled,
			PasswordConfigured: s.app.WebControlPasswordConfigured(),
		})
		return
	}
	writeJSON(w, http.StatusOK, s.control.Status(cfg.WebControlEnabled, cfg.WebControlPort, cfg.WebControlLANEnabled, cfg.WebControlAuthEnabled))
}

func (s *Server) handleUpdateWebControl(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("web control service is unavailable"))
		return
	}
	var request struct {
		Enabled     *bool   `json:"enabled"`
		Port        *int    `json:"port"`
		LANEnabled  *bool   `json:"lanEnabled"`
		AuthEnabled *bool   `json:"authEnabled"`
		Password    *string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Enabled == nil && request.Port == nil && request.LANEnabled == nil && request.AuthEnabled == nil && request.Password == nil {
		writeError(w, http.StatusBadRequest, errors.New("web control setting is required"))
		return
	}

	previous := s.app.Config()
	enabled := previous.WebControlEnabled
	port := previous.WebControlPort
	lanEnabled := previous.WebControlLANEnabled
	authEnabled := previous.WebControlAuthEnabled
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if request.Port != nil {
		port = *request.Port
	}
	if request.LANEnabled != nil {
		lanEnabled = *request.LANEnabled
	}
	if request.AuthEnabled != nil {
		authEnabled = *request.AuthEnabled
	}
	passwordChanged := request.Password != nil && strings.TrimSpace(*request.Password) != ""
	if authEnabled && !s.app.WebControlPasswordConfigured() && !passwordChanged {
		writeError(w, http.StatusBadRequest, errors.New("启用网页密码认证前请先设置密码"))
		return
	}
	if passwordChanged {
		if err := s.app.SetWebControlPassword(*request.Password); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	updated, err := s.app.UpdateConfig(model.ConfigUpdate{
		WebControlEnabled:     &enabled,
		WebControlPort:        &port,
		WebControlLANEnabled:  &lanEnabled,
		WebControlAuthEnabled: &authEnabled,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.control.Apply(updated.WebControlEnabled, updated.WebControlPort, updated.WebControlLANEnabled); err != nil {
		rollbackEnabled := previous.WebControlEnabled
		rollbackPort := previous.WebControlPort
		rollbackLAN := previous.WebControlLANEnabled
		rollbackAuth := previous.WebControlAuthEnabled
		_, rollbackErr := s.app.UpdateConfig(model.ConfigUpdate{
			WebControlEnabled:     &rollbackEnabled,
			WebControlPort:        &rollbackPort,
			WebControlLANEnabled:  &rollbackLAN,
			WebControlAuthEnabled: &rollbackAuth,
		})
		if rollbackErr != nil {
			writeError(w, http.StatusConflict, fmt.Errorf("apply web control: %w; rollback config: %v", err, rollbackErr))
			return
		}
		writeError(w, http.StatusConflict, err)
		return
	}
	if passwordChanged || previous.WebControlAuthEnabled != updated.WebControlAuthEnabled {
		s.control.InvalidateSessions()
	}

	writeJSON(w, http.StatusOK, s.control.Status(updated.WebControlEnabled, updated.WebControlPort, updated.WebControlLANEnabled, updated.WebControlAuthEnabled))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": application.Version})
}

func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			writeError(w, http.StatusForbidden, errors.New("invalid remote address"))
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			writeError(w, http.StatusForbidden, errors.New("management API is local-only"))
			return
		}

		requestHost := r.Host
		if parsedHost, _, splitErr := net.SplitHostPort(r.Host); splitErr == nil {
			requestHost = parsedHost
		}
		requestHost = strings.Trim(requestHost, "[]")
		if requestHost != "localhost" {
			hostIP := net.ParseIP(requestHost)
			if hostIP == nil || !hostIP.IsLoopback() {
				writeError(w, http.StatusForbidden, errors.New("invalid host header"))
				return
			}
		}

		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			origin := r.Header.Get("Origin")
			if origin != "" && !localOrigin(origin) {
				writeError(w, http.StatusForbidden, errors.New("cross-origin mutation rejected"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func localOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://[::1]:")
}

func decodeJSON(r *http.Request, target any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error":   http.StatusText(status),
		"message": err.Error(),
	})
}
