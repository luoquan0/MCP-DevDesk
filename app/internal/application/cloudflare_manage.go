package application

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "encoding/pem"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
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
    InstanceID    string `json:"instanceId"`
    Domain        string `json:"domain"`
    TunnelID      string `json:"tunnelId"`
    TunnelName    string `json:"tunnelName"`
    DNSDeleted    bool   `json:"dnsDeleted"`
    TunnelDeleted bool   `json:"tunnelDeleted"`
    TunnelShared  bool   `json:"tunnelShared"`
    Message       string `json:"message"`
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
    domain := strings.ToLower(strings.TrimSpace(cfg.Domain))
    tunnelID := strings.ToLower(strings.TrimSpace(cfg.TunnelID))
    tunnelName := strings.TrimSpace(cfg.TunnelName)
    if domain == "" && tunnelID == "" {
        return CloudflareUnbindResult{}, errors.New("该实例没有可解绑的 Cloudflare 资源")
    }
    sharedUsers := a.tunnelUsers(tunnelID, id)
    deleteTunnel := tunnelID != "" && len(sharedUsers) == 0

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

    dnsDeleted, err := a.deleteCloudflareDNSRecord(ctx, domain, tunnelID)
    if err != nil {
        managed.desiredRunning = originalDesired
        if tunnelStatus.Running {
            _ = managed.process.StartTunnel(cfg)
        }
        return CloudflareUnbindResult{}, err
    }

    tunnelDeleted := false
    if deleteTunnel {
        if _, err := a.tunnel.Delete(ctx, cfg); err != nil {
            rollbackProblem := a.rollbackCloudflareDNS(cfg, dnsDeleted)
            managed.desiredRunning = originalDesired
            if tunnelStatus.Running {
                _ = managed.process.StartTunnel(cfg)
            }
            return CloudflareUnbindResult{}, fmt.Errorf("DNS 已处理，但 Tunnel 删除失败: %w%s", err, rollbackProblem)
        }
        tunnelDeleted = true
    }

    cfg.Domain = ""
    cfg.TunnelID = ""
    if _, err := managed.config.Replace(cfg); err != nil {
        managed.desiredRunning = originalDesired
        return CloudflareUnbindResult{}, fmt.Errorf("Cloudflare 资源已处理，但清理本地实例配置失败: %w", err)
    }
    managed.desiredRunning = originalDesired

    message := fmt.Sprintf("已解绑 %s，并清理 DNS %s", record.Name, domain)
    if tunnelDeleted {
        message += fmt.Sprintf("；Tunnel %s 也已删除", tunnelName)
    } else if len(sharedUsers) > 0 {
        message += fmt.Sprintf("；Tunnel %s 仍被 %s 使用，因此已保留", tunnelName, strings.Join(sharedUsers, "、"))
    }
    return CloudflareUnbindResult{
        InstanceID: id,
        Domain: domain,
        TunnelID: tunnelID,
        TunnelName: tunnelName,
        DNSDeleted: dnsDeleted,
        TunnelDeleted: tunnelDeleted,
        TunnelShared: len(sharedUsers) > 0,
        Message: message,
    }, nil
}

