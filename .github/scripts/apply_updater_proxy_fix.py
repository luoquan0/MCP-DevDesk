from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    text = target.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected block not found in {path}: {old[:120]!r}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")


manager = "app/internal/updater/manager.go"
replace_once(
    manager,
    '''type Settings struct {
\tRepository     string            `json:"repository"`
\tChannel        string            `json:"channel"`
\tCheckOnStartup bool              `json:"checkOnStartup"`
\tProgress       *DownloadProgress `json:"progress,omitempty"`
}

type SettingsUpdate struct {
\tRepository     *string `json:"repository"`
\tChannel        *string `json:"channel"`
\tCheckOnStartup *bool   `json:"checkOnStartup"`
}
''',
    '''type Settings struct {
\tRepository     string            `json:"repository"`
\tChannel        string            `json:"channel"`
\tCheckOnStartup bool              `json:"checkOnStartup"`
\tProxyHost      string            `json:"proxyHost"`
\tProxyPort      int               `json:"proxyPort"`
\tProgress       *DownloadProgress `json:"progress,omitempty"`
}

type SettingsUpdate struct {
\tRepository     *string `json:"repository"`
\tChannel        *string `json:"channel"`
\tCheckOnStartup *bool   `json:"checkOnStartup"`
\tProxyHost      *string `json:"proxyHost"`
\tProxyPort      *int    `json:"proxyPort"`
}
''',
)

replace_once(
    manager,
    '''\tif update.CheckOnStartup != nil {
\t\tnext.CheckOnStartup = *update.CheckOnStartup
\t}
\tif err := validateSettings(next); err != nil {
''',
    '''\tif update.CheckOnStartup != nil {
\t\tnext.CheckOnStartup = *update.CheckOnStartup
\t}
\tif update.ProxyHost != nil {
\t\tnext.ProxyHost = strings.TrimSpace(*update.ProxyHost)
\t}
\tif update.ProxyPort != nil {
\t\tnext.ProxyPort = *update.ProxyPort
\t}
\tif err := validateSettings(next); err != nil {
''',
)

text = Path(manager).read_text(encoding="utf-8")
if text.count("m.client.Do(req)") != 3:
    raise SystemExit(f"expected 3 updater HTTP calls, found {text.count('m.client.Do(req)')}")
Path(manager).write_text(text.replace("m.client.Do(req)", "m.updateHTTPClient().Do(req)"), encoding="utf-8")

replace_once(
    manager,
    '''func (m *Manager) normalizeLocked() {
\tm.current.Repository = strings.TrimSpace(m.current.Repository)
\tm.current.Channel = strings.ToLower(strings.TrimSpace(m.current.Channel))
\tm.current.Progress = nil
''',
    '''func (m *Manager) normalizeLocked() {
\tm.current.Repository = strings.TrimSpace(m.current.Repository)
\tm.current.Channel = strings.ToLower(strings.TrimSpace(m.current.Channel))
\tm.current.ProxyHost = strings.TrimSpace(m.current.ProxyHost)
\tm.current.Progress = nil
''',
)

replace_once(
    manager,
    '''\tswitch settings.Channel {
\tcase "stable", "prerelease":
\tdefault:
\t\treturn errors.New("update channel must be stable or prerelease")
\t}
\treturn nil
}
''',
    '''\tswitch settings.Channel {
\tcase "stable", "prerelease":
\tdefault:
\t\treturn errors.New("update channel must be stable or prerelease")
\t}
\treturn validateProxySettings(settings)
}
''',
)

Path("app/internal/updater/proxy.go").write_text(
    '''package updater

import (
\t"errors"
\t"net"
\t"net/http"
\t"net/url"
\t"strconv"
\t"strings"
\t"time"
)

// updateHTTPClient returns a client dedicated to GitHub update traffic. The
// proxy configured here never affects MCP Core, cloudflared, or project access.
func (m *Manager) updateHTTPClient() *http.Client {
\tsettings := m.Settings()
\ttransport := http.DefaultTransport.(*http.Transport).Clone()
\ttransport.ResponseHeaderTimeout = metadataRequestTimeout
\ttransport.TLSHandshakeTimeout = 15 * time.Second
\t// Empty fields explicitly mean direct access instead of inheriting an
\t// unrelated HTTP_PROXY/HTTPS_PROXY environment variable.
\ttransport.Proxy = nil
\tif host := strings.TrimSpace(settings.ProxyHost); host != "" && settings.ProxyPort > 0 {
\t\tproxyURL := &url.URL{
\t\t\tScheme: "http",
\t\t\tHost:   net.JoinHostPort(host, strconv.Itoa(settings.ProxyPort)),
\t\t}
\t\ttransport.Proxy = http.ProxyURL(proxyURL)
\t}
\treturn &http.Client{Transport: transport}
}

func validateProxySettings(settings Settings) error {
\thost := strings.TrimSpace(settings.ProxyHost)
\tport := settings.ProxyPort
\tif host == "" && port == 0 {
\t\treturn nil
\t}
\tif host == "" {
\t\treturn errors.New("update proxy IP/host is required when a proxy port is set")
\t}
\tif port < 1 || port > 65535 {
\t\treturn errors.New("update proxy port must be between 1 and 65535")
\t}
\tif strings.Contains(host, "://") || strings.ContainsAny(host, "/\\\\@ \\t\\r\\n") {
\t\treturn errors.New("update proxy must be an IP address or hostname without scheme, path, or credentials")
\t}
\treturn nil
}
''',
    encoding="utf-8",
)

