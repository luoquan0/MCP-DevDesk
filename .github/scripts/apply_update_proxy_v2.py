from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    text = target.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected block not found in {path}: {old[:160]!r}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")


# Replace updater proxy transport with HTTP + SOCKS5 auto detection and testing.
Path("app/internal/updater/proxy.go").write_text(r'''package updater

import (
    "context"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"
)

const proxyConnectTimeout = 4 * time.Second

type ProxyTestResult struct {
    OK        bool   `json:"ok"`
    Protocol  string `json:"protocol"`
    LatencyMS int64  `json:"latencyMs"`
    Message   string `json:"message"`
}

type autoProxyRoundTripper struct {
    manager   *Manager
    httpProxy http.RoundTripper
    socks5    http.RoundTripper
}

func (t *autoProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
    if mode := t.manager.cachedProxyMode(); mode != "" {
        if mode == "socks5" {
            return t.socks5.RoundTrip(req)
        }
        return t.httpProxy.RoundTrip(req)
    }

    first := req.Clone(req.Context())
    response, httpErr := t.httpProxy.RoundTrip(first)
    if httpErr == nil {
        t.manager.setCachedProxyMode("http")
        return response, nil
    }

    second := req.Clone(req.Context())
    response, socksErr := t.socks5.RoundTrip(second)
    if socksErr == nil {
        t.manager.setCachedProxyMode("socks5")
        return response, nil
    }
    return nil, fmt.Errorf("proxy connection failed (HTTP: %v; SOCKS5: %v)", httpErr, socksErr)
}

// updateHTTPClient returns a client dedicated to GitHub update traffic. The
// configured proxy never affects MCP Core, cloudflared, or project access.
// Proxy protocol is auto-detected between HTTP CONNECT and unauthenticated
// SOCKS5 so common local clients can be configured with just IP + port.
func (m *Manager) updateHTTPClient() *http.Client {
    settings := m.Settings()
    if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
        return &http.Client{Transport: directUpdateTransport()}
    }
    return &http.Client{Transport: &autoProxyRoundTripper{
        manager:   m,
        httpProxy: httpProxyTransport(settings),
        socks5:    socks5ProxyTransport(settings),
    }}
}

func directUpdateTransport() *http.Transport {
    transport := http.DefaultTransport.(*http.Transport).Clone()
    transport.Proxy = nil
    transport.DialContext = (&net.Dialer{Timeout: proxyConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
    transport.ResponseHeaderTimeout = metadataRequestTimeout
    transport.TLSHandshakeTimeout = 15 * time.Second
    return transport
}

func httpProxyTransport(settings Settings) *http.Transport {
    transport := directUpdateTransport()
    proxyURL := &url.URL{
        Scheme: "http",
        Host:   net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)),
    }
    transport.Proxy = http.ProxyURL(proxyURL)
    return transport
}

func socks5ProxyTransport(settings Settings) *http.Transport {
    transport := directUpdateTransport()
    transport.DialContext = socks5DialContext(net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)))
    return transport
}

func (m *Manager) TestProxy(ctx context.Context) (ProxyTestResult, error) {
    settings := m.Settings()
    if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
        return ProxyTestResult{}, errors.New("请先填写代理 IP 和端口")
    }

    modes := []struct {
        name      string
        transport http.RoundTripper
    }{
        {name: "HTTP", transport: httpProxyTransport(settings)},
        {name: "SOCKS5", transport: socks5ProxyTransport(settings)},
    }

    var failures []string
    for _, mode := range modes {
        attemptCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
        start := time.Now()
        request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, "https://github.com/", nil)
        if err != nil {
            cancel()
            return ProxyTestResult{}, err
        }
        request.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)
        response, err := (&http.Client{Transport: mode.transport}).Do(request)
        latency := time.Since(start)
        if err == nil {
            _, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
            _ = response.Body.Close()
        }
        cancel()
        if err == nil && response.StatusCode >= 200 && response.StatusCode < 400 {
            protocol := strings.ToLower(mode.name)
            m.setCachedProxyMode(protocol)
            return ProxyTestResult{
                OK:        true,
                Protocol:  mode.name,
                LatencyMS: latency.Milliseconds(),
                Message:   fmt.Sprintf("代理可用 · %s · %d ms", mode.name, latency.Milliseconds()),
            }, nil
        }
        if err != nil {
            failures = append(failures, fmt.Sprintf("%s: %v", mode.name, err))
        } else {
            failures = append(failures, fmt.Sprintf("%s: GitHub HTTP %d", mode.name, response.StatusCode))
        }
    }
    return ProxyTestResult{}, fmt.Errorf("代理测试失败（已尝试 HTTP 和 SOCKS5）：%s", strings.Join(failures, "; "))
}

func (m *Manager) cachedProxyMode() string {
    m.proxyModeMu.RLock()
    defer m.proxyModeMu.RUnlock()
    return m.proxyMode
}

func (m *Manager) setCachedProxyMode(mode string) {
    m.proxyModeMu.Lock()
    m.proxyMode = mode
    m.proxyModeMu.Unlock()
}

func (m *Manager) resetCachedProxyMode() {
    m.setCachedProxyMode("")
}

func validateProxySettings(settings Settings) error {
    host := strings.TrimSpace(settings.ProxyHost)
    port := settings.ProxyPort
    if host == "" && port == 0 {
        return nil
    }
    if host == "" {
        return errors.New("update proxy IP/host is required when a proxy port is set")
    }
    if port < 1 || port > 65535 {
        return errors.New("update proxy port must be between 1 and 65535")
    }
    if strings.Contains(host, "://") || strings.ContainsAny(host, "/\\@ \t\r\n") {
        return errors.New("update proxy must be an IP address or hostname without scheme, path, or credentials")
    }
    return nil
}

func socks5DialContext(proxyAddress string) func(context.Context, string, string) (net.Conn, error) {
    return func(ctx context.Context, network, targetAddress string) (net.Conn, error) {
        dialer := &net.Dialer{Timeout: proxyConnectTimeout, KeepAlive: 30 * time.Second}
        conn, err := dialer.DialContext(ctx, "tcp", proxyAddress)
        if err != nil {
            return nil, err
        }
        success := false
        defer func() {
            if !success {
                _ = conn.Close()
            }
        }()

        deadline := time.Now().Add(proxyConnectTimeout)
        if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
            deadline = value
        }
        _ = conn.SetDeadline(deadline)

        if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
            return nil, err
        }
        greeting := make([]byte, 2)
        if _, err := io.ReadFull(conn, greeting); err != nil {
            return nil, err
        }
        if greeting[0] != 0x05 || greeting[1] != 0x00 {
            return nil, fmt.Errorf("SOCKS5 proxy does not allow unauthenticated connections")
        }

        host, portText, err := net.SplitHostPort(targetAddress)
        if err != nil {
            return nil, err
        }
        port, err := strconv.Atoi(portText)
        if err != nil || port < 1 || port > 65535 {
            return nil, fmt.Errorf("invalid target port %q", portText)
        }
        request := []byte{0x05, 0x01, 0x00}
        if ip := net.ParseIP(host); ip != nil {
            if ipv4 := ip.To4(); ipv4 != nil {
                request = append(request, 0x01)
                request = append(request, ipv4...)
            } else {
                request = append(request, 0x04)
                request = append(request, ip.To16()...)
            }
        } else {
            if len(host) == 0 || len(host) > 255 {
                return nil, errors.New("SOCKS5 target hostname is invalid")
            }
            request = append(request, 0x03, byte(len(host)))
            request = append(request, host...)
        }
        request = append(request, byte(port>>8), byte(port))
        if _, err := conn.Write(request); err != nil {
            return nil, err
        }

        header := make([]byte, 4)
        if _, err := io.ReadFull(conn, header); err != nil {
            return nil, err
        }
        if header[0] != 0x05 || header[1] != 0x00 {
            return nil, fmt.Errorf("SOCKS5 connect failed with code 0x%02x", header[1])
        }
        if err := discardSOCKS5Address(conn, header[3]); err != nil {
            return nil, err
        }
        _ = conn.SetDeadline(time.Time{})
        success = true
        return conn, nil
    }
}

func discardSOCKS5Address(reader io.Reader, addressType byte) error {
    var length int
    switch addressType {
    case 0x01:
        length = 4
    case 0x04:
        length = 16
    case 0x03:
        size := []byte{0}
        if _, err := io.ReadFull(reader, size); err != nil {
            return err
        }
        length = int(size[0])
    default:
        return fmt.Errorf("unsupported SOCKS5 address type 0x%02x", addressType)
    }
    buffer := make([]byte, length+2)
    _, err := io.ReadFull(reader, buffer)
    return err
}
''', encoding="utf-8")

