package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type WebControlStatus struct {
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	URL       string `json:"url"`
	LastError string `json:"lastError,omitempty"`
}

// ControlServer exposes the existing local management UI on an optional
// second loopback port. The first release intentionally remains local-only;
// LAN/public binding can be added later together with dedicated authentication.
type ControlServer struct {
	mu        sync.RWMutex
	handler   http.Handler
	server    *http.Server
	port      int
	lastError string
}

func NewControlServer(handler http.Handler) *ControlServer {
	return &ControlServer{handler: handler}
}

func (c *ControlServer) Apply(enabled bool, port int) error {
	if !enabled {
		c.mu.Lock()
		old := c.server
		c.server = nil
		c.port = port
		c.lastError = ""
		c.mu.Unlock()
		shutdownHTTPServerAsync(old)
		return nil
	}

	c.mu.RLock()
	if c.server != nil && c.port == port {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		c.mu.Lock()
		c.lastError = err.Error()
		c.mu.Unlock()
		return fmt.Errorf("start web control on %s: %w", address, err)
	}

	next := &http.Server{
		Addr:              address,
		Handler:           c.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	c.mu.Lock()
	old := c.server
	c.server = next
	c.port = port
	c.lastError = ""
	c.mu.Unlock()

	go func() {
		err := next.Serve(listener)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		c.mu.Lock()
		if c.server == next {
			c.server = nil
			c.lastError = err.Error()
		}
		c.mu.Unlock()
	}()
	shutdownHTTPServerAsync(old)
	return nil
}

func (c *ControlServer) Status(enabled bool, configuredPort int) WebControlStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	port := configuredPort
	if c.port != 0 {
		port = c.port
	}
	status := WebControlStatus{
		Enabled:   enabled,
		Running:   c.server != nil,
		Port:      configuredPort,
		LastError: c.lastError,
	}
	if enabled && port > 0 {
		status.URL = fmt.Sprintf("http://127.0.0.1:%d/#/control", port)
	}
	return status
}

func (c *ControlServer) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	server := c.server
	c.server = nil
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
