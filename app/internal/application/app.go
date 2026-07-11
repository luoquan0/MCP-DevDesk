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

	"mcp-devdesk/internal/buildinfo"
	"mcp-devdesk/internal/config"
	"mcp-devdesk/internal/model"
	processmanager "mcp-devdesk/internal/process"
	projectstore "mcp-devdesk/internal/projects"
	"mcp-devdesk/internal/projecttools"
	"mcp-devdesk/internal/secrets"
	"mcp-devdesk/internal/tunnel"
)

const Version = buildinfo.Version

type App struct {
	rootDir  string
	dataDir  string
	config   *config.Store
	secrets  *secrets.Store
	process  *processmanager.Manager
	projects *projectstore.Store
	tunnel   *tunnel.Client

	mu             sync.RWMutex
	desiredRunning bool
	tunnelDesired  bool
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
	projectsStore, err := projectstore.NewStore(dataDir, configStore.Get().Workspace)
	if err != nil {
		return nil, err
	}
	app := &App{
		rootDir:  rootDir,
		dataDir:  dataDir,
		config:   configStore,
		secrets:  secretStore,
		process:  processmanager.NewManager(rootDir, dataDir, secretStore),
		projects: projectsStore,
		tunnel:   tunnel.NewClient(),
	}
	app.startWatchdog()
	return app, nil
}

func (a *App) RootDir() string { return a.rootDir }
func (a *App) DataDir() string { return a.dataDir }

func (a *App) Projects() []projectstore.Project { return a.projects.List() }

func (a *App) AddProject(name, path string) (projectstore.Project, error) {
	return a.projects.Add(name, path)
}

func (a *App) RemoveProject(id string) error {
	return a.projects.Remove(id, a.config.Get().Workspace)
}

func (a *App) ProjectDetails(id string) (projecttools.Details, error) {
	project, ok := a.projects.Get(id)
	if !ok {
		return projecttools.Details{}, errors.New("project not found")
	}
	return projecttools.Inspect(project.Path)
}

func (a *App) ProjectDiff(id string) (projecttools.Diff, error) {
	project, ok := a.projects.Get(id)
	if !ok {
		return projecttools.Diff{}, errors.New("project not found")
	}
	return projecttools.GetDiff(project.Path)
}

func (a *App) CreateWorktree(id, targetPath, branch, base string) error {
	project, ok := a.projects.Get(id)
	if !ok {
		return errors.New("project not found")
	}
	return projecttools.CreateWorktree(project.Path, targetPath, branch, base)
}

func (a *App) RemoveWorktree(id, targetPath string) error {
	project, ok := a.projects.Get(id)
	if !ok {
		return errors.New("project not found")
	}
	return projecttools.RemoveWorktree(project.Path, targetPath)
}

