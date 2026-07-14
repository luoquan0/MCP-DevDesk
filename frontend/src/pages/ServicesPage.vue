<script setup lang="ts">
import { reactive, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();

const form = reactive({
  workspace: "",
  mcpPort: 8765,
  adminPort: 17860,
  coreMode: "legacy",
  toolProfile: "full",
  autoStart: false,
  watchdog: true,
});

watch(() => app.config, (config) => {
  if (!config) return;
  form.workspace = config.workspace;
  form.mcpPort = config.mcpPort;
  form.adminPort = config.adminPort;
  form.coreMode = config.coreMode;
  form.toolProfile = config.toolProfile;
  form.autoStart = config.autoStart;
  form.watchdog = config.watchdog;
}, { immediate: true, deep: true });

async function run(action: "start" | "stop" | "restart" | "takeover") {
  if (action === "stop") {
    const accepted = await ui.ask({
      title: "停止全部服务",
      message: "MCP 和由本程序管理的 Tunnel 将停止，远程连接会立即中断。",
      confirmLabel: "停止服务",
      danger: true,
    });
    if (!accepted) return;
  }
  try {
    await app.serviceAction(action);
  } catch (error) {
    ui.toast("服务操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function save() {
  try {
    const requestedMcpPort = Number(form.mcpPort);
    const requestedAdminPort = Number(form.adminPort);
    if (!Number.isInteger(requestedMcpPort) || requestedMcpPort < 1024 || requestedMcpPort > 65535) {
      throw new Error("MCP 端口必须是 1024 到 65535 之间的整数。");
    }
    if (!Number.isInteger(requestedAdminPort) || requestedAdminPort < 1024 || requestedAdminPort > 65535) {
      throw new Error("管理端口必须是 1024 到 65535 之间的整数。");
    }
    if (requestedMcpPort === requestedAdminPort) throw new Error("MCP 端口和管理端口不能相同。");

    const oldPort = app.config?.mcpPort;
    const oldCoreMode = app.config?.coreMode;
    const oldToolProfile = app.config?.toolProfile;
    const runtimeWasRunning = Boolean(app.status?.mcp.running);

    if (oldPort && oldPort !== requestedMcpPort) {
      const accepted = await ui.ask({
        title: "切换 MCP 与 Tunnel 端口",
        message: `端口将从 ${oldPort} 切换为 ${requestedMcpPort}。新 MCP 就绪后，Cloudflare Tunnel 会自动跟随。`,
        confirmLabel: "切换端口",
      });
      if (!accepted) {
        form.mcpPort = oldPort;
        return;
      }
    }

    if (oldCoreMode && oldCoreMode !== form.coreMode) {
      const accepted = await ui.ask({
        title: "切换 MCP 核心",
        message: form.coreMode === "go"
          ? "将切换到新版 Go MCP 核心。切换会重启服务；已经添加到 ChatGPT 的同域名连接可能需要重新授权。建议一个实例长期固定一种核心。"
          : "将切回 Python 兼容核心。切换会重启服务；已经添加到 ChatGPT 的同域名连接可能需要重新授权。建议为兼容核心创建独立实例和域名。",
        confirmLabel: "切换核心",
        danger: Boolean(app.config?.domain),
      });
      if (!accepted) {
        form.coreMode = oldCoreMode;
        return;
      }
    }

    await app.saveConfig({
      adminPort: requestedAdminPort,
      coreMode: form.coreMode as "legacy" | "go",
      toolProfile: form.toolProfile as "full" | "read-only" | "compat-readonly-all",
      autoStart: form.autoStart,
      watchdog: form.watchdog,
    });

    const portChanged = Boolean(oldPort && oldPort !== requestedMcpPort);
    if (portChanged) await app.changePort(requestedMcpPort);

    const runtimeConfigChanged = oldCoreMode !== form.coreMode || oldToolProfile !== form.toolProfile;
    if (!portChanged && runtimeConfigChanged && runtimeWasRunning) {
      await app.serviceAction("restart");
    }
  } catch (error) {
    ui.toast("保存失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack services-page">
    <PageHeader
      eyebrow="Local runtime"
      title="服务"
      description="管理 MCP 核心、端口、自动启动和运行守护。"
    >
      <template #actions>
        <AppButton v-if="!app.status?.mcp.running" tone="primary" icon="play" :loading="app.actionPending === 'start'" @click="run('start')">启动</AppButton>
        <template v-else>
          <AppButton tone="secondary" icon="restart" :loading="app.actionPending === 'restart'" @click="run('restart')">重启</AppButton>
          <AppButton tone="danger" icon="stop" :loading="app.actionPending === 'stop'" @click="run('stop')">停止</AppButton>
        </template>
      </template>
    </PageHeader>

    <div v-if="app.status?.mcpPortOwner.occupied && !app.status.mcpPortOwner.managed && !app.status.mcp.running" class="inline-alert is-warning">
      <AppIcon name="warning" :size="19" />
      <div>
        <strong>当前端口被旧 MCP 实例占用</strong>
        <span>{{ app.status.mcpPortOwner.processName || '未知进程' }} · PID {{ app.status.mcpPortOwner.pid }} · {{ app.status.mcpPortOwner.processPath }}</span>
      </div>
      <AppButton tone="secondary" :loading="app.actionPending === 'takeover'" @click="run('takeover')">接管并启动</AppButton>
    </div>

    <section class="service-state-grid">
      <AppCard class="service-state-card">
        <div class="service-state-header">
          <div class="service-state-icon is-blue"><AppIcon name="services" :size="22" /></div>
          <StatusPill :tone="app.status?.mcp.running ? 'success' : 'neutral'">{{ app.status?.mcp.running ? 'Running' : 'Stopped' }}</StatusPill>
        </div>
        <h2>MCP Core</h2>
        <p>本地文件、Git 和命令工具的核心进程。</p>
        <div class="service-value-row">
          <span>本地端点</span><code>{{ app.status?.localMcpUrl || '--' }}</code>
        </div>
        <div class="service-value-row">
          <span>进程</span><strong>{{ app.status?.mcp.running ? `PID ${app.status.mcp.pid}` : '未运行' }}</strong>
        </div>
      </AppCard>

      <AppCard class="service-state-card">
        <div class="service-state-header">
          <div class="service-state-icon is-purple"><AppIcon name="cloud" :size="22" /></div>
          <StatusPill :tone="app.status?.tunnel.running ? (app.status.tunnelInventory.duplicateCount ? 'warning' : 'success') : 'neutral'">
            {{ app.status?.tunnel.running ? 'Connected' : 'Offline' }}
          </StatusPill>
        </div>
        <h2>Cloudflare Tunnel</h2>
        <p>将固定域名安全转发到当前 MCP 端口。</p>
        <div class="service-value-row">
          <span>目标</span><code>{{ app.status?.tunnelInventory.expectedLocalUrl || '--' }}</code>
        </div>
        <div class="service-value-row">
          <span>进程</span><strong>{{ app.status?.tunnelInventory.count ?? 0 }} 个</strong>
        </div>
      </AppCard>
    </section>

    <form class="settings-grid" @submit.prevent="save">
      <AppCard class="settings-main-card">
        <div class="card-heading">
          <div><span class="eyebrow">Workspace & ports</span><h3>基础设置</h3></div>
        </div>
        <div class="field-grid">
          <label class="field span-2">
            <span>工作目录</span>
            <input v-model="form.workspace" type="text" spellcheck="false" readonly aria-readonly="true" />
            <small>工作目录只能在“项目”页面修改；当前页面仅显示正在使用的目录。</small>
          </label>
          <label class="field">
            <span>MCP 端口</span>
            <input v-model.number="form.mcpPort" type="number" min="1024" max="65535" />
            <small>修改后会同步 Cloudflare Tunnel。</small>
          </label>
          <label class="field">
            <span>管理端口</span>
            <input v-model.number="form.adminPort" type="number" min="1024" max="65535" />
            <small>仅绑定本机管理界面。</small>
          </label>
          <label class="field span-2">
            <span>MCP 核心</span>
            <select v-model="form.coreMode">
              <option value="legacy">旧核心 · 稳定兼容模式</option>
              <option value="go">Go 核心 · 0.7 新版</option>
            </select>
            <small>切换核心会在保存后自动重启 MCP；Cloudflare 域名和端口保持不变。</small>
          </label>
          <label class="field span-2">
            <span>工具配置</span>
            <select v-model="form.toolProfile">
              <option value="full">Full · 全部工具</option>
              <option value="read-only">Read only · 只读工具</option>
              <option value="compat-readonly-all">Compatibility read only</option>
            </select>
          </label>
        </div>
      </AppCard>

      <AppCard class="settings-side-card">
        <div class="card-heading">
          <div><span class="eyebrow">Runtime behavior</span><h3>运行方式</h3></div>
        </div>
        <div class="toggle-list">
          <ToggleSwitch v-model="form.autoStart" label="自动启动服务" description="打开管理器后自动启动 MCP 与 Tunnel。" />
          <ToggleSwitch v-model="form.watchdog" label="运行守护" description="异常退出后自动重启受管理的进程。" />
        </div>
        <div class="settings-card-footer">
          <AppButton tone="primary" type="submit" :loading="app.actionPending === 'save-config' || app.actionPending === 'change-port'">保存设置</AppButton>
        </div>
      </AppCard>
    </form>

    <AppCard>
      <div class="card-heading">
        <div><span class="eyebrow">Process details</span><h3>程序路径</h3></div>
      </div>
      <div class="path-list">
        <div><span>当前核心</span><code>{{ app.config?.coreMode === 'go' ? app.config?.goCoreExecutable : app.config?.coreExecutable || '--' }}</code></div>
        <div><span>旧核心</span><code>{{ app.config?.coreExecutable || '--' }}</code></div>
        <div><span>Go 核心</span><code>{{ app.config?.goCoreExecutable || '--' }}</code></div>
        <div><span>Cloudflare 客户端</span><code>{{ app.config?.cloudflaredExecutable || '--' }}</code></div>
      </div>
    </AppCard>
  </div>
</template>
