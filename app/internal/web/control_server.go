package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-devdesk/internal/application"
)

const (
	webControlSessionCookie = "mcp_devdesk_web_session"
	webControlSessionTTL    = 12 * time.Hour
	webControlLoginWindow   = 5 * time.Minute
	webControlLoginBlock    = 10 * time.Minute
	webControlLoginMaxFails = 6
)

type webControlLoginAttempt struct {
	WindowStarted time.Time
	Failures      int
	BlockedUntil  time.Time
}

type WebControlStatus struct {
	Enabled            bool     `json:"enabled"`
	Running            bool     `json:"running"`
	Port               int      `json:"port"`
	LANEnabled         bool     `json:"lanEnabled"`
	AuthEnabled        bool     `json:"authEnabled"`
	PasswordConfigured bool     `json:"passwordConfigured"`
	URL                string   `json:"url"`
	LANURLs            []string `json:"lanUrls,omitempty"`
	LastError          string   `json:"lastError,omitempty"`
}

type WebControlAuthStatus struct {
	Required      bool `json:"required"`
	Authenticated bool `json:"authenticated"`
}

// ControlServer exposes a deliberately limited browser control plane. It can
// bind to loopback only or to all IPv4 interfaces for LAN use. The internal
// 17860 management server remains loopback-only and is never reused directly.
type ControlServer struct {
	mu            sync.RWMutex
	app           *application.App
	handler       http.Handler
	server        *http.Server
	port          int
	lanEnabled    bool
	lastError     string
	sessions      map[string]time.Time
	loginAttempts map[string]webControlLoginAttempt
}

func NewControlServer(app *application.App) *ControlServer {
	return &ControlServer{
		app:           app,
		sessions:      make(map[string]time.Time),
		loginAttempts: make(map[string]webControlLoginAttempt),
	}
}

func (c *ControlServer) SetHandler(handler http.Handler) {
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
}

func (c *ControlServer) Apply(enabled bool, port int, lanEnabled bool) error {
	if !enabled {
		c.mu.Lock()
		old := c.server
		c.server = nil
		c.port = port
		c.lanEnabled = lanEnabled
		c.lastError = ""
		c.sessions = make(map[string]time.Time)
		c.loginAttempts = make(map[string]webControlLoginAttempt)
		c.mu.Unlock()
		shutdownHTTPServerAsync(old)
		return nil
	}

	c.mu.RLock()
	old := c.server
	oldPort := c.port
	oldLANEnabled := c.lanEnabled
	if old != nil && oldPort == port && oldLANEnabled == lanEnabled {
		c.mu.RUnlock()
		return nil
	}
	handler := c.handler
	c.mu.RUnlock()
	if handler == nil {
		return errors.New("web control handler is not configured")
	}

	rebindingSamePort := old != nil && oldPort == port
	if rebindingSamePort {
		_ = old.Close()
		c.mu.Lock()
		if c.server == old {
			c.server = nil
		}
		c.mu.Unlock()
	}

	address := webControlBindAddress(port, lanEnabled)
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		if rebindingSamePort {
			c.restoreListener(handler, oldPort, oldLANEnabled)
		}
		c.mu.Lock()
		c.lastError = err.Error()
		c.mu.Unlock()
		return fmt.Errorf("start web control on %s: %w", address, err)
	}

	next := newWebControlHTTPServer(address, handler)

	c.mu.Lock()
	c.server = next
	c.port = port
	c.lanEnabled = lanEnabled
	c.lastError = ""
	c.sessions = make(map[string]time.Time)
	c.loginAttempts = make(map[string]webControlLoginAttempt)
	c.mu.Unlock()

	c.serve(next, listener)
	if !rebindingSamePort {
		shutdownHTTPServerAsync(old)
	}
	return nil
}

func webControlBindAddress(port int, lanEnabled bool) string {
	host := "127.0.0.1"
	if lanEnabled {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func newWebControlHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
}

func (c *ControlServer) serve(server *http.Server, listener net.Listener) {
	go func() {
		err := server.Serve(listener)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		c.mu.Lock()
		if c.server == server {
			c.server = nil
			c.lastError = err.Error()
		}
		c.mu.Unlock()
	}()
}

func (c *ControlServer) restoreListener(handler http.Handler, port int, lanEnabled bool) {
	address := webControlBindAddress(port, lanEnabled)
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return
	}
	server := newWebControlHTTPServer(address, handler)
	c.mu.Lock()
	c.server = server
	c.port = port
	c.lanEnabled = lanEnabled
	c.mu.Unlock()
	c.serve(server, listener)
}

