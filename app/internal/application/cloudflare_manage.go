package application

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "runtime"
    "strconv"
    "strings"
    "time"

    "mcp-devdesk/internal/model"
    processmanager "mcp-devdesk/internal/process"
)

const cloudflaredLatestReleaseURL = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"

var cloudflaredVersionPattern = regexp.MustCompile(`(?i)cloudflared version\s+([0-9]+(?:\.[0-9]+){1,3})`)

type CloudflareUnbindResult struct {
    InstanceID string `json:"instanceId"`
    Domain     string `json:"domain"`
    TunnelID   string `json:"tunnelId"`
    TunnelName string `json:"tunnelName"`
    Message    string `json:"message"`
}

type CloudflaredUpdateStatus struct {
    Installed       bool   `json:"installed"`
    Executable      string `json:"executable"`
    CurrentVersion  string `json:"currentVersion"`
    LatestVersion   string `json:"latestVersion"`
    UpdateAvailable bool   `json:"updateAvailable"`
    AssetName       string `json:"assetName"`
    PageURL         string `json:"pageUrl"`
    downloadURL     string
    sha256          string
}

type CloudflaredUpdateResult struct {
    Status           CloudflaredUpdateStatus `json:"status"`
    PreviousVersion  string                  `json:"previousVersion"`
    RestartedTunnels int                     `json:"restartedTunnels"`
    RestartErrors    []string                `json:"restartErrors,omitempty"`
    Message          string                  `json:"message"`
}

func (a *App) UnbindInstanceCloudflare(ctx context.Context, id string) (CloudflareUnbindResult, error) {
    if id == model.PrimaryInstanceID {
        return a.unbindPrimaryCloudflare(ctx)
    }
    record, managed, err := a.instanceRecordAndRuntime(id)
    if err != nil {
        return CloudflareUnbindResult{}, err
    }
    managed.mu.Lock()
    defer managed.mu.Unlock()

    cfg := managed.config.Get()
    if err := a.ensureTunnelExclusive(cfg.TunnelID, id); err != nil {
        return CloudflareUnbindResult{}, err
    }
    domain, tunnelID, tunnelName := cfg.Domain, cfg.TunnelID, cfg.TunnelName
    if strings.TrimSpace(tunnelID) == "" {
        return CloudflareUnbindResult{}, errors.New("该实例没有可解绑的 Cloudflare Tunnel")
    }

    _, tunnelStatus, _ := managed.process.Status()
    originalDesired := managed.desiredRunning
    managed.desiredRunning = false
    if err := managed.process.StopTunnel(); err != nil {
        managed.desiredRunning = originalDesired
        return CloudflareUnbindResult{}, err
    }
    if tunnelStatus.Running {
        if err := waitForManagedProcessStopped(managed.process, false, 8*time.Second); err != nil {
            managed.desiredRunning = originalDesired
            return CloudflareUnbindResult{}, err
        }
    }
    if _, err := a.tunnel.Delete(ctx, cfg); err != nil {
        managed.desiredRunning = originalDesired
        if tunnelStatus.Running {
            _ = managed.process.StartTunnel(cfg)
        }
        return CloudflareUnbindResult{}, err
    }

    cfg.Domain = ""
    cfg.TunnelID = ""
    if _, err := managed.config.Replace(cfg); err != nil {
        managed.desiredRunning = originalDesired
        return CloudflareUnbindResult{}, fmt.Errorf("Cloudflare 已删除，但清理本地实例配置失败: %w", err)
    }
    managed.desiredRunning = originalDesired
    return CloudflareUnbindResult{
        InstanceID: id,
        Domain: domain,
        TunnelID: tunnelID,
        TunnelName: tunnelName,
        Message: fmt.Sprintf("已解绑 %s，并删除 Tunnel %s 及其 Cloudflare 路由", record.Name, tunnelName),
    }, nil
}

func (a *App) unbindPrimaryCloudflare(ctx context.Context) (CloudflareUnbindResult, error) {
    a.mu.Lock()
    defer a.mu.Unlock()

    cfg := a.config.Get()
    if err := a.ensureTunnelExclusive(cfg.TunnelID, model.PrimaryInstanceID); err != nil {
        return CloudflareUnbindResult{}, err
    }
    domain, tunnelID, tunnelName := cfg.Domain, cfg.TunnelID, cfg.TunnelName
    if strings.TrimSpace(tunnelID) == "" {
        return CloudflareUnbindResult{}, errors.New("主实例没有可解绑的 Cloudflare Tunnel")
    }

    _, tunnelStatus, _ := a.process.Status()
    previousDesired := a.tunnelDesired
    a.tunnelDesired = false
    if err := a.process.StopTunnel(); err != nil {
        a.tunnelDesired = previousDesired
        return CloudflareUnbindResult{}, err
    }
    if _, err := a.tunnel.Delete(ctx, cfg); err != nil {
        a.tunnelDesired = previousDesired
        if tunnelStatus.Running {
            _ = a.process.StartTunnel(cfg)
        }
        return CloudflareUnbindResult{}, err
    }
    cfg.Domain = ""
    cfg.TunnelID = ""
    if _, err := a.config.Replace(cfg); err != nil {
        a.tunnelDesired = previousDesired
        return CloudflareUnbindResult{}, fmt.Errorf("Cloudflare 已删除，但清理本地主实例配置失败: %w", err)
    }
    return CloudflareUnbindResult{
        InstanceID: model.PrimaryInstanceID,
        Domain: domain,
        TunnelID: tunnelID,
        TunnelName: tunnelName,
        Message: fmt.Sprintf("已解绑主实例，并删除 Tunnel %s 及其 Cloudflare 路由", tunnelName),
    }, nil
}