func (a *App) unbindPrimaryCloudflare(ctx context.Context) (CloudflareUnbindResult, error) {
    a.mu.Lock()
    defer a.mu.Unlock()

    cfg := a.config.Get()
    domain := strings.ToLower(strings.TrimSpace(cfg.Domain))
    tunnelID := strings.ToLower(strings.TrimSpace(cfg.TunnelID))
    tunnelName := strings.TrimSpace(cfg.TunnelName)
    if domain == "" && tunnelID == "" {
        return CloudflareUnbindResult{}, errors.New("主实例没有可解绑的 Cloudflare 资源")
    }
    sharedUsers := a.tunnelUsers(tunnelID, model.PrimaryInstanceID)
    deleteTunnel := tunnelID != "" && len(sharedUsers) == 0

    _, tunnelStatus, _ := a.process.Status()
    previousDesired := a.tunnelDesired
    a.tunnelDesired = false
    if err := a.process.StopTunnel(); err != nil {
        a.tunnelDesired = previousDesired
        return CloudflareUnbindResult{}, err
    }

    dnsDeleted, err := a.deleteCloudflareDNSRecord(ctx, domain, tunnelID)
    if err != nil {
        a.tunnelDesired = previousDesired
        if tunnelStatus.Running {
            _ = a.process.StartTunnel(cfg)
        }
        return CloudflareUnbindResult{}, err
    }

    tunnelDeleted := false
    if deleteTunnel {
        if _, err := a.tunnel.Delete(ctx, cfg); err != nil {
            rollbackProblem := a.rollbackCloudflareDNS(cfg, dnsDeleted)
            a.tunnelDesired = previousDesired
            if tunnelStatus.Running {
                _ = a.process.StartTunnel(cfg)
            }
            return CloudflareUnbindResult{}, fmt.Errorf("DNS 已处理，但 Tunnel 删除失败: %w%s", err, rollbackProblem)
        }
        tunnelDeleted = true
    }

    cfg.Domain = ""
    cfg.TunnelID = ""
    if _, err := a.config.Replace(cfg); err != nil {
        a.tunnelDesired = previousDesired
        return CloudflareUnbindResult{}, fmt.Errorf("Cloudflare 资源已处理，但清理本地主实例配置失败: %w", err)
    }

    message := fmt.Sprintf("已解绑主实例，并清理 DNS %s", domain)
    if tunnelDeleted {
        message += fmt.Sprintf("；Tunnel %s 也已删除", tunnelName)
    } else if len(sharedUsers) > 0 {
        message += fmt.Sprintf("；Tunnel %s 仍被 %s 使用，因此已保留", tunnelName, strings.Join(sharedUsers, "、"))
    }
    return CloudflareUnbindResult{
        InstanceID: model.PrimaryInstanceID,
        Domain: domain,
        TunnelID: tunnelID,
        TunnelName: tunnelName,
        DNSDeleted: dnsDeleted,
        TunnelDeleted: tunnelDeleted,
        TunnelShared: len(sharedUsers) > 0,
        Message: message,
    }, nil
}

func (a *App) rollbackCloudflareDNS(cfg model.Config, deleted bool) string {
    if !deleted || strings.TrimSpace(cfg.Domain) == "" || strings.TrimSpace(cfg.TunnelID) == "" {
        return ""
    }
    rollbackCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
    defer cancel()
    if _, err := a.tunnel.RepairDNS(rollbackCtx, cfg); err != nil {
        return fmt.Sprintf("；DNS 回滚失败: %v", err)
    }
    return "；DNS 已自动回滚"
}

func (a *App) tunnelUsers(tunnelID, exceptID string) []string {
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
    return users
}

type cloudflareOriginCert struct {
    ZoneID    string `json:"zoneID"`
    AccountID string `json:"accountID"`
    APIToken  string `json:"apiToken"`
    Endpoint  string `json:"endpoint,omitempty"`
}

type cloudflareAPIError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func readCloudflareOriginCert() (cloudflareOriginCert, error) {
    raw, err := os.ReadFile(processmanager.CertificatePath())
    if err != nil {
        return cloudflareOriginCert{}, fmt.Errorf("读取 Cloudflare 授权证书失败: %w", err)
    }
    var cert cloudflareOriginCert
    rest := raw
    for len(rest) > 0 {
        block, remaining := pem.Decode(rest)
        rest = remaining
        if block == nil {
            break
        }
        if block.Type != "ARGO TUNNEL TOKEN" {
            continue
        }
        if err := json.Unmarshal(block.Bytes, &cert); err != nil {
            return cloudflareOriginCert{}, fmt.Errorf("解析 Cloudflare 授权证书失败: %w", err)
        }
        break
    }
    if strings.TrimSpace(cert.ZoneID) == "" || strings.TrimSpace(cert.APIToken) == "" {
        return cloudflareOriginCert{}, errors.New("Cloudflare cert.pem 中没有可用于 DNS 管理的 zoneID/apiToken，请重新授权 Cloudflare")
    }
    return cert, nil
}

func cloudflareAPIBase(endpoint string) string {
    if strings.EqualFold(strings.TrimSpace(endpoint), "fed") {
        return "https://api.fed.cloudflare.com/client/v4"
    }
    return "https://api.cloudflare.com/client/v4"
}

