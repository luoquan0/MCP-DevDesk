from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def replace_once(rel, old, new):
    path = ROOT / rel
    text = path.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"anchor not found in {rel}: {old[:120]!r}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def write(rel, content):
    path = ROOT / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


replace_once(
    "app/internal/updater/proxy.go",
    "func directUpdateTransport() *http.Transport {",
    "// HTTPClient returns the same direct/HTTP/SOCKS5-aware client used by MCP DevDesk updates.\n"
    "// Auxiliary component updaters use it so the user's update proxy applies consistently.\n"
    "func (m *Manager) HTTPClient() *http.Client { return m.updateHTTPClient() }\n\n"
    "func directUpdateTransport() *http.Transport {",
)

replace_once(
    "app/internal/tunnel/cloudflare.go",
    "func NewClient() *Client { return &Client{} }\n",
    "func NewClient() *Client { return &Client{} }\n\n"
    "func (c *Client) Version(ctx context.Context, cfg model.Config) (string, error) {\n"
    "\tif _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {\n"
    "\t\treturn \"\", err\n"
    "\t}\n"
    "\treturn c.run(ctx, cfg, \"--version\")\n"
    "}\n",
)

replace_once(
    "app/internal/tunnel/cloudflare.go",
    "func waitForDNS(ctx context.Context, domain string, timeout time.Duration) (bool, error) {",
    "func deleteTunnelArguments(tunnelID string) []string {\n"
    "\treturn []string{\"tunnel\", \"delete\", \"--force\", tunnelID}\n"
    "}\n\n"
    "// Delete removes the named Tunnel from Cloudflare. --force asks cloudflared to\n"
    "// cascade the Tunnel dependencies, including DNS routes attached to it.\n"
    "func (c *Client) Delete(ctx context.Context, cfg model.Config) (string, error) {\n"
    "\ttunnelID := strings.ToLower(strings.TrimSpace(cfg.TunnelID))\n"
    "\tif !uuidPattern.MatchString(tunnelID) {\n"
    "\t\treturn \"\", errors.New(\"当前配置没有有效的 Tunnel UUID\")\n"
    "\t}\n"
    "\tif _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {\n"
    "\t\treturn \"\", fmt.Errorf(\"cloudflared.exe 不存在: %w\", err)\n"
    "\t}\n"
    "\tif _, err := os.Stat(processmanager.CertificatePath()); err != nil {\n"
    "\t\treturn \"\", errors.New(\"Cloudflare 尚未授权，无法清理远端 Tunnel\")\n"
    "\t}\n"
    "\tcommandCtx, cancel := context.WithTimeout(ctx, 60*time.Second)\n"
    "\tdefer cancel()\n"
    "\toutput, err := c.run(commandCtx, cfg, deleteTunnelArguments(tunnelID)... )\n"
    "\tif err != nil {\n"
    "\t\tlower := strings.ToLower(output)\n"
    "\t\tif !strings.Contains(lower, \"not found\") && !strings.Contains(lower, \"does not exist\") && !strings.Contains(lower, \"no tunnel\") {\n"
    "\t\t\treturn output, fmt.Errorf(\"删除 Cloudflare Tunnel 失败: %w; %s\", err, compactOutput(output))\n"
    "\t\t}\n"
    "\t}\n"
    "\t_ = os.Remove(processmanager.CredentialsPath(tunnelID))\n"
    "\treturn output, nil\n"
    "}\n\n"
    "func waitForDNS(ctx context.Context, domain string, timeout time.Duration) (bool, error) {",
)

replace_once(
    "app/internal/web/server.go",
    "\tmux.HandleFunc(\"POST /api/instances/{id}/cloudflare/repair-dns\", s.handleRepairInstanceTunnelDNS)\n",
    "\tmux.HandleFunc(\"POST /api/instances/{id}/cloudflare/repair-dns\", s.handleRepairInstanceTunnelDNS)\n"
    "\tmux.HandleFunc(\"DELETE /api/instances/{id}/cloudflare\", s.handleUnbindInstanceCloudflare)\n",
)
replace_once(
    "app/internal/web/server.go",
    "\tmux.HandleFunc(\"POST /api/cloudflare/configure\", s.handleCloudflareConfigure)\n",
    "\tmux.HandleFunc(\"POST /api/cloudflare/configure\", s.handleCloudflareConfigure)\n"
    "\tmux.HandleFunc(\"POST /api/cloudflared/update/check\", s.handleCheckCloudflaredUpdate)\n"
    "\tmux.HandleFunc(\"POST /api/cloudflared/update/install\", s.handleInstallCloudflaredUpdate)\n",
)

