package application

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-devdesk/internal/config"
	"mcp-devdesk/internal/model"
	processmanager "mcp-devdesk/internal/process"
	"mcp-devdesk/internal/secrets"
	"mcp-devdesk/internal/tunnel"
)

const Version = "0.1.0-dev"

type App struct {
	rootDir string
	dataDir string
	config  *config.Store
	secrets *secrets.Store
	process *processmanager.Manager
	tunnel  *tunnel.Client

	mu             sync.RWMutex
	desiredRunning bool
	watchdogCancel context.CancelFunc
}

func New(rootDir, dataDir string) (*App, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o700); err != nil {
		return nil, err
	}

	configStore, err := config.NewStore(rootDir, dataDir)
	if err != nil {
		return nil, err
	}
	secretStore := secrets.NewStore(dataDir)
	app := &App{
		rootDir: rootDir,
		dataDir: dataDir,
		config:  configStore,
		secrets: secretStore,
		process: processmanager.NewManager(rootDir, dataDir, secretStore),
		tunnel:  tunnel.NewClient(),
	}
	app.startWatchdog()
	return app, nil
}

func (a *App) RootDir() string { return a.rootDir }
func (a *App) DataDir() string { return a.dataDir }

func (a *App) Config() model.PublicConfig {
	return a.config.Get().Public()
}

func (a *App) UpdateConfig(update model.ConfigUpdate) (model.PublicConfig, error) {
	cfg, err := a.config.Update(update)
	if err != nil {
		return model.PublicConfig{}, err
	}
	return cfg.Public(), nil
}

func (a *App) Status() model.ServiceStatus {
	cfg := a.config.Get()
	mcp, tunnelStatus, login := a.process.Status()
	cf := a.tunnel.Status(cfg, login)
	adminURL := "http://" + cfg.AdminHost + ":" + strconv.Itoa(cfg.AdminPort)
	localMCPURL := "http://" + cfg.MCPHost + ":" + strconv.Itoa(cfg.MCPPort) + "/mcp"
	remoteMCPURL := ""
	authorizeURL := ""
	if cfg.Domain != "" {
		remoteMCPURL = "https://" + cfg.Domain + "/mcp"
		authorizeURL = "https://" + cfg.Domain + "/oauth/authorize"
	}
	ok, message := a.configurationStatus(cfg)

	return model.ServiceStatus{
		Version:          Version,
		RootDirectory:    a.rootDir,
		DataDirectory:    a.dataDir,
		AdminURL:         adminURL,
		LocalMCPURL:      localMCPURL,
		RemoteMCPURL:     remoteMCPURL,
		AuthorizeURL:     authorizeURL,
		MCP:              mcp,
		Tunnel:           tunnelStatus,
		Cloudflare:       cf,
		PermissionMode:   cfg.PermissionMode,
		FileScope:        cfg.FileScope,
		AllowNetwork:     cfg.AllowNetwork,
		WatchdogEnabled:  cfg.Watchdog,
		ConfigurationOK:  ok,
		ConfigurationMsg: message,
	}
}

func (a *App) StartServices(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.desiredRunning = true

	cfg := a.config.Get()
	mcp, tunnelStatus, _ := a.process.Status()
	if !mcp.Running {
		if err := a.process.StartMCP(cfg); err != nil {
			a.desiredRunning = false
			return err
		}
		if err := waitForPort(ctx, cfg.MCPHost, cfg.MCPPort, 15*time.Second); err != nil {
			_ = a.process.StopMCP()
			a.desiredRunning = false
			return fmt.Errorf("MCP 服务未能监听端口: %w", err)
		}
	}

	if cfg.Domain != "" && cfg.TunnelID != "" && !tunnelStatus.Running {
		if err := a.process.StartTunnel(cfg); err != nil {
			return fmt.Errorf("MCP 已启动，但 Tunnel 启动失败: %w", err)
		}
	}
	return nil
}

func (a *App) StopServices() error {
	a.mu.Lock()
	a.desiredRunning = false
	a.mu.Unlock()
	return a.process.StopAll()
}

func (a *App) RestartServices(ctx context.Context) error {
	if err := a.StopServices(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(600 * time.Millisecond):
	}
	return a.StartServices(ctx)
}

func (a *App) StartCloudflareLogin() error {
	cfg := a.config.Get()
	_, _, login := a.process.Status()
	if login.Running {
		return errors.New("Cloudflare 登录正在进行中")
	}
	return a.process.StartCloudflareLogin(cfg)
}

func (a *App) ConfigureTunnel(ctx context.Context, request model.ConfigureTunnelRequest) (model.ConfigureTunnelResult, error) {
	cfg := a.config.Get()
	result, err := a.tunnel.Configure(ctx, cfg, request)
	if err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	cfg.Domain = result.Domain
	cfg.TunnelName = result.TunnelName
	cfg.TunnelID = result.TunnelID
	if _, err := a.config.Replace(cfg); err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	return result, nil
}

