<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import SecuritySettingsSection from "@/components/settings/SecuritySettingsSection.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore, type ThemeMode } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();
type SettingsSection = "appearance" | "passwords" | "software" | "security";
const activeSettingsSection = ref<SettingsSection>("appearance");
const settingsSections: Array<{ id: SettingsSection; label: string; description: string; icon: string }> = [
  { id: "appearance", label: "外观设计", description: "主题与界面显示", icon: "monitor" },
  { id: "passwords", label: "密码设置", description: "网页登录与 OAuth 凭据", icon: "key" },
  { id: "software", label: "软件设置", description: "桌面、网页、日志与全局提示词", icon: "settings" },
  { id: "security", label: "安全设置", description: "权限、文件范围与联网能力", icon: "shield" },
];
const secretsLoading = ref(false);
const secretsEncrypted = ref(false);
const restartAfterSave = ref(true);
const webControlEnabled = ref(false);
const webControlPort = ref(17861);
const webControlLanEnabled = ref(false);
const webControlAuthEnabled = ref(false);
const webControlPassword = ref("");
const globalPromptEnabled = ref(false);
const globalPromptDraft = ref("");
const updateChannel = ref<"stable" | "prerelease">("stable");
const updateCheckOnStartup = ref(true);
const updateProxyHost = ref("");
const updateProxyPort = ref<string | number>("");
const updateChecking = ref(false);
const updateSettingsSaving = ref(false);
const updateProxyTesting = ref(false);
const updateProxyTestMessage = ref("");
const updateActionFeedback = ref("");
let updateProxyAutoSaveTimer: number | undefined;
let lastSavedProxySignature = "";
const appearanceFileInput = ref<HTMLInputElement | null>(null);
const appearanceSaving = ref(false);
const appearanceUploading = ref(false);
const appearanceForm = reactive({
  theme: "system" as ThemeMode,
  customColorsEnabled: false,
  primaryColor: "#007aff",
  secondaryColor: "#5856d6",
  backgroundOpacity: 30,
});
const globalPromptBytes = computed(() => new TextEncoder().encode(globalPromptDraft.value).length);
const globalPromptActive = computed(() => globalPromptEnabled.value && Boolean(globalPromptDraft.value.trim()));
const appearanceBackgroundUrl = computed(() => app.appearance?.hasBackgroundImage
  ? `/api/appearance/background?rev=${encodeURIComponent(String(app.appearance.backgroundRevision))}`
  : "");
const globalPromptTemplate = `# 全局 Agent 规则

## 通用执行原则
- 优先完成用户当前要求中能够直接执行的步骤，不要为了汇报进度而中断可继续完成的工作。
- 修改代码后执行与改动相关的测试、构建或验证；无法执行时明确说明原因。
- 不覆盖项目自己的 AGENTS.md 规则；项目级约束应保留在项目目录中。`;
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

watch(() => app.projectPromptSettings, (settings) => {
  globalPromptEnabled.value = Boolean(settings?.enabled);
  globalPromptDraft.value = settings?.globalPrompt || "";
}, { immediate: true });

watch(() => app.appearance, (settings) => {
  if (!settings) return;
  appearanceForm.theme = settings.theme;
  appearanceForm.customColorsEnabled = settings.customColorsEnabled;
  appearanceForm.primaryColor = settings.primaryColor;
  appearanceForm.secondaryColor = settings.secondaryColor;
  appearanceForm.backgroundOpacity = settings.backgroundOpacity;
  ui.applyAppearance(settings);
}, { immediate: true, deep: true });

watch(() => app.updateSettings, (settings) => {
  if (!settings) return;
  updateChannel.value = settings.channel;
  updateCheckOnStartup.value = settings.checkOnStartup;
  updateProxyHost.value = settings.proxyHost || "";
  updateProxyPort.value = settings.proxyPort > 0 ? String(settings.proxyPort) : "";
  lastSavedProxySignature = `${updateProxyHost.value.trim()}:${normalizeUpdateProxyPortText(updateProxyPort.value)}`;
}, { immediate: true, deep: true });