func (c *ControlServer) Status(enabled bool, configuredPort int, lanEnabled bool, authEnabled bool) WebControlStatus {
	c.mu.RLock()
	running := c.server != nil
	lastError := c.lastError
	c.mu.RUnlock()

	status := WebControlStatus{
		Enabled:            enabled,
		Running:            running,
		Port:               configuredPort,
		LANEnabled:         lanEnabled,
		AuthEnabled:        authEnabled,
		PasswordConfigured: c.app.WebControlPasswordConfigured(),
		LastError:          lastError,
	}
	if enabled && configuredPort > 0 {
		status.URL = fmt.Sprintf("http://127.0.0.1:%d/#/", configuredPort)
		if lanEnabled {
			status.LANURLs = webControlLANURLs(configuredPort)
		}
	}
	return status
}

func (c *ControlServer) AuthStatus(r *http.Request) WebControlAuthStatus {
	required := c.app.Config().WebControlAuthEnabled
	return WebControlAuthStatus{
		Required:      required,
		Authenticated: !required || c.authenticated(r),
	}
}

func (c *ControlServer) Login(w http.ResponseWriter, r *http.Request, password string) (bool, time.Duration) {
	if !c.app.Config().WebControlAuthEnabled {
		return true, 0
	}
	key := webControlLoginKey(r.RemoteAddr)
	now := time.Now()
	if retryAfter := c.loginRetryAfter(key, now); retryAfter > 0 {
		return false, retryAfter
	}
	if !c.app.VerifyWebControlPassword(password) {
		return false, c.recordLoginFailure(key, now)
	}
	c.clearLoginFailures(key)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return false, 0
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expires := now.Add(webControlSessionTTL)
	c.mu.Lock()
	c.pruneSessionsLocked(now)
	c.sessions[token] = expires
	c.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     webControlSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
		MaxAge:   int(webControlSessionTTL.Seconds()),
	})
	return true, 0
}

func webControlLoginKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return strings.Trim(host, "[]")
}

func (c *ControlServer) loginRetryAfter(key string, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt, ok := c.loginAttempts[key]
	if !ok {
		return 0
	}
	if attempt.BlockedUntil.After(now) {
		return attempt.BlockedUntil.Sub(now)
	}
	if now.Sub(attempt.WindowStarted) >= webControlLoginWindow {
		delete(c.loginAttempts, key)
	}
	return 0
}

func (c *ControlServer) recordLoginFailure(key string, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt := c.loginAttempts[key]
	if attempt.WindowStarted.IsZero() || now.Sub(attempt.WindowStarted) >= webControlLoginWindow {
		attempt = webControlLoginAttempt{WindowStarted: now}
	}
	attempt.Failures++
	if attempt.Failures >= webControlLoginMaxFails {
		attempt.BlockedUntil = now.Add(webControlLoginBlock)
	}
	c.loginAttempts[key] = attempt
	if attempt.BlockedUntil.After(now) {
		return attempt.BlockedUntil.Sub(now)
	}
	return 0
}

func (c *ControlServer) clearLoginFailures(key string) {
	c.mu.Lock()
	delete(c.loginAttempts, key)
	c.mu.Unlock()
}

func (c *ControlServer) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(webControlSessionCookie); err == nil {
		c.mu.Lock()
		delete(c.sessions, cookie.Value)
		c.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webControlSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (c *ControlServer) InvalidateSessions() {
	c.mu.Lock()
	c.sessions = make(map[string]time.Time)
	c.loginAttempts = make(map[string]webControlLoginAttempt)
	c.mu.Unlock()
}

func (c *ControlServer) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(webControlSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneSessionsLocked(now)
	expires, ok := c.sessions[cookie.Value]
	return ok && expires.After(now)
}

func (c *ControlServer) pruneSessionsLocked(now time.Time) {
	for token, expires := range c.sessions {
		if !expires.After(now) {
			delete(c.sessions, token)
		}
	}
}

func webControlLANURLs(port int) []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	urls := make([]string, 0, 4)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() || !ip.IsPrivate() {
				continue
			}
			url := fmt.Sprintf("http://%s:%d/#/", ip.String(), port)
			if _, ok := seen[url]; ok {
				continue
			}
			seen[url] = struct{}{}
			urls = append(urls, url)
		}
	}
	sort.Strings(urls)
	return urls
}

func (c *ControlServer) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	server := c.server
	c.server = nil
	c.sessions = make(map[string]time.Time)
	c.loginAttempts = make(map[string]webControlLoginAttempt)
	c.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func shutdownHTTPServerAsync(server *http.Server) {
	if server == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
}