func (a *App) ensureTunnelExclusive(tunnelID, exceptID string) error {
    tunnelID = strings.TrimSpace(tunnelID)
    if tunnelID == "" {
        return nil
    }
    users := make([]string, 0, 2)
    primary := a.config.Get()
    if exceptID != model.PrimaryInstanceID && strings.EqualFold(strings.TrimSpace(primary.TunnelID), tunnelID) {
        users = append(users, "主实例")
    }

    records := a.instances.List()
    a.instanceMu.RLock()
    runtimes := make(map[string]*managedInstance, len(records))
    for _, record := range records {
        runtimes[record.ID] = a.instanceRuntime[record.ID]
    }
    a.instanceMu.RUnlock()
    for _, record := range records {
        if record.ID == exceptID || runtimes[record.ID] == nil {
            continue
        }
        if strings.EqualFold(strings.TrimSpace(runtimes[record.ID].config.Get().TunnelID), tunnelID) {
            users = append(users, record.Name)
        }
    }
    if len(users) > 0 {
        return fmt.Errorf("当前 Tunnel 仍被其他实例复用（%s），为避免误删它们的 DNS，不能直接解绑整个 Tunnel", strings.Join(users, "、"))
    }
    return nil
}

func (a *App) CheckCloudflaredUpdate(ctx context.Context) (CloudflaredUpdateStatus, error) {
    cfg := a.config.Get()
    currentVersion, installed := a.currentCloudflaredVersion(ctx, cfg)
    status := CloudflaredUpdateStatus{
        Installed:      installed,
        Executable:     cfg.CloudflaredExecutable,
        CurrentVersion: currentVersion,
    }

    assetName, err := cloudflaredAssetName()
    if err != nil {
        return status, err
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudflaredLatestReleaseURL, nil)
    if err != nil {
        return status, err
    }
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("User-Agent", "MCP-DevDesk/"+Version)
    response, err := a.updates.HTTPClient().Do(req)
    if err != nil {
        return status, fmt.Errorf("检查 cloudflared 更新失败: %w", err)
    }
    defer response.Body.Close()
    if response.StatusCode < 200 || response.StatusCode >= 300 {
        return status, fmt.Errorf("检查 cloudflared 更新失败: GitHub HTTP %d", response.StatusCode)
    }
    var release struct {
        TagName string `json:"tag_name"`
        HTMLURL string `json:"html_url"`
        Assets []struct {
            Name string `json:"name"`
            URL string `json:"browser_download_url"`
            Digest string `json:"digest"`
        } `json:"assets"`
    }
    if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
        return status, fmt.Errorf("解析 cloudflared Release 失败: %w", err)
    }
    latest := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
    var downloadURL, digest string
    for _, asset := range release.Assets {
        if asset.Name == assetName {
            downloadURL = asset.URL
            digest = strings.TrimPrefix(strings.TrimSpace(asset.Digest), "sha256:")
            break
        }
    }
    if downloadURL == "" {
        return status, fmt.Errorf("Cloudflare Release %s 缺少 %s", latest, assetName)
    }
    if len(digest) != 64 {
        return status, errors.New("Cloudflare Release 未提供可验证的 SHA256 digest，已拒绝自动更新")
    }
    status.LatestVersion = latest
    status.AssetName = assetName
    status.PageURL = release.HTMLURL
    status.downloadURL = downloadURL
    status.sha256 = strings.ToLower(digest)
    status.UpdateAvailable = !installed || compareCloudflaredVersions(currentVersion, latest) < 0
    return status, nil
}