Path("app/internal/updater/proxy_test.go").write_text(
    '''package updater

import (
\t"net/http"
\t"testing"
)

func TestUpdateProxySettingsPersistAndConfigureTransport(t *testing.T) {
\tdataDir := t.TempDir()
\tmanager, err := NewManager(dataDir, "0.12.13", "owner/repository")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\thost := "127.0.0.1"
\tport := 7890
\tsettings, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port})
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif settings.ProxyHost != host || settings.ProxyPort != port {
\t\tt.Fatalf("proxy settings = %+v", settings)
\t}

\ttransport, ok := manager.updateHTTPClient().Transport.(*http.Transport)
\tif !ok {
\t\tt.Fatalf("HTTP transport type = %T", manager.updateHTTPClient().Transport)
\t}
\trequest, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repository/releases/latest", nil)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tproxyURL, err := transport.Proxy(request)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
\t\tt.Fatalf("proxy URL = %v", proxyURL)
\t}

\treloaded, err := NewManager(dataDir, "0.12.13", "owner/repository")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tgot := reloaded.Settings()
\tif got.ProxyHost != host || got.ProxyPort != port {
\t\tt.Fatalf("reloaded proxy = %+v", got)
\t}
}

func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {
\tmanager, err := NewManager(t.TempDir(), "0.12.13", "owner/repository")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\thost := "127.0.0.1"
\tzero := 0
\tif _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &zero}); err == nil {
\t\tt.Fatal("expected proxy without port to be rejected")
\t}
\tempty := ""
\tport := 7890
\tif _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &port}); err == nil {
\t\tt.Fatal("expected proxy port without host to be rejected")
\t}
\tinvalid := "http://127.0.0.1"
\tif _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &invalid, ProxyPort: &port}); err == nil {
\t\tt.Fatal("expected proxy host with scheme to be rejected")
\t}
}

func TestUpdateProxyCanBeDisabledAgain(t *testing.T) {
\tmanager, err := NewManager(t.TempDir(), "0.12.13", "owner/repository")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\thost := "127.0.0.1"
\tport := 7890
\tif _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port}); err != nil {
\t\tt.Fatal(err)
\t}
\tempty := ""
\tzero := 0
\tif _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &zero}); err != nil {
\t\tt.Fatal(err)
\t}
\ttransport := manager.updateHTTPClient().Transport.(*http.Transport)
\tif transport.Proxy != nil {
\t\tt.Fatal("expected direct update transport after clearing proxy")
\t}
}
''',
    encoding="utf-8",
)

types_path = "frontend/src/types/api.ts"
replace_once(
    types_path,
    '''export interface UpdateSettings {
  repository: string;
  channel: "stable" | "prerelease";
  checkOnStartup: boolean;
}
''',
    '''export interface UpdateSettings {
  repository: string;
  channel: "stable" | "prerelease";
  checkOnStartup: boolean;
  proxyHost: string;
  proxyPort: number;
}
''',
)

store_path = "frontend/src/stores/app.ts"
replace_once(
    store_path,
    '''    async saveUpdateSettings(settings: UpdateSettings) {
      this.updateSettings = await this.runAction("save-update-settings", () => api<UpdateSettings>("/api/update/settings", {
        method: "PUT",
        body: settings as unknown as BodyInit,
      }));
      useUiStore().toast("更新设置已保存", settings.repository ? `GitHub：${settings.repository}` : "尚未配置 GitHub 仓库。", "success");
      return this.updateSettings;
    },
''',
    '''    async saveUpdateSettings(settings: Pick<UpdateSettings, "channel" | "checkOnStartup" | "proxyHost" | "proxyPort">) {
      this.updateSettings = await this.runAction("save-update-settings", () => api<UpdateSettings>("/api/update/settings", {
        method: "PUT",
        body: settings as unknown as BodyInit,
      }));
      const proxyLabel = settings.proxyHost && settings.proxyPort
        ? `更新代理：http://${settings.proxyHost}:${settings.proxyPort}`
        : "更新代理：直连";
      useUiStore().toast("更新设置已保存", proxyLabel, "success");
      return this.updateSettings;
    },
''',
)