write("app/internal/application/cloudflare_manage.go", r'''package application

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
''')

write("app/internal/web/cloudflare_manage.go", r'''package web

import (
    "context"
    "net/http"
    "time"
)

func (s *Server) handleUnbindInstanceCloudflare(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
    defer cancel()
    result, err := s.app.UnbindInstanceCloudflare(ctx, r.PathValue("id"))
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCheckCloudflaredUpdate(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
    defer cancel()
    result, err := s.app.CheckCloudflaredUpdate(ctx)
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleInstallCloudflaredUpdate(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
    defer cancel()
    result, err := s.app.InstallCloudflaredUpdate(ctx)
    if err != nil {
        writeError(w, http.StatusBadRequest, err)
        return
    }
    writeJSON(w, http.StatusOK, result)
}
''')

write("app/internal/application/cloudflare_manage_test.go", r'''package application

import "testing"

func TestCompareCloudflaredVersions(t *testing.T) {
    cases := []struct {
        left string
        right string
        want int
    }{
        {"2026.8.2", "2026.8.3", -1},
        {"2026.8.3", "2026.8.3", 0},
        {"2026.9.0", "2026.8.3", 1},
        {"v2026.8.3", "2026.8.3", 0},
    }
    for _, tc := range cases {
        got := compareCloudflaredVersions(tc.left, tc.right)
        if got != tc.want {
            t.Fatalf("compareCloudflaredVersions(%q, %q)=%d want %d", tc.left, tc.right, got, tc.want)
        }
    }
}

func TestCloudflaredVersionPattern(t *testing.T) {
    match := cloudflaredVersionPattern.FindStringSubmatch("cloudflared version 2026.8.3 (built 2026-08-31)")
    if len(match) < 2 || match[1] != "2026.8.3" {
        t.Fatalf("unexpected version match: %#v", match)
    }
}
''')

write("app/internal/tunnel/cloudflare_delete_test.go", r'''package tunnel

import (
    "reflect"
    "testing"
)

func TestDeleteTunnelArgumentsForceCascade(t *testing.T) {
    got := deleteTunnelArguments("12345678-1234-1234-1234-123456789abc")
    want := []string{"tunnel", "delete", "--force", "12345678-1234-1234-1234-123456789abc"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("deleteTunnelArguments()=%v want %v", got, want)
    }
}
''')

