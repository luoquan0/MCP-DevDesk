<script setup lang="ts">
import { reactive } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();

const form = reactive({
  domain: app.config?.domain ?? "",
  tunnelName: app.config?.tunnelName ?? "mcp-devdesk",
  reuse: true,
});

async function login() {
  try {
    await app.startCloudflareLogin();
  } catch (error) {
    ui.toast("授权启动失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function configure() {
  try {
    await app.configureTunnel({ ...form });
  } catch (error) {
    ui.toast("Tunnel 配置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function syncPort() {
  const accepted = await ui.ask({
    title: "同步 Tunnel 到当前端口",
    message: `程序会清理当前 Tunnel 的旧连接，并创建一个指向 ${app.status?.tunnelInventory.expectedLocalUrl || '当前 MCP 端口'} 的连接。`,
    confirmLabel: "同步端口",
  });
  if (!accepted) return;
  try {
    await app.syncTunnelPort();
  } catch (error) {
    ui.toast("同步失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function stopProcess(pid: number, name?: string) {
  const accepted = await ui.ask({
    title: "关闭 Tunnel 进程",
    message: `将结束 ${name || 'cloudflared'}（PID ${pid}）。对应公网连接会立即中断。`,
    confirmLabel: "关闭进程",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.stopTunnelProcess(pid);
  } catch (error) {
    ui.toast("进程关闭失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack cloudflare-page">
    <PageHeader
      eyebrow="Secure access"
      title="Cloudflare"
      description="管理固定域名、Tunnel 身份和本机 cloudflared 连接。"
    >
      <template #actions>
        <AppButton tone="secondary" icon="refresh" :loading="app.refreshing" @click="app.refreshStatus()">刷新状态</AppButton>
        <AppButton tone="primary" icon="network" :loading="app.actionPending === 'sync-tunnel'" @click="syncPort">同步端口</AppButton>
      </template>
    </PageHeader>

    <section class="cloudflare-summary-grid">
      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top">
          <div class="summary-symbol is-orange"><AppIcon name="cloud" :size="22" /></div>
          <StatusPill :tone="app.status?.cloudflare.authenticated ? 'success' : 'warning'">
            {{ app.status?.cloudflare.authenticated ? '已授权' : '未授权' }}
          </StatusPill>
        </div>
        <span>Cloudflare 账户</span>
        <strong>{{ app.status?.cloudflare.authenticated ? '可以管理 Tunnel' : '需要浏览器授权' }}</strong>
        <AppButton tone="quiet" compact icon="external" :loading="app.actionPending === 'cloudflare-login'" @click="login">
          {{ app.status?.cloudflare.authenticated ? '重新授权' : '开始授权' }}
        </AppButton>
      </AppCard>

      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top">
          <div class="summary-symbol is-blue"><AppIcon name="globe" :size="22" /></div>
          <StatusPill :tone="app.status?.cloudflare.domain ? 'success' : 'neutral'">固定域名</StatusPill>
        </div>
        <span>公网 MCP 地址</span>
        <strong class="break-all">{{ app.status?.remoteMcpUrl || '尚未配置' }}</strong>
        <small class="mono">{{ app.status?.cloudflare.tunnelId || 'Tunnel ID 未生成' }}</small>
      </AppCard>

      <AppCard class="cloudflare-summary-card">
        <div class="summary-card-top">
          <div class="summary-symbol is-mint"><AppIcon name="network" :size="22" /></div>
          <StatusPill :tone="app.status?.tunnel.running ? 'success' : 'neutral'">
            {{ app.status?.tunnel.running ? 'Connected' : 'Offline' }}
          </StatusPill>
        </div>
        <span>本地转发目标</span>
        <strong class="mono">{{ app.status?.tunnelInventory.expectedLocalUrl || '--' }}</strong>
        <small>{{ app.status?.tunnelInventory.count ?? 0 }} 个进程 · {{ app.status?.tunnelInventory.duplicateCount ?? 0 }} 个重复</small>
      </AppCard>
    </section>

    <section class="cloudflare-layout">
      <AppCard class="cloudflare-config-card">
        <div class="card-heading">
          <div><span class="eyebrow">Fixed domain</span><h3>固定域名配置</h3></div>
          <StatusPill :tone="app.status?.cloudflare.authenticated ? 'success' : 'warning'">
            {{ app.status?.cloudflare.authenticated ? 'Ready' : 'Login required' }}
          </StatusPill>
        </div>
        <form class="stack-form" @submit.prevent="configure">
          <label class="field">
            <span>完整域名</span>
            <input v-model="form.domain" placeholder="mcp.example.com" spellcheck="false" />
            <small>域名必须托管在当前 Cloudflare 账户中。</small>
          </label>
          <label class="field">
            <span>Tunnel 名称</span>
            <input v-model="form.tunnelName" placeholder="mcp-devdesk" spellcheck="false" />
          </label>
          <label class="checkbox-row">
            <input v-model="form.reuse" type="checkbox" />
            <span><strong>优先复用同名 Tunnel</strong><small>存在时保留 UUID，只更新 DNS 与本地目标。</small></span>
          </label>
          <div class="form-footer">
            <span>当前 MCP 端口：<code>{{ app.config?.mcpPort || '--' }}</code></span>
            <AppButton tone="primary" type="submit" icon="cloud" :loading="app.actionPending === 'configure-tunnel'" :disabled="!app.status?.cloudflare.authenticated">
              自动配置
            </AppButton>
          </div>
        </form>
      </AppCard>

      <AppCard class="tunnel-process-card">
        <div class="card-heading">
          <div><span class="eyebrow">Process supervision</span><h3>隧道进程</h3></div>
          <div class="heading-pills">
            <StatusPill tone="neutral">{{ app.status?.tunnelInventory.count ?? 0 }} 个</StatusPill>
            <StatusPill v-if="app.status?.tunnelInventory.duplicateCount" tone="warning">重复 {{ app.status.tunnelInventory.duplicateCount }}</StatusPill>
          </div>
        </div>

        <div v-if="app.status?.tunnelInventory.processes.length" class="tunnel-list">
          <article v-for="process in app.status.tunnelInventory.processes" :key="process.pid" class="tunnel-row" :class="{ 'is-current': process.matchesConfig, 'is-duplicate': process.duplicate }">
            <div class="tunnel-row-icon"><AppIcon name="network" :size="17" /></div>
            <div class="tunnel-row-main">
              <div class="tunnel-row-title">
                <strong>{{ process.tunnelName || process.tunnelId || 'Cloudflare Tunnel' }}</strong>
                <span class="tunnel-tags">
                  <StatusPill v-if="process.managed" tone="info" :dot="false">本程序管理</StatusPill>
                  <StatusPill v-if="process.matchesConfig" tone="success" :dot="false">当前配置</StatusPill>
                  <StatusPill v-if="process.duplicate" tone="warning" :dot="false">重复</StatusPill>
                </span>
              </div>
              <code>{{ process.localUrl || '未识别本地目标' }}</code>
              <small>PID {{ process.pid }} · {{ process.tunnelId || 'UUID 未识别' }}</small>
            </div>
            <AppButton tone="danger" compact :loading="app.actionPending === `stop-tunnel-${process.pid}`" @click="stopProcess(process.pid, process.tunnelName)">关闭</AppButton>
          </article>
        </div>
        <div v-else class="inline-empty large">
          <AppIcon name="cloud" :size="26" />
          <div><strong>没有运行中的 Tunnel</strong><span>启动服务或完成固定域名配置后，连接会出现在这里。</span></div>
        </div>
      </AppCard>
    </section>
  </div>
</template>
