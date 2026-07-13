package application

import (
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
	instancestore "mcp-devdesk/internal/instances"
	devlogging "mcp-devdesk/internal/logging"
	"mcp-devdesk/internal/model"
	processmanager "mcp-devdesk/internal/process"
)

type managedInstance struct {
	mu             sync.Mutex
	config         *config.Store
	process        *processmanager.Manager
	desiredRunning bool
}

func (a *App) loadManagedInstances() error {
	for _, record := range a.instances.List() {
		dataDir := a.instances.DataDir(record.ID)
		cfgStore, err := config.NewStore(a.rootDir, dataDir)
		if err != nil {
			return fmt.Errorf("load instance %s config: %w", record.Name, err)
		}
		for _, path := range applicationLogPaths(dataDir) {
			_ = devlogging.TrimFile(path)
		}
		runtime := &managedInstance{config: cfgStore}
		runtime.process = processmanager.NewManager(a.rootDir, dataDir, a.secrets, func() bool {
			return cfgStore.Get().LoggingEnabled
		})
		a.instanceRuntime[record.ID] = runtime
	}
	return nil
}

func (a *App) Instances() []model.MCPInstance {
	result := []model.MCPInstance{a.primaryInstanceView()}
	records := a.instances.List()
	a.instanceMu.RLock()
	defer a.instanceMu.RUnlock()
	for _, record := range records {
		runtime := a.instanceRuntime[record.ID]
		if runtime == nil {
			continue
		}
		result = append(result, a.managedInstanceView(record, runtime))
	}
	return result
}

func (a *App) Instance(id string) (model.MCPInstance, error) {
	if id == model.PrimaryInstanceID {
		return a.primaryInstanceView(), nil
	}
	record, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.MCPInstance{}, err
	}
	return a.managedInstanceView(record, runtime), nil
}