func (a *App) InstallCloudflaredUpdate(ctx context.Context) (CloudflaredUpdateResult, error) {
    release, err := a.CheckCloudflaredUpdate(ctx)
    if err != nil {
        return CloudflaredUpdateResult{}, err
    }
    previous := release.CurrentVersion
    if !release.UpdateAvailable {
        return CloudflaredUpdateResult{Status: release, PreviousVersion: previous, Message: "cloudflared 已经是最新版本"}, nil
    }

    cfg := a.config.Get()
    target := filepath.Clean(cfg.CloudflaredExecutable)
    if strings.TrimSpace(target) == "" {
        return CloudflaredUpdateResult{}, errors.New("cloudflared 可执行文件路径为空")
    }
    if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
        return CloudflaredUpdateResult{}, err
    }
    temp, err := os.CreateTemp(filepath.Dir(target), ".cloudflared-update-*.exe")
    if err != nil {
        return CloudflaredUpdateResult{}, err
    }
    tempPath := temp.Name()
    keepTemp := false
    defer func() {
        _ = temp.Close()
        if !keepTemp {
            _ = os.Remove(tempPath)
        }
    }()

    request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.downloadURL, nil)
    if err != nil {
        return CloudflaredUpdateResult{}, err
    }
    request.Header.Set("User-Agent", "MCP-DevDesk/"+Version)
    response, err := a.updates.HTTPClient().Do(request)
    if err != nil {
        return CloudflaredUpdateResult{}, fmt.Errorf("下载 cloudflared 失败: %w", err)
    }
    if response.StatusCode < 200 || response.StatusCode >= 300 {
        response.Body.Close()
        return CloudflaredUpdateResult{}, fmt.Errorf("下载 cloudflared 失败: GitHub HTTP %d", response.StatusCode)
    }
    hasher := sha256.New()
    _, copyErr := io.Copy(io.MultiWriter(temp, hasher), response.Body)
    response.Body.Close()
    closeErr := temp.Close()
    if copyErr != nil {
        return CloudflaredUpdateResult{}, fmt.Errorf("保存 cloudflared 更新文件失败: %w", copyErr)
    }
    if closeErr != nil {
        return CloudflaredUpdateResult{}, closeErr
    }
    actualDigest := hex.EncodeToString(hasher.Sum(nil))
    if !strings.EqualFold(actualDigest, release.sha256) {
        return CloudflaredUpdateResult{}, fmt.Errorf("cloudflared SHA256 校验失败: expected %s, got %s", release.sha256, actualDigest)
    }
    _ = os.Chmod(tempPath, 0o755)

    states, restoreDesired := a.pauseCloudflaredWatchdogs(target)
    restored := false
    defer func() {
        if !restored {
            restoreDesired()
        }
    }()

    for _, state := range states {
        _ = state.manager.StopLogin()
        _ = state.manager.StopTunnel()
    }
    if processes, listErr := processmanager.ListCloudflaredProcesses(); listErr == nil {
        for _, process := range processes {
            if process.ProcessPath == "" || samePath(process.ProcessPath, target) {
                _ = processmanager.StopCloudflaredProcess(process.PID)
            }
        }
    }
    if err := waitForCloudflaredExecutableStopped(target, 10*time.Second); err != nil {
        restoreDesired()
        restored = true
        restartCloudflaredStates(states)
        return CloudflaredUpdateResult{}, err
    }

    backup := target + ".devdesk-backup"
    _ = os.Remove(backup)
    targetExists := false
    if _, statErr := os.Stat(target); statErr == nil {
        targetExists = true
        if err := os.Rename(target, backup); err != nil {
            restoreDesired()
            restored = true
            restartCloudflaredStates(states)
            return CloudflaredUpdateResult{}, fmt.Errorf("备份旧 cloudflared.exe 失败: %w", err)
        }
    }
    if err := os.Rename(tempPath, target); err != nil {
        if targetExists {
            _ = os.Rename(backup, target)
        }
        restoreDesired()
        restored = true
        restartCloudflaredStates(states)
        return CloudflaredUpdateResult{}, fmt.Errorf("替换 cloudflared.exe 失败: %w", err)
    }
    keepTemp = true

    verifyCfg := cfg
    verifyCfg.CloudflaredExecutable = target
    verifyVersion, ok := a.currentCloudflaredVersion(ctx, verifyCfg)
    if !ok || compareCloudflaredVersions(verifyVersion, release.LatestVersion) < 0 {
        _ = os.Remove(target)
        if targetExists {
            _ = os.Rename(backup, target)
        }
        restoreDesired()
        restored = true
        restartCloudflaredStates(states)
        return CloudflaredUpdateResult{}, fmt.Errorf("新 cloudflared.exe 验证失败，检测版本 %q", verifyVersion)
    }
    _ = os.Remove(backup)

    restoreDesired()
    restored = true
    restarted, restartErrors := restartCloudflaredStates(states)
    release.CurrentVersion = verifyVersion
    release.Installed = true
    release.UpdateAvailable = compareCloudflaredVersions(verifyVersion, release.LatestVersion) < 0
    message := fmt.Sprintf("cloudflared 已更新 %s → %s，并恢复 %d 个 Tunnel", previous, verifyVersion, restarted)
    return CloudflaredUpdateResult{
        Status: release,
        PreviousVersion: previous,
        RestartedTunnels: restarted,
        RestartErrors: restartErrors,
        Message: message,
    }, nil
}

