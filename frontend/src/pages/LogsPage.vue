<script setup lang="ts">
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

    <section class="logs-layout">
      <AppCard class="log-source-card" :padded="false">
        <div class="log-source-header"><span>日志来源</span><small>{{ sources.length }}</small></div>
        <nav class="log-source-list">
          <button v-for="source in sources" :key="source.id" type="button" :class="{ 'is-active': activeLog === source.id }" @click="loadLog(source.id)">
            <AppIcon :name="source.icon" :size="16" />
            <span>{{ source.label }}</span>
            <AppIcon name="chevron-right" :size="14" />
          </button>
        </nav>
      </AppCard>

      <AppCard class="log-view-card" :padded="false">
        <div class="log-view-toolbar">
          <div>
            <strong>{{ sources.find((source) => source.id === activeLog)?.label }}</strong>
            <span class="mono">{{ logData?.path || '正在读取…' }}</span>
          </div>
          <StatusPill tone="neutral">{{ logData?.lines.length ?? 0 }} 行</StatusPill>
        </div>
        <div class="log-console" :class="{ 'is-loading': loading }">
          <pre>{{ logData?.lines.length ? logData.lines.join('\n') : '日志文件尚未产生内容。' }}</pre>
        </div>
      </AppCard>

      <AppCard class="diagnostic-card" :padded="false">
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