watch([() => app.webControl, () => app.config?.webControlPort], ([status, configuredPort]) => {
  webControlEnabled.value = Boolean(status?.enabled ?? app.config?.webControlEnabled);
  webControlPort.value = status?.port || configuredPort || 17861;
  webControlLanEnabled.value = Boolean(status?.lanEnabled ?? app.config?.webControlLanEnabled);
  webControlAuthEnabled.value = Boolean(status?.authEnabled ?? app.config?.webControlAuthEnabled);
}, { immediate: true });

const themes: Array<{ id: ThemeMode; label: string; description: string; icon: string }> = [
  { id: "system", label: "跟随系统", description: "根据 Windows 外观自动切换", icon: "monitor" },
  { id: "light", label: "浅色", description: "明亮、克制的 Apple 风格", icon: "sun" },
  { id: "dark", label: "深色", description: "适合夜间和长时间运行", icon: "moon" },
];

const hexColorPattern = /^#[0-9a-fA-F]{6}$/;

function appearancePreview() {
  const current = app.appearance;
  if (!current) return;
  if (appearanceForm.customColorsEnabled && (!hexColorPattern.test(appearanceForm.primaryColor) || !hexColorPattern.test(appearanceForm.secondaryColor))) return;
  ui.applyAppearance({
    ...current,
    theme: appearanceForm.theme,
    primaryColor: appearanceForm.primaryColor,
    secondaryColor: appearanceForm.secondaryColor,
    backgroundOpacity: appearanceForm.backgroundOpacity,
  });
}