page = "frontend/src/pages/SettingsPage.vue"
replace_once(
    page,
    '''const updateRepository = ref("");
const updateChannel = ref<"stable" | "prerelease">("stable");
const updateCheckOnStartup = ref(true);
const updateChecking = ref(false);
''',
    '''const updateChannel = ref<"stable" | "prerelease">("stable");
const updateCheckOnStartup = ref(true);
const updateProxyHost = ref("");
const updateProxyPort = ref("");
const updateChecking = ref(false);
''',
)

replace_once(
    page,
    '''watch(() => app.updateSettings, (settings) => {
  if (!settings) return;
  updateRepository.value = settings.repository;
  updateChannel.value = settings.channel;
  updateCheckOnStartup.value = settings.checkOnStartup;
}, { immediate: true, deep: true });
''',
    '''watch(() => app.updateSettings, (settings) => {
  if (!settings) return;
  updateChannel.value = settings.channel;
  updateCheckOnStartup.value = settings.checkOnStartup;
  updateProxyHost.value = settings.proxyHost || "";
  updateProxyPort.value = settings.proxyPort > 0 ? String(settings.proxyPort) : "";
}, { immediate: true, deep: true });
''',
)

replace_once(
    page,
    '''async function saveUpdatePreferences() {
  try {
    await app.saveUpdateSettings({
      repository: updateRepository.value.trim(),
      channel: updateChannel.value,
      checkOnStartup: updateCheckOnStartup.value,
    });
  } catch (error) {
    ui.toast("保存更新设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function checkForUpdate() {
  if (!updateRepository.value.trim()) {
    ui.toast("请先配置 GitHub 仓库", "格式例如：your-name/mcp-devdesk", "info");
    return;
  }
  updateChecking.value = true;
  try {
    const current = app.updateSettings;
    if (!current || current.repository !== updateRepository.value.trim() || current.channel !== updateChannel.value || current.checkOnStartup !== updateCheckOnStartup.value) {
      await app.saveUpdateSettings({
        repository: updateRepository.value.trim(),
        channel: updateChannel.value,
        checkOnStartup: updateCheckOnStartup.value,
      });
    }
    await app.checkForUpdate(false);
  } catch {
    // checkForUpdate already reports a user-facing error.
  } finally {
    updateChecking.value = false;
  }
}
''',
    '''function updateProxyPayload() {
  const proxyHost = updateProxyHost.value.trim();
  const proxyPortText = updateProxyPort.value.trim();
  if (!proxyHost && !proxyPortText) return { proxyHost: "", proxyPort: 0 };
  if (!proxyHost) {
    ui.toast("代理地址不完整", "请填写代理 IP；不使用代理时 IP 和端口都留空。", "danger");
    return null;
  }
  const proxyPort = Number(proxyPortText);
  if (!Number.isInteger(proxyPort) || proxyPort < 1 || proxyPort > 65535) {
    ui.toast("代理端口无效", "请输入 1 - 65535 之间的代理端口。", "danger");
    return null;
  }
  if (proxyHost.includes("://") || /[\/\\@\s]/.test(proxyHost)) {
    ui.toast("代理 IP 格式无效", "这里只填写 IP 或主机名，不要填写 http://、路径、账号或密码。", "danger");
    return null;
  }
  return { proxyHost, proxyPort };
}

async function saveUpdatePreferences() {
  const proxy = updateProxyPayload();
  if (!proxy) return;
  try {
    await app.saveUpdateSettings({
      channel: updateChannel.value,
      checkOnStartup: updateCheckOnStartup.value,
      ...proxy,
    });
  } catch (error) {
    ui.toast("保存更新设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function checkForUpdate() {
  const proxy = updateProxyPayload();
  if (!proxy) return;
  updateChecking.value = true;
  try {
    const current = app.updateSettings;
    if (!current || current.channel !== updateChannel.value || current.checkOnStartup !== updateCheckOnStartup.value || current.proxyHost !== proxy.proxyHost || current.proxyPort !== proxy.proxyPort) {
      await app.saveUpdateSettings({
        channel: updateChannel.value,
        checkOnStartup: updateCheckOnStartup.value,
        ...proxy,
      });
    }
    await app.checkForUpdate(false);
  } catch {
    // checkForUpdate already reports a user-facing error.
  } finally {
    updateChecking.value = false;
  }
}
''',
)