type cloudflaredRuntimeState struct {
    manager *processmanager.Manager
    cfg model.Config
    wasRunning bool
}

func (a *App) pauseCloudflaredWatchdogs(target string) ([]cloudflaredRuntimeState, func()) {
    states := make([]cloudflaredRuntimeState, 0, len(a.instanceRuntime)+1)
    a.mu.Lock()
    primaryDesired := a.tunnelDesired
    a.tunnelDesired = false
    primaryCfg := a.config.Get()
    _, primaryTunnel, _ := a.process.Status()
    if samePath(primaryCfg.CloudflaredExecutable, target) {
        states = append(states, cloudflaredRuntimeState{manager: a.process, cfg: primaryCfg, wasRunning: primaryTunnel.Running})
    }
    a.mu.Unlock()

    a.instanceMu.RLock()
    runtimes := make([]*managedInstance, 0, len(a.instanceRuntime))
    for _, managed := range a.instanceRuntime {
        runtimes = append(runtimes, managed)
    }
    a.instanceMu.RUnlock()
    desired := make(map[*managedInstance]bool, len(runtimes))
    for _, managed := range runtimes {
        managed.mu.Lock()
        desired[managed] = managed.desiredRunning
        managed.desiredRunning = false
        cfg := managed.config.Get()
        _, tunnelStatus, _ := managed.process.Status()
        if samePath(cfg.CloudflaredExecutable, target) {
            states = append(states, cloudflaredRuntimeState{manager: managed.process, cfg: cfg, wasRunning: tunnelStatus.Running})
        }
        managed.mu.Unlock()
    }
    return states, func() {
        a.mu.Lock()
        a.tunnelDesired = primaryDesired
        a.mu.Unlock()
        for managed, value := range desired {
            managed.mu.Lock()
            managed.desiredRunning = value
            managed.mu.Unlock()
        }
    }
}

func restartCloudflaredStates(states []cloudflaredRuntimeState) (int, []string) {
    restarted := 0
    var problems []string
    for _, state := range states {
        if !state.wasRunning || strings.TrimSpace(state.cfg.TunnelID) == "" || strings.TrimSpace(state.cfg.Domain) == "" {
            continue
        }
        if err := state.manager.StartTunnel(state.cfg); err != nil {
            problems = append(problems, fmt.Sprintf("%s: %v", state.cfg.TunnelName, err))
            continue
        }
        restarted++
    }
    return restarted, problems
}

func waitForCloudflaredExecutableStopped(target string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        processes, err := processmanager.ListCloudflaredProcesses()
        if err == nil {
            found := false
            for _, process := range processes {
                if process.ProcessPath == "" || samePath(process.ProcessPath, target) {
                    found = true
                    break
                }
            }
            if !found {
                return nil
            }
        }
        time.Sleep(200 * time.Millisecond)
    }
    return errors.New("等待 cloudflared 进程退出超时，请先停止正在运行的 Tunnel 后重试")
}

func (a *App) currentCloudflaredVersion(ctx context.Context, cfg model.Config) (string, bool) {
    if _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {
        return "", false
    }
    versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    output, err := a.tunnel.Version(versionCtx, cfg)
    if err != nil {
        return "", true
    }
    match := cloudflaredVersionPattern.FindStringSubmatch(output)
    if len(match) < 2 {
        return strings.TrimSpace(output), true
    }
    return match[1], true
}

func cloudflaredAssetName() (string, error) {
    if runtime.GOOS != "windows" {
        return "", fmt.Errorf("当前自动更新仅支持 Windows，检测到 %s/%s", runtime.GOOS, runtime.GOARCH)
    }
    switch runtime.GOARCH {
    case "amd64", "386":
        return "cloudflared-windows-" + runtime.GOARCH + ".exe", nil
    default:
        return "", fmt.Errorf("Cloudflare 官方 Release 暂未提供受支持的 Windows %s 资产", runtime.GOARCH)
    }
}

func compareCloudflaredVersions(left, right string) int {
    parse := func(value string) []int {
        value = strings.TrimPrefix(strings.TrimSpace(value), "v")
        fields := strings.Split(value, ".")
        values := make([]int, len(fields))
        for index, field := range fields {
            values[index], _ = strconv.Atoi(field)
        }
        return values
    }
    a, b := parse(left), parse(right)
    length := len(a)
    if len(b) > length { length = len(b) }
    for index := 0; index < length; index++ {
        av, bv := 0, 0
        if index < len(a) { av = a[index] }
        if index < len(b) { bv = b[index] }
        if av < bv { return -1 }
        if av > bv { return 1 }
    }
    return 0
}
