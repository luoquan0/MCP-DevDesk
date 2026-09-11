<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import CloudflarePage from "@/pages/CloudflarePage.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { ConnectionMode } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const openai = reactive({ tunnelId: "", executable: "", proxy: "", apiKey: "" });

watch(() => app.config, (config) => {
  if (!config) return;
  openai.tunnelId = config.openAITunnelId || "";
  openai.executable = config.openAITunnelClientExecutable || "";
  openai.proxy = config.openAITunnelProxy || "";
}, { immediate: true });

const mode = computed<ConnectionMode>(() => app.config?.connectionMode || "cloudflare");
const serviceRunning = computed(() => Boolean(app.status?.mcp.running || app.status?.tunnel.running));
const openaiReady = computed(() => Boolean(app.status?.openAITunnel.configured && app.status?.openAITunnel.credentialsReady && app.status?.openAITunnel.installed && app.config?.coreMode === "go"));

const modes: Array<{ id: ConnectionMode; label: string; detail: string; icon: string }> = [
  { id: "cloudflare", label: "Cloudflare", detail: "固定域名公网连接", icon: "cloud" },
  { id: "openai", label: "OpenAI Secure Tunnel", detail: "私有 Tunnel，不暴露公网端口", icon: "network" },
  { id: "local", label: "Local", detail: "仅本机 MCP", icon: "monitor" },
];