write("frontend/src/components/settings/CloudflaredUpdateCard.vue", r'''<script setup lang="ts">
import { onMounted, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { api } from "@/services/api";
import { useUiStore } from "@/stores/ui";

interface CloudflaredUpdateStatus {
  installed: boolean;
  executable: string;
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
  assetName: string;
  pageUrl: string;
}
interface CloudflaredUpdateResult {
  status: CloudflaredUpdateStatus;
  previousVersion: string;
  restartedTunnels: number;
  restartErrors?: string[];
  message: string;
}

const ui = useUiStore();
const status = ref<CloudflaredUpdateStatus | null>(null);
const checking = ref(false);
const installing = ref(false);
const feedback = ref("");

async function check(showToast = true) {
  if (checking.value) return;
  checking.value = true;
  feedback.value = "正在检查 Cloudflare 官方 Release…";
  try {
    status.value = await api<CloudflaredUpdateStatus>("/api/cloudflared/update/check", { method: "POST" });
    feedback.value = status.value.updateAvailable
      ? `发现 cloudflared ${status.value.latestVersion}`
      : `cloudflared ${status.value.currentVersion || status.value.latestVersion} 已是最新`;
    if (showToast) ui.toast(status.value.updateAvailable ? "发现 cloudflared 更新" : "cloudflared 已是最新", feedback.value, status.value.updateAvailable ? "info" : "success");
  } catch (error) {
    feedback.value = error instanceof Error ? error.message : String(error);
    if (showToast) ui.toast("检查 cloudflared 更新失败", feedback.value, "danger");
  } finally {
    checking.value = false;
  }
}

async function install() {
  if (installing.value) return;
  if (!status.value?.updateAvailable) await check(false);
  if (!status.value?.updateAvailable) return;
  const accepted = await ui.ask({
    title: "更新 cloudflared",
    message: `将把 ${status.value.currentVersion || "当前版本"} 更新到 ${status.value.latestVersion}。更新时会短暂停止使用同一 cloudflared.exe 的 Tunnel，校验 SHA256 后替换文件，再自动恢复原来运行中的 Tunnel。`,
    confirmLabel: "更新 cloudflared",
  });
  if (!accepted) return;
  installing.value = true;
  feedback.value = "正在下载、校验并替换 cloudflared.exe…";
  try {
    const result = await api<CloudflaredUpdateResult>("/api/cloudflared/update/install", { method: "POST" });
    status.value = result.status;
    if (result.restartErrors?.length) {
      feedback.value = `${result.message}；${result.restartErrors.length} 个 Tunnel 恢复失败`;
      ui.toast("cloudflared 已更新，但部分 Tunnel 未恢复", result.restartErrors.join("；"), "warning");
    } else {
      feedback.value = result.message;
      ui.toast("cloudflared 更新完成", result.message, "success");
    }
  } catch (error) {
    feedback.value = error instanceof Error ? error.message : String(error);
    ui.toast("cloudflared 更新失败", feedback.value, "danger");
  } finally {
    installing.value = false;
  }
}

onMounted(() => { void check(false); });
</script>

<template>
  <AppCard class="cloudflared-update-card">
    <div class="card-heading">
      <div>
        <span class="eyebrow">Cloudflare Tunnel Runtime</span>
        <h3>cloudflared 更新</h3>
        <p>独立维护隧道程序版本。使用上方“软件更新”的代理设置连接 GitHub，并校验 Cloudflare Release 提供的 SHA256 digest。</p>
      </div>
      <StatusPill :tone="status?.updateAvailable ? 'info' : (status?.installed ? 'success' : 'warning')">
        {{ status?.updateAvailable ? `可更新 ${status.latestVersion}` : (status?.currentVersion ? `当前 ${status.currentVersion}` : '检测中') }}
      </StatusPill>
    </div>

    <div class="cloudflared-version-grid">
      <div><span>当前版本</span><strong>{{ status?.currentVersion || '--' }}</strong></div>
      <div><span>官方最新</span><strong>{{ status?.latestVersion || '--' }}</strong></div>
      <div class="cloudflared-path"><span>程序位置</span><code>{{ status?.executable || 'cloudflared.exe' }}</code></div>
    </div>

    <div class="form-footer top-divider">
      <small>{{ feedback || '进入软件设置时会自动检查一次；更新过程只重启 Tunnel，不会停止 MCP 核心。' }}</small>
      <div class="form-footer-actions">
        <AppButton tone="secondary" icon="refresh" :loading="checking" :disabled="installing" @click="check(true)">检查 cloudflared</AppButton>
        <AppButton v-if="status?.updateAvailable" tone="primary" icon="cloud" :loading="installing" :disabled="checking" @click="install">立即更新</AppButton>
      </div>
    </div>
  </AppCard>
</template>

<style scoped>
.cloudflared-version-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 16px; }
.cloudflared-version-grid > div { min-width: 0; padding: 14px 16px; border: 1px solid var(--border-subtle); border-radius: 14px; background: color-mix(in srgb, var(--surface-card) 88%, transparent); }
.cloudflared-version-grid span { display: block; color: var(--text-tertiary); font-size: 12px; margin-bottom: 6px; }
.cloudflared-version-grid strong, .cloudflared-version-grid code { display: block; overflow-wrap: anywhere; }
.cloudflared-path { grid-column: 1 / -1; }
@media (max-width: 720px) { .cloudflared-version-grid { grid-template-columns: 1fr; } .cloudflared-path { grid-column: auto; } }
</style>
''')

