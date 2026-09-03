<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { Config, FileScope, PermissionMode, ScreenCaptureMode, ScreenWindowInfo } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const screenVisionSaving = ref(false);
const screenWindowsLoading = ref(false);
const screenWindowQuery = ref("");
const form = reactive({
  permissionMode: "safe" as PermissionMode,
  fileScope: "workspace" as FileScope,
  allowNetwork: false,
  screenCaptureEnabled: false,
  screenCaptureMode: "active" as ScreenCaptureMode,
  screenCaptureWindowId: "",
  screenCaptureWindowProcessId: 0,
  screenCaptureWindowTitle: "",
  screenCaptureWindowProcess: "",
  allowedRootsText: "",
});

function syncScreenVision(config: Config | null) {
  if (!config) return;
  form.screenCaptureEnabled = config.screenCaptureEnabled ?? false;
  form.screenCaptureMode = config.screenCaptureMode || "active";
  form.screenCaptureWindowId = config.screenCaptureWindowId || "";
  form.screenCaptureWindowProcessId = config.screenCaptureWindowProcessId || 0;
  form.screenCaptureWindowTitle = config.screenCaptureWindowTitle || "";
  form.screenCaptureWindowProcess = config.screenCaptureWindowProcess || "";
}

watch(() => app.config, (config) => {
  if (!config) return;
  form.permissionMode = config.permissionMode;
  form.fileScope = config.fileScope;
  form.allowNetwork = config.allowNetwork;
  form.allowedRootsText = (config.allowedRoots ?? []).join("\n");
  if (!screenVisionSaving.value) syncScreenVision(config);
}, { immediate: true, deep: true });

const modes = [
  { id: "safe" as const, title: "安全模式", subtitle: "默认推荐", icon: "shield", tone: "success", description: "限制内联脚本、Shell 展开和高风险命令。联网可单独开启。" },
  { id: "trusted" as const, title: "信任模式", subtitle: "日常开发", icon: "key", tone: "warning", description: "允许常用开发命令、包管理器、联网和脚本执行。" },
  { id: "dangerous" as const, title: "危险模式", subtitle: "完全控制", icon: "warning", tone: "danger", description: "关闭应用层命令门控。远程会话可以执行当前 Windows 用户能够执行的操作。" },
];

const screenModes: Array<{ id: ScreenCaptureMode; title: string; subtitle: string; icon: string }> = [
  { id: "window", title: "指定窗口", subtitle: "锁定到你手动选择的一扇窗口", icon: "lock" },
  { id: "active", title: "当前窗口", subtitle: "每次调用时读取当时的前台窗口", icon: "monitor" },
  { id: "desktop", title: "整个桌面", subtitle: "读取所有显示器组成的虚拟桌面", icon: "overview" },
];

const screenWindows = computed(() => app.config?.screenWindows ?? []);
const filteredScreenWindows = computed(() => {
  const query = screenWindowQuery.value.trim().toLowerCase();
  if (!query) return screenWindows.value;
  return screenWindows.value.filter((window) =>
    window.title.toLowerCase().includes(query)
    || (window.processName || "").toLowerCase().includes(query)
    || window.id.toLowerCase().includes(query));
});
const selectedScreenWindow = computed(() => screenWindows.value.find((window) => window.id.toLowerCase() === form.screenCaptureWindowId.toLowerCase()));
const selectedScreenMode = computed(() => screenModes.find((mode) => mode.id === form.screenCaptureMode) ?? screenModes[1]);
const screenVisionReady = computed(() => {
  if (!form.screenCaptureEnabled) return false;
  if (form.screenCaptureMode !== "window") return true;
  return Boolean(form.screenCaptureWindowId && selectedScreenWindow.value);
});