func (a *App) SwitchProject(ctx context.Context, id string) error {
	project, ok := a.projects.Get(id)
	if !ok {
		return errors.New("project not found")
	}
	if !pathIsDirectory(project.Path) {
		return errors.New("project directory is unavailable")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	oldCfg := a.config.Get()
	if strings.EqualFold(oldCfg.Workspace, project.Path) {
		return a.projects.Touch(id)
	}
	mcpStatus, _, _ := a.process.Status()
	wasRunning := mcpStatus.Running && mcpStatus.Managed
	if owner, err := processmanager.FindTCPListener(oldCfg.MCPPort); err == nil && owner.Occupied && !wasRunning {
		return fmt.Errorf("MCP port is owned by an unmanaged process (PID %d); take over the service before switching projects", owner.PID)
	}
	newCfg := oldCfg
	newCfg.Workspace = project.Path
	newCfg.AllowedRoots = []string{project.Path}

	if wasRunning {
		if err := a.process.StopMCP(); err != nil {
			return err
		}
		if err := waitForManagedProcessStopped(a.process, true, 8*time.Second); err != nil {
			return err
		}
	}
	if _, err := a.config.Replace(newCfg); err != nil {
		return err
	}
	rollback := func(cause error) error {
		_, _ = a.config.Replace(oldCfg)
		if wasRunning {
			if err := a.process.StartMCP(oldCfg); err != nil {
				return fmt.Errorf("%w; rollback failed: %v", cause, err)
			}
			if err := a.waitForMCP(ctx, oldCfg, 12*time.Second); err != nil {
				return fmt.Errorf("%w; rollback not ready: %v", cause, err)
			}
		}
		return cause
	}
	if wasRunning {
		if err := a.process.StartMCP(newCfg); err != nil {
			return rollback(fmt.Errorf("start MCP for project: %w", err))
		}
		if err := a.waitForMCP(ctx, newCfg, 15*time.Second); err != nil {
			return rollback(fmt.Errorf("project MCP not ready: %w", err))
		}
	}
	return a.projects.Touch(id)
}

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
	tunnelInventory, tunnelInventoryErr := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	if tunnelInventoryErr == nil && !tunnelStatus.Running {
		for _, process := range tunnelInventory.Processes {
			if !process.MatchesConfig {
				continue
			}
			tunnelStatus = model.ProcessStatus{
				Name:    "tunnel",
				Running: true,
				Managed: process.Managed,
				PID:     process.PID,
			}
			break
		}
	}
	if tunnelInventoryErr != nil && tunnelStatus.LastError == "" {
		tunnelStatus.LastError = tunnelInventoryErr.Error()
	}
	portOwner, _ := processmanager.FindTCPListener(cfg.MCPPort)
	managedPort := false
	if portOwner.Occupied && mcp.Running {
		managedPort = portOwner.PID == mcp.PID || samePath(portOwner.ProcessPath, cfg.CoreExecutable)
	}
	oauthClientID := "mcp-devdesk"
	if oauthValues, err := a.secrets.GetOrCreate(); err == nil && oauthValues.ClientID != "" {
		oauthClientID = oauthValues.ClientID
	}
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
		Version:         Version,
		RootDirectory:   a.rootDir,
		DataDirectory:   a.dataDir,
		AdminURL:        adminURL,
		LocalMCPURL:     localMCPURL,
		RemoteMCPURL:    remoteMCPURL,
		AuthorizeURL:    authorizeURL,
		OAuthClientID:   oauthClientID,
		OAuthClientType: "public",
		OAuthTokenAuth:  "none",
		MCP:             mcp,
		MCPPortOwner: model.PortOwner{
			Occupied:    portOwner.Occupied,
			PID:         portOwner.PID,
			ParentPID:   portOwner.ParentPID,
			ProcessName: portOwner.ProcessName,
			ProcessPath: portOwner.ProcessPath,
			Managed:     managedPort,
		},
		Tunnel:           tunnelStatus,
		TunnelInventory:  tunnelInventory,
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
	a.tunnelDesired = cfg.Domain != "" && cfg.TunnelID != ""
	return a.startServicesLocked(ctx, cfg)
}

func (a *App) startServicesLocked(ctx context.Context, cfg model.Config) error {
	mcp, tunnelStatus, _ := a.process.Status()
	if !mcp.Running {
		owner, err := processmanager.FindTCPListener(cfg.MCPPort)
		if err != nil {
			a.desiredRunning = false
			a.tunnelDesired = false
			return fmt.Errorf("检查 MCP 端口失败: %w", err)
		}
		if owner.Occupied {
			a.desiredRunning = false
			a.tunnelDesired = false
			name := owner.ProcessName
			if name == "" {
				name = "未知进程"
			}
			return fmt.Errorf("MCP 端口 %d 已被 %s（PID %d）占用；当前公网域名可能仍连接到旧实例，请使用“接管旧实例并启动”", cfg.MCPPort, name, owner.PID)
		}
		if err := a.process.StartMCP(cfg); err != nil {
			a.desiredRunning = false
			a.tunnelDesired = false
			return err
		}
		if err := a.waitForMCP(ctx, cfg, 15*time.Second); err != nil {
			_ = a.process.StopMCP()
			a.desiredRunning = false
			a.tunnelDesired = false
			return fmt.Errorf("MCP 服务未能监听端口: %w", err)
		}
	}

	if a.tunnelDesired && cfg.Domain != "" && cfg.TunnelID != "" && !tunnelStatus.Running {
		inventory, err := a.tunnelInventoryForConfig(cfg, tunnelStatus)
		if err != nil {
			return fmt.Errorf("MCP 已启动，但无法检查 Tunnel 进程: %w", err)
		}
		if inventory.MatchingCount > 0 {
			return nil
		}
		for _, process := range inventory.Processes {
			if tunnelIdentityMatches(process, cfg) {
				return fmt.Errorf("检测到同一 Cloudflare Tunnel 仍在指向 %s（PID %d），为避免重复连接已阻止启动；请同步端口或关闭旧进程", displayTunnelTarget(process), process.PID)
			}
		}
		if err := a.process.StartTunnel(cfg); err != nil {
			return fmt.Errorf("MCP 已启动，但 Tunnel 启动失败: %w", err)
		}
	}
	return nil
}