cloudflare_page = r'''<script setup lang="ts">
import { computed, reactive } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { api } from "@/services/api";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();

const form = reactive({
  domain: app.config?.domain ?? "",
  tunnelName: app.config?.tunnelName ?? "mcp-devdesk",
  reuse: true,
});

const bindings = computed(() => app.instances.filter((instance) => instance.domain && instance.tunnelId));
function sharedTunnelCount(tunnelId?: string) {
  if (!tunnelId) return 0;
  return app.instances.filter((instance) => instance.tunnelId?.toLowerCase() === tunnelId.toLowerCase()).length;
}

async function login() {
  try { await app.startCloudflareLogin(); }
  catch (error) { ui.toast("授权启动失败", error instanceof Error ? error.message : String(error), "danger"); }
}

async function configure() {
  try { await app.configureTunnel({ ...form }); }
  catch (error) { ui.toast("Tunnel 配置失败", error instanceof Error ? error.message : String(error), "danger"); }
}

async function syncPort() {
  const accepted = await ui.ask({
    title: "同步 Tunnel 到当前端口",
    message: `程序会清理当前 Tunnel 的旧连接，并创建一个指向 ${app.status?.tunnelInventory.expectedLocalUrl || '当前 MCP 端口'} 的连接。`,
    confirmLabel: "同步端口",
  });
  if (!accepted) return;
  try { await app.syncTunnelPort(); }
  catch (error) { ui.toast("同步失败", error instanceof Error ? error.message : String(error), "danger"); }
}

async function stopProcess(pid: number, name?: string) {
  const accepted = await ui.ask({
    title: "关闭 Tunnel 进程",
    message: `将结束 ${name || 'cloudflared'}（PID ${pid}）。对应公网连接会立即中断。`,
    confirmLabel: "关闭进程",
    danger: true,
  });
  if (!accepted) return;
  try { await app.stopTunnelProcess(pid); }
  catch (error) { ui.toast("进程关闭失败", error instanceof Error ? error.message : String(error), "danger"); }
}

async function unbind(instance: (typeof app.instances)[number]) {
  const shared = sharedTunnelCount(instance.tunnelId);
  if (shared > 1) {
    ui.toast("该 Tunnel 正在被复用", `共有 ${shared} 个 MCP 实例使用 ${instance.tunnelName || instance.tunnelId}，为避免误删其他 DNS，暂不允许删除整个 Tunnel。`, "warning");
    return;
  }
  const accepted = await ui.ask({
    title: "解绑并清理 Cloudflare",
    message: `将停止 ${instance.name} 的 Tunnel，并从 Cloudflare 删除 ${instance.tunnelName || instance.tunnelId}。该 Tunnel 关联的 DNS 路由（包括 ${instance.domain}）会一起清理，本地 MCP 项目和端口不会删除。`,
    confirmLabel: "删除 DNS + Tunnel",
    danger: true,
  });
  if (!accepted) return;
  try {
    const result = await api<{ message: string }>(`/api/instances/${encodeURIComponent(instance.id)}/cloudflare`, { method: "DELETE" });
    await Promise.all([app.loadInstances(), app.refreshStatus(true), instance.id === "primary" ? app.loadConfig() : Promise.resolve()]);
    if (instance.id === "primary") {
      form.domain = app.config?.domain || "";
      form.tunnelName = app.config?.tunnelName || "mcp-devdesk";
    }
    ui.toast("Cloudflare 已解绑", result.message, "success");
  } catch (error) {
    ui.toast("解绑 Cloudflare 失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack cloudflare-page">
    <PageHeader eyebrow="Secure access" title="Cloudflare" description="管理固定域名、Tunnel 身份、本机 cloudflared 连接和已绑定资源。">
      <template #actions>
        <AppButton tone="secondary" icon="refresh" :loading="app.refreshing" @click="app.refreshStatus()">刷新状态</AppButton>
        <AppButton tone="primary" icon="network" :loading="app.actionPending === 'sync-tunnel'" @click="syncPort">同步端口</AppButton>
      </template>
    </PageHeader>

    <section class="cloudflare-summary-grid">
      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top"><div class="summary-symbol is-orange"><AppIcon name="cloud" :size="22" /></div><StatusPill :tone="app.status?.cloudflare.authenticated ? 'success' : 'warning'">{{ app.status?.cloudflare.authenticated ? '已授权' : '未授权' }}</StatusPill></div>
        <span>Cloudflare 账户</span><strong>{{ app.status?.cloudflare.authenticated ? '可以管理 Tunnel' : '需要浏览器授权' }}</strong>
        <AppButton tone="quiet" compact icon="external" :loading="app.actionPending === 'cloudflare-login'" @click="login">{{ app.status?.cloudflare.authenticated ? '重新授权' : '开始授权' }}</AppButton>
      </AppCard>
      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top"><div class="summary-symbol is-blue"><AppIcon name="globe" :size="22" /></div><StatusPill :tone="app.status?.cloudflare.domain ? 'success' : 'neutral'">固定域名</StatusPill></div>
        <span>公网 MCP 地址</span><strong class="break-all">{{ app.status?.remoteMcpUrl || '尚未配置' }}</strong><small class="mono">{{ app.status?.cloudflare.tunnelId || 'Tunnel ID 未生成' }}</small>
      </AppCard>
      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top"><div class="summary-symbol is-mint"><AppIcon name="network" :size="22" /></div><StatusPill :tone="app.status?.tunnel.running ? 'success' : 'neutral'">{{ app.status?.tunnel.running ? 'Connected' : 'Offline' }}</StatusPill></div>
        <span>本地转发目标</span><strong class="mono">{{ app.status?.tunnelInventory.expectedLocalUrl || '--' }}</strong><small>{{ app.status?.tunnelInventory.count ?? 0 }} 个进程 · {{ app.status?.tunnelInventory.duplicateCount ?? 0 }} 个重复</small>
      </AppCard>
    </section>

    <AppCard class="binding-card">
      <div class="card-heading">
        <div><span class="eyebrow">Managed bindings</span><h3>已绑定域名与 Tunnel</h3><p>这里列出 MCP DevDesk 当前管理的 Cloudflare 域名。解绑会同时清理该 Tunnel 在 Cloudflare 上的路由依赖。</p></div>
        <StatusPill :tone="bindings.length ? 'info' : 'neutral'">{{ bindings.length }} 个绑定</StatusPill>
      </div>
      <div v-if="bindings.length" class="binding-list">
        <article v-for="instance in bindings" :key="instance.id" class="binding-row">
          <div class="binding-icon"><AppIcon name="globe" :size="18" /></div>
          <div class="binding-main">
            <div class="binding-title"><strong>{{ instance.domain }}</strong><StatusPill :tone="instance.tunnel.running ? 'success' : 'neutral'" :dot="false">{{ instance.tunnel.running ? 'Tunnel 在线' : 'Tunnel 离线' }}</StatusPill><StatusPill v-if="sharedTunnelCount(instance.tunnelId) > 1" tone="warning" :dot="false">复用 {{ sharedTunnelCount(instance.tunnelId) }}</StatusPill></div>
            <span>{{ instance.name }} · MCP {{ instance.mcpPort }} · {{ instance.tunnelName || '未命名 Tunnel' }}</span>
            <code>{{ instance.tunnelId }}</code>
          </div>
          <AppButton tone="danger" compact :disabled="!app.status?.cloudflare.authenticated || sharedTunnelCount(instance.tunnelId) > 1" @click="unbind(instance)">解绑并清理</AppButton>
        </article>
      </div>
      <div v-else class="inline-empty large"><AppIcon name="cloud" :size="26" /><div><strong>还没有已绑定资源</strong><span>配置固定域名后，域名、Tunnel UUID 和对应 MCP 实例会集中显示在这里。</span></div></div>
      <small class="binding-note">只展示 MCP DevDesk 当前配置中管理的绑定，不会删除 Cloudflare 账户里由其他工具创建的 Tunnel。</small>
    </AppCard>

    <section class="cloudflare-layout">
      <AppCard class="cloudflare-config-card">
        <div class="card-heading"><div><span class="eyebrow">Fixed domain</span><h3>固定域名配置</h3></div><StatusPill :tone="app.status?.cloudflare.authenticated ? 'success' : 'warning'">{{ app.status?.cloudflare.authenticated ? 'Ready' : 'Login required' }}</StatusPill></div>
        <form class="stack-form" @submit.prevent="configure">
          <label class="field"><span>完整域名</span><input v-model="form.domain" placeholder="mcp.example.com" spellcheck="false" /><small>域名必须托管在当前 Cloudflare 账户中。</small></label>
          <label class="field"><span>Tunnel 名称</span><input v-model="form.tunnelName" placeholder="mcp-devdesk" spellcheck="false" /></label>
          <label class="checkbox-row"><input v-model="form.reuse" type="checkbox" /><span><strong>优先复用同名 Tunnel</strong><small>存在时保留 UUID，只更新 DNS 与本地目标。</small></span></label>
          <div class="form-footer"><span>当前 MCP 端口：<code>{{ app.config?.mcpPort || '--' }}</code></span><AppButton tone="primary" type="submit" icon="cloud" :loading="app.actionPending === 'configure-tunnel'" :disabled="!app.status?.cloudflare.authenticated">自动配置</AppButton></div>
        </form>
      </AppCard>

      <AppCard class="tunnel-process-card">
        <div class="card-heading"><div><span class="eyebrow">Process supervision</span><h3>隧道进程</h3></div><div class="heading-pills"><StatusPill tone="neutral">{{ app.status?.tunnelInventory.count ?? 0 }} 个</StatusPill><StatusPill v-if="app.status?.tunnelInventory.duplicateCount" tone="warning">重复 {{ app.status.tunnelInventory.duplicateCount }}</StatusPill></div></div>
        <div v-if="app.status?.tunnelInventory.processes.length" class="tunnel-list">
          <article v-for="process in app.status.tunnelInventory.processes" :key="process.pid" class="tunnel-row" :class="{ 'is-current': process.matchesConfig, 'is-duplicate': process.duplicate }">
            <div class="tunnel-row-icon"><AppIcon name="network" :size="17" /></div>
            <div class="tunnel-row-main"><div class="tunnel-row-title"><strong>{{ process.tunnelName || process.tunnelId || 'Cloudflare Tunnel' }}</strong><span class="tunnel-tags"><StatusPill v-if="process.managed" tone="info" :dot="false">本程序管理</StatusPill><StatusPill v-if="process.matchesConfig" tone="success" :dot="false">当前配置</StatusPill><StatusPill v-if="process.duplicate" tone="warning" :dot="false">重复</StatusPill></span></div><code>{{ process.localUrl || '未识别本地目标' }}</code><small>PID {{ process.pid }} · {{ process.tunnelId || 'UUID 未识别' }}</small></div>
            <AppButton tone="danger" compact :loading="app.actionPending === `stop-tunnel-${process.pid}`" @click="stopProcess(process.pid, process.tunnelName)">关闭</AppButton>
          </article>
        </div>
        <div v-else class="inline-empty large"><AppIcon name="cloud" :size="26" /><div><strong>没有运行中的 Tunnel</strong><span>启动服务或完成固定域名配置后，连接会出现在这里。</span></div></div>
      </AppCard>
    </section>
  </div>
</template>

<style scoped>
.binding-list { display: grid; gap: 10px; margin-top: 16px; }
.binding-row { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 12px; align-items: center; padding: 14px; border: 1px solid var(--border-subtle); border-radius: 16px; background: color-mix(in srgb, var(--surface-card) 88%, transparent); }
.binding-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 12px; background: color-mix(in srgb, var(--accent) 12%, transparent); }
.binding-main { min-width: 0; display: grid; gap: 4px; }
.binding-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.binding-main span, .binding-note { color: var(--text-tertiary); font-size: 12px; }
.binding-main code { overflow-wrap: anywhere; }
.binding-note { display: block; margin-top: 12px; }
@media (max-width: 760px) { .binding-row { grid-template-columns: auto minmax(0, 1fr); } .binding-row > .app-button { grid-column: 1 / -1; justify-self: stretch; } }
</style>
'''
write("frontend/src/pages/CloudflarePage.vue", cloudflare_page)