# Manager runtime cache for detected proxy protocol and reset on settings change.
manager = "app/internal/updater/manager.go"
replace_once(manager,
'''\tprogressMu   sync.RWMutex
\tprogress     DownloadProgress
}''',
'''\tprogressMu   sync.RWMutex
\tprogress     DownloadProgress
\tproxyModeMu  sync.RWMutex
\tproxyMode    string
}''')
replace_once(manager,
'''\tprevious := m.current
\tm.current = next
\tif err := m.saveLocked(); err != nil {''',
'''\tprevious := m.current
\tproxyChanged := previous.ProxyHost != next.ProxyHost || previous.ProxyPort != next.ProxyPort
\tm.current = next
\tif err := m.saveLocked(); err != nil {''')
replace_once(manager,
'''\t\tm.current = previous
\t\treturn Settings{}, err
\t}
\treturn m.current, nil
}''',
'''\t\tm.current = previous
\t\treturn Settings{}, err
\t}
\tif proxyChanged {
\t\tm.resetCachedProxyMode()
\t}
\treturn m.current, nil
}''')

# Application proxy-test wrapper.
app = "app/internal/application/app.go"
replace_once(app,
'''func (a *App) CheckForUpdate(ctx context.Context) (appupdater.Release, error) {
\treturn a.updates.Check(ctx)
}

func (a *App) PrepareUpdate''',
'''func (a *App) CheckForUpdate(ctx context.Context) (appupdater.Release, error) {
\treturn a.updates.Check(ctx)
}

func (a *App) TestUpdateProxy(ctx context.Context) (appupdater.ProxyTestResult, error) {
\treturn a.updates.TestProxy(ctx)
}

func (a *App) PrepareUpdate''')