func normalizeCloudflareCNAMEContent(value string) string {
    return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func cloudflareAPIErrorText(status int, errorsList []cloudflareAPIError) string {
    if len(errorsList) == 0 {
        return fmt.Sprintf("HTTP %d", status)
    }
    parts := make([]string, 0, len(errorsList))
    for _, item := range errorsList {
        if item.Message != "" {
            parts = append(parts, item.Message)
        }
    }
    if len(parts) == 0 {
        return fmt.Sprintf("HTTP %d", status)
    }
    return strings.Join(parts, "; ")
}

func doCloudflareAPI(ctx context.Context, cert cloudflareOriginCert, method, endpoint string) (*http.Response, error) {
    request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
    if err != nil {
        return nil, err
    }
    request.Header.Set("Authorization", "Bearer "+cert.APIToken)
    request.Header.Set("Accept", "application/json")
    request.Header.Set("User-Agent", "MCP-DevDesk/"+Version)
    client := &http.Client{Timeout: 20 * time.Second}
    return client.Do(request)
}

func (a *App) deleteCloudflareDNSRecord(ctx context.Context, domain, tunnelID string) (bool, error) {
    domain = strings.ToLower(strings.TrimSpace(domain))
    tunnelID = strings.ToLower(strings.TrimSpace(tunnelID))
    if domain == "" {
        return false, nil
    }
    cert, err := readCloudflareOriginCert()
    if err != nil {
        return false, err
    }
    query := url.Values{}
    query.Set("type", "CNAME")
    query.Set("name", domain)
    query.Set("per_page", "100")
    endpoint := fmt.Sprintf("%s/zones/%s/dns_records?%s", cloudflareAPIBase(cert.Endpoint), url.PathEscape(cert.ZoneID), query.Encode())
    response, err := doCloudflareAPI(ctx, cert, http.MethodGet, endpoint)
    if err != nil {
        return false, fmt.Errorf("查询 Cloudflare DNS %s 失败: %w", domain, err)
    }
    var listPayload struct {
        Success bool `json:"success"`
        Errors []cloudflareAPIError `json:"errors"`
        Result []struct {
            ID string `json:"id"`
            Type string `json:"type"`
            Name string `json:"name"`
            Content string `json:"content"`
        } `json:"result"`
    }
    decodeErr := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&listPayload)
    response.Body.Close()
    if decodeErr != nil {
        return false, fmt.Errorf("解析 Cloudflare DNS 查询结果失败: %w", decodeErr)
    }
    if response.StatusCode < 200 || response.StatusCode >= 300 || !listPayload.Success {
        return false, fmt.Errorf("查询 Cloudflare DNS %s 失败: %s", domain, cloudflareAPIErrorText(response.StatusCode, listPayload.Errors))
    }

    expectedTarget := normalizeCloudflareCNAMEContent(tunnelID + ".cfargotunnel.com")
    matched := make([]string, 0, 1)
    unexpectedTarget := ""
    for _, record := range listPayload.Result {
        if !strings.EqualFold(record.Name, domain) || !strings.EqualFold(record.Type, "CNAME") {
            continue
        }
        if tunnelID == "" || normalizeCloudflareCNAMEContent(record.Content) == expectedTarget {
            matched = append(matched, record.ID)
        } else {
            unexpectedTarget = record.Content
        }
    }
    if len(matched) == 0 {
        if unexpectedTarget != "" {
            return false, fmt.Errorf("拒绝删除 DNS %s：它现在指向 %s，不是当前 Tunnel %s", domain, unexpectedTarget, tunnelID)
        }
        return false, nil
    }

    for _, recordID := range matched {
        deleteEndpoint := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase(cert.Endpoint), url.PathEscape(cert.ZoneID), url.PathEscape(recordID))
        deleteResponse, deleteErr := doCloudflareAPI(ctx, cert, http.MethodDelete, deleteEndpoint)
        if deleteErr != nil {
            return false, fmt.Errorf("删除 Cloudflare DNS %s 失败: %w", domain, deleteErr)
        }
        var deletePayload struct {
            Success bool `json:"success"`
            Errors []cloudflareAPIError `json:"errors"`
        }
        decodeDeleteErr := json.NewDecoder(io.LimitReader(deleteResponse.Body, 1<<20)).Decode(&deletePayload)
        deleteResponse.Body.Close()
        if decodeDeleteErr != nil {
            return false, fmt.Errorf("解析 Cloudflare DNS 删除结果失败: %w", decodeDeleteErr)
        }
        if deleteResponse.StatusCode < 200 || deleteResponse.StatusCode >= 300 || !deletePayload.Success {
            return false, fmt.Errorf("删除 Cloudflare DNS %s 失败: %s", domain, cloudflareAPIErrorText(deleteResponse.StatusCode, deletePayload.Errors))
        }
    }
    return true, nil
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

    restarted, restartErrors := restartCloudflaredStates(states)
    restoreDesired()
    restored = true
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