replace_once(
    page,
    '''          <p>从公开 GitHub Releases 检查并安装新版本。下载包必须同时提供 SHA256 文件，否则 DevDesk 会拒绝更新。</p>
''',
    '''          <p>从内置 GitHub Releases 更新源检查并安装新版本。可选填写 HTTP 代理 IP 和端口；下载包仍必须通过 SHA256 校验。</p>
''',
)

replace_once(
    page,
    '''        <StatusPill :tone="app.updateRelease?.updateAvailable ? 'info' : updateRepository.trim() ? 'neutral' : 'warning'">
          {{ app.updateRelease?.updateAvailable ? `可更新 ${app.updateRelease.latestVersion}` : updateRepository.trim() ? `当前 ${app.status?.version || '--'}` : '未配置仓库' }}
        </StatusPill>
''',
    '''        <StatusPill :tone="app.updateRelease?.updateAvailable ? 'info' : 'neutral'">
          {{ app.updateRelease?.updateAvailable ? `可更新 ${app.updateRelease.latestVersion}` : `当前 ${app.status?.version || '--'}` }}
        </StatusPill>
''',
)

replace_once(
    page,
    '''      <div class="software-update-grid">
        <label class="field span-2">
          <span>GitHub 仓库</span>
          <input v-model.trim="updateRepository" type="text" spellcheck="false" placeholder="owner/repository，例如 your-name/mcp-devdesk" />
          <small>GitHub Actions 正式构建会自动带入当前仓库；本地源码构建没有 remote 时可在这里手动填写。仅支持公开 GitHub Releases，不需要 Token。</small>
        </label>
        <label class="field">
          <span>更新通道</span>
          <select v-model="updateChannel">
            <option value="stable">稳定版</option>
            <option value="prerelease">测试版 / 预发布版</option>
          </select>
        </label>
        <div class="software-update-toggle">
          <ToggleSwitch v-model="updateCheckOnStartup" label="启动时检查更新" description="软件启动后只检查一次 GitHub，不会后台频繁请求。" />
        </div>
      </div>
''',
    '''      <div class="software-update-grid">
        <label class="field">
          <span>更新代理 IP</span>
          <input v-model.trim="updateProxyHost" type="text" spellcheck="false" placeholder="例如 127.0.0.1" />
          <small>可选 HTTP 代理。只填写 IP 或主机名；留空表示直连 GitHub。</small>
        </label>
        <label class="field">
          <span>代理端口</span>
          <input v-model="updateProxyPort" type="number" min="1" max="65535" inputmode="numeric" placeholder="例如 7890" />
          <small>与代理 IP 配套使用，例如 Clash 常用 7890。</small>
        </label>
        <label class="field">
          <span>更新通道</span>
          <select v-model="updateChannel">
            <option value="stable">稳定版</option>
            <option value="prerelease">测试版 / 预发布版</option>
          </select>
        </label>
        <div class="software-update-toggle">
          <ToggleSwitch v-model="updateCheckOnStartup" label="启动时检查更新" description="检查版本、SHA256 和下载更新包都会使用上面的代理。" />
        </div>
      </div>
''',
)

replace_once(
    page,
    '''          <AppButton tone="secondary" icon="refresh" :loading="updateChecking" :disabled="!updateRepository.trim()" @click="checkForUpdate">检查更新</AppButton>
''',
    '''          <AppButton tone="secondary" icon="refresh" :loading="updateChecking" @click="checkForUpdate">检查更新</AppButton>
''',
)

docs = Path("docs/RELEASE.md")
if docs.exists():
    docs_text = docs.read_text(encoding="utf-8")
    if "## 软件更新代理" not in docs_text:
        docs.write_text(
            docs_text.rstrip()
            + "\n\n## 软件更新代理\n\n"
            + "MCP DevDesk 的更新源由正式构建内置，界面不再显示或编辑 GitHub 仓库。软件设置可选填写 HTTP 代理 IP/主机名与端口；填写后 Release 检查、SHA256 下载和 Portable ZIP 下载统一走该代理，留空则直连。代理设置只影响软件更新，不影响 MCP Core、Cloudflare Tunnel 或项目联网。\n",
            encoding="utf-8",
        )