# Web route + bounded proxy test endpoint.
server = "app/internal/web/server.go"
replace_once(server,
'''\tmux.HandleFunc("PUT /api/update/settings", s.handleSaveUpdateSettings)
\tmux.HandleFunc("POST /api/update/check", s.handleCheckForUpdate)''',
'''\tmux.HandleFunc("PUT /api/update/settings", s.handleSaveUpdateSettings)
\tmux.HandleFunc("POST /api/update/proxy-test", s.handleTestUpdateProxy)
\tmux.HandleFunc("POST /api/update/check", s.handleCheckForUpdate)''')
replace_once(server,
'''func (s *Server) handleCheckForUpdate(w http.ResponseWriter, r *http.Request) {
\tctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
\tdefer cancel()
\trelease, err := s.app.CheckForUpdate(ctx)
\tif err != nil {
\t\twriteError(w, http.StatusBadRequest, err)
\t\treturn
\t}
\twriteJSON(w, http.StatusOK, release)
}

func (s *Server) handleInstallUpdate''',
'''func (s *Server) handleTestUpdateProxy(w http.ResponseWriter, r *http.Request) {
\tctx, cancel := context.WithTimeout(r.Context(), 13*time.Second)
\tdefer cancel()
\tresult, err := s.app.TestUpdateProxy(ctx)
\tif err != nil {
\t\twriteError(w, http.StatusBadRequest, err)
\t\treturn
\t}
\twriteJSON(w, http.StatusOK, result)
}

func (s *Server) handleCheckForUpdate(w http.ResponseWriter, r *http.Request) {
\tctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
\tdefer cancel()
\trelease, err := s.app.CheckForUpdate(ctx)
\tif err != nil {
\t\twriteError(w, http.StatusBadRequest, err)
\t\treturn
\t}
\twriteJSON(w, http.StatusOK, release)
}

func (s *Server) handleInstallUpdate''')