async function saveAppearance(showToast = true) {
  if (appearanceForm.customColorsEnabled && (!hexColorPattern.test(appearanceForm.primaryColor) || !hexColorPattern.test(appearanceForm.secondaryColor))) {
    ui.toast("颜色格式无效", "主配色和副配色必须是 #RRGGBB 格式。", "danger");
    return;
  }
  if (!hexColorPattern.test(appearanceForm.primaryColor)) appearanceForm.primaryColor = "#007aff";
  if (!hexColorPattern.test(appearanceForm.secondaryColor)) appearanceForm.secondaryColor = "#5856d6";
  appearanceSaving.value = true;
  try {
    await app.saveAppearance({ ...appearanceForm });
    if (showToast) ui.toast("外观设置已保存", "桌面软件和网页端会同步使用这套外观。", "success");
  } catch (error) {
    ui.toast("保存外观失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    appearanceSaving.value = false;
  }
}

async function selectAppearanceTheme(theme: ThemeMode) {
  appearanceForm.theme = theme;
  appearancePreview();
  await saveAppearance(false);
}

async function saveAppearanceColors() {
  appearancePreview();
  await saveAppearance(false);
}

async function resetAppearanceColors() {
  appearanceForm.customColorsEnabled = false;
  appearanceForm.primaryColor = "#007aff";
  appearanceForm.secondaryColor = "#5856d6";
  appearancePreview();
  await saveAppearance();
}

async function toggleCustomColors(enabled: boolean) {
  appearanceForm.customColorsEnabled = enabled;
  appearancePreview();
  await saveAppearance(false);
}

function chooseAppearanceBackground() {
  appearanceFileInput.value?.click();
}

async function handleAppearanceBackground(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  appearanceUploading.value = true;
  try {
    await app.uploadAppearanceBackground(file);
  } catch (error) {
    ui.toast("上传背景图失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    appearanceUploading.value = false;
    input.value = "";
  }
}

async function removeAppearanceBackground() {
  try {
    await app.removeAppearanceBackground();
  } catch (error) {
    ui.toast("移除背景图失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function normalizeUpdateProxyPortText(value: string | number | null | undefined) {
  return value == null ? "" : String(value).trim();
}

function updateProxyPayload(showErrors = true) {
  const proxyHost = updateProxyHost.value.trim();
  const proxyPortText = normalizeUpdateProxyPortText(updateProxyPort.value);
  if (!proxyHost && !proxyPortText) return { proxyHost: "", proxyPort: 0 };
  if (!proxyHost) {
    if (showErrors) ui.toast("代理地址不完整", "请填写代理 IP；不使用代理时 IP 和端口都留空。", "danger");
    return null;
  }
  const proxyPort = Number(proxyPortText);
  if (!Number.isInteger(proxyPort) || proxyPort < 1 || proxyPort > 65535) {
    if (showErrors) ui.toast("代理端口无效", "请输入 1 - 65535 之间的代理端口。", "danger");
    return null;
  }
  if (proxyHost.includes("://") || /[\/\@\s]/.test(proxyHost)) {
    if (showErrors) ui.toast("代理 IP 格式无效", "这里只填写 IP 或主机名，不要填写 http://、路径、账号或密码。", "danger");
    return null;
  }
  return { proxyHost, proxyPort };
}

function proxySignature(proxy: { proxyHost: string; proxyPort: number }) {
  return `${proxy.proxyHost}:${proxy.proxyPort || ""}`;
}

function scheduleUpdateProxyAutoSave() {
  if (updateProxyAutoSaveTimer) window.clearTimeout(updateProxyAutoSaveTimer);
  updateProxyAutoSaveTimer = undefined;
  const proxy = updateProxyPayload(false);
  if (!proxy) return;
  const signature = proxySignature(proxy);
  if (signature === lastSavedProxySignature) return;
  updateActionFeedback.value = proxy.proxyHost ? "代理地址已填写，正在自动保存…" : "正在切换为直连模式…";
  updateProxyAutoSaveTimer = window.setTimeout(() => {
    updateProxyAutoSaveTimer = undefined;
    void saveUpdatePreferences(true);
  }, 650);
}

function markUpdateAction(label: string) {
  updateActionFeedback.value = `已收到点击：${label}`;
}

async function saveUpdatePreferences(auto = false) {
  if (!auto && updateProxyAutoSaveTimer) {
    window.clearTimeout(updateProxyAutoSaveTimer);
    updateProxyAutoSaveTimer = undefined;
  }
  const proxy = updateProxyPayload(!auto);
  if (!proxy) return;
  const current = app.updateSettings;
  if (!auto && current
    && current.channel === updateChannel.value
    && current.checkOnStartup === updateCheckOnStartup.value
    && current.proxyHost === proxy.proxyHost
    && current.proxyPort === proxy.proxyPort) {
    updateActionFeedback.value = proxy.proxyHost ? "代理设置已经保存" : "当前已经是直连模式";
    return;
  }
  if (updateSettingsSaving.value) {
    updateActionFeedback.value = "上一轮更新设置仍在保存，最多 6 秒会自动结束";
    return;
  }
  updateSettingsSaving.value = true;
  updateActionFeedback.value = proxy.proxyHost ? "正在保存代理模式…" : "正在保存直连模式…";
  try {
    await app.saveUpdateSettings({
      channel: updateChannel.value,
      checkOnStartup: updateCheckOnStartup.value,
      ...proxy,
    });
    lastSavedProxySignature = proxySignature(proxy);
    updateActionFeedback.value = proxy.proxyHost
      ? `已使用代理模式 · ${proxy.proxyHost}:${proxy.proxyPort}`
      : "已恢复直连模式";
  } catch (error) {
    updateActionFeedback.value = error instanceof Error ? error.message : String(error);
    ui.toast("保存更新设置失败", updateActionFeedback.value, "danger");
  } finally {
    updateSettingsSaving.value = false;
  }
}

async function testUpdateProxy() {
  if (updateProxyTesting.value) {
    updateActionFeedback.value = "上一轮代理测试仍在进行，最多 14 秒会自动结束";
    return;
  }
  markUpdateAction("测试代理");
  const proxy = updateProxyPayload();
  if (!proxy) return;
  if (!proxy.proxyHost || !proxy.proxyPort) {
    ui.toast("未配置代理", "请先填写代理 IP 和端口；留空表示直连，无需测试。", "info");
    return;
  }
  updateProxyTesting.value = true;
  updateProxyTestMessage.value = "正在自动测试 HTTP / SOCKS5...";
  updateActionFeedback.value = "已收到点击：测试代理 · 正在连接…";
  try {
    if (proxySignature(proxy) !== lastSavedProxySignature) await saveUpdatePreferences(false);
    const result = await app.testUpdateProxy();
    updateProxyTestMessage.value = `${result.protocol} 可用 · ${result.latencyMs} ms`;
    updateActionFeedback.value = `已使用代理模式 · ${result.protocol} · ${result.latencyMs} ms`;
    ui.toast("已使用代理模式", result.message, "success");
  } catch (error) {
    updateProxyTestMessage.value = error instanceof Error ? error.message : String(error);
    updateActionFeedback.value = `代理测试失败 · ${updateProxyTestMessage.value}`;
    ui.toast("代理测试失败", updateProxyTestMessage.value, "danger");
  } finally {
    updateProxyTesting.value = false;
  }
}

async function checkForUpdate() {
  if (updateChecking.value) {
    updateActionFeedback.value = "上一轮检查仍在进行，最多 19 秒会自动结束";
    return;
  }
  markUpdateAction("检查更新");
  const proxy = updateProxyPayload();
  if (!proxy) return;
  updateChecking.value = true;
  updateActionFeedback.value = "已收到点击：检查更新 · 正在请求 GitHub…";
  try {
    const current = app.updateSettings;
    if (!current || current.channel !== updateChannel.value || current.checkOnStartup !== updateCheckOnStartup.value || current.proxyHost !== proxy.proxyHost || current.proxyPort !== proxy.proxyPort) {
      await saveUpdatePreferences(false);
    }
    await app.checkForUpdate(false);
    updateActionFeedback.value = "检查更新完成";
  } catch {
    updateActionFeedback.value = "检查更新失败，请查看右下角提示";
  } finally {
    updateChecking.value = false;
  }
}

async function installAvailableUpdate() {
  try {
    await app.installUpdate();
  } catch (error) {
    ui.toast("启动更新失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

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

async function saveWebControl() {
  const port = Number(webControlPort.value);
  if (!Number.isInteger(port) || port < 1024 || port > 65535) {
    ui.toast("网页端口无效", "请输入 1024 - 65535 之间的整数端口。", "danger");
    return;
  }
  try {
    await app.saveWebControl(
      webControlEnabled.value,
      port,
      webControlLanEnabled.value,
      webControlAuthEnabled.value,
      webControlPassword.value,
    );
    webControlPassword.value = "";
  } catch (error) {
    ui.toast("保存网页控制失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function openWebControl() {
  const url = app.webControl?.url || `http://127.0.0.1:${webControlPort.value}/#/`;
  if (!app.webControl?.running) {
    ui.toast("网页控制尚未运行", "请先开启网页控制并保存设置。", "info");
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}

async function saveGlobalPrompt() {
  try {
    await app.saveGlobalProjectPrompt(globalPromptEnabled.value, globalPromptDraft.value);
  } catch (error) {
    ui.toast("保存全局提示词失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function useGlobalPromptTemplate() {
  globalPromptDraft.value = globalPromptTemplate;
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

onMounted(() => {
  if (!app.webControlClient) void loadSecrets();
});
</script>

<template>
  <div class="page-stack settings-page">
    <PageHeader
      eyebrow="Application preferences"
      title="设置"
      description="按栏目管理界面、密码、软件和安全设置。"
    />

    <nav class="settings-section-tabs" aria-label="设置栏目">
      <button
        v-for="section in settingsSections"
        :key="section.id"
        type="button"
        :class="{ 'is-active': activeSettingsSection === section.id }"
        @click="activeSettingsSection = section.id"
      >
        <span class="settings-section-tab-icon"><AppIcon :name="section.icon" :size="18" /></span>
        <span><strong>{{ section.label }}</strong><small>{{ section.description }}</small></span>
      </button>
    </nav>

    <section v-if="activeSettingsSection === 'appearance'" class="appearance-settings-stack">
      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Appearance</span><h3>基础外观</h3><p>保留跟随系统、浅色和深色三种基础模式。</p></div>
        </div>
        <div class="theme-grid">
          <button v-for="theme in themes" :key="theme.id" type="button" class="theme-option" :class="{ 'is-selected': appearanceForm.theme === theme.id }" @click="selectAppearanceTheme(theme.id)">
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

      <section class="appearance-custom-grid">
        <AppCard>
          <div class="card-heading appearance-card-heading">
            <div><span class="eyebrow">Custom palette</span><h3>自定义配色</h3><p>主配色用于按钮、选中状态和强调色；副配色用于辅助强调和紫色系元素。</p></div>
            <AppButton tone="secondary" compact :loading="appearanceSaving" @click="resetAppearanceColors">使用主题默认</AppButton>
          </div>
          <ToggleSwitch
            :model-value="appearanceForm.customColorsEnabled"
            label="启用自定义配色"
            description="关闭时保留当前颜色值，但界面使用浅色/深色主题自身的默认配色。"
            @update:model-value="toggleCustomColors"
          />
          <div class="appearance-color-list" :class="{ 'is-disabled': !appearanceForm.customColorsEnabled }">
            <label class="appearance-color-field">
              <span><strong>主配色</strong><small>主要按钮、焦点和当前选中状态</small></span>
              <div class="appearance-color-control">
                <input v-model="appearanceForm.primaryColor" type="color" :disabled="!appearanceForm.customColorsEnabled" @input="appearancePreview" @change="saveAppearanceColors" />
                <input v-model.trim="appearanceForm.primaryColor" type="text" maxlength="7" spellcheck="false" :disabled="!appearanceForm.customColorsEnabled" @input="appearancePreview" @change="saveAppearanceColors" />
              </div>
            </label>
            <label class="appearance-color-field">
              <span><strong>副配色</strong><small>辅助标签、图标和第二强调色</small></span>
              <div class="appearance-color-control">
                <input v-model="appearanceForm.secondaryColor" type="color" :disabled="!appearanceForm.customColorsEnabled" @input="appearancePreview" @change="saveAppearanceColors" />
                <input v-model.trim="appearanceForm.secondaryColor" type="text" maxlength="7" spellcheck="false" :disabled="!appearanceForm.customColorsEnabled" @input="appearancePreview" @change="saveAppearanceColors" />
              </div>
            </label>
          </div>
          <div class="appearance-palette-preview" :class="{ 'is-disabled': !appearanceForm.customColorsEnabled }">
            <span :style="{ background: appearanceForm.primaryColor }">主配色</span>
            <span :style="{ background: appearanceForm.secondaryColor }">副配色</span>
          </div>
        </AppCard>

        <AppCard>
          <div class="card-heading appearance-card-heading">
            <div><span class="eyebrow">Background image</span><h3>自定义背景</h3><p>上传 PNG、JPEG、GIF 或 WebP，最大 15 MB；图片会保存在 DevDesk 数据目录并同步给网页端。</p></div>
            <StatusPill :tone="app.appearance?.hasBackgroundImage ? 'success' : 'neutral'">{{ app.appearance?.hasBackgroundImage ? '已启用' : '未设置' }}</StatusPill>
          </div>
          <input ref="appearanceFileInput" class="appearance-file-input" type="file" accept="image/png,image/jpeg,image/gif,image/webp" @change="handleAppearanceBackground" />
          <div class="appearance-background-preview" :class="{ 'has-image': Boolean(appearanceBackgroundUrl) }">
            <img v-if="appearanceBackgroundUrl" :src="appearanceBackgroundUrl" alt="背景预览" :style="{ opacity: appearanceForm.backgroundOpacity / 100 }" />
            <div v-else><AppIcon name="monitor" :size="28" /><strong>尚未上传背景图</strong><span>可以使用电脑或手机选择图片上传。</span></div>
          </div>
          <label class="appearance-opacity-field">
            <span><strong>背景图透明度</strong><small>0% 完全隐藏 · 100% 完整显示</small></span>
            <div>
              <input v-model.number="appearanceForm.backgroundOpacity" type="range" min="0" max="100" step="1" @input="appearancePreview" @change="saveAppearance(false)" />
              <output>{{ appearanceForm.backgroundOpacity }}%</output>
            </div>
          </label>
          <div class="form-footer appearance-background-actions">
            <small>自定义背景只改变界面底层背景，不会影响项目文件或 MCP 配置。</small>
            <div class="form-footer-actions">
              <AppButton v-if="app.appearance?.hasBackgroundImage" tone="quiet" @click="removeAppearanceBackground">移除背景</AppButton>
              <AppButton tone="primary" icon="folder" :loading="appearanceUploading" @click="chooseAppearanceBackground">{{ app.appearance?.hasBackgroundImage ? '更换图片' : '上传图片' }}</AppButton>
            </div>
          </div>
        </AppCard>
      </section>
    </section>

    <section v-if="activeSettingsSection === 'software'" class="settings-grid equal">
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

    <SecuritySettingsSection v-if="activeSettingsSection === 'security'" />

    <AppCard v-if="activeSettingsSection === 'software'" class="web-control-settings-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">Browser control</span>
          <h3>网页控制</h3>
          <p>网页端直接复用桌面版完整界面和功能；手机通过响应式布局访问同一套页面，并可远程浏览电脑目录。</p>
        </div>
        <StatusPill :tone="app.webControl?.running ? 'success' : webControlEnabled ? 'warning' : 'neutral'">
          {{ app.webControl?.running ? '运行中' : webControlEnabled ? '未启动' : '已关闭' }}
        </StatusPill>
      </div>
      <div class="web-control-settings-grid">
        <div class="toggle-list">
          <ToggleSwitch
            v-model="webControlEnabled"
            label="启用网页控制"
            description="保存后立即启停独立网页端口。"
          />
          <ToggleSwitch
            v-model="webControlLanEnabled"
            label="允许局域网访问"
            description="开启后监听电脑全部 IPv4 网卡，手机连接同一局域网即可访问。"
          />
          <ToggleSwitch
            v-model="webControlAuthEnabled"
            label="启用密码认证"
            description="建议局域网访问时开启；未登录的设备只能看到登录页。"
          />
        </div>
        <div class="web-control-fields">
          <label class="field web-control-port-field">
            <span>网页端口</span>
            <input v-model.number="webControlPort" type="number" min="1024" max="65535" step="1" inputmode="numeric" />
            <small>不能与内部管理端口 {{ app.config?.adminPort || 17860 }} 或 MCP 端口 {{ app.config?.mcpPort || '--' }} 重复。</small>
          </label>
          <div class="settings-cross-link-note"><AppIcon name="key" :size="15" /><span>网页登录密码已归入“密码设置”栏目。</span></div>
        </div>
      </div>
      <div v-if="webControlLanEnabled && !webControlAuthEnabled" class="inline-alert warning">
        <AppIcon name="info" :size="16" />
        <span>局域网访问未启用密码认证时，同一网络中的其他设备也能修改项目和控制 MCP。建议同时开启密码认证。</span>
      </div>
      <div v-if="app.webControl?.running" class="web-control-addresses top-divider">
        <div><span>本机地址</span><code>{{ app.webControl.url }}</code></div>
        <div v-for="url in app.webControl.lanUrls || []" :key="url"><span>局域网地址</span><code>{{ url }}</code></div>
      </div>
      <div class="form-footer top-divider">
        <small>
          登录后进入与桌面软件一致的完整界面；在网页“项目”页点击浏览时会打开远程电脑目录选择器。
          <template v-if="app.webControl?.lastError"> · {{ app.webControl.lastError }}</template>
        </small>
        <div class="form-footer-actions">
          <AppButton tone="secondary" icon="globe" :disabled="!app.webControl?.running" @click="openWebControl">打开网页</AppButton>
          <AppButton tone="primary" :loading="app.actionPending === 'save-web-control'" @click="saveWebControl">保存网页设置</AppButton>
        </div>
      </div>
    </AppCard>

    <AppCard v-if="activeSettingsSection === 'software'" class="global-prompt-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">Global AI instructions</span>
          <h3>全局提示词</h3>
          <p>只保存在 MCP DevDesk 设置中，不写入任何项目目录。只有开关开启且内容非空时才会注入 Go MCP。</p>
        </div>
        <StatusPill :tone="globalPromptActive ? 'success' : globalPromptEnabled ? 'warning' : 'neutral'">
          {{ globalPromptActive ? '已生效' : globalPromptEnabled ? '已开启但为空' : '已关闭' }}
        </StatusPill>
      </div>
      <div class="toggle-list">
        <ToggleSwitch
          v-model="globalPromptEnabled"
          label="启用全局提示词"
          description="关闭时即使保留了文本也不会注入；项目目录中的 AGENTS.md 不受此开关影响。"
        />
      </div>
      <label class="field project-prompt-field top-divider">
        <span>全局提示词内容</span>
        <textarea
          v-model="globalPromptDraft"
          rows="8"
          spellcheck="false"
          placeholder="# 全局 Agent 规则&#10;&#10;只填写所有项目都应该遵守的通用规则。项目专属规则请写入该项目根目录的 AGENTS.md。"
        />
        <small>{{ globalPromptBytes }} / {{ app.projectPromptSettings?.maxPromptBytes || 32768 }} bytes · 实际生效条件：开关开启 + 内容非空。</small>
      </label>
      <div class="form-footer">
        <small>保存后会同步到运行中的 Go MCP 实例；全局规则只存在 DevDesk 数据目录，不会复制到每个项目文件夹。</small>
        <div class="form-footer-actions">
          <AppButton tone="secondary" @click="useGlobalPromptTemplate">使用通用模板</AppButton>
          <AppButton tone="primary" :loading="app.actionPending === 'save-global-project-prompt'" @click="saveGlobalPrompt">保存全局提示词</AppButton>
        </div>
      </div>
    </AppCard>

    <AppCard v-if="activeSettingsSection === 'passwords'" class="web-password-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">Web login password</span>
          <h3>网页登录密码</h3>
          <p>用于手机或其他局域网设备登录 MCP DevDesk 网页端。</p>
        </div>
        <StatusPill :tone="app.webControl?.passwordConfigured ? 'success' : 'neutral'">{{ app.webControl?.passwordConfigured ? '已设置' : '未设置' }}</StatusPill>
      </div>
      <label class="field settings-password-field">
        <span>新密码</span>
        <input v-model="webControlPassword" type="password" autocomplete="new-password" :placeholder="app.webControl?.passwordConfigured ? '留空保持原密码' : '至少 8 个字符'" />
        <small>{{ app.webControl?.passwordConfigured ? '填写新值会替换当前密码，并注销已有网页会话。' : '开启网页密码认证前需要先设置至少 8 位密码。' }}</small>
      </label>
      <div class="form-footer">
        <small>网页控制的启用、端口和局域网开关仍在“软件设置”栏目。</small>
        <AppButton tone="primary" :loading="app.actionPending === 'save-web-control'" :disabled="!webControlPassword.trim() && !app.webControl?.passwordConfigured" @click="saveWebControl">保存网页登录密码</AppButton>
      </div>
    </AppCard>

    <AppCard v-if="activeSettingsSection === 'passwords' && app.webControlClient" class="credentials-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">Local-only credentials</span>
          <h3>OAuth 与 Token 密钥仅限桌面端</h3>
          <p>局域网页不会读取、生成或修改所有者密码、客户端密钥和 Token 签名密钥。请在电脑上的 MCP DevDesk 桌面窗口中管理这些敏感凭据。</p>
        </div>
        <StatusPill tone="warning">本机专用</StatusPill>
      </div>
    </AppCard>

    <AppCard v-if="activeSettingsSection === 'passwords' && !app.webControlClient" class="credentials-card">
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

    <AppCard v-if="activeSettingsSection === 'software'" class="software-update-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">GitHub Releases</span>
          <h3>软件更新</h3>
          <p>从内置 GitHub Releases 更新源检查并安装新版本。代理只需填写 IP 和端口，自动兼容 HTTP / SOCKS5；下载包仍必须通过 SHA256 校验。</p>
        </div>
        <StatusPill :tone="app.updateRelease?.updateAvailable ? 'info' : 'neutral'">
          {{ app.updateRelease?.updateAvailable ? `可更新 ${app.updateRelease.latestVersion}` : `当前 ${app.status?.version || '--'}` }}
        </StatusPill>
      </div>

      <div class="software-update-grid">
        <label class="field">
          <span>更新代理 IP</span>
          <input v-model.trim="updateProxyHost" type="text" spellcheck="false" placeholder="例如 127.0.0.1" @input="scheduleUpdateProxyAutoSave" />
          <small>可选代理。只填写 IP 或主机名；程序会自动识别 HTTP 或 SOCKS5，留空表示直连 GitHub。</small>
        </label>
        <label class="field">
          <span>代理端口</span>
          <input v-model="updateProxyPort" type="number" min="1" max="65535" inputmode="numeric" placeholder="例如 7890" @input="scheduleUpdateProxyAutoSave" />
          <small>与代理 IP 配套使用，例如 7890、1080、10808；可用“测试代理”确认协议和连通性。</small>
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

      <div class="software-update-facts top-divider">
        <div><span>当前版本</span><strong>{{ app.status?.version || '--' }}</strong></div>
        <div><span>GitHub 最新</span><strong>{{ app.updateRelease?.latestVersion || '尚未检查' }}</strong></div>
        <div><span>安装包</span><code>{{ app.updateRelease?.assetName || `MCP-DevDesk-Portable-amd64.zip` }}</code></div>
      </div>

      <div v-if="app.updateRelease?.notes" class="software-update-notes top-divider">
        <strong>{{ app.updateRelease.name || app.updateRelease.tagName }}</strong>
        <p>{{ app.updateRelease.notes }}</p>
      </div>

      <div class="form-footer top-divider">
        <small>{{ updateActionFeedback || updateProxyTestMessage || '代理 IP 和端口填完整后会自动保存；也可以手动测试代理或检查更新。代理仅用于软件更新。' }}</small>
        <div class="form-footer-actions software-update-actions">
          <button type="button" class="app-button is-quiet" :disabled="updateSettingsSaving" @pointerdown.prevent.stop="saveUpdatePreferences(false)" @keydown.enter.prevent="saveUpdatePreferences(false)" @keydown.space.prevent="saveUpdatePreferences(false)">
            <span v-if="updateSettingsSaving" class="button-spinner" /><span>保存更新设置</span>
          </button>
          <button type="button" class="app-button is-secondary" :disabled="updateProxyTesting" @pointerdown.prevent.stop="testUpdateProxy" @keydown.enter.prevent="testUpdateProxy" @keydown.space.prevent="testUpdateProxy">
            <span v-if="updateProxyTesting" class="button-spinner" /><AppIcon v-else name="shield" :size="16" /><span>测试代理</span>
          </button>
          <button type="button" class="app-button is-secondary" :disabled="updateChecking" @pointerdown.prevent.stop="checkForUpdate" @keydown.enter.prevent="checkForUpdate" @keydown.space.prevent="checkForUpdate">
            <span v-if="updateChecking" class="button-spinner" /><AppIcon v-else name="refresh" :size="16" /><span>检查更新</span>
          </button>
          <button v-if="app.updateRelease?.updateAvailable" type="button" class="app-button is-primary" :disabled="app.actionPending === 'install-update'" @pointerdown.prevent.stop="installAvailableUpdate" @keydown.enter.prevent="installAvailableUpdate" @keydown.space.prevent="installAvailableUpdate">
            <span v-if="app.actionPending === 'install-update'" class="button-spinner" /><AppIcon v-else name="play" :size="16" /><span>立即更新到 {{ app.updateRelease.latestVersion }}</span>
          </button>
        </div>
      </div>
    </AppCard>

    <AppCard v-if="activeSettingsSection === 'software'" class="about-card">
      <div class="about-mark">
        <img class="brand-logo-image" src="/brand-logo.png" alt="MCP DevDesk" />
      </div>
      <div class="about-copy">
        <h3>MCP DevDesk</h3>
        <p>Windows 本地 MCP、Cloudflare Tunnel 与开发工作区管理器。</p>
        <span>版本 {{ app.status?.version || '--' }}</span>
      </div>
      <StatusPill :tone="app.updateRelease?.updateAvailable ? 'info' : 'neutral'">{{ app.updateRelease?.updateAvailable ? '有新版本' : 'GitHub Releases' }}</StatusPill>
    </AppCard>
  </div>
</template>