replace_once(
    "frontend/src/pages/SettingsPage.vue",
    'import SecuritySettingsSection from "@/components/settings/SecuritySettingsSection.vue";\n',
    'import SecuritySettingsSection from "@/components/settings/SecuritySettingsSection.vue";\nimport CloudflaredUpdateCard from "@/components/settings/CloudflaredUpdateCard.vue";\n',
)

settings_path = ROOT / "frontend/src/pages/SettingsPage.vue"
settings = settings_path.read_text(encoding="utf-8")
start = settings.find('<AppCard v-if="activeSettingsSection === \'software\'" class="software-update-card">')
if start < 0:
    raise SystemExit("software-update-card start not found")
pos = start
depth = 0
end = -1
while pos < len(settings):
    next_open = settings.find("<AppCard", pos)
    next_close = settings.find("</AppCard>", pos)
    if next_close < 0:
        break
    if next_open >= 0 and next_open < next_close:
        depth += 1
        pos = next_open + len("<AppCard")
    else:
        depth -= 1
        pos = next_close + len("</AppCard>")
        if depth == 0:
            end = pos
            break
if end < 0:
    raise SystemExit("software-update-card end not found")
settings = settings[:end] + '\n\n    <CloudflaredUpdateCard v-if="activeSettingsSection === \'software\'" />' + settings[end:]
settings_path.write_text(settings, encoding="utf-8")

print("Cloudflare resource manager + cloudflared updater patch applied")