# Replace proxy tests to cover persistence + HTTP-to-SOCKS fallback behavior without external network.
Path("app/internal/updater/proxy_test.go").write_text(r'''package updater

import (
    "context"
    "errors"
    "net/http"
    "testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestUpdateProxySettingsPersist(t *testing.T) {
    dataDir := t.TempDir()
    manager, err := NewManager(dataDir, "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    host := "127.0.0.1"
    port := 10808
    settings, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port})
    if err != nil { t.Fatal(err) }
    if settings.ProxyHost != host || settings.ProxyPort != port {
        t.Fatalf("proxy settings = %+v", settings)
    }
    reloaded, err := NewManager(dataDir, "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    got := reloaded.Settings()
    if got.ProxyHost != host || got.ProxyPort != port {
        t.Fatalf("reloaded proxy = %+v", got)
    }
}

func TestAutoProxyRoundTripperFallsBackToSOCKS5(t *testing.T) {
    manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    primaryCalls := 0
    secondaryCalls := 0
    transport := &autoProxyRoundTripper{
        manager: manager,
        httpProxy: roundTripFunc(func(*http.Request) (*http.Response, error) {
            primaryCalls++
            return nil, errors.New("not an HTTP proxy")
        }),
        socks5: roundTripFunc(func(request *http.Request) (*http.Response, error) {
            secondaryCalls++
            return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: request}, nil
        }),
    }
    request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://github.com/", nil)
    response, err := transport.RoundTrip(request)
    if err != nil { t.Fatal(err) }
    _ = response.Body.Close()
    if primaryCalls != 1 || secondaryCalls != 1 {
        t.Fatalf("calls http=%d socks=%d", primaryCalls, secondaryCalls)
    }
    if manager.cachedProxyMode() != "socks5" {
        t.Fatalf("cached proxy mode = %q", manager.cachedProxyMode())
    }
}

func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {
    manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    host := "127.0.0.1"
    zero := 0
    if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &zero}); err == nil {
        t.Fatal("expected proxy without port to be rejected")
    }
    empty := ""
    port := 10808
    if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &port}); err == nil {
        t.Fatal("expected proxy port without host to be rejected")
    }
    invalid := "http://127.0.0.1"
    if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &invalid, ProxyPort: &port}); err == nil {
        t.Fatal("expected proxy host with scheme to be rejected")
    }
}

func TestUpdateProxyCanBeDisabledAgain(t *testing.T) {
    manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    host := "127.0.0.1"
    port := 10808
    if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port}); err != nil { t.Fatal(err) }
    manager.setCachedProxyMode("socks5")
    empty := ""
    zero := 0
    if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &zero}); err != nil { t.Fatal(err) }
    if manager.cachedProxyMode() != "" {
        t.Fatalf("proxy mode should reset, got %q", manager.cachedProxyMode())
    }
    transport := manager.updateHTTPClient().Transport.(*http.Transport)
    if transport.Proxy != nil {
        t.Fatal("expected direct update transport after clearing proxy")
    }
}

func TestProxyTestRequiresConfiguredProxy(t *testing.T) {
    manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
    if err != nil { t.Fatal(err) }
    if _, err := manager.TestProxy(context.Background()); err == nil {
        t.Fatal("expected missing proxy to fail")
    }
}
''', encoding="utf-8")

# Frontend API types.
types = "frontend/src/types/api.ts"
replace_once(types,
'''export interface UpdateSettings {
  repository: string;
  channel: "stable" | "prerelease";
  checkOnStartup: boolean;
  proxyHost: string;
  proxyPort: number;
}

export interface UpdateRelease''',
'''export interface UpdateSettings {
  repository: string;
  channel: "stable" | "prerelease";
  checkOnStartup: boolean;
  proxyHost: string;
  proxyPort: number;
}

export interface UpdateProxyTestResult {
  ok: boolean;
  protocol: "HTTP" | "SOCKS5" | string;
  latencyMs: number;
  message: string;
}

export interface UpdateRelease''')

# Store action.
store = "frontend/src/stores/app.ts"
replace_once(store,
'''  UpdateInstallResult,
  UpdateRelease,
  UpdateSettings,
  WebControlStatus,''',
'''  UpdateInstallResult,
  UpdateProxyTestResult,
  UpdateRelease,
  UpdateSettings,
  WebControlStatus,''')
replace_once(store,
'''    async checkForUpdate(silent = false) {
      try {
        this.updateRelease = await api<UpdateRelease>("/api/update/check", { method: "POST" });''',
'''    async testUpdateProxy() {
      return this.runAction("test-update-proxy", () => api<UpdateProxyTestResult>("/api/update/proxy-test", { method: "POST" }));
    },
    async checkForUpdate(silent = false) {
      try {
        this.updateRelease = await api<UpdateRelease>("/api/update/check", { method: "POST" });''')