func (a *App) CreateInstance(ctx context.Context, request model.MCPInstanceCreateRequest) (model.MCPInstance, error) {
	workspace, projectID, err := a.resolveInstanceWorkspace(request.ProjectID, request.Workspace)
	if err != nil {
		return model.MCPInstance{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = filepath.Base(workspace)
	}
	base := a.config.Get()
	cfg := base
	cfg.Workspace = workspace
	cfg.AllowedRoots = []string{workspace}
	cfg.MCPPort = request.MCPPort
	if cfg.MCPPort == 0 {
		cfg.MCPPort, err = a.nextAvailableInstancePort()
		if err != nil {
			return model.MCPInstance{}, err
		}
	}
	cfg.Domain = strings.ToLower(strings.TrimSpace(request.Domain))
	cfg.TunnelName = strings.TrimSpace(request.TunnelName)
	if cfg.TunnelName == "" {
		cfg.TunnelName = uniqueTunnelName(name, cfg.MCPPort)
	}
	cfg.TunnelID = ""
	if request.CoreMode != "" {
		cfg.CoreMode = request.CoreMode
	}
	if request.PermissionMode != "" {
		cfg.PermissionMode = request.PermissionMode
	}
	if request.FileScope != "" {
		cfg.FileScope = request.FileScope
	}
	if request.ToolProfile != "" {
		cfg.ToolProfile = request.ToolProfile
	}
	if request.AllowNetwork != nil {
		cfg.AllowNetwork = *request.AllowNetwork
	}
	if request.AutoStart != nil {
		cfg.AutoStart = *request.AutoStart
	} else {
		cfg.AutoStart = false
	}
	if request.Watchdog != nil {
		cfg.Watchdog = *request.Watchdog
	} else {
		cfg.Watchdog = true
	}
	if request.LoggingEnabled != nil {
		cfg.LoggingEnabled = *request.LoggingEnabled
	}
	normalizeInstanceConfig(&cfg)
	if err := config.Validate(cfg); err != nil {
		return model.MCPInstance{}, err
	}
	if err := a.validateInstanceUniqueness("", cfg); err != nil {
		return model.MCPInstance{}, err
	}
	if err := ensurePortAvailable(cfg.MCPPort); err != nil {
		return model.MCPInstance{}, err
	}

	record, err := a.instances.Add(name, projectID)
	if err != nil {
		return model.MCPInstance{}, err
	}
	dataDir := a.instances.DataDir(record.ID)
	cfgStore, err := config.NewStore(a.rootDir, dataDir)
	if err != nil {
		_ = a.instances.Remove(record.ID)
		_ = os.RemoveAll(dataDir)
		return model.MCPInstance{}, err
	}
	if _, err := cfgStore.Replace(cfg); err != nil {
		_ = a.instances.Remove(record.ID)
		_ = os.RemoveAll(dataDir)
		return model.MCPInstance{}, err
	}
	runtime := &managedInstance{config: cfgStore}
	runtime.process = processmanager.NewManager(a.rootDir, dataDir, a.secrets, func() bool {
		return cfgStore.Get().LoggingEnabled
	})
	a.instanceMu.Lock()
	a.instanceRuntime[record.ID] = runtime
	a.instanceMu.Unlock()

	if cfg.AutoStart {
		if _, err := a.StartInstance(ctx, record.ID); err != nil {
			_ = runtime.process.StopAll()
			a.instanceMu.Lock()
			delete(a.instanceRuntime, record.ID)
			a.instanceMu.Unlock()
			_ = a.instances.Remove(record.ID)
			_ = os.RemoveAll(dataDir)
			return model.MCPInstance{}, fmt.Errorf("instance auto-start failed; creation was rolled back: %w", err)
		}
	}
	return a.managedInstanceView(record, runtime), nil
}

func (a *App) UpdateInstance(ctx context.Context, id string, request model.MCPInstanceUpdateRequest) (model.MCPInstance, error) {
	if id == model.PrimaryInstanceID {
		return model.MCPInstance{}, errors.New("主实例请在项目、服务和设置页面中修改")
	}
	record, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.MCPInstance{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	oldCfg := runtime.config.Get()
	newCfg := oldCfg
	newRecord := record
	if request.Name != nil {
		newRecord.Name = strings.TrimSpace(*request.Name)
	}
	if request.ProjectID != nil {
		newRecord.ProjectID = strings.TrimSpace(*request.ProjectID)
	}
	if request.Workspace != nil || request.ProjectID != nil {
		workspace := oldCfg.Workspace
		if request.Workspace != nil {
			workspace = *request.Workspace
		}
		resolved, projectID, resolveErr := a.resolveInstanceWorkspace(newRecord.ProjectID, workspace)
		if resolveErr != nil {
			return model.MCPInstance{}, resolveErr
		}
		newCfg.Workspace = resolved
		newCfg.AllowedRoots = []string{resolved}
		newRecord.ProjectID = projectID
	}
	if request.MCPPort != nil {
		newCfg.MCPPort = *request.MCPPort
	}
	if request.Domain != nil {
		newCfg.Domain = *request.Domain
	}
	if request.TunnelName != nil {
		newName := strings.TrimSpace(*request.TunnelName)
		if !strings.EqualFold(newName, oldCfg.TunnelName) {
			newCfg.TunnelID = ""
		}
		newCfg.TunnelName = newName
	}
	if request.CoreMode != nil {
		newCfg.CoreMode = *request.CoreMode
	}
	if request.PermissionMode != nil {
		newCfg.PermissionMode = *request.PermissionMode
	}
	if request.FileScope != nil {
		newCfg.FileScope = *request.FileScope
	}
	if request.ToolProfile != nil {
		newCfg.ToolProfile = *request.ToolProfile
	}
	if request.AllowNetwork != nil {
		newCfg.AllowNetwork = *request.AllowNetwork
	}
	if request.AutoStart != nil {
		newCfg.AutoStart = *request.AutoStart
	}
	if request.Watchdog != nil {
		newCfg.Watchdog = *request.Watchdog
	}
	if request.LoggingEnabled != nil {
		newCfg.LoggingEnabled = *request.LoggingEnabled
	}
	normalizeInstanceConfig(&newCfg)
	if err := config.Validate(newCfg); err != nil {
		return model.MCPInstance{}, err
	}
	if err := a.validateInstanceUniqueness(id, newCfg); err != nil {
		return model.MCPInstance{}, err
	}
	if oldCfg.MCPPort != newCfg.MCPPort {
		if err := ensurePortAvailable(newCfg.MCPPort); err != nil {
			return model.MCPInstance{}, err
		}
	}

	mcpStatus, tunnelStatus, _ := runtime.process.Status()
	wasRunning := mcpStatus.Running || tunnelStatus.Running || runtime.desiredRunning
	if mcpStatus.Running || tunnelStatus.Running {
		if err := runtime.process.StopAll(); err != nil {
			return model.MCPInstance{}, err
		}
		if err := waitForManagedProcessStopped(runtime.process, true, 8*time.Second); err != nil {
			return model.MCPInstance{}, err
		}
		if err := waitForManagedProcessStopped(runtime.process, false, 8*time.Second); err != nil {
			return model.MCPInstance{}, err
		}
	}
	rollback := func(cause error) error {
		_, _ = runtime.config.Replace(oldCfg)
		_, _ = a.instances.Update(id, record.Name, record.ProjectID)
		runtime.desiredRunning = wasRunning
		if wasRunning {
			if startErr := a.startManagedInstanceLocked(ctx, runtime, oldCfg); startErr != nil {
				return fmt.Errorf("%w; rollback start failed: %v", cause, startErr)
			}
		}
		return cause
	}
	if _, err := runtime.config.Replace(newCfg); err != nil {
		return model.MCPInstance{}, rollback(err)
	}
	updatedRecord, err := a.instances.Update(id, newRecord.Name, newRecord.ProjectID)
	if err != nil {
		return model.MCPInstance{}, rollback(err)
	}
	runtime.desiredRunning = wasRunning
	if wasRunning {
		if err := a.startManagedInstanceLocked(ctx, runtime, newCfg); err != nil {
			return model.MCPInstance{}, rollback(fmt.Errorf("restart updated instance: %w", err))
		}
	}
	return a.managedInstanceView(updatedRecord, runtime), nil
}

func (a *App) DeleteInstance(id string) error {
	if id == model.PrimaryInstanceID {
		return errors.New("主实例不能删除")
	}
	_, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.desiredRunning = false
	if err := runtime.process.StopAll(); err != nil {
		runtime.mu.Unlock()
		return err
	}
	if err := waitForManagedProcessStopped(runtime.process, true, 8*time.Second); err != nil {
		runtime.mu.Unlock()
		return err
	}
	if err := waitForManagedProcessStopped(runtime.process, false, 8*time.Second); err != nil {
		runtime.mu.Unlock()
		return err
	}
	runtime.mu.Unlock()
	if err := a.instances.Remove(id); err != nil {
		return err
	}
	a.instanceMu.Lock()
	delete(a.instanceRuntime, id)
	a.instanceMu.Unlock()
	return os.RemoveAll(a.instances.DataDir(id))
}

func (a *App) StartInstance(ctx context.Context, id string) (model.MCPInstance, error) {
	if id == model.PrimaryInstanceID {
		if err := a.StartServices(ctx); err != nil {
			return a.primaryInstanceView(), err
		}
		return a.primaryInstanceView(), nil
	}
	record, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.MCPInstance{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.desiredRunning = true
	cfg := runtime.config.Get()
	if err := a.startManagedInstanceLocked(ctx, runtime, cfg); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	updated, _ := a.instances.Touch(id)
	if updated.ID != "" {
		record = updated
	}
	return a.managedInstanceView(record, runtime), nil
}

func (a *App) StopInstance(id string) (model.MCPInstance, error) {
	if id == model.PrimaryInstanceID {
		if err := a.StopServices(); err != nil {
			return a.primaryInstanceView(), err
		}
		return a.primaryInstanceView(), nil
	}
	record, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.MCPInstance{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.desiredRunning = false
	if err := runtime.process.StopAll(); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	if err := waitForManagedProcessStopped(runtime.process, true, 8*time.Second); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	if err := waitForManagedProcessStopped(runtime.process, false, 8*time.Second); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	return a.managedInstanceView(record, runtime), nil
}

func (a *App) RestartInstance(ctx context.Context, id string) (model.MCPInstance, error) {
	if id == model.PrimaryInstanceID {
		if err := a.RestartServices(ctx); err != nil {
			return a.primaryInstanceView(), err
		}
		return a.primaryInstanceView(), nil
	}
	record, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.MCPInstance{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.desiredRunning = true
	if err := runtime.process.StopAll(); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	if err := waitForManagedProcessStopped(runtime.process, true, 8*time.Second); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	if err := waitForManagedProcessStopped(runtime.process, false, 8*time.Second); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	if err := a.startManagedInstanceLocked(ctx, runtime, runtime.config.Get()); err != nil {
		return a.managedInstanceView(record, runtime), err
	}
	return a.managedInstanceView(record, runtime), nil
}

func (a *App) ConfigureInstanceTunnel(ctx context.Context, id string, request model.ConfigureTunnelRequest) (model.ConfigureTunnelResult, error) {
	if id == model.PrimaryInstanceID {
		return a.ConfigureTunnel(ctx, request)
	}
	_, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	cfg := runtime.config.Get()
	if strings.TrimSpace(request.TunnelName) == "" {
		request.TunnelName = cfg.TunnelName
	}
	candidate := cfg
	candidate.Domain = request.Domain
	candidate.TunnelName = request.TunnelName
	normalizeInstanceConfig(&candidate)
	if err := a.validateInstanceUniqueness(id, candidate); err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	result, err := a.tunnel.Configure(ctx, cfg, request)
	if err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	cfg.Domain = result.Domain
	cfg.TunnelName = result.TunnelName
	cfg.TunnelID = result.TunnelID
	if _, err := runtime.config.Replace(cfg); err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	_, tunnelStatus, _ := runtime.process.Status()
	if tunnelStatus.Running {
		if err := runtime.process.StopTunnel(); err != nil {
			return model.ConfigureTunnelResult{}, err
		}
		if err := waitForManagedProcessStopped(runtime.process, false, 8*time.Second); err != nil {
			return model.ConfigureTunnelResult{}, err
		}
		if err := runtime.process.StartTunnel(cfg); err != nil {
			return model.ConfigureTunnelResult{}, err
		}
	}
	_, _ = a.instances.Touch(id)
	return result, nil
}

func (a *App) InstanceLogs(id, name string, maxLines int) (model.LogResponse, error) {
	if id == model.PrimaryInstanceID {
		return a.Logs(name, maxLines)
	}
	if _, _, err := a.instanceRecordAndRuntime(id); err != nil {
		return model.LogResponse{}, err
	}
	if maxLines <= 0 || maxLines > devlogging.MaxEntries {
		maxLines = devlogging.MaxEntries
	}
	paths := applicationLogPaths(a.instances.DataDir(id))
	path, ok := paths[name]
	if !ok {
		return model.LogResponse{}, errors.New("unknown log name")
	}
	_ = devlogging.TrimFile(path)
	lines, truncated, err := tailLines(path, maxLines)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.LogResponse{}, err
	}
	return model.LogResponse{Name: name, Path: path, Lines: lines, Truncated: truncated}, nil
}

func (a *App) StartAutoInstances(ctx context.Context) []error {
	var failures []error
	for _, record := range a.instances.List() {
		_, runtime, err := a.instanceRecordAndRuntime(record.ID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !runtime.config.Get().AutoStart {
			continue
		}
		instanceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, startErr := a.StartInstance(instanceCtx, record.ID)
		cancel()
		if startErr != nil {
			failures = append(failures, fmt.Errorf("start %s: %w", record.Name, startErr))
		}
	}
	return failures
}

func (a *App) StopAllInstances() error {
	a.instanceMu.RLock()
	items := make([]*managedInstance, 0, len(a.instanceRuntime))
	for _, runtime := range a.instanceRuntime {
		items = append(items, runtime)
	}
	a.instanceMu.RUnlock()
	var failures []string
	for _, runtime := range items {
		runtime.mu.Lock()
		runtime.desiredRunning = false
		if err := runtime.process.StopAll(); err != nil {
			failures = append(failures, err.Error())
		}
		runtime.mu.Unlock()
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (a *App) startManagedInstanceLocked(ctx context.Context, runtime *managedInstance, cfg model.Config) error {
	mcpStatus, tunnelStatus, _ := runtime.process.Status()
	if !mcpStatus.Running {
		owner, err := processmanager.FindTCPListener(cfg.MCPPort)
		if err != nil {
			return err
		}
		if owner.Occupied {
			return fmt.Errorf("MCP 端口 %d 已被 %s（PID %d）占用", cfg.MCPPort, owner.ProcessName, owner.PID)
		}
		if err := runtime.process.StartMCP(cfg); err != nil {
			return err
		}
		if err := waitForMCPManager(ctx, runtime.process, cfg, 15*time.Second); err != nil {
			_ = runtime.process.StopMCP()
			return err
		}
	}
	if cfg.Domain == "" || cfg.TunnelID == "" || tunnelStatus.Running {
		return nil
	}
	inventory, err := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	if err != nil {
		return err
	}
	if inventory.MatchingCount > 0 {
		return nil
	}
	for _, process := range inventory.Processes {
		if tunnelIdentityMatches(process, cfg) {
			return fmt.Errorf("同一 Tunnel 仍在指向 %s（PID %d），请先停止旧连接", displayTunnelTarget(process), process.PID)
		}
	}
	return runtime.process.StartTunnel(cfg)
}

func (a *App) primaryInstanceView() model.MCPInstance {
	cfg := a.config.Get()
	status := a.Status()
	projectID := ""
	name := filepath.Base(cfg.Workspace)
	for _, project := range a.projects.List() {
		if strings.EqualFold(filepath.Clean(project.Path), filepath.Clean(cfg.Workspace)) {
			projectID = project.ID
			name = project.Name
			break
		}
	}
	return model.MCPInstance{
		ID:                   model.PrimaryInstanceID,
		Name:                 name,
		ProjectID:            projectID,
		Primary:              true,
		TunnelMode:           "independent",
		Workspace:            cfg.Workspace,
		MCPHost:              cfg.MCPHost,
		MCPPort:              cfg.MCPPort,
		LocalMCPURL:          status.LocalMCPURL,
		RemoteMCPURL:         status.RemoteMCPURL,
		AuthorizeURL:         status.AuthorizeURL,
		Domain:               cfg.Domain,
		TunnelName:           cfg.TunnelName,
		TunnelID:             cfg.TunnelID,
		CoreMode:             cfg.CoreMode,
		PermissionMode:       cfg.PermissionMode,
		FileScope:            cfg.FileScope,
		ToolProfile:          cfg.ToolProfile,
		AllowNetwork:         cfg.AllowNetwork,
		AutoStart:            cfg.AutoStart,
		Watchdog:             cfg.Watchdog,
		LoggingEnabled:       cfg.LoggingEnabled,
		DataDirectory:        a.dataDir,
		MCP:                  status.MCP,
		Tunnel:               status.Tunnel,
		MCPPortOwner:         status.MCPPortOwner,
		ConfigurationOK:      status.ConfigurationOK,
		ConfigurationMessage: status.ConfigurationMsg,
	}
}

func (a *App) managedInstanceView(record instancestore.Record, runtime *managedInstance) model.MCPInstance {
	cfg := runtime.config.Get()
	mcpStatus, tunnelStatus, _ := runtime.process.Status()
	inventory, inventoryErr := a.tunnelInventoryForConfig(cfg, tunnelStatus)
	if inventoryErr == nil && !tunnelStatus.Running {
		for _, process := range inventory.Processes {
			if process.MatchesConfig {
				tunnelStatus = model.ProcessStatus{Name: "tunnel", Running: true, Managed: process.Managed, PID: process.PID}
				break
			}
		}
	}
	owner, _ := processmanager.FindTCPListener(cfg.MCPPort)
	managedPort := owner.Occupied && mcpStatus.Running && (owner.PID == mcpStatus.PID || samePath(owner.ProcessPath, selectedCoreExecutable(cfg)))
	localURL := "http://" + cfg.MCPHost + ":" + strconv.Itoa(cfg.MCPPort) + "/mcp"
	remoteURL := ""
	authorizeURL := ""
	if cfg.Domain != "" {
		remoteURL = "https://" + cfg.Domain + "/mcp"
		authorizeURL = "https://" + cfg.Domain + "/oauth/authorize"
	}
	ok, message := a.configurationStatus(cfg)
	createdAt := record.CreatedAt
	updatedAt := record.UpdatedAt
	return model.MCPInstance{
		ID:             record.ID,
		Name:           record.Name,
		ProjectID:      record.ProjectID,
		TunnelMode:     "independent",
		Workspace:      cfg.Workspace,
		MCPHost:        cfg.MCPHost,
		MCPPort:        cfg.MCPPort,
		LocalMCPURL:    localURL,
		RemoteMCPURL:   remoteURL,
		AuthorizeURL:   authorizeURL,
		Domain:         cfg.Domain,
		TunnelName:     cfg.TunnelName,
		TunnelID:       cfg.TunnelID,
		CoreMode:       cfg.CoreMode,
		PermissionMode: cfg.PermissionMode,
		FileScope:      cfg.FileScope,
		ToolProfile:    cfg.ToolProfile,
		AllowNetwork:   cfg.AllowNetwork,
		AutoStart:      cfg.AutoStart,
		Watchdog:       cfg.Watchdog,
		LoggingEnabled: cfg.LoggingEnabled,
		DataDirectory:  a.instances.DataDir(record.ID),
		MCP:            mcpStatus,
		Tunnel:         tunnelStatus,
		MCPPortOwner: model.PortOwner{
			Occupied: owner.Occupied, PID: owner.PID, ParentPID: owner.ParentPID,
			ProcessName: owner.ProcessName, ProcessPath: owner.ProcessPath, Managed: managedPort,
		},
		ConfigurationOK:      ok,
		ConfigurationMessage: message,
		CreatedAt:            &createdAt,
		UpdatedAt:            &updatedAt,
	}
}

func (a *App) instanceRecordAndRuntime(id string) (instancestore.Record, *managedInstance, error) {
	record, ok := a.instances.Get(id)
	if !ok {
		return instancestore.Record{}, nil, errors.New("instance not found")
	}
	a.instanceMu.RLock()
	runtime := a.instanceRuntime[id]
	a.instanceMu.RUnlock()
	if runtime == nil {
		return instancestore.Record{}, nil, errors.New("instance runtime is unavailable")
	}
	return record, runtime, nil
}

func (a *App) resolveInstanceWorkspace(projectID, workspace string) (string, string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		project, ok := a.projects.Get(projectID)
		if !ok {
			return "", "", errors.New("project not found")
		}
		workspace = project.Path
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", "", errors.New("workspace is required")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", "", err
	}
	absolute = filepath.Clean(absolute)
	if !pathIsDirectory(absolute) {
		return "", "", errors.New("workspace directory is unavailable")
	}
	if projectID == "" {
		for _, project := range a.projects.List() {
			if strings.EqualFold(filepath.Clean(project.Path), absolute) {
				projectID = project.ID
				break
			}
		}
	}
	return absolute, projectID, nil
}

func (a *App) nextAvailableInstancePort() (int, error) {
	used := map[int]struct{}{a.config.Get().MCPPort: {}}
	a.instanceMu.RLock()
	for _, runtime := range a.instanceRuntime {
		used[runtime.config.Get().MCPPort] = struct{}{}
	}
	a.instanceMu.RUnlock()
	start := a.config.Get().MCPPort + 1
	if start < 8766 {
		start = 8766
	}
	for port := start; port <= 65535; port++ {
		if _, exists := used[port]; exists || port == a.config.Get().AdminPort {
			continue
		}
		owner, err := processmanager.FindTCPListener(port)
		if err == nil && !owner.Occupied {
			return port, nil
		}
	}
	return 0, errors.New("没有可用的 MCP 端口")
}

func (a *App) validateInstanceUniqueness(excludeID string, candidate model.Config) error {
	primary := a.config.Get()
	if excludeID != model.PrimaryInstanceID {
		if candidate.MCPPort == primary.MCPPort {
			return fmt.Errorf("端口 %d 已被主实例使用", candidate.MCPPort)
		}
		if candidate.Domain != "" && strings.EqualFold(candidate.Domain, primary.Domain) {
			return fmt.Errorf("域名 %s 已被主实例使用", candidate.Domain)
		}
		if candidate.TunnelName != "" && strings.EqualFold(candidate.TunnelName, primary.TunnelName) {
			return fmt.Errorf("Tunnel 名称 %s 已被主实例使用", candidate.TunnelName)
		}
	}
	a.instanceMu.RLock()
	defer a.instanceMu.RUnlock()
	for id, runtime := range a.instanceRuntime {
		if id == excludeID {
			continue
		}
		cfg := runtime.config.Get()
		if cfg.MCPPort == candidate.MCPPort {
			return fmt.Errorf("端口 %d 已被其他 MCP 实例使用", candidate.MCPPort)
		}
		if candidate.Domain != "" && strings.EqualFold(candidate.Domain, cfg.Domain) {
			return fmt.Errorf("域名 %s 已被其他 MCP 实例使用", candidate.Domain)
		}
		if candidate.TunnelName != "" && strings.EqualFold(candidate.TunnelName, cfg.TunnelName) {
			return fmt.Errorf("Tunnel 名称 %s 已被其他 MCP 实例使用", candidate.TunnelName)
		}
	}
	return nil
}

func normalizeInstanceConfig(cfg *model.Config) {
	if absolute, err := filepath.Abs(strings.TrimSpace(cfg.Workspace)); err == nil {
		cfg.Workspace = filepath.Clean(absolute)
	}
	cfg.AllowedRoots = []string{cfg.Workspace}
	cfg.Domain = strings.ToLower(strings.TrimSpace(cfg.Domain))
	cfg.TunnelName = strings.TrimSpace(cfg.TunnelName)
	if cfg.PermissionMode == "trusted" || cfg.PermissionMode == "dangerous" {
		cfg.AllowNetwork = true
	}
}

func ensurePortAvailable(port int) error {
	owner, err := processmanager.FindTCPListener(port)
	if err != nil {
		return err
	}
	if owner.Occupied {
		return fmt.Errorf("端口 %d 已被 %s（PID %d）占用", port, owner.ProcessName, owner.PID)
	}
	return nil
}

func uniqueTunnelName(name string, port int) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(name) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
		case builder.Len() > 0 && builder.String()[builder.Len()-1] != '-':
			builder.WriteByte('-')
		}
	}
	value := strings.Trim(builder.String(), "-")
	if value == "" {
		value = "instance"
	}
	if len(value) > 36 {
		value = value[:36]
	}
	return "mcp-devdesk-" + value + "-" + strconv.Itoa(port)
}

func waitForMCPManager(ctx context.Context, manager *processmanager.Manager, cfg model.Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(cfg.MCPHost, strconv.Itoa(cfg.MCPPort))
	for time.Now().Before(deadline) {
		mcp, _, _ := manager.Status()
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

func (a *App) watchdogInstances(ctx context.Context) {
	a.instanceMu.RLock()
	ids := make([]string, 0, len(a.instanceRuntime))
	for id := range a.instanceRuntime {
		ids = append(ids, id)
	}
	a.instanceMu.RUnlock()
	sort.Strings(ids)
	for _, id := range ids {
		_, runtime, err := a.instanceRecordAndRuntime(id)
		if err != nil {
			continue
		}
		if !runtime.mu.TryLock() {
			continue
		}
		cfg := runtime.config.Get()
		if cfg.Watchdog && runtime.desiredRunning {
			mcp, tunnelStatus, _ := runtime.process.Status()
			if !mcp.Running {
				owner, ownerErr := processmanager.FindTCPListener(cfg.MCPPort)
				if ownerErr == nil && !owner.Occupied {
					_ = runtime.process.StartMCP(cfg)
					_ = waitForMCPManager(ctx, runtime.process, cfg, 12*time.Second)
				}
			}
			if cfg.Domain != "" && cfg.TunnelID != "" && !tunnelStatus.Running {
				inventory, inventoryErr := a.tunnelInventoryForConfig(cfg, tunnelStatus)
				if inventoryErr == nil && inventory.MatchingCount == 0 {
					identityConflict := false
					for _, process := range inventory.Processes {
						if tunnelIdentityMatches(process, cfg) {
							identityConflict = true
							break
						}
					}
					if !identityConflict {
						_ = runtime.process.StartTunnel(cfg)
					}
				}
			}
		}
		runtime.mu.Unlock()
	}
}