func (a *App) TakeoverAndStart(ctx context.Context) error {
	cfg := a.config.Get()
	mcp, _, _ := a.process.Status()
	if mcp.Running {
		return a.RestartServices(ctx)
	}
	owner, err := processmanager.FindTCPListener(cfg.MCPPort)
	if err != nil {
		return err
	}
	if owner.Occupied {
		if !strings.EqualFold(owner.ProcessName, "coding-tools-mcp.exe") {
			return fmt.Errorf("端口 %d 被 %s（PID %d）占用，出于安全考虑不会自动终止非 MCP 进程", cfg.MCPPort, owner.ProcessName, owner.PID)
		}
		if err := processmanager.KillPortOwner(owner); err != nil {
			return err
		}
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if portAvailable(cfg.MCPHost, cfg.MCPPort) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		if !portAvailable(cfg.MCPHost, cfg.MCPPort) {
			return fmt.Errorf("旧 MCP 进程已终止，但端口 %d 仍未释放", cfg.MCPPort)
		}
	}
	return a.StartServices(ctx)
}

func (a *App) StopServices() error {
	a.mu.Lock()
	a.desiredRunning = false
	a.tunnelDesired = false
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

func (a *App) TunnelInventory() (model.TunnelInventory, error) {
	cfg := a.config.Get()
	_, tunnelStatus, _ := a.process.Status()
	return a.tunnelInventoryForConfig(cfg, tunnelStatus)
}

func (a *App) StopTunnelProcess(pid int) (model.TunnelInventory, error) {
	cfg := a.config.Get()
	_, tunnelStatus, _ := a.process.Status()
	inventory, err := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	if err != nil {
		return model.TunnelInventory{}, err
	}
	found := false
	selectedMatchesConfig := false
	for _, process := range inventory.Processes {
		if process.PID != pid {
			continue
		}
		found = true
		selectedMatchesConfig = process.MatchesConfig
		break
	}
	if !found {
		return model.TunnelInventory{}, fmt.Errorf("未找到 cloudflared PID %d", pid)
	}
	if err := processmanager.StopCloudflaredProcess(pid); err != nil {
		return model.TunnelInventory{}, err
	}
	if err := waitForCloudflaredPID(pid, 8*time.Second); err != nil {
		return model.TunnelInventory{}, err
	}
	_, tunnelStatus, _ = a.process.Status()
	updated, err := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	if err != nil {
		return model.TunnelInventory{}, err
	}
	if selectedMatchesConfig && updated.MatchingCount == 0 {
		a.mu.Lock()
		a.tunnelDesired = false
		a.mu.Unlock()
	}
	return updated, nil
}

func (a *App) SyncTunnelPort(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg := a.config.Get()
	if cfg.TunnelID == "" || cfg.TunnelName == "" || cfg.Domain == "" {
		return errors.New("请先完成 Cloudflare Tunnel 和固定域名配置")
	}

	mcp, _, _ := a.process.Status()
	if !mcp.Running {
		owner, err := processmanager.FindTCPListener(cfg.MCPPort)
		if err != nil {
			return err
		}
		if owner.Occupied {
			if !strings.EqualFold(owner.ProcessName, "coding-tools-mcp.exe") {
				return fmt.Errorf("当前 MCP 端口 %d 被 %s（PID %d）占用，不能同步 Tunnel", cfg.MCPPort, owner.ProcessName, owner.PID)
			}
		} else {
			if err := a.process.StartMCP(cfg); err != nil {
				return err
			}
			if err := a.waitForMCP(ctx, cfg, 15*time.Second); err != nil {
				_ = a.process.StopMCP()
				return fmt.Errorf("MCP 服务未能监听当前端口: %w", err)
			}
		}
	}

	if err := a.stopTunnelIdentityProcesses(cfg); err != nil {
		return err
	}
	if err := waitForTunnelIdentityStopped(cfg, 8*time.Second); err != nil {
		return err
	}
	if err := waitForManagedProcessStopped(a.process, false, 8*time.Second); err != nil {
		return err
	}
	if err := a.process.StartTunnel(cfg); err != nil {
		return err
	}
	a.desiredRunning = true
	a.tunnelDesired = true
	return nil
}

func (a *App) ChangeMCPPort(ctx context.Context, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	oldCfg := a.config.Get()
	if oldCfg.MCPPort == port {
		return nil
	}
	newCfg := oldCfg
	newCfg.MCPPort = port
	if err := config.Validate(newCfg); err != nil {
		return err
	}
	owner, err := processmanager.FindTCPListener(port)
	if err != nil {
		return fmt.Errorf("检查新端口失败: %w", err)
	}
	if owner.Occupied {
		name := owner.ProcessName
		if name == "" {
			name = "未知进程"
		}
		return fmt.Errorf("新端口 %d 已被 %s（PID %d）占用", port, name, owner.PID)
	}

	mcpStatus, tunnelStatus, _ := a.process.Status()
	oldInventory, inventoryErr := a.tunnelInventoryForConfig(oldCfg, tunnelStatus)
	if inventoryErr != nil {
		return inventoryErr
	}
	wasDesired := a.desiredRunning
	wasTunnelDesired := a.tunnelDesired
	oldTunnelWasActive := tunnelStatus.Running || oldInventory.MatchingCount > 0
	wasActive := wasDesired || mcpStatus.Running || tunnelStatus.Running || oldInventory.MatchingCount > 0
	if !wasActive {
		_, err := a.config.Replace(newCfg)
		return err
	}

	managedMCPWasRunning := mcpStatus.Running && mcpStatus.Managed
	if err := a.process.StopTunnel(); err != nil {
		return err
	}
	if err := waitForManagedProcessStopped(a.process, false, 8*time.Second); err != nil {
		return err
	}
	if managedMCPWasRunning {
		if err := a.process.StopMCP(); err != nil {
			return err
		}
		if err := waitForManagedProcessStopped(a.process, true, 8*time.Second); err != nil {
			return err
		}
	}

	rollback := func(cause error, oldTunnelStopped bool) error {
		_ = a.process.StopAll()
		_ = waitForManagedProcessStopped(a.process, true, 5*time.Second)
		_ = waitForManagedProcessStopped(a.process, false, 5*time.Second)
		a.desiredRunning = wasDesired
		a.tunnelDesired = wasTunnelDesired
		var rollbackProblems []string
		oldMCPReady := false
		if managedMCPWasRunning && portAvailable(oldCfg.MCPHost, oldCfg.MCPPort) {
			if startErr := a.process.StartMCP(oldCfg); startErr != nil {
				rollbackProblems = append(rollbackProblems, "恢复旧 MCP 失败: "+startErr.Error())
			} else if waitErr := a.waitForMCP(ctx, oldCfg, 12*time.Second); waitErr != nil {
				rollbackProblems = append(rollbackProblems, "旧 MCP 端口恢复失败: "+waitErr.Error())
			} else {
				oldMCPReady = true
			}
		} else if owner, ownerErr := processmanager.FindTCPListener(oldCfg.MCPPort); ownerErr == nil {
			oldMCPReady = owner.Occupied && strings.EqualFold(owner.ProcessName, "coding-tools-mcp.exe")
		}
		if oldTunnelStopped && oldTunnelWasActive && oldCfg.TunnelID != "" && oldMCPReady {
			if startErr := a.process.StartTunnel(oldCfg); startErr != nil {
				rollbackProblems = append(rollbackProblems, "恢复旧 Tunnel 失败: "+startErr.Error())
			}
		} else if oldTunnelStopped && oldTunnelWasActive && !oldMCPReady {
			rollbackProblems = append(rollbackProblems, "旧 MCP 未恢复，因此未重新启动旧 Tunnel")
		}
		if len(rollbackProblems) > 0 {
			return fmt.Errorf("%w；%s", cause, strings.Join(rollbackProblems, "；"))
		}
		return cause
	}

	if err := a.process.StartMCP(newCfg); err != nil {
		return rollback(fmt.Errorf("启动新端口 MCP 失败: %w", err), false)
	}
	if err := a.waitForMCP(ctx, newCfg, 15*time.Second); err != nil {
		return rollback(fmt.Errorf("新端口 MCP 未就绪: %w", err), false)
	}

	oldTunnelStopped := false
	if oldCfg.TunnelID != "" || (oldCfg.Domain != "" && oldCfg.TunnelName != "") {
		oldTunnelStopped = true
		if err := a.stopTunnelIdentityProcesses(oldCfg); err != nil {
			return rollback(fmt.Errorf("关闭旧 Tunnel 连接失败: %w", err), true)
		}
		if err := waitForTunnelIdentityStopped(oldCfg, 8*time.Second); err != nil {
			return rollback(err, true)
		}
	}

	if newCfg.Domain != "" && newCfg.TunnelID != "" {
		if err := a.process.StartTunnel(newCfg); err != nil {
			return rollback(fmt.Errorf("Cloudflare 切换到新端口失败: %w", err), oldTunnelStopped)
		}
	}
	if _, err := a.config.Replace(newCfg); err != nil {
		return rollback(fmt.Errorf("保存新端口失败: %w", err), oldTunnelStopped)
	}
	a.desiredRunning = true
	a.tunnelDesired = newCfg.Domain != "" && newCfg.TunnelID != ""
	return nil
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
		"manager":      filepath.Join(a.dataDir, "logs", "manager.log"),
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
	mcp, tunnelStatus, _ := a.process.Status()
	owner, _ := processmanager.FindTCPListener(cfg.MCPPort)
	portHealthy := !owner.Occupied || (mcp.Running && (owner.PID == mcp.PID || samePath(owner.ProcessPath, cfg.CoreExecutable)))
	tunnelInventory, _ := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	result := map[string]any{
		"version":                 Version,
		"rootDirectory":           a.rootDir,
		"dataDirectory":           a.dataDir,
		"workspaceExists":         pathIsDirectory(cfg.Workspace),
		"coreExists":              pathIsFile(cfg.CoreExecutable),
		"cloudflaredExists":       pathIsFile(cfg.CloudflaredExecutable),
		"cloudflareAuthenticated": pathIsFile(processmanager.CertificatePath()),
		"credentialsExist":        cfg.TunnelID != "" && pathIsFile(processmanager.CredentialsPath(cfg.TunnelID)),
		"mcpPortAvailable":        portHealthy,
		"mcpPortConflict":         owner.Occupied && !portHealthy,
		"mcpPortOwnerPid":         owner.PID,
		"mcpPortOwnerName":        owner.ProcessName,
		"mcpPortOwnerPath":        owner.ProcessPath,
		"tunnelProcessCount":      tunnelInventory.Count,
		"tunnelDuplicateCount":    tunnelInventory.DuplicateCount,
		"tunnelExpectedLocalUrl":  tunnelInventory.ExpectedLocalURL,
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
	tunnelDesired := a.tunnelDesired
	a.mu.RUnlock()
	if !desired {
		return
	}

	mcp, tunnelStatus, _ := a.process.Status()
	if !mcp.Running {
		owner, err := processmanager.FindTCPListener(cfg.MCPPort)
		if err != nil {
			a.logWatchdog("MCP 端口检查失败: " + err.Error())
			return
		}
		if owner.Occupied {
			a.logWatchdog(fmt.Sprintf("MCP 端口 %d 被 %s (PID %d) 占用，watchdog 不会接管旧实例", cfg.MCPPort, owner.ProcessName, owner.PID))
			return
		}
		a.logWatchdog("MCP 进程退出，正在重启")
		if err := a.process.StartMCP(cfg); err != nil {
			a.logWatchdog("MCP 重启失败: " + err.Error())
			return
		}
		if err := a.waitForMCP(ctx, cfg, 12*time.Second); err != nil {
			a.logWatchdog("MCP 端口检测失败: " + err.Error())
			return
		}
	}
	if tunnelDesired && cfg.Domain != "" && cfg.TunnelID != "" && !tunnelStatus.Running {
		inventory, err := a.tunnelInventoryForConfig(cfg, tunnelStatus)
		if err != nil {
			a.logWatchdog("Tunnel 进程检查失败: " + err.Error())
			return
		}
		if inventory.MatchingCount > 0 {
			return
		}
		for _, process := range inventory.Processes {
			if tunnelIdentityMatches(process, cfg) {
				a.logWatchdog(fmt.Sprintf("同一 Tunnel PID %d 仍指向 %s，watchdog 不会重复启动", process.PID, displayTunnelTarget(process)))
				return
			}
		}
		a.logWatchdog("Tunnel 进程退出，正在重启")
		if err := a.process.StartTunnel(cfg); err != nil {
			a.logWatchdog("Tunnel 重启失败: " + err.Error())
		}
	}
}

func (a *App) tunnelInventoryForConfig(cfg model.Config, managedStatus model.ProcessStatus) (model.TunnelInventory, error) {
	processes, err := processmanager.ListCloudflaredProcesses()
	if err != nil {
		return model.TunnelInventory{}, err
	}
	inventory := model.TunnelInventory{
		ExpectedLocalURL: "http://" + cfg.MCPHost + ":" + strconv.Itoa(cfg.MCPPort),
		Processes:        processes,
	}
	identityGroups := map[string][]int{}
	for index := range inventory.Processes {
		process := &inventory.Processes[index]
		process.Managed = managedStatus.Running && process.PID == managedStatus.PID
		process.MatchesConfig = tunnelIdentityMatches(*process, cfg) && tunnelTargetMatches(*process, cfg)
		if process.MatchesConfig {
			inventory.MatchingCount++
		}
		identity := tunnelIdentityKey(*process)
		if identity != "" {
			identityGroups[identity] = append(identityGroups[identity], index)
		}
	}
	for _, indexes := range identityGroups {
		if len(indexes) <= 1 {
			continue
		}
		inventory.DuplicateCount += len(indexes) - 1
		for _, index := range indexes {
			inventory.Processes[index].Duplicate = true
		}
	}
	inventory.Count = len(inventory.Processes)
	sort.SliceStable(inventory.Processes, func(left, right int) bool {
		leftProcess := inventory.Processes[left]
		rightProcess := inventory.Processes[right]
		if leftProcess.MatchesConfig != rightProcess.MatchesConfig {
			return leftProcess.MatchesConfig
		}
		if leftProcess.Managed != rightProcess.Managed {
			return leftProcess.Managed
		}
		if leftProcess.Duplicate != rightProcess.Duplicate {
			return leftProcess.Duplicate
		}
		return leftProcess.PID < rightProcess.PID
	})
	return inventory, nil
}

func tunnelIdentityKey(process model.TunnelProcess) string {
	if process.TunnelID != "" {
		return "id:" + strings.ToLower(process.TunnelID)
	}
	if process.TunnelName != "" {
		return "name:" + strings.ToLower(process.TunnelName)
	}
	return ""
}

func tunnelIdentityMatches(process model.TunnelProcess, cfg model.Config) bool {
	if cfg.TunnelID != "" && process.TunnelID != "" {
		return strings.EqualFold(cfg.TunnelID, process.TunnelID)
	}
	if cfg.TunnelName != "" && process.TunnelName != "" {
		return strings.EqualFold(cfg.TunnelName, process.TunnelName)
	}
	return false
}

func tunnelTargetMatches(process model.TunnelProcess, cfg model.Config) bool {
	if process.LocalPort != cfg.MCPPort {
		return false
	}
	if process.LocalHost == "" {
		return false
	}
	if strings.EqualFold(process.LocalHost, cfg.MCPHost) {
		return true
	}
	return isLoopbackHost(process.LocalHost) && isLoopbackHost(cfg.MCPHost)
}

func displayTunnelTarget(process model.TunnelProcess) string {
	if process.LocalURL != "" {
		return process.LocalURL
	}
	if process.LocalPort > 0 {
		return net.JoinHostPort(process.LocalHost, strconv.Itoa(process.LocalPort))
	}
	return "未知本地地址"
}

func (a *App) stopTunnelIdentityProcesses(cfg model.Config) error {
	processes, err := processmanager.ListCloudflaredProcesses()
	if err != nil {
		return err
	}
	var failures []string
	for _, process := range processes {
		if !tunnelIdentityMatches(process, cfg) {
			continue
		}
		if err := processmanager.StopCloudflaredProcess(process.PID); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	return nil
}

func waitForCloudflaredPID(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		processes, err := processmanager.ListCloudflaredProcesses()
		if err != nil {
			return err
		}
		found := false
		for _, process := range processes {
			if process.PID == pid {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("等待 cloudflared PID %d 退出超时", pid)
}

func waitForTunnelIdentityStopped(cfg model.Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		processes, err := processmanager.ListCloudflaredProcesses()
		if err != nil {
			return err
		}
		found := false
		for _, process := range processes {
			if tunnelIdentityMatches(process, cfg) {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("等待旧 Cloudflare Tunnel 进程退出超时")
}

func waitForManagedProcessStopped(manager *processmanager.Manager, mcp bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mcpStatus, tunnelStatus, _ := manager.Status()
		status := tunnelStatus
		if mcp {
			status = mcpStatus
		}
		if !status.Running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if mcp {
		return errors.New("等待 MCP 进程退出超时")
	}
	return errors.New("等待 Tunnel 进程退出超时")
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

func (a *App) waitForMCP(ctx context.Context, cfg model.Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(cfg.MCPHost, strconv.Itoa(cfg.MCPPort))
	for time.Now().Before(deadline) {
		mcp, _, _ := a.process.Status()
		if !mcp.Running {
			if mcp.LastError != "" {
				return errors.New(mcp.LastError)
			}
			return errors.New("MCP 进程在端口就绪前退出")
		}
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

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
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
