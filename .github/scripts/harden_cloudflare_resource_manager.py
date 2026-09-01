from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def replace_once(rel, old, new):
    path = ROOT / rel
    text = path.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"anchor not found in {rel}: {old[:160]!r}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def replace_between(rel, start, end, replacement):
    path = ROOT / rel
    text = path.read_text(encoding="utf-8")
    a = text.find(start)
    b = text.find(end, a + len(start))
    if a < 0 or b < 0:
        raise SystemExit(f"range not found in {rel}: {start!r} .. {end!r}")
    path.write_text(text[:a] + replacement + text[b:], encoding="utf-8")


replace_once(
    "app/internal/tunnel/cloudflare.go",
    "// Delete removes the named Tunnel from Cloudflare. --force asks cloudflared to\n// cascade the Tunnel dependencies, including DNS routes attached to it.\n",
    "// Delete removes the named Tunnel from Cloudflare. DNS hostnames are managed\n// separately by Cloudflare, so callers must explicitly remove the DNS record first.\n",
)
replace_once(
    "app/internal/tunnel/cloudflare.go",
    'if !strings.Contains(lower, "not found") && !strings.Contains(lower, "does not exist") && !strings.Contains(lower, "no tunnel") {',
    'if !strings.Contains(lower, "not found") && !strings.Contains(lower, "does not exist") && !strings.Contains(lower, "no tunnel") && !strings.Contains(lower, "already been deleted") && !strings.Contains(lower, "already deleted") {',
)

replace_once("app/internal/application/cloudflare_manage.go", '    "encoding/json"\n', '    "encoding/json"\n    "encoding/pem"\n')
replace_once("app/internal/application/cloudflare_manage.go", '    "net/http"\n', '    "net/http"\n    "net/url"\n')

new_unbind = r'''func (a *App) UnbindInstanceCloudflare(ctx context.Context, id string) (CloudflareUnbindResult, error) {
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

'''
replace_between(
    "app/internal/application/cloudflare_manage.go",
    "func (a *App) UnbindInstanceCloudflare",
    "func (a *App) CheckCloudflaredUpdate",
    new_unbind,
)

replace_once(
    "app/internal/application/cloudflare_manage.go",
    "type CloudflareUnbindResult struct {\n    InstanceID string `json:\"instanceId\"`\n    Domain     string `json:\"domain\"`\n    TunnelID   string `json:\"tunnelId\"`\n    TunnelName string `json:\"tunnelName\"`\n    Message    string `json:\"message\"`\n}",
    "type CloudflareUnbindResult struct {\n    InstanceID    string `json:\"instanceId\"`\n    Domain        string `json:\"domain\"`\n    TunnelID      string `json:\"tunnelId\"`\n    TunnelName    string `json:\"tunnelName\"`\n    DNSDeleted    bool   `json:\"dnsDeleted\"`\n    TunnelDeleted bool   `json:\"tunnelDeleted\"`\n    TunnelShared  bool   `json:\"tunnelShared\"`\n    Message       string `json:\"message\"`\n}",
)

replace_once(
    "app/internal/application/cloudflare_manage.go",
    "    restoreDesired()\n    restored = true\n    restarted, restartErrors := restartCloudflaredStates(states)\n",
    "    restarted, restartErrors := restartCloudflaredStates(states)\n    restoreDesired()\n    restored = true\n",
)

replace_once(
    "frontend/src/pages/CloudflarePage.vue",
    '''  const shared = sharedTunnelCount(instance.tunnelId);
  if (shared > 1) {
    ui.toast("该 Tunnel 正在被复用", `共有 ${shared} 个 MCP 实例使用 ${instance.tunnelName || instance.tunnelId}，为避免误删其他 DNS，暂不允许删除整个 Tunnel。`, "warning");
    return;
  }
  const accepted = await ui.ask({
    title: "解绑并清理 Cloudflare",
    message: `将停止 ${instance.name} 的 Tunnel，并从 Cloudflare 删除 ${instance.tunnelName || instance.tunnelId}。该 Tunnel 关联的 DNS 路由（包括 ${instance.domain}）会一起清理，本地 MCP 项目和端口不会删除。`,
    confirmLabel: "删除 DNS + Tunnel",
    danger: true,
  });''',
    '''  const shared = sharedTunnelCount(instance.tunnelId);
  const sharedBinding = shared > 1;
  const accepted = await ui.ask({
    title: "解绑并清理 Cloudflare",
    message: sharedBinding
      ? `${instance.tunnelName || instance.tunnelId} 还被其他 MCP 实例复用。本次只删除 ${instance.domain} 的 DNS 并解除当前实例绑定，Tunnel 本身会保留。`
      : `将删除 DNS ${instance.domain}，并删除 Cloudflare Tunnel ${instance.tunnelName || instance.tunnelId}。本地 MCP 项目和端口不会删除。`,
    confirmLabel: sharedBinding ? "删除当前 DNS" : "删除 DNS + Tunnel",
    danger: true,
  });''',
)
replace_once(
    "frontend/src/pages/CloudflarePage.vue",
    ':disabled="!app.status?.cloudflare.authenticated || sharedTunnelCount(instance.tunnelId) > 1" @click="unbind(instance)"',
    ':disabled="!app.status?.cloudflare.authenticated" @click="unbind(instance)"',
)
replace_once(
    "frontend/src/pages/CloudflarePage.vue",
    "这里列出 MCP DevDesk 当前管理的 Cloudflare 域名。解绑会同时清理该 Tunnel 在 Cloudflare 上的路由依赖。",
    "这里列出 MCP DevDesk 当前管理的 Cloudflare 域名。解绑会显式删除当前 DNS；Tunnel 未被其他 MCP 复用时再一起删除 Tunnel。",
)

replace_once(
    "app/internal/application/cloudflare_manage_test.go",
    "func TestCloudflaredVersionPattern(t *testing.T) {",
    '''func TestNormalizeCloudflareCNAMEContent(t *testing.T) {
    got := normalizeCloudflareCNAMEContent("  ABCD.cfargotunnel.com. ")
    if got != "abcd.cfargotunnel.com" {
        t.Fatalf("normalizeCloudflareCNAMEContent()=%q", got)
    }
}

func TestCloudflaredVersionPattern(t *testing.T) {''',
)

print("Cloudflare resource cleanup hardened")
