from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


settings_path = Path("frontend/src/pages/SettingsPage.vue")
settings = settings_path.read_text(encoding="utf-8")
settings = replace_once(
    settings,
    '''async function saveUpdatePreferences(auto = false) {
  const proxy = updateProxyPayload(!auto);
  if (!proxy) return;
''',
    '''async function saveUpdatePreferences(auto = false) {
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
''',
    "manual save dedupe",
)

button_replacements = [
    (
        '@pointerdown.prevent.stop="saveUpdatePreferences(false)" @click="saveUpdatePreferences(false)"',
        '@pointerdown.prevent.stop="saveUpdatePreferences(false)" @keydown.enter.prevent="saveUpdatePreferences(false)" @keydown.space.prevent="saveUpdatePreferences(false)"',
        "save button events",
    ),
    (
        '@pointerdown.prevent.stop="testUpdateProxy" @click="testUpdateProxy"',
        '@pointerdown.prevent.stop="testUpdateProxy" @keydown.enter.prevent="testUpdateProxy" @keydown.space.prevent="testUpdateProxy"',
        "proxy test button events",
    ),
    (
        '@pointerdown.prevent.stop="checkForUpdate" @click="checkForUpdate"',
        '@pointerdown.prevent.stop="checkForUpdate" @keydown.enter.prevent="checkForUpdate" @keydown.space.prevent="checkForUpdate"',
        "check update button events",
    ),
    (
        '@pointerdown.prevent.stop="installAvailableUpdate" @click="installAvailableUpdate"',
        '@pointerdown.prevent.stop="installAvailableUpdate" @keydown.enter.prevent="installAvailableUpdate" @keydown.space.prevent="installAvailableUpdate"',
        "install update button events",
    ),
]
for old, new, label in button_replacements:
    settings = replace_once(settings, old, new, label)
settings_path.write_text(settings, encoding="utf-8")