async function selectMode(next: ConnectionMode) {
  if (next === mode.value) return;
  if (serviceRunning.value) {
    ui.toast("请先停止主服务", "切换连接方式会改变 MCP 对外入口，停止服务后再切换。", "warning");
    return;
  }
  try {
    await app.saveConfig({ connectionMode: next });
  } catch (error) {
    ui.toast("连接方式切换失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveOpenAI() {
  try {
    await app.saveConfig({
      openAITunnelId: openai.tunnelId.trim(),
      openAITunnelClientExecutable: openai.executable.trim(),
      openAITunnelProxy: openai.proxy.trim(),
    });
    if (openai.apiKey.trim()) {
      await app.saveSecrets({ openAITunnelApiKey: openai.apiKey.trim(), restart: false });
      openai.apiKey = "";
    }
    await app.refreshStatus(true);
    ui.toast("OpenAI Secure Tunnel 已保存", "Runtime API Key 仅加密保存在本机，不会写入命令行。", "success");
  } catch (error) {
    ui.toast("OpenAI Tunnel 配置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function useGoCore() {
  if (serviceRunning.value) {
    ui.toast("请先停止主服务", "切换 MCP Core 前必须停止当前服务。", "warning");
    return;
  }
  try {
    await app.saveConfig({ coreMode: "go", confirmCoreSwitch: true });
  } catch (error) {
    ui.toast("切换 Go Core 失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack connection-page">
    <PageHeader eyebrow="Connection" title="连接方式" description="为主 MCP 实例选择 Cloudflare 固定域名、OpenAI Secure Tunnel 或仅本机 Local。三种模式互斥，切换前需要停止主服务。" />

    <section class="mode-grid">
      <button v-for="item in modes" :key="item.id" class="mode-card" :class="{ 'is-active': mode === item.id }" type="button" @click="selectMode(item.id)">
        <span class="mode-icon"><AppIcon :name="item.icon" :size="20" /></span>
        <span class="mode-copy"><strong>{{ item.label }}</strong><small>{{ item.detail }}</small></span>
        <StatusPill :tone="mode === item.id ? 'success' : 'neutral'" :dot="false">{{ mode === item.id ? '当前' : '选择' }}</StatusPill>
      </button>
    </section>

    <CloudflarePage v-if="mode === 'cloudflare'" />

    <template v-else-if="mode === 'openai'">
      <section class="summary-grid">
        <AppCard>
          <span class="eyebrow">Private transport</span>
          <h3>OpenAI Secure Tunnel</h3>
          <p>ChatGPT 通过 Tunnel 控制面连接本机 tunnel-client，本机 MCP 继续监听 127.0.0.1，不需要开放公网端口。</p>
          <StatusPill :tone="app.status?.tunnel.running ? 'success' : openaiReady ? 'info' : 'warning'">{{ app.status?.tunnel.running ? 'Tunnel 在线' : openaiReady ? '可以启动' : '需要配置' }}</StatusPill>
        </AppCard>
        <AppCard>
          <span class="eyebrow">Local target</span>
          <h3 class="mono break-all">{{ app.status?.localMcpUrl || 'http://127.0.0.1:--/mcp' }}</h3>
          <p>tunnel-client 使用本机专用随机凭证访问 MCP；该凭证不会暴露给 ChatGPT。</p>
        </AppCard>
      </section>

      <AppCard v-if="app.config?.coreMode !== 'go'" class="warning-card">
        <div class="warning-row"><AppIcon name="warning" :size="20" /><div><strong>需要 Go MCP Core</strong><span>OpenAI Secure Tunnel 的本机 sidecar 授权边界只在 Go Core 中提供。</span></div><AppButton tone="primary" :disabled="serviceRunning" @click="useGoCore">切换 Go Core</AppButton></div>
      </AppCard>

      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Secure tunnel configuration</span><h3>OpenAI Tunnel 配置</h3><p>需要 OpenAI Tunnels 的 Runtime API Key（Read + Use）和 Tunnel ID。API Key 只写入本机加密 secrets。</p></div><StatusPill :tone="openaiReady ? 'success' : 'warning'">{{ openaiReady ? 'Ready' : 'Incomplete' }}</StatusPill></div>
        <form class="stack-form" @submit.prevent="saveOpenAI">
          <label class="field"><span>Tunnel ID</span><input v-model="openai.tunnelId" placeholder="tunnel_0123456789abcdef0123456789abcdef" spellcheck="false" /><small>格式：tunnel_ + 32 位小写十六进制字符。</small></label>
          <label class="field"><span>tunnel-client.exe</span><input v-model="openai.executable" placeholder="C:\\path\\to\\tunnel-client.exe" spellcheck="false" /><small>使用 OpenAI 官方 tunnel-client 可执行文件。</small></label>
          <label class="field"><span>Runtime API Key</span><input v-model="openai.apiKey" type="password" autocomplete="off" :placeholder="app.status?.openAITunnel.credentialsReady ? '已配置；留空保持不变' : '输入 Runtime API Key'" /><small>长期 daemon 使用 Runtime key；不要填写 Admin key。</small></label>
          <label class="field"><span>Control-plane HTTP Proxy（可选）</span><input v-model="openai.proxy" placeholder="http://127.0.0.1:7890" spellcheck="false" /></label>
          <div class="form-footer"><span>ChatGPT Connector：选择 <strong>Tunnel</strong>，认证使用 <strong>No authentication</strong>。</span><AppButton tone="primary" type="submit" icon="network" :loading="app.actionPending === 'save-config' || app.actionPending === 'save-secrets'">保存配置</AppButton></div>
        </form>
      </AppCard>
    </template>

    <template v-else>
      <section class="summary-grid">
        <AppCard>
          <span class="eyebrow">Loopback only</span><h3>Local MCP</h3>
          <p>只启动 MCP Core，不启动 Cloudflare 或 OpenAI Tunnel。适合本机调试、局域外完全不暴露的场景。</p>
          <StatusPill :tone="app.status?.mcp.running ? 'success' : 'neutral'">{{ app.status?.mcp.running ? '本机在线' : '已停止' }}</StatusPill>
        </AppCard>
        <AppCard>
          <span class="eyebrow">Endpoint</span><h3 class="mono break-all">{{ app.status?.localMcpUrl || 'http://127.0.0.1:--/mcp' }}</h3>
          <p>OAuth、安全模式、任务隔离和结构化检查仍正常工作，只是不创建公网或私有 Tunnel。</p>
        </AppCard>
      </section>
    </template>
  </div>
</template>

<style scoped>
.connection-page { gap: 18px; }
.mode-grid, .summary-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.mode-card { appearance: none; border: 1px solid var(--border-subtle); border-radius: 18px; background: color-mix(in srgb, var(--surface-card) 90%, transparent); color: var(--text-primary); padding: 16px; display: grid; grid-template-columns: auto minmax(0,1fr) auto; gap: 12px; align-items: center; text-align: left; cursor: pointer; }
.mode-card:hover, .mode-card.is-active { border-color: color-mix(in srgb, var(--accent) 45%, var(--border-subtle)); background: color-mix(in srgb, var(--accent) 8%, var(--surface-card)); }
.mode-icon { width: 38px; height: 38px; display: grid; place-items: center; border-radius: 12px; background: color-mix(in srgb, var(--accent) 12%, transparent); }
.mode-copy { min-width: 0; display: grid; gap: 4px; }
.mode-copy small, .summary-grid p, .card-heading p, .field small, .form-footer { color: var(--text-tertiary); }
.summary-grid :deep(.app-card) { min-height: 168px; display: grid; align-content: start; gap: 10px; }
.warning-card { border-color: color-mix(in srgb, var(--warning) 35%, var(--border-subtle)); }
.warning-row { display: grid; grid-template-columns: auto minmax(0,1fr) auto; gap: 12px; align-items: center; }
.warning-row div { display: grid; gap: 3px; }.warning-row span { color: var(--text-tertiary); font-size: 12px; }
@media (max-width: 900px) { .mode-grid { grid-template-columns: 1fr; } .summary-grid { grid-template-columns: 1fr; } }
@media (max-width: 620px) { .warning-row { grid-template-columns: auto minmax(0,1fr); } .warning-row :deep(.app-button) { grid-column: 1 / -1; } }
</style>