# Settings UI: test button, explicit status, HTTP/SOCKS5 wording.
page = "frontend/src/pages/SettingsPage.vue"
replace_once(page,
'''const updateProxyPort = ref("");
const updateChecking = ref(false);''',
'''const updateProxyPort = ref("");
const updateChecking = ref(false);
const updateProxyTesting = ref(false);
const updateProxyTestMessage = ref("");''')
replace_once(page,
'''async function checkForUpdate() {
  const proxy = updateProxyPayload();''',
'''async function testUpdateProxy() {
  const proxy = updateProxyPayload();
  if (!proxy) return;
  if (!proxy.proxyHost || !proxy.proxyPort) {
    ui.toast("未配置代理", "请先填写代理 IP 和端口；留空表示直连，无需测试。", "info");
    return;
  }
  updateProxyTesting.value = true;
  updateProxyTestMessage.value = "正在自动测试 HTTP / SOCKS5...";
  try {
    await app.saveUpdateSettings({
      channel: updateChannel.value,
      checkOnStartup: updateCheckOnStartup.value,
      ...proxy,
    });
    const result = await app.testUpdateProxy();
    updateProxyTestMessage.value = `${result.protocol} 可用 · ${result.latencyMs} ms`;
    ui.toast("代理测试成功", result.message, "success");
  } catch (error) {
    updateProxyTestMessage.value = error instanceof Error ? error.message : String(error);
    ui.toast("代理测试失败", updateProxyTestMessage.value, "danger");
  } finally {
    updateProxyTesting.value = false;
  }
}

async function checkForUpdate() {
  const proxy = updateProxyPayload();''')
replace_once(page,
'''          <small>可选 HTTP 代理。只填写 IP 或主机名；留空表示直连 GitHub。</small>''',
'''          <small>可选代理。只填写 IP 或主机名；程序会自动识别 HTTP 或 SOCKS5，留空表示直连 GitHub。</small>''')
replace_once(page,
'''          <small>与代理 IP 配套使用，例如 Clash 常用 7890。</small>''',
'''          <small>与代理 IP 配套使用，例如 7890、1080、10808；可用“测试代理”确认协议和连通性。</small>''')
replace_once(page,
'''      <div class="form-footer top-divider">
        <small>立即更新会先下载 Release ZIP 并校验 SHA256，再启动独立 updater。程序文件会替换，data/devdesk、项目目录和 AGENTS.md 不会被更新包覆盖；失败时自动回滚旧二进制。</small>
        <div class="form-footer-actions">
          <AppButton tone="quiet" :loading="app.actionPending === 'save-update-settings'" @click="saveUpdatePreferences">保存更新设置</AppButton>
          <AppButton tone="secondary" icon="refresh" :loading="updateChecking" @click="checkForUpdate">检查更新</AppButton>''',
'''      <div class="form-footer top-divider">
        <small>
          {{ updateProxyTestMessage || '立即更新会先下载 Release ZIP 并校验 SHA256，再启动独立 updater。代理仅用于软件更新，不影响 MCP 和 Cloudflare。' }}
        </small>
        <div class="form-footer-actions">
          <AppButton tone="quiet" :loading="app.actionPending === 'save-update-settings'" @click.stop="saveUpdatePreferences">保存更新设置</AppButton>
          <AppButton tone="secondary" icon="shield" :loading="updateProxyTesting || app.actionPending === 'test-update-proxy'" @click.stop="testUpdateProxy">测试代理</AppButton>
          <AppButton tone="secondary" icon="refresh" :loading="updateChecking" @click.stop="checkForUpdate">检查更新</AppButton>''')
replace_once(page,
'''          <p>从内置 GitHub Releases 更新源检查并安装新版本。可选填写 HTTP 代理 IP 和端口；下载包仍必须通过 SHA256 校验。</p>''',
'''          <p>从内置 GitHub Releases 更新源检查并安装新版本。代理只需填写 IP 和端口，自动兼容 HTTP / SOCKS5；下载包仍必须通过 SHA256 校验。</p>''')

# Release docs note.
docs = Path("docs/RELEASE.md")
docs_text = docs.read_text(encoding="utf-8")
note = '''\n### 代理连通性测试\n\n更新代理支持只填写 IP/主机名和端口，程序自动尝试 HTTP CONNECT 与无认证 SOCKS5。设置页提供“测试代理”，测试请求有独立短超时并显示识别到的协议与耗时；错误代理不会再让“检查更新”长时间看起来无响应。代理仅用于 GitHub Release 元数据、SHA256 和更新包下载。\n'''
if "### 代理连通性测试" not in docs_text:
    docs.write_text(docs_text.rstrip() + "\n" + note, encoding="utf-8")
