<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore, type ThemeMode } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();
const secretsLoading = ref(false);
const secretsEncrypted = ref(false);
const restartAfterSave = ref(true);
const secretForm = reactive({
  ownerPassword: "",
  clientId: "",
  clientSecret: "",
  tokenSecret: "",
  redirectUrisText: "",
});
const visible = reactive<Record<keyof typeof secretForm, boolean>>({
  ownerPassword: false,
  clientId: true,
  clientSecret: false,
  tokenSecret: false,
  redirectUrisText: true,
});

const themes: Array<{ id: ThemeMode; label: string; description: string; icon: string }> = [
  { id: "system", label: "跟随系统", description: "根据 Windows 外观自动切换", icon: "monitor" },
  { id: "light", label: "浅色", description: "明亮、克制的 Apple 风格", icon: "sun" },
  { id: "dark", label: "深色", description: "适合夜间和长时间运行", icon: "moon" },
];

async function setStartup(enabled: boolean) {
  try {
    await app.updateStartup(enabled);
  } catch (error) {
    ui.toast("开机启动设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function setLogging(enabled: boolean) {
  try {
    await app.updateLogging(enabled);
  } catch (error) {
    ui.toast("日志设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function loadSecrets() {
  secretsLoading.value = true;
  try {
    const values = await app.revealSecrets();
    secretForm.ownerPassword = values.ownerPassword ?? "";
    secretForm.clientId = values.clientId ?? "";
    secretForm.clientSecret = values.clientSecret ?? "";
    secretForm.tokenSecret = values.tokenSecret ?? "";
    secretForm.redirectUrisText = (values.redirectUris ?? []).join("\n");
    secretsEncrypted.value = Boolean(values.encryptedAtRest);
  } catch (error) {
    ui.toast("读取凭据失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    secretsLoading.value = false;
  }
}

async function generateSecret(field: "ownerPassword" | "clientId" | "clientSecret" | "tokenSecret" | "all") {
  try {
    const values = await app.generateSecret(field);
    if (values.ownerPassword) secretForm.ownerPassword = values.ownerPassword;
    if (values.clientId) secretForm.clientId = values.clientId;
    if (values.clientSecret) secretForm.clientSecret = values.clientSecret;
    if (values.tokenSecret) secretForm.tokenSecret = values.tokenSecret;
    ui.toast(field === "all" ? "随机凭据已生成" : "随机值已生成", "确认无误后点击保存凭据。", "success");
  } catch (error) {
    ui.toast("随机生成失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function copySecret(label: string, value: string) {
  if (!value) return;
  try {
    await navigator.clipboard.writeText(value);
    ui.toast("已复制", `${label}已复制到剪贴板。`, "success");
  } catch (error) {
    ui.toast("复制失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveSecrets() {
  try {
    const result = await app.saveSecrets({
      ownerPassword: secretForm.ownerPassword,
      clientId: secretForm.clientId,
      clientSecret: secretForm.clientSecret,
      tokenSecret: secretForm.tokenSecret,
      redirectUris: secretForm.redirectUrisText.split(/\r?\n/).map((value) => value.trim()).filter(Boolean),
      restart: restartAfterSave.value,
    });
    secretsEncrypted.value = Boolean(result.secrets.encryptedAtRest);
    if (result.restartError) {
      ui.toast("凭据已保存，但重启失败", result.restartError, "danger");
      return;
    }
    if (result.restarted) {
      ui.toast("凭据已保存", "MCP 服务已自动重启并使用新的凭据。", "success");
    } else if (result.restartRequired) {
      ui.toast("凭据已保存", "MCP 正在运行，需要手动重启后才会生效。", "info");
    } else {
      ui.toast("凭据已保存", "下次启动 MCP 时将使用新的凭据。", "success");
    }
  } catch (error) {
    ui.toast("保存凭据失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function exportDiagnostics() {
  const link = document.createElement("a");
  link.href = "/api/diagnostics/export";
  link.download = "";
  document.body.appendChild(link);
  link.click();
  link.remove();
  ui.toast("正在导出诊断报告", "报告包含运行状态、资源指标和已脱敏的管理器日志，不包含 OAuth 密钥或代理密码。", "info");
}

onMounted(loadSecrets);
</script>

<template>
  <div class="page-stack settings-page">
    <PageHeader
      eyebrow="Application preferences"
      title="设置"
      description="调整界面外观、Windows 集成和本地数据位置。"
    />

    <AppCard>
      <div class="card-heading">
        <div><span class="eyebrow">Appearance</span><h3>外观</h3></div>
      </div>
      <div class="theme-grid">
        <button v-for="theme in themes" :key="theme.id" type="button" class="theme-option" :class="{ 'is-selected': ui.theme === theme.id }" @click="ui.setTheme(theme.id)">
          <div class="theme-preview" :class="`theme-${theme.id}`">
            <span class="theme-preview-sidebar" />
            <span class="theme-preview-card one" />
            <span class="theme-preview-card two" />
          </div>
          <div class="theme-option-copy">
            <span class="theme-option-icon"><AppIcon :name="theme.icon" :size="16" /></span>
            <div><strong>{{ theme.label }}</strong><small>{{ theme.description }}</small></div>
            <span class="radio-mark"><i /></span>
          </div>
        </button>
      </div>
    </AppCard>

    <section class="settings-grid equal">
      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Windows integration</span><h3>桌面集成</h3></div>
          <StatusPill :tone="app.desktop?.nativeWindow ? 'success' : 'neutral'">{{ app.desktop?.nativeWindow ? 'Native' : 'Unavailable' }}</StatusPill>
        </div>
        <div class="toggle-list">
          <ToggleSwitch
            :model-value="Boolean(app.desktop?.startupEnabled)"
            label="Windows 登录时后台运行"
            description="在当前用户登录后启动托盘与本地管理服务。"
            @update:model-value="setStartup"
          />
        </div>
        <div class="detail-list compact top-divider">
          <div><span>窗口模式</span><strong>{{ app.desktop?.windowModeLabel || '--' }}</strong></div>
          <div><span>渲染引擎</span><strong>{{ app.desktop?.renderEngine || 'WebView2' }}</strong></div>
          <div><span>WebView2</span><strong>{{ app.desktop?.runtimeVersion || '--' }}</strong></div>
          <div><span>系统托盘</span><strong>{{ app.desktop?.trayAvailable ? '可用' : '不可用' }}</strong></div>
          <div><span>单实例</span><strong>{{ app.desktop?.singleInstance ? '启用' : '关闭' }}</strong></div>
        </div>
      </AppCard>

      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Local storage</span><h3>数据位置</h3></div>
        </div>
        <div class="toggle-list settings-log-toggle">
          <ToggleSwitch
            :model-value="app.config?.loggingEnabled !== false"
            label="记录运行日志"
            description="开启后每个日志文件只保留最新 100 条，并限制单文件最大 2 MB；关闭后不再写入新日志。"
            @update:model-value="setLogging"
          />
        </div>
        <div class="path-list settings-paths">
          <div><span>程序根目录</span><code>{{ app.status?.rootDirectory || '--' }}</code></div>
          <div><span>数据目录</span><code>{{ app.status?.dataDirectory || '--' }}</code></div>
          <div><span>管理地址</span><code>{{ app.status?.adminUrl || '--' }}</code></div>
        </div>
        <div class="top-divider settings-diagnostic-action">
          <AppButton tone="secondary" icon="logs" @click="exportDiagnostics">导出脱敏诊断报告</AppButton>
          <small>用于排查长时间运行、端口、进程和 WebView2 问题。</small>
        </div>
      </AppCard>
    </section>

    <AppCard class="credentials-card">
      <div class="card-heading credentials-heading">
        <div>
          <span class="eyebrow">OAuth credentials</span>
          <h3>连接密钥与密码</h3>
          <p>可使用自定义值，也可以由系统安全随机生成。所有内容仅通过本机管理接口读取和修改。</p>
        </div>
        <div class="credentials-heading-actions">
          <StatusPill :tone="secretsEncrypted ? 'success' : 'warning'">{{ secretsEncrypted ? 'DPAPI encrypted' : 'Platform fallback' }}</StatusPill>
          <AppButton tone="secondary" icon="refresh" :loading="secretsLoading" @click="loadSecrets">重新读取</AppButton>
          <AppButton tone="secondary" icon="key" @click="generateSecret('all')">全部随机生成</AppButton>
        </div>
      </div>

      <form class="credentials-form" @submit.prevent="saveSecrets">
        <div class="credential-field">
          <span class="credential-label"><strong>所有者密码</strong><small>登录授权时使用，至少 12 个字符。</small></span>
          <div class="credential-input-row">
            <input v-model="secretForm.ownerPassword" :type="visible.ownerPassword ? 'text' : 'password'" autocomplete="new-password" spellcheck="false" />
            <AppButton tone="quiet" compact @click="visible.ownerPassword = !visible.ownerPassword">{{ visible.ownerPassword ? '隐藏' : '显示' }}</AppButton>
            <AppButton tone="quiet" compact icon="copy" @click="copySecret('所有者密码', secretForm.ownerPassword)">复制</AppButton>
            <AppButton tone="quiet" compact icon="refresh" @click="generateSecret('ownerPassword')">随机</AppButton>
          </div>
        </div>

        <div class="credential-field">
          <span class="credential-label"><strong>OAuth 客户端 ID</strong><small>可以自定义，用于识别当前客户端。</small></span>
          <div class="credential-input-row">
            <input v-model="secretForm.clientId" type="text" autocomplete="off" spellcheck="false" />
            <AppButton tone="quiet" compact icon="copy" @click="copySecret('客户端 ID', secretForm.clientId)">复制</AppButton>
            <AppButton tone="quiet" compact icon="refresh" @click="generateSecret('clientId')">随机</AppButton>
          </div>
        </div>

        <div class="credential-field">
          <span class="credential-label"><strong>OAuth 客户端密钥</strong><small>至少 16 个字符，建议使用随机值。</small></span>
          <div class="credential-input-row">
            <input v-model="secretForm.clientSecret" :type="visible.clientSecret ? 'text' : 'password'" autocomplete="new-password" spellcheck="false" />
            <AppButton tone="quiet" compact @click="visible.clientSecret = !visible.clientSecret">{{ visible.clientSecret ? '隐藏' : '显示' }}</AppButton>
            <AppButton tone="quiet" compact icon="copy" @click="copySecret('客户端密钥', secretForm.clientSecret)">复制</AppButton>
            <AppButton tone="quiet" compact icon="refresh" @click="generateSecret('clientSecret')">随机</AppButton>
          </div>
        </div>

        <div class="credential-field">
          <span class="credential-label"><strong>Token 签名密钥</strong><small>必须是 64 位十六进制字符，用于签发和校验 Token。</small></span>
          <div class="credential-input-row">
            <input v-model="secretForm.tokenSecret" :type="visible.tokenSecret ? 'text' : 'password'" autocomplete="new-password" spellcheck="false" maxlength="64" />
            <AppButton tone="quiet" compact @click="visible.tokenSecret = !visible.tokenSecret">{{ visible.tokenSecret ? '隐藏' : '显示' }}</AppButton>
            <AppButton tone="quiet" compact icon="copy" @click="copySecret('Token 签名密钥', secretForm.tokenSecret)">复制</AppButton>
            <AppButton tone="quiet" compact icon="refresh" @click="generateSecret('tokenSecret')">随机</AppButton>
          </div>
        </div>

        <div class="credential-field">
          <span class="credential-label"><strong>OAuth 回调地址</strong><small>静态客户端使用；每行一个，必须是 HTTPS 或本机回环 HTTP 地址。</small></span>
          <div class="credential-input-row credential-textarea-row">
            <textarea v-model="secretForm.redirectUrisText" rows="3" spellcheck="false" placeholder="留空：自动兼容 MCP 客户端回调；填写：强制固定回调地址" />
            <AppButton tone="quiet" compact icon="copy" @click="copySecret('OAuth 回调地址', secretForm.redirectUrisText)">复制</AppButton>
          </div>
        </div>

        <div class="credentials-footer">
          <label class="credentials-restart-option">
            <input v-model="restartAfterSave" type="checkbox" />
            <span><strong>保存后自动重启 MCP</strong><small>当前服务正在运行时，立即加载新凭据。</small></span>
          </label>
          <AppButton tone="primary" type="submit" icon="key" :loading="app.actionPending === 'save-secrets'">保存凭据</AppButton>
        </div>
      </form>
    </AppCard>

    <AppCard class="about-card">
      <div class="about-mark">
        <img class="brand-logo-image" src="/brand-logo.png" alt="MCP DevDesk" />
      </div>
      <div class="about-copy">
        <h3>MCP DevDesk</h3>
        <p>Windows 本地 MCP、Cloudflare Tunnel 与开发工作区管理器。</p>
        <span>版本 {{ app.status?.version || '--' }}</span>
      </div>
      <AppButton tone="secondary" icon="info" disabled>检查更新</AppButton>
    </AppCard>
  </div>
</template>
