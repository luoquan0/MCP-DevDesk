<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();
const router = useRouter();

const workspaceName = computed(() => app.config?.workspace?.split(/[\\/]/).filter(Boolean).at(-1) ?? "尚未配置工作区");
const systemLabel = computed(() => app.healthy ? "一切运行正常" : app.connectionError ? "管理器连接失败" : "有项目需要处理");
const systemDescription = computed(() => {
  if (app.connectionError) return app.connectionError;
  if (!app.status?.configurationOk) return app.status?.configurationMessage || "请完成必要配置后启动服务。";
  if (!app.status.mcp.running) return "MCP 服务当前未运行，启动后即可接收远程工具调用。";
  if (app.status.cloudflare.tunnelId && !app.status.tunnel.running) return "MCP 已运行，但 Cloudflare Tunnel 当前离线。";
  return "本地 MCP、固定域名和桌面管理器都已就绪。";
});

async function run(action: "start" | "stop" | "restart") {
  try {
    await app.serviceAction(action);
  } catch (error) {
    ui.toast("操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function copy(value?: string) {
  if (!value) return;
  await navigator.clipboard.writeText(value);
  ui.toast("已复制", value, "success");
}
</script>

<template>
  <div class="page-stack overview-page">
    <PageHeader
      eyebrow="Workspace overview"
      title="概览"
      description="查看本地 MCP、Cloudflare Tunnel 与当前工作区的整体状态。"
    >
      <template #actions>
        <AppButton
          v-if="!app.status?.mcp.running"
          tone="primary"
          icon="play"
          :loading="app.actionPending === 'start'"
          @click="run('start')"
        >启动服务</AppButton>
        <AppButton
          v-else
          tone="secondary"
          icon="restart"
          :loading="app.actionPending === 'restart'"
          @click="run('restart')"
        >重新启动</AppButton>
      </template>
    </PageHeader>

    <AppCard class="system-hero">
      <div class="system-hero-orb" :class="{ 'is-healthy': app.healthy }">
        <AppIcon :name="app.healthy ? 'check' : 'activity'" :size="26" />
      </div>
      <div class="system-hero-copy">
        <StatusPill :tone="app.healthy ? 'success' : app.connectionError ? 'danger' : 'warning'">
          {{ app.healthy ? 'Operational' : 'Attention' }}
        </StatusPill>
        <h2>{{ systemLabel }}</h2>
        <p>{{ systemDescription }}</p>
      </div>
      <div class="system-hero-actions">
        <button type="button" class="url-chip" @click="copy(app.status?.remoteMcpUrl)">
          <AppIcon name="globe" :size="16" />
          <span>{{ app.status?.remoteMcpUrl || app.status?.localMcpUrl || '等待配置' }}</span>
          <AppIcon name="copy" :size="14" />
        </button>
      </div>
    </AppCard>

    <section class="metric-grid">
      <AppCard class="metric-card" interactive @click="router.push('/services')">
        <div class="metric-icon is-blue"><AppIcon name="services" /></div>
        <div class="metric-copy">
          <span>MCP 服务</span>
          <strong>{{ app.status?.mcp.running ? '运行中' : '已停止' }}</strong>
          <small class="mono">{{ app.status?.localMcpUrl || '127.0.0.1:--' }}</small>
        </div>
        <StatusPill :tone="app.status?.mcp.running ? 'success' : 'neutral'">
          {{ app.status?.mcp.running ? `PID ${app.status.mcp.pid}` : 'Offline' }}
        </StatusPill>
      </AppCard>

      <AppCard class="metric-card" interactive @click="router.push('/cloudflare')">
        <div class="metric-icon is-indigo"><AppIcon name="cloud" /></div>
        <div class="metric-copy">
          <span>Cloudflare Tunnel</span>
          <strong>{{ app.status?.tunnelInventory.count ?? 0 }} 个进程</strong>
          <small>{{ app.status?.tunnelInventory.duplicateCount ? `发现 ${app.status.tunnelInventory.duplicateCount} 个重复连接` : '没有重复连接' }}</small>
        </div>
        <StatusPill :tone="app.status?.tunnel.running ? (app.status.tunnelInventory.duplicateCount ? 'warning' : 'success') : 'neutral'">
          {{ app.status?.tunnel.running ? 'Connected' : 'Offline' }}
        </StatusPill>
      </AppCard>

      <AppCard class="metric-card" interactive @click="router.push('/projects')">
        <div class="metric-icon is-mint"><AppIcon name="folder" /></div>
        <div class="metric-copy">
          <span>当前项目</span>
          <strong>{{ workspaceName }}</strong>
          <small class="truncate">{{ app.config?.workspace || '选择本地工作目录' }}</small>
        </div>
        <AppIcon name="chevron-right" :size="17" class="metric-chevron" />
      </AppCard>

      <AppCard class="metric-card" interactive @click="router.push('/security')">
        <div class="metric-icon is-orange"><AppIcon name="shield" /></div>
        <div class="metric-copy">
          <span>权限策略</span>
          <strong>{{ app.status?.permissionMode === 'dangerous' ? '危险模式' : app.status?.permissionMode === 'trusted' ? '信任模式' : '安全模式' }}</strong>
          <small>{{ app.status?.allowNetwork ? '允许联网操作' : '网络访问受限' }}</small>
        </div>
        <StatusPill :tone="app.status?.permissionMode === 'dangerous' ? 'danger' : app.status?.permissionMode === 'trusted' ? 'warning' : 'success'">
          {{ app.status?.fileScope || 'workspace' }}
        </StatusPill>
      </AppCard>
    </section>

    <section class="overview-grid">
      <AppCard class="overview-panel">
        <div class="card-heading">
          <div>
            <span class="eyebrow">Current workspace</span>
            <h3>{{ workspaceName }}</h3>
          </div>
          <AppButton tone="quiet" compact icon="chevron-right" @click="router.push('/projects')">查看项目</AppButton>
        </div>
        <div class="workspace-visual">
          <div class="workspace-symbol"><AppIcon name="folder" :size="28" /></div>
          <div>
            <strong>{{ app.config?.workspace || '尚未设置工作区' }}</strong>
            <span>所有直接文件工具会以此目录作为当前项目根。</span>
          </div>
        </div>
        <div class="detail-list compact">
          <div><span>工具配置</span><strong>{{ app.status?.toolProfile || '--' }}</strong></div>
          <div><span>MCP 端口</span><strong class="mono">{{ app.config?.mcpPort || '--' }}</strong></div>
          <div><span>Watchdog</span><strong>{{ app.config?.watchdog ? '已启用' : '已关闭' }}</strong></div>
        </div>
      </AppCard>

      <AppCard class="overview-panel">
        <div class="card-heading">
          <div>
            <span class="eyebrow">Service health</span>
            <h3>运行状态</h3>
          </div>
          <AppButton tone="quiet" compact icon="activity" @click="router.push('/logs')">诊断</AppButton>
        </div>
        <div class="service-list">
          <div class="service-row">
            <span class="service-dot" :class="app.status?.mcp.running ? 'is-success' : 'is-muted'" />
            <div><strong>MCP Core</strong><span>{{ app.status?.mcp.lastError || '本地工具服务' }}</span></div>
            <small>{{ app.status?.mcp.running ? `PID ${app.status.mcp.pid}` : 'Stopped' }}</small>
          </div>
          <div class="service-row">
            <span class="service-dot" :class="app.status?.tunnel.running ? 'is-success' : 'is-muted'" />
            <div><strong>Cloudflare Tunnel</strong><span>{{ app.status?.cloudflare.domain || '尚未配置固定域名' }}</span></div>
            <small>{{ app.status?.tunnel.running ? `${app.status.tunnelInventory.count} process` : 'Offline' }}</small>
          </div>
          <div class="service-row">
            <span class="service-dot is-success" />
            <div><strong>Desktop Manager</strong><span>{{ app.desktop?.windowModeLabel || 'Windows 原生窗口' }}</span></div>
            <small>Ready</small>
          </div>
        </div>
      </AppCard>

      <AppCard class="overview-panel span-2">
        <div class="card-heading">
          <div>
            <span class="eyebrow">Tunnel processes</span>
            <h3>连接概览</h3>
          </div>
          <AppButton tone="quiet" compact icon="cloud" @click="router.push('/cloudflare')">管理连接</AppButton>
        </div>
        <div v-if="app.status?.tunnelInventory.processes.length" class="process-summary-list">
          <div v-for="process in app.status.tunnelInventory.processes.slice(0, 4)" :key="process.pid" class="process-summary-row">
            <div class="process-avatar"><AppIcon name="network" :size="16" /></div>
            <div class="process-summary-copy">
              <strong>{{ process.tunnelName || process.tunnelId || 'Cloudflare Tunnel' }}</strong>
              <span class="mono">{{ process.localUrl || '本地目标未知' }}</span>
            </div>
            <StatusPill :tone="process.duplicate ? 'warning' : process.matchesConfig ? 'success' : 'neutral'">
              {{ process.duplicate ? '重复' : process.matchesConfig ? '当前配置' : `PID ${process.pid}` }}
            </StatusPill>
          </div>
        </div>
        <div v-else class="inline-empty">
          <AppIcon name="cloud" :size="24" />
          <div><strong>没有运行中的 Tunnel</strong><span>启动服务后，这里会显示 cloudflared 连接。</span></div>
        </div>
      </AppCard>
    </section>
  </div>
</template>