func (a *App) Secrets(reveal bool) (model.SecretSummary, error) {
	return a.secrets.Summary(reveal)
}

func (a *App) Logs(name string, maxLines int) (model.LogResponse, error) {
	if maxLines <= 0 || maxLines > 2000 {
		maxLines = 300
	}
	paths := map[string]string{
		"mcp-out":      filepath.Join(a.dataDir, "logs", "mcp-stdout.log"),
		"mcp-error":    filepath.Join(a.dataDir, "logs", "mcp-stderr.log"),
		"tunnel-out":   filepath.Join(a.dataDir, "logs", "tunnel-stdout.log"),
		"tunnel-error": filepath.Join(a.dataDir, "logs", "tunnel-stderr.log"),
		"login":        filepath.Join(a.dataDir, "logs", "cloudflare-login.log"),
		"login-error":  filepath.Join(a.dataDir, "logs", "cloudflare-login-error.log"),
		"watchdog":     filepath.Join(a.dataDir, "logs", "watchdog.log"),
	}
	path, ok := paths[name]
	if !ok {
		return model.LogResponse{}, errors.New("unknown log name")
	}
	lines, truncated, err := tailLines(path, maxLines)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.LogResponse{}, err
	}
	return model.LogResponse{Name: name, Path: path, Lines: lines, Truncated: truncated}, nil
}

func (a *App) Diagnostics() map[string]any {
	cfg := a.config.Get()
	result := map[string]any{
		"version":                 Version,
		"rootDirectory":           a.rootDir,
		"dataDirectory":           a.dataDir,
		"workspaceExists":         pathIsDirectory(cfg.Workspace),
		"coreExists":              pathIsFile(cfg.CoreExecutable),
		"cloudflaredExists":       pathIsFile(cfg.CloudflaredExecutable),
		"cloudflareAuthenticated": pathIsFile(processmanager.CertificatePath()),
		"credentialsExist":        cfg.TunnelID != "" && pathIsFile(processmanager.CredentialsPath(cfg.TunnelID)),
		"mcpPortAvailable":        portAvailable(cfg.MCPHost, cfg.MCPPort),
		"adminHostLoopback":       isLoopbackHost(cfg.AdminHost),
	}
	return result
}

func (a *App) Close() error {
	if a.watchdogCancel != nil {
		a.watchdogCancel()
	}
	return a.StopServices()
}

func (a *App) configurationStatus(cfg model.Config) (bool, string) {
	var problems []string
	if !pathIsDirectory(cfg.Workspace) {
		problems = append(problems, "工作区不存在")
	}
	if !pathIsFile(cfg.CoreExecutable) {
		problems = append(problems, "MCP 核心程序不存在")
	}
	if !pathIsFile(cfg.CloudflaredExecutable) {
		problems = append(problems, "cloudflared.exe 不存在")
	}
	if cfg.Domain != "" && cfg.TunnelID == "" {
		problems = append(problems, "已填写域名但尚未配置 Tunnel")
	}
	if len(problems) == 0 {
		return true, "配置完整"
	}
	return false, strings.Join(problems, "；")
}

func (a *App) startWatchdog() {
	ctx, cancel := context.WithCancel(context.Background())
	a.watchdogCancel = cancel
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.watchdogTick(ctx)
			}
		}
	}()
}

func (a *App) watchdogTick(ctx context.Context) {
	cfg := a.config.Get()
	if !cfg.Watchdog {
		return
	}
	a.mu.RLock()
	desired := a.desiredRunning
	a.mu.RUnlock()
	if !desired {
		return
	}

	mcp, tunnelStatus, _ := a.process.Status()
	if !mcp.Running {
		a.logWatchdog("MCP 进程退出，正在重启")
		if err := a.process.StartMCP(cfg); err != nil {
			a.logWatchdog("MCP 重启失败: " + err.Error())
			return
		}
		if err := waitForPort(ctx, cfg.MCPHost, cfg.MCPPort, 12*time.Second); err != nil {
			a.logWatchdog("MCP 端口检测失败: " + err.Error())
			return
		}
	}
	if cfg.Domain != "" && cfg.TunnelID != "" && !tunnelStatus.Running {
		a.logWatchdog("Tunnel 进程退出，正在重启")
		if err := a.process.StartTunnel(cfg); err != nil {
			a.logWatchdog("Tunnel 重启失败: " + err.Error())
		}
	}
}

func (a *App) logWatchdog(message string) {
	path := filepath.Join(a.dataDir, "logs", "watchdog.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "[%s] %s\n", time.Now().Format(time.RFC3339), message)
}

func waitForPort(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(host, strconv.Itoa(port))
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout waiting for %s", address)
}

func tailLines(path string, limit int) ([]string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	total := 0
	for scanner.Scan() {
		total++
		if len(lines) < limit {
			lines = append(lines, scanner.Text())
			continue
		}
		copy(lines, lines[1:])
		lines[len(lines)-1] = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	return lines, total > limit, nil
}

func pathIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func portAvailable(host string, port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func SortedDiagnosticKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