async function persistScreenVision(update: Partial<Config>) {
  if (screenVisionSaving.value) return;
  const wasEnabled = Boolean(app.config?.screenCaptureEnabled);
  const willBeEnabled = update.screenCaptureEnabled ?? wasEnabled;
  screenVisionSaving.value = true;
  try {
    await app.saveConfig(update);
    if (app.status?.mcp.running && (wasEnabled || willBeEnabled)) await app.serviceAction("restart");
    await app.loadConfig();
    syncScreenVision(app.config);
  } catch (error) {
    syncScreenVision(app.config);
    ui.toast("屏幕视觉设置失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    screenVisionSaving.value = false;
  }
}

async function refreshScreenWindows() {
  if (screenWindowsLoading.value || screenVisionSaving.value) return;
  screenWindowsLoading.value = true;
  try {
    await app.loadConfig();
    syncScreenVision(app.config);
  } catch (error) {
    ui.toast("读取窗口失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    screenWindowsLoading.value = false;
  }
}

async function selectScreenMode(mode: ScreenCaptureMode) {
  if (screenVisionSaving.value || form.screenCaptureMode === mode) return;
  form.screenCaptureMode = mode;
  await persistScreenVision({ screenCaptureMode: mode });
  if (mode === "window") await refreshScreenWindows();
}

async function selectScreenWindow(window: ScreenWindowInfo) {
  form.screenCaptureWindowId = window.id;
  form.screenCaptureWindowProcessId = window.processId;
  form.screenCaptureWindowTitle = window.title;
  form.screenCaptureWindowProcess = window.processName || "";
  await persistScreenVision({
    screenCaptureWindowId: window.id,
    screenCaptureWindowProcessId: window.processId,
    screenCaptureWindowTitle: window.title,
    screenCaptureWindowProcess: window.processName || "",
  });
}

async function toggleScreenVision(enabled: boolean) {
  if (screenVisionSaving.value) return;
  if (enabled && app.config?.coreMode !== "go") {
    ui.toast("无法启用屏幕视觉", "屏幕视觉只由 Go MCP Core 提供，请先在软件设置中切换到 Go Core。", "warning");
    return;
  }
  if (enabled && form.permissionMode === "safe") {
    ui.toast("无法启用屏幕视觉", "屏幕画面可能包含敏感信息，请先选择“信任模式”或“危险模式”。", "warning");
    return;
  }
  if (enabled && form.screenCaptureMode === "window" && !form.screenCaptureWindowId) {
    await refreshScreenWindows();
    ui.toast("请先选择窗口", "指定窗口模式需要先从下面的窗口列表选择一个目标。", "warning");
    return;
  }
  if (enabled && !app.config?.screenCaptureEnabled) {
    const accepted = await ui.ask({
      title: "启用屏幕视觉（测试）",
      message: "启用后，已连接并获得授权的 MCP 客户端只能按你选择的捕获模式读取画面。截图仅在工具被调用时生成，不持续录屏、不保存历史；画面仍可能包含聊天、账号、文件名等敏感内容。",
      confirmLabel: "确认启用",
      danger: true,
    });
    if (!accepted) return;
  }
  form.screenCaptureEnabled = enabled;
  await persistScreenVision({ screenCaptureEnabled: enabled });
}

async function savePermissions() {
  if (form.screenCaptureEnabled && form.permissionMode === "safe") {
    ui.toast("无法启用屏幕视觉", "屏幕画面可能包含敏感信息，请先选择“信任模式”或“危险模式”。", "warning");
    return;
  }
  if (form.screenCaptureEnabled && app.config?.coreMode !== "go") {
    ui.toast("无法启用屏幕视觉", "屏幕视觉只由 Go MCP Core 提供。", "warning");
    return;
  }
  if (form.screenCaptureEnabled && form.screenCaptureMode === "window" && !form.screenCaptureWindowId) {
    ui.toast("请先选择窗口", "指定窗口模式必须选择一个目标窗口。", "warning");
    return;
  }
  if (form.permissionMode === "dangerous" || form.fileScope === "computer") {
    const accepted = await ui.ask({
      title: "应用高风险权限",
      message: "危险模式或整台电脑范围可能允许远程会话访问工作区外文件、安装软件和删除数据。请只在完全可信的连接中使用。",
      confirmLabel: "确认应用",
      danger: true,
    });
    if (!accepted) return;
  }
  try {
    await app.saveConfig({
      permissionMode: form.permissionMode,
      fileScope: form.fileScope,
      allowNetwork: form.permissionMode !== "safe" ? true : form.allowNetwork,
      screenCaptureEnabled: form.screenCaptureEnabled,
      screenCaptureMode: form.screenCaptureMode,
      screenCaptureWindowId: form.screenCaptureWindowId,
      screenCaptureWindowProcessId: form.screenCaptureWindowProcessId,
      screenCaptureWindowTitle: form.screenCaptureWindowTitle,
      screenCaptureWindowProcess: form.screenCaptureWindowProcess,
      allowedRoots: form.allowedRootsText.split(/\r?\n/).map((value) => value.trim()).filter(Boolean),
    });
    if (app.status?.mcp.running) await app.serviceAction("restart");
    await app.loadConfig();
  } catch (error) {
    ui.toast("权限保存失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <section class="settings-security-section">
    <div class="settings-section-heading">
      <div>
        <span class="eyebrow">Permission & security</span>
        <h2>权限与安全</h2>
        <p>控制远程 MCP 会话能够运行的命令、访问的路径、网络能力和屏幕视觉。</p>
      </div>
      <AppButton tone="primary" icon="shield" :loading="app.actionPending === 'save-config'" @click="savePermissions">保存权限</AppButton>
    </div>

    <div v-if="form.permissionMode === 'dangerous' || form.fileScope === 'computer'" class="inline-alert is-danger">
      <AppIcon name="warning" :size="20" />
      <div><strong>高风险权限已选择</strong><span>当前会话可能访问工作区以外的文件并执行不可逆命令。保存前请再次确认。</span></div>
    </div>

    <AppCard>
      <div class="card-heading">
        <div><span class="eyebrow">Execution policy</span><h3>权限模式</h3></div>
        <StatusPill :tone="form.permissionMode === 'dangerous' ? 'danger' : form.permissionMode === 'trusted' ? 'warning' : 'success'">
          {{ form.permissionMode }}
        </StatusPill>
      </div>
      <div class="permission-mode-grid">
        <button v-for="mode in modes" :key="mode.id" type="button" class="permission-mode-card" :class="{ 'is-selected': form.permissionMode === mode.id, [`is-${mode.tone}`]: true }" @click="form.permissionMode = mode.id">
          <div class="permission-mode-top">
            <span class="permission-mode-icon"><AppIcon :name="mode.icon" :size="21" /></span>
            <span class="radio-mark"><i /></span>
          </div>
          <strong>{{ mode.title }}</strong>
          <small>{{ mode.subtitle }}</small>
          <p>{{ mode.description }}</p>
        </button>
      </div>
    </AppCard>

    <section class="security-grid">
      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Filesystem</span><h3>文件访问范围</h3></div></div>
        <div class="segmented-control vertical">
          <label :class="{ 'is-selected': form.fileScope === 'workspace' }"><input v-model="form.fileScope" type="radio" value="workspace" /><span><strong>当前工作区</strong><small>仅允许当前项目目录。</small></span></label>
          <label :class="{ 'is-selected': form.fileScope === 'roots' }"><input v-model="form.fileScope" type="radio" value="roots" /><span><strong>授权根目录</strong><small>允许多个指定目录。</small></span></label>
          <label :class="{ 'is-selected': form.fileScope === 'computer' }"><input v-model="form.fileScope" type="radio" value="computer" /><span><strong>整台电脑</strong><small>Shell 可访问当前 Windows 用户拥有权限的位置。</small></span></label>
        </div>
        <label class="field security-roots-field">
          <span>授权根目录</span>
          <textarea v-model="form.allowedRootsText" rows="4" spellcheck="false" placeholder="每行填写一个目录，例如 D:\\Projects" />
          <small>仅在“授权根目录”范围下生效；当前工作区始终自动允许。</small>
        </label>
      </AppCard>

      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Network</span><h3>联网能力</h3></div></div>
        <ToggleSwitch v-model="form.allowNetwork" :disabled="form.permissionMode !== 'safe'" label="允许联网命令" :description="form.permissionMode === 'safe' ? '允许 npm、pip、Git、curl 等工具访问网络。' : '信任模式和危险模式默认允许联网。'" />
        <div class="capability-list">
          <div><AppIcon name="network" :size="16" /><span>包管理器与远程 Git</span><StatusPill :tone="form.permissionMode !== 'safe' || form.allowNetwork ? 'success' : 'neutral'">{{ form.permissionMode !== 'safe' || form.allowNetwork ? '允许' : '禁止' }}</StatusPill></div>
          <div><AppIcon name="terminal" :size="16" /><span>内联脚本与 Shell 展开</span><StatusPill :tone="form.permissionMode === 'safe' ? 'neutral' : 'success'">{{ form.permissionMode === 'safe' ? '限制' : '允许' }}</StatusPill></div>
          <div><AppIcon name="warning" :size="16" /><span>高风险命令</span><StatusPill :tone="form.permissionMode === 'dangerous' ? 'danger' : 'neutral'">{{ form.permissionMode === 'dangerous' ? '不拦截' : '受保护' }}</StatusPill></div>
        </div>
      </AppCard>

      <AppCard class="screen-vision-card">
        <div class="card-heading screen-vision-heading">
          <div>
            <span class="eyebrow">Screen Vision · Experimental</span>
            <h3>屏幕视觉（测试）</h3>
            <p>选择 AI 能读取的画面范围。模式和开关会立即保存；如果 MCP 正在运行，会自动重启加载新策略。</p>
          </div>
          <StatusPill :tone="screenVisionReady ? 'warning' : form.screenCaptureEnabled ? 'info' : 'neutral'">
            {{ screenVisionReady ? `已启用 · ${selectedScreenMode.title}` : form.screenCaptureEnabled ? '等待目标' : '已关闭' }}
          </StatusPill>
        </div>

        <ToggleSwitch
          :model-value="form.screenCaptureEnabled"
          :disabled="screenVisionSaving || form.permissionMode === 'safe' || app.config?.coreMode !== 'go'"
          label="允许 AI 按需读取屏幕画面"
          :description="app.config?.coreMode !== 'go' ? '需要先切换到 Go MCP Core。' : form.permissionMode === 'safe' ? '需要先切换到信任模式或危险模式。' : '只在 MCP 工具主动调用时截图；空闲时不录屏、不启动持续捕获。'"
          @update:model-value="toggleScreenVision"
        />

        <div class="screen-vision-mode-grid">
          <button
            v-for="mode in screenModes"
            :key="mode.id"
            type="button"
            class="screen-vision-mode-card"
            :class="{ 'is-selected': form.screenCaptureMode === mode.id }"
            :disabled="screenVisionSaving"
            @click="selectScreenMode(mode.id)"
          >
            <span class="screen-vision-mode-icon"><AppIcon :name="mode.icon" :size="20" /></span>
            <span><strong>{{ mode.title }}</strong><small>{{ mode.subtitle }}</small></span>
            <span class="radio-mark"><i /></span>
          </button>
        </div>

        <div v-if="form.screenCaptureMode === 'window'" class="screen-window-picker">
          <div class="screen-window-picker-heading">
            <div>
              <strong>选择允许读取的窗口</strong>
              <small>目标用窗口 ID + 进程 ID 锁定。窗口关闭或身份变化后必须重新选择，避免误读到别的程序。</small>
            </div>
            <AppButton tone="secondary" icon="refresh" compact :loading="screenWindowsLoading" :disabled="screenVisionSaving" @click="refreshScreenWindows">刷新窗口</AppButton>
          </div>

          <div v-if="form.screenCaptureWindowId" class="screen-window-selected" :class="{ 'is-missing': !selectedScreenWindow }">
            <AppIcon :name="selectedScreenWindow ? 'lock' : 'warning'" :size="18" />
            <div>
              <strong>{{ selectedScreenWindow?.title || form.screenCaptureWindowTitle || form.screenCaptureWindowId }}</strong>
              <small>
                {{ selectedScreenWindow?.processName || form.screenCaptureWindowProcess || '未知进程' }}
                · {{ form.screenCaptureWindowId }}
                <template v-if="!selectedScreenWindow"> · 当前不可用，请刷新后重新选择</template>
              </small>
            </div>
            <StatusPill :tone="selectedScreenWindow ? 'success' : 'danger'">{{ selectedScreenWindow ? '已锁定' : '已失效' }}</StatusPill>
          </div>

          <label class="screen-window-search">
            <AppIcon name="search" :size="16" />
            <input v-model.trim="screenWindowQuery" type="search" spellcheck="false" placeholder="搜索窗口标题、进程名或窗口 ID" />
          </label>

          <div class="screen-window-list">
            <button
              v-for="window in filteredScreenWindows"
              :key="`${window.id}-${window.processId}`"
              type="button"
              class="screen-window-option"
              :class="{ 'is-selected': form.screenCaptureWindowId.toLowerCase() === window.id.toLowerCase() && form.screenCaptureWindowProcessId === window.processId }"
              :disabled="screenVisionSaving"
              @click="selectScreenWindow(window)"
            >
              <span class="screen-window-option-icon"><AppIcon name="monitor" :size="18" /></span>
              <span class="screen-window-option-copy">
                <strong>{{ window.title }}</strong>
                <small>{{ window.processName || '未知进程' }} · PID {{ window.processId }} · {{ window.bounds.width }}×{{ window.bounds.height }}</small>
              </span>
              <StatusPill v-if="window.active" tone="info">当前前台</StatusPill>
              <span class="screen-window-radio"><i /></span>
            </button>
            <div v-if="!filteredScreenWindows.length" class="screen-window-empty">
              <AppIcon name="monitor" :size="22" />
              <span>{{ screenWindowsLoading ? '正在读取 Windows 窗口…' : '没有找到匹配窗口，尝试刷新或清空搜索条件。' }}</span>
            </div>
          </div>
        </div>

        <div class="capability-list screen-vision-facts">
          <div><AppIcon name="lock" :size="16" /><span>当前允许范围</span><StatusPill :tone="form.screenCaptureEnabled ? 'warning' : 'neutral'">{{ form.screenCaptureEnabled ? selectedScreenMode.title : '不可读' }}</StatusPill></div>
          <div><AppIcon name="shield" :size="16" /><span>截图历史</span><StatusPill tone="success">不保存</StatusPill></div>
          <div><AppIcon name="terminal" :size="16" /><span>鼠标与键盘控制</span><StatusPill tone="neutral">未开放</StatusPill></div>
        </div>
        <p class="field-hint">当前测试功能仅由 Go MCP Core 提供。部分受保护、DRM、最小化或硬件加速窗口可能返回黑屏；关闭本开关后视觉工具会从 MCP 工具列表中移除。</p>
      </AppCard>
    </section>
  </section>
</template>

<style scoped>
.screen-vision-card {
  grid-column: 1 / -1;
}

.screen-vision-heading p {
  max-width: 760px;
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 13px;
}

.screen-vision-mode-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-top: 16px;
}

.screen-vision-mode-card {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) 18px;
  align-items: center;
  gap: 11px;
  min-height: 82px;
  padding: 13px 14px;
  border: 1px solid var(--hairline);
  border-radius: 14px;
  color: var(--text);
  text-align: left;
  background: color-mix(in srgb, var(--surface-solid) 62%, transparent);
  cursor: pointer;
  transition: border-color var(--transition-fast), background var(--transition-fast), transform var(--transition-fast);
}

.screen-vision-mode-card:hover:not(:disabled) {
  transform: translateY(-1px);
  border-color: var(--hairline-strong);
  background: var(--surface-hover);
}

.screen-vision-mode-card.is-selected {
  border-color: color-mix(in srgb, var(--accent) 54%, var(--hairline));
  background: var(--accent-soft);
}

.screen-vision-mode-card:disabled {
  opacity: 0.62;
  cursor: wait;
}

.screen-vision-mode-icon,
.screen-window-option-icon {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: 11px;
  color: var(--accent);
  background: var(--accent-soft);
}

.screen-vision-mode-card > span:nth-child(2),
.screen-window-option-copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.screen-vision-mode-card strong,
.screen-window-option-copy strong {
  overflow: hidden;
  font-size: 13px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.screen-vision-mode-card small,
.screen-window-option-copy small,
.screen-window-picker-heading small,
.screen-window-selected small {
  color: var(--text-tertiary);
  font-size: 11px;
}

.screen-window-picker {
  display: grid;
  gap: 12px;
  margin-top: 16px;
  padding: 16px;
  border: 1px solid var(--hairline);
  border-radius: 16px;
  background: color-mix(in srgb, var(--surface-muted) 70%, transparent);
}

.screen-window-picker-heading,
.screen-window-selected,
.screen-window-option {
  display: flex;
  align-items: center;
  gap: 12px;
}

.screen-window-picker-heading {
  justify-content: space-between;
}

.screen-window-picker-heading > div,
.screen-window-selected > div {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.screen-window-selected {
  padding: 11px 12px;
  border: 1px solid color-mix(in srgb, var(--success) 28%, var(--hairline));
  border-radius: 12px;
  background: var(--success-soft);
}

.screen-window-selected.is-missing {
  border-color: color-mix(in srgb, var(--danger) 32%, var(--hairline));
  background: var(--danger-soft);
}

.screen-window-selected > div {
  flex: 1;
}

.screen-window-search {
  display: flex;
  align-items: center;
  gap: 9px;
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--hairline);
  border-radius: 11px;
  color: var(--text-tertiary);
  background: var(--surface-solid);
}

.screen-window-search:focus-within {
  border-color: color-mix(in srgb, var(--accent) 45%, var(--hairline));
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.screen-window-search input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  color: var(--text);
  background: transparent;
}

.screen-window-list {
  display: grid;
  max-height: 292px;
  gap: 6px;
  overflow: auto;
  padding-right: 3px;
}

.screen-window-option {
  width: 100%;
  min-width: 0;
  min-height: 58px;
  padding: 9px 11px;
  border: 1px solid transparent;
  border-radius: 12px;
  color: var(--text);
  text-align: left;
  background: color-mix(in srgb, var(--surface-solid) 72%, transparent);
  cursor: pointer;
}

.screen-window-option:hover:not(:disabled) {
  border-color: var(--hairline);
  background: var(--surface-hover);
}

.screen-window-option.is-selected {
  border-color: color-mix(in srgb, var(--accent) 46%, var(--hairline));
  background: var(--accent-soft);
}

.screen-window-option-copy {
  flex: 1;
}

.screen-window-radio {
  display: grid;
  width: 18px;
  height: 18px;
  flex: none;
  place-items: center;
  border: 1px solid var(--hairline-strong);
  border-radius: 50%;
}

.screen-window-option.is-selected .screen-window-radio {
  border-color: var(--accent);
}

.screen-window-option.is-selected .screen-window-radio i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
}

.screen-window-empty {
  display: flex;
  min-height: 84px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--text-tertiary);
  font-size: 12px;
  text-align: center;
}

.screen-vision-facts {
  margin-top: 14px;
}

@media (max-width: 900px) {
  .screen-vision-mode-grid {
    grid-template-columns: 1fr;
  }

  .screen-window-picker-heading {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
