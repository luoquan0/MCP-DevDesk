<script setup lang="ts">
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useControlStore } from "@/stores/control";
import { useUiStore } from "@/stores/ui";

const control = useControlStore();
const ui = useUiStore();

async function run(action: "start" | "stop" | "restart") {
  try {
    await control.serviceAction(action);
    ui.toast("服务操作完成", action === "start" ? "MCP 已启动" : action === "stop" ? "MCP 已停止" : "MCP 已重启", "success");
  } catch (error) {
    ui.toast("服务操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <section class="mobile-control-page-stack">
    <div class="mobile-control-page-title"><div><span class="eyebrow">Services</span><h1>服务控制</h1><p>在手机上启动、停止或重启当前 MCP 主实例。</p></div></div>

    <AppCard class="mobile-service-card">
      <div class="card-heading"><div><h3>MCP 服务</h3><p>{{ control.overview?.workspace }}</p></div><StatusPill :tone="control.overview?.mcpRunning ? 'success' : 'neutral'">{{ control.overview?.mcpRunning ? '运行中' : '已停止' }}</StatusPill></div>
      <div class="mobile-service-actions">
        <AppButton tone="primary" :disabled="control.overview?.mcpRunning" :loading="control.actionPending === 'service-start'" @click="run('start')">启动 MCP</AppButton>
        <AppButton tone="secondary" :disabled="!control.overview?.mcpRunning" :loading="control.actionPending === 'service-stop'" @click="run('stop')">停止 MCP</AppButton>
        <AppButton tone="secondary" :loading="control.actionPending === 'service-restart'" @click="run('restart')">重启 MCP</AppButton>
      </div>
      <div class="detail-list compact top-divider">
        <div><span>核心</span><strong>{{ control.overview?.coreMode === 'go' ? 'Go Core' : 'Legacy' }}</strong></div>
        <div><span>MCP 端口</span><strong>{{ control.overview?.mcpPort || '--' }}</strong></div>
        <div><span>Tunnel</span><strong>{{ control.overview?.tunnelRunning ? '运行中' : '已停止' }}</strong></div>
        <div><span>公网地址</span><code>{{ control.overview?.remoteMcpUrl || '--' }}</code></div>
      </div>
    </AppCard>
  </section>
</template>
