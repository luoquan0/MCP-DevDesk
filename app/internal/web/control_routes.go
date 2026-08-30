package web

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) ControlHandler() (http.Handler, error) {
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	if s.appMux == nil {
		return nil, errors.New("management routes are unavailable")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/control/auth/status", s.handleControlAuthStatus)
	mux.HandleFunc("POST /api/control/auth/login", s.handleControlLogin)
	mux.HandleFunc("POST /api/control/auth/logout", s.handleControlLogout)

	controlAPI := http.NewServeMux()
	controlAPI.HandleFunc("GET /api/control/overview", s.handleControlOverview)
	controlAPI.HandleFunc("GET /api/control/directories", s.handleControlDirectories)
	mux.Handle("/api/control/", s.controlRequireAuth(controlAPI))

	// After authentication the LAN browser uses the exact same management API
	// and Vue pages as the desktop WebView. This keeps the browser experience in
	// lock-step with the desktop app instead of maintaining a second UI surface.
	mux.Handle("/api/", s.controlRequireAuth(s.appMux))
	mux.Handle("/", noCache(http.FileServer(http.FS(assets))))

	return s.securityHeaders(s.controlNetworkPolicy(s.controlOriginPolicy(mux))), nil
}

func (s *Server) handleControlAuthStatus(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("web control service is unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, s.control.AuthStatus(r))
}

func (s *Server) handleControlLogin(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("web control service is unavailable"))
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.control.Login(w, request.Password) {
		writeError(w, http.StatusUnauthorized, errors.New("密码错误"))
		return
	}
	writeJSON(w, http.StatusOK, WebControlAuthStatus{Required: s.app.Config().WebControlAuthEnabled, Authenticated: true})
}

func (s *Server) handleControlLogout(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("web control service is unavailable"))
		return
	}
	s.control.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleControlOverview(w http.ResponseWriter, _ *http.Request) {
	cfg := s.app.Config()
	status := s.app.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       status.Version,
		"workspace":     cfg.Workspace,
		"coreMode":      cfg.CoreMode,
		"mcpPort":       cfg.MCPPort,
		"mcpRunning":    status.MCP.Running,
		"tunnelRunning": status.Tunnel.Running,
		"localMcpUrl":   status.LocalMCPURL,
		"remoteMcpUrl":  status.RemoteMCPURL,
	})
}

func (s *Server) handleControlDirectories(w http.ResponseWriter, r *http.Request) {
	listing, err := listControlDirectories(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) controlRequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.control == nil {
			writeError(w, http.StatusServiceUnavailable, errors.New("web control service is unavailable"))
			return
		}
		status := s.control.AuthStatus(r)
		if status.Required && !status.Authenticated {
			writeError(w, http.StatusUnauthorized, errors.New("请先登录网页控制台"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) controlNetworkPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			writeError(w, http.StatusForbidden, errors.New("invalid remote address"))
			return
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			writeError(w, http.StatusForbidden, errors.New("web control only accepts local or private LAN clients"))
			return
		}

		requestHost := r.Host
		if parsedHost, _, splitErr := net.SplitHostPort(r.Host); splitErr == nil {
			requestHost = parsedHost
		}
		requestHost = strings.Trim(requestHost, "[]")
		if !strings.EqualFold(requestHost, "localhost") {
			hostIP := net.ParseIP(requestHost)
			if hostIP == nil || (!hostIP.IsLoopback() && !hostIP.IsPrivate()) {
				writeError(w, http.StatusForbidden, errors.New("web control host must be a local or private IP address"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) controlOriginPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !strings.EqualFold(parsed.Host, r.Host) {
					writeError(w, http.StatusForbidden, errors.New("cross-origin mutation rejected"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