logs_path = Path("frontend/src/pages/LogsPage.vue")
logs_path.write_text(r'''<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { LogResponse } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const activeLog = ref("manager");
const logData = ref<LogResponse | null>(null);
const loading = ref(false);

const sources = [
  { id: "manager", label: "管理器", icon: "monitor" },
  { id: "mcp-error", label: "MCP 错误", icon: "warning" },
  { id: "mcp-out", label: "MCP 输出", icon: "terminal" },
  { id: "tunnel-error", label: "Tunnel 错误", icon: "cloud" },
  { id: "tunnel-out", label: "Tunnel 输出", icon: "network" },
  { id: "login", label: "Cloudflare 登录", icon: "key" },
  { id: "watchdog", label: "运行守护", icon: "activity" },
  { id: "audit", label: "MCP 审计", icon: "shield" },
];

const activeSource = computed(() => sources.find((source) => source.id === activeLog.value) ?? sources[0]);
const diagnosticRows = computed(() => Object.entries(app.diagnostics || {}).filter(([key]) => !key.endsWith("Path") && !key.endsWith("Url")));

async function loadLog(name = activeLog.value) {
  loading.value = true;
  activeLog.value = name;
  try {
    logData.value = await app.loadLog(name, 100);
  } catch (error) {
    ui.toast("日志读取失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    loading.value = false;
  }
}

async function refreshAll() {
  await Promise.all([loadLog(), app.loadDiagnostics()]);
}

onMounted(() => loadLog());
</script>

<template>
  <div class="page-stack logs-page">
    <PageHeader
      eyebrow="Observability"
      title="日志与诊断"
      description="集中查看管理器、MCP、Tunnel 和系统环境信息；每类日志最多保留最新 100 条。"
    >
      <template #actions>
        <AppButton tone="secondary" icon="refresh" :loading="loading" @click="refreshAll">刷新</AppButton>
      </template>
    </PageHeader>

    <section class="logs-compact-stack">
      <AppCard class="log-view-card log-view-compact" :padded="false">
        <div class="log-view-toolbar log-view-toolbar-compact">
          <label class="log-source-picker">
            <span class="log-source-picker-label">日志来源</span>
            <span class="log-source-select-wrap">
              <AppIcon :name="activeSource.icon" :size="16" />
              <select v-model="activeLog" :disabled="loading" @change="loadLog(activeLog)">
                <option v-for="source in sources" :key="source.id" :value="source.id">{{ source.label }}</option>
              </select>
            </span>
          </label>

          <div class="log-view-current">
            <strong>{{ activeSource.label }}</strong>
            <span class="mono" :title="logData?.path || ''">{{ logData?.path || '正在读取…' }}</span>
          </div>

          <StatusPill tone="neutral">{{ logData?.lines.length ?? 0 }} 行</StatusPill>
        </div>

        <div class="log-console log-console-compact" :class="{ 'is-loading': loading }">
          <pre>{{ logData?.lines.length ? logData.lines.join('\n') : '日志文件尚未产生内容。' }}</pre>
        </div>
      </AppCard>

      <AppCard class="diagnostic-card diagnostic-card-compact" :padded="false">
        <div class="diagnostic-header">
          <div><strong>环境诊断</strong><span>本机组件和端口检查</span></div>
          <AppIcon name="activity" :size="18" />
        </div>
        <div class="diagnostic-list">
          <div v-for="([key, value]) in diagnosticRows" :key="key">
            <span class="diagnostic-mark" :class="value === true ? 'is-success' : value === false ? 'is-danger' : 'is-neutral'">
              <AppIcon :name="value === true ? 'check' : value === false ? 'x' : 'info'" :size="12" />
            </span>
            <div><strong>{{ key }}</strong><span>{{ String(value) }}</span></div>
          </div>
        </div>
      </AppCard>
    </section>
  </div>
</template>

<style scoped>
.logs-compact-stack {
  display: grid;
  gap: 16px;
  min-width: 0;
}

.log-view-card.log-view-compact {
  min-height: 0 !important;
  overflow: hidden;
  grid-template-rows: auto auto;
}

.log-view-toolbar-compact {
  display: grid;
  grid-template-columns: minmax(220px, 300px) minmax(0, 1fr) auto;
  align-items: end;
  gap: 16px;
  min-height: 0;
  padding: 14px 16px;
}

.log-source-picker {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.log-source-picker-label {
  color: var(--text-secondary) !important;
  font-size: 11px !important;
  font-weight: 650;
}

.log-source-select-wrap {
  position: relative;
  display: flex;
  min-width: 0;
  align-items: center;
}

.log-source-select-wrap .app-icon {
  position: absolute;
  left: 12px;
  z-index: 1;
  color: var(--accent);
  pointer-events: none;
}

.log-source-select-wrap select {
  width: 100%;
  min-width: 0;
  min-height: 40px;
  padding: 0 34px 0 38px;
  border: 1px solid var(--hairline-strong);
  border-radius: 11px;
  color: var(--text);
  background: color-mix(in srgb, var(--surface-solid) 90%, transparent);
  cursor: pointer;
}

.log-source-select-wrap select:disabled {
  cursor: wait;
  opacity: 0.68;
}

.log-view-current {
  display: grid !important;
  min-width: 0;
  gap: 4px !important;
  padding-bottom: 3px;
}

.log-view-current strong {
  font-size: 12px !important;
}

.log-view-current .mono {
  display: block;
  min-width: 0;
  overflow: hidden;
  color: var(--text-tertiary);
  font-size: 9px !important;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-console.log-console-compact {
  width: 100%;
  height: clamp(300px, 44vh, 460px);
  min-height: 300px;
  max-height: 460px;
  overflow: auto;
}

.diagnostic-card.diagnostic-card-compact {
  min-height: 0 !important;
  grid-template-rows: auto auto;
}

@media (max-width: 880px) {
  .log-view-toolbar-compact {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .log-source-picker {
    grid-column: 1 / -1;
  }
}

@media (max-width: 640px) {
  .log-view-toolbar-compact {
    grid-template-columns: 1fr;
    align-items: stretch;
  }

  .log-source-picker,
  .log-view-current {
    grid-column: 1;
  }

  .log-view-toolbar-compact :deep(.status-pill) {
    justify-self: start;
  }

  .log-console.log-console-compact {
    height: 340px;
    min-height: 340px;
  }
}
</style>
''', encoding="utf-8")


design_doc = Path("DESIGN.md")
design = design_doc.read_text(encoding="utf-8")
needle = "- Use an inspector drawer for detailed errors and process metadata.\n"
replacement = needle + "- Log source switching should prefer a compact dropdown above the console instead of a permanently tall source sidebar; log consoles should use a bounded scrollable height.\n"
if replacement not in design:
    design = replace_once(design, needle, replacement, "log UI design rule")
design_doc.write_text(design, encoding="utf-8")
