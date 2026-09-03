package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"mcp-devdesk/internal/buildinfo"
	"mcp-devdesk/internal/mcpcore"
)

func main() {
	var allowedRoots stringListFlag
	workspace := flag.String("workspace", ".", "workspace exposed by the Go MCP core")
	host := flag.String("host", "127.0.0.1", "listen host")
	port := flag.Int("port", 18765, "listen port")
	permissionMode := flag.String("permission-mode", envOrDefault("CODING_TOOLS_MCP_PERMISSION_MODE", "safe"), "safe, trusted, or dangerous")
	fileScope := flag.String("file-scope", envOrDefault("CODING_TOOLS_MCP_FILE_SCOPE", "workspace"), "workspace, roots, or computer")
	flag.Var(&allowedRoots, "allowed-root", "additional allowed root directory; may be repeated")
	allowNetwork := flag.Bool("allow-network", false, "allow command sessions to use network-capable tools")
	screenCapture := flag.Bool("enable-screen-capture", false, "enable opt-in on-demand Windows screen vision tools")
	oauthMode := flag.Bool("oauth-mode", false, "enable OAuth 2.1 authorization")
	dataDir := flag.String("data-dir", "", "data directory for OAuth clients and audit logs")
	serverURL := flag.String("server-url", os.Getenv("CODING_TOOLS_MCP_SERVER_URL"), "public server base URL used for OAuth metadata")
	auditPath := flag.String("audit-path", "", "JSONL audit log path")
	loggingConfig := flag.String("logging-config", "", "MCP DevDesk config file used to enable or disable audit logging")
	instructionsFile := flag.String("instructions-file", "", "UTF-8 MCP DevDesk managed project instructions file")
	toolProfile := flag.String("tool-profile", envOrDefault("CODING_TOOLS_MCP_TOOL_PROFILE", "full"), "full, read-only, or compat-readonly-all")
	flag.Parse()

	resolvedWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatal(err)
	}
	info, err := os.Stat(resolvedWorkspace)
	if err != nil || !info.IsDir() {
		log.Fatalf("workspace is not an existing directory: %s", resolvedWorkspace)
	}
	if *port < 1024 || *port > 65535 {
		log.Fatalf("port must be between 1024 and 65535")
	}
	if ip := net.ParseIP(*host); ip == nil || !ip.IsLoopback() {
		log.Fatalf("Go core host must be a loopback IP")
	}
	resolvedDataDir := strings.TrimSpace(*dataDir)
	if resolvedDataDir == "" {
		resolvedDataDir = filepath.Join(resolvedWorkspace, ".mcp-devdesk")
	}
	if absoluteDataDir, absErr := filepath.Abs(resolvedDataDir); absErr == nil {
		resolvedDataDir = absoluteDataDir
	}
	if err := os.MkdirAll(resolvedDataDir, 0o700); err != nil {
		log.Fatalf("create data directory: %v", err)
	}
	resolvedAuditPath := strings.TrimSpace(*auditPath)
	if resolvedAuditPath == "" {
		resolvedAuditPath = filepath.Join(resolvedDataDir, "logs", "mcp-audit.jsonl")
	}
	managedInstructions := ""
	if path := strings.TrimSpace(*instructionsFile); path != "" {
		raw, readErr := os.ReadFile(path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			log.Fatalf("read instructions file: %v", readErr)
		}
		if readErr == nil {
			if len(raw) > 96*1024 {
				log.Fatalf("instructions file cannot exceed 98304 bytes")
			}
			if !utf8.Valid(raw) {
				log.Fatalf("instructions file must be valid UTF-8")
			}
			managedInstructions = strings.TrimSpace(string(raw))
		}
	}

	baseURL := strings.TrimSuffix(strings.TrimSpace(*serverURL), "/")
	if baseURL == "" {
		baseURL = "http://" + net.JoinHostPort(*host, strconv.Itoa(*port))
	}
	resourceURL := baseURL
	if !strings.HasSuffix(strings.ToLower(resourceURL), "/mcp") {
		resourceURL += "/mcp"
	}
	issuerURL := strings.TrimSuffix(baseURL, "/mcp")
	oauthOptions := mcpcore.OAuthOptions{}
	if *oauthMode {
		oauthOptions = mcpcore.OAuthOptions{
			Enabled:         true,
			Issuer:          issuerURL,
			Resource:        resourceURL,
			OwnerPassword:   os.Getenv("CODING_TOOLS_MCP_OAUTH_PASSWORD"),
			ClientID:        envOrDefault("CODING_TOOLS_MCP_OAUTH_CLIENT_ID", "mcp-devdesk"),
			ClientSecret:    os.Getenv("CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET"),
			RedirectURIs:    splitEnvLines(os.Getenv("CODING_TOOLS_MCP_OAUTH_REDIRECT_URIS")),
			TokenSecret:     os.Getenv("CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET"),
			DataDir:         resolvedDataDir,
			AccessTokenTTL:  durationFromEnv("CODING_TOOLS_MCP_ACCESS_TOKEN_TTL", time.Hour),
			RefreshTokenTTL: durationFromEnv("CODING_TOOLS_MCP_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		}
	}

	core, err := mcpcore.New(mcpcore.Options{
		Name:                    "mcp-devdesk-go-core",
		Version:                 buildinfo.Version,
		Workspace:               resolvedWorkspace,
		ManagedInstructions:     managedInstructions,
		ManagedInstructionsFile: strings.TrimSpace(*instructionsFile),
		PermissionMode:          *permissionMode,
		AllowNetwork:            *allowNetwork,
		ScreenCaptureEnabled:    *screenCapture,
		AuditPath:               resolvedAuditPath,
		LoggingConfig:           strings.TrimSpace(*loggingConfig),
		FileScope:               *fileScope,
		AllowedRoots:            append([]string(nil), allowedRoots...),
		ToolProfile:             *toolProfile,
		OAuth:                   oauthOptions,
		AllowedOrigins:          []string{issuerURL},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer core.Close()
	address := net.JoinHostPort(*host, strconv.Itoa(*port))
	server := &http.Server{
		Addr:              address,
		Handler:           logRequests(core.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Go MCP core %s listening on http://%s/mcp", buildinfo.Version, address)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case signalValue := <-signals:
		log.Printf("received %s, shutting down", signalValue)
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			log.Fatal(serveErr)
		}
		return
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		if closeErr := server.Close(); closeErr != nil {
			log.Printf("forced close failed: %v", closeErr)
		}
	}
	if err := mcpcore.NormalizeServeError(<-serverErrors); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, status, time.Since(started).Round(time.Millisecond))
	})
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*values = append(*values, value)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func splitEnvLines(value string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
