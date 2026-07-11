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
}

type DesktopController interface {
	Open() error
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

	s := &Server{app: app, desktop: desktop}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)
	mux.HandleFunc("POST /api/services/start", s.handleStartServices)
	mux.HandleFunc("POST /api/services/stop", s.handleStopServices)
	mux.HandleFunc("POST /api/services/restart", s.handleRestartServices)
	mux.HandleFunc("POST /api/services/takeover", s.handleTakeoverServices)
	mux.HandleFunc("POST /api/services/change-port", s.handleChangeMCPPort)
	mux.HandleFunc("POST /api/cloudflare/login", s.handleCloudflareLogin)
	mux.HandleFunc("POST /api/cloudflare/configure", s.handleCloudflareConfigure)
	mux.HandleFunc("GET /api/tunnels/processes", s.handleTunnelProcesses)
	mux.HandleFunc("DELETE /api/tunnels/processes/{pid}", s.handleStopTunnelProcess)
	mux.HandleFunc("POST /api/tunnels/sync-port", s.handleSyncTunnelPort)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("GET /api/secrets", s.handleSecrets)
	mux.HandleFunc("GET /api/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("GET /api/system/desktop", s.handleDesktopStatus)
	mux.HandleFunc("PUT /api/system/startup", s.handleStartup)
	mux.HandleFunc("POST /api/ui/open", s.handleOpenUI)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.Handle("/", noCache(http.FileServer(http.FS(assets))))

	s.server = &http.Server{
		Addr:              address,
		Handler:           s.securityHeaders(s.localOnly(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	return s, nil
}

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

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var update model.ConfigUpdate
	if err := decodeJSON(r, &update); err != nil {
		writeError(w, http.StatusBadRequest, err)
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

func (s *Server) handleDiagnostics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Diagnostics())
}

func (s *Server) handleDesktopStatus(w http.ResponseWriter, _ *http.Request) {
	if s.desktop == nil {
		writeJSON(w, http.StatusOK, model.DesktopStatus{Available: false, WindowModeLabel: "桌面集成不可用"})
		return
	}
	writeJSON(w, http.StatusOK, s.desktop.Status())
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
