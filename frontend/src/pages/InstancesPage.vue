<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import RemoteFolderPicker from "@/components/ui/RemoteFolderPicker.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { MCPInstance, MCPInstanceCreateRequest, MCPInstanceUpdateRequest } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const showCreate = ref(false);
const browsing = ref(false);
const remotePickerOpen = ref(false);
const remotePickerTarget = ref<"create" | "edit">("create");
const remotePickerInitialPath = ref("");
const remotePickerTitle = ref("");
const editingId = ref("");
const tunnelEditingId = ref("");
const logInstanceId = ref("");
const logSource = ref("manager");
const logLines = ref<string[]>([]);
const logPath = ref("");
const logLoading = ref(false);

const createForm = reactive({
  name: "",
  projectId: "",
  workspace: "",
  mcpPort: 0,
  coreMode: "go" as "go" | "legacy",
  permissionMode: "trusted" as "safe" | "trusted" | "dangerous",
  toolProfile: "full" as "full" | "read-only" | "compat-readonly-all",
  fileScope: "workspace" as "workspace" | "roots" | "computer",
  allowNetwork: true,
  autoStart: false,
  watchdog: true,
  loggingEnabled: true,
});

const editForm = reactive({
  name: "",
  projectId: "",
  workspace: "",
  mcpPort: 0,
  coreMode: "go" as "go" | "legacy",
  permissionMode: "trusted" as "safe" | "trusted" | "dangerous",
  toolProfile: "full" as "full" | "read-only" | "compat-readonly-all",
  fileScope: "workspace" as "workspace" | "roots" | "computer",
  allowNetwork: true,
  autoStart: false,
  watchdog: true,
  loggingEnabled: true,
});

const tunnelForm = reactive({ domain: "", tunnelName: "", reuse: true });
const runningCount = computed(() => app.instances.filter((instance) => instance.mcp.running).length);
const tunnelCount = computed(() => app.instances.filter((instance) => instance.tunnel.running).length);
const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error);

function resetCreate() {
  createForm.name = "";
  createForm.projectId = "";
  createForm.workspace = "";
  createForm.mcpPort = 0;
  createForm.coreMode = "go";
  createForm.permissionMode = "trusted";
  createForm.toolProfile = "full";
  createForm.fileScope = "workspace";
  createForm.allowNetwork = true;
  createForm.autoStart = false;
  createForm.watchdog = true;
  createForm.loggingEnabled = true;
}

function selectCreateProject() {
  const project = app.projects.find((item) => item.id === createForm.projectId);
  if (!project) return;
  createForm.workspace = project.path;
  if (!createForm.name.trim()) createForm.name = project.name;
}

function selectEditProject() {
  const project = app.projects.find((item) => item.id === editForm.projectId);
  if (project) editForm.workspace = project.path;
}

async function browseWorkspace(target: "create" | "edit") {
  if (app.webControlClient) {
    const current = target === "create" ? createForm.workspace : editForm.workspace;
    remotePickerTarget.value = target;
    remotePickerInitialPath.value = current || app.config?.workspace || "";
    remotePickerTitle.value = target === "create" ? "选择 MCP 实例绑定的电脑项目目录" : "选择实例的新项目目录";
    remotePickerOpen.value = true;
    return;
  }
  browsing.value = true;
  try {
    const current = target === "create" ? createForm.workspace : editForm.workspace;
    const result = await app.pickFolder(current || app.config?.workspace || "", "选择 MCP 实例绑定的项目目录");
    if (!result.canceled && result.path) {
      if (target === "create") {
        createForm.workspace = result.path;
        createForm.projectId = "";
        if (!createForm.name.trim()) createForm.name = result.path.split(/[\\/]/).filter(Boolean).at(-1) || "新实例";
      } else {
        editForm.workspace = result.path;
        editForm.projectId = "";
      }
    }
  } catch (error) {
    ui.toast("无法打开文件夹选择器", errorMessage(error), "danger");
  } finally {
    browsing.value = false;
  }
}

function applyRemoteWorkspace(path: string) {
  if (remotePickerTarget.value === "create") {
    createForm.workspace = path;
    createForm.projectId = "";
    if (!createForm.name.trim()) createForm.name = path.split(/[\\/]/).filter(Boolean).at(-1) || "新实例";
  } else {
    editForm.workspace = path;
    editForm.projectId = "";
  }
  remotePickerOpen.value = false;
}

async function createInstance() {
  if (!createForm.workspace.trim()) {
    ui.toast("请选择项目目录", "每个 MCP 实例必须绑定一个本地项目目录。", "danger");
    return;
  }
  const request: MCPInstanceCreateRequest = {
    name: createForm.name.trim(),
    projectId: createForm.projectId || undefined,
    workspace: createForm.workspace.trim(),
    mcpPort: createForm.mcpPort > 0 ? createForm.mcpPort : undefined,
    coreMode: createForm.coreMode,
    permissionMode: createForm.permissionMode,
    toolProfile: createForm.toolProfile,
    fileScope: createForm.fileScope,
    allowNetwork: createForm.allowNetwork,
    autoStart: createForm.autoStart,
    watchdog: createForm.watchdog,
    loggingEnabled: createForm.loggingEnabled,
  };
  try {
    await app.createInstance(request);
    resetCreate();
    showCreate.value = false;
  } catch (error) {
    ui.toast("创建 MCP 实例失败", errorMessage(error), "danger");
  }
}

function startEdit(instance: MCPInstance) {
  editingId.value = instance.id;
  tunnelEditingId.value = "";
  editForm.name = instance.name;
  editForm.projectId = instance.projectId || "";
  editForm.workspace = instance.workspace;
  editForm.mcpPort = instance.mcpPort;
  editForm.coreMode = instance.coreMode;
  editForm.permissionMode = instance.permissionMode;
  editForm.toolProfile = instance.toolProfile;
  editForm.fileScope = instance.fileScope;
  editForm.allowNetwork = instance.allowNetwork;
  editForm.autoStart = instance.autoStart;
  editForm.watchdog = instance.watchdog;
  editForm.loggingEnabled = instance.loggingEnabled;
}

async function saveEdit(instance: MCPInstance) {
  const coreChanged = instance.coreMode !== editForm.coreMode;
  if (coreChanged && (instance.mcp.running || instance.tunnel.running)) {
    ui.toast("请先停止实例再切换核心", "核心切换会使现有 OAuth 会话和 ChatGPT 工具连接失效；停止后可直接切换，或复制为新的独立实例。", "danger");
    editForm.coreMode = instance.coreMode;
    return;
  }
  let confirmCoreSwitch = false;
  if (coreChanged && instance.domain) {
    const accepted = await ui.ask({
      title: "确认切换公网实例核心",
      message: `实例“${instance.name}”使用域名 ${instance.domain}。切换核心后需要在 ChatGPT 中重新连接或重新授权。更稳妥的方式是使用“复制到另一核心”。`,
      confirmLabel: "仍然切换",
      danger: true,
    });
    if (!accepted) {
      editForm.coreMode = instance.coreMode;
      return;
    }
    confirmCoreSwitch = true;
  }
  if (!coreChanged && instance.mcp.running) {
    const accepted = await ui.ask({
      title: "保存并重启 MCP 实例",
      message: `实例“${instance.name}”正在运行。保存配置后会安全重启该实例，其他 MCP 实例不会中断。`,
      confirmLabel: "保存并重启",
    });
    if (!accepted) return;
  }
  const request: MCPInstanceUpdateRequest = {
    name: editForm.name.trim(),
    projectId: editForm.projectId,
    workspace: editForm.workspace.trim(),
    mcpPort: editForm.mcpPort,
    coreMode: editForm.coreMode,
    permissionMode: editForm.permissionMode,
    toolProfile: editForm.toolProfile,
    fileScope: editForm.fileScope,
    allowNetwork: editForm.allowNetwork,
    autoStart: editForm.autoStart,
    watchdog: editForm.watchdog,
    loggingEnabled: editForm.loggingEnabled,
    confirmCoreSwitch,
  };
  try {
    await app.updateInstance(instance.id, request);
    editingId.value = "";
  } catch (error) {
    ui.toast("更新 MCP 实例失败", errorMessage(error), "danger");
  }
}

async function cloneWithOtherCore(instance: MCPInstance) {
  const targetCore = instance.coreMode === "go" ? "legacy" : "go";
  const targetLabel = targetCore === "go" ? "Go 核心" : "Python 兼容核心";
  const accepted = await ui.ask({
    title: "复制为独立 MCP 实例",
    message: `将复制“${instance.name}”的项目目录和权限配置，自动分配新端口并使用${targetLabel}。不会复制公网域名、Tunnel 凭据或运行状态。`,
    confirmLabel: "复制实例",
  });
  if (!accepted) return;
  try {
    await app.cloneInstance(instance.id, { coreMode: targetCore });
  } catch (error) {
    ui.toast("复制 MCP 实例失败", errorMessage(error), "danger");
  }
}

async function runInstanceAction(instance: MCPInstance, action: "start" | "stop" | "restart") {
  try {
    await app.instanceAction(instance.id, action);
  } catch (error) {
    ui.toast("实例操作失败", errorMessage(error), "danger");
  }
}

async function deleteInstance(instance: MCPInstance) {
  const accepted = await ui.ask({
    title: "删除 MCP 实例",
    message: `确定删除“${instance.name}”吗？只会删除该实例的配置和日志，不会删除项目目录 ${instance.workspace}。`,
    confirmLabel: "删除实例",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.deleteInstance(instance.id);
    if (editingId.value === instance.id) editingId.value = "";
    if (logInstanceId.value === instance.id) logInstanceId.value = "";
  } catch (error) {
    ui.toast("删除 MCP 实例失败", errorMessage(error), "danger");
  }
}

function startTunnelEdit(instance: MCPInstance) {
  tunnelEditingId.value = instance.id;
  editingId.value = "";
  tunnelForm.domain = instance.domain || "";
  tunnelForm.tunnelName = instance.tunnelName || `mcp-devdesk-${instance.mcpPort}`;
  tunnelForm.reuse = Boolean(instance.tunnelId);
}

async function configureTunnel(instance: MCPInstance) {
  if (!tunnelForm.domain.trim()) {
    ui.toast("请输入完整域名", "例如 api-mcp.example.com", "danger");
    return;
  }
  try {
    await app.configureInstanceTunnel(instance.id, {
      domain: tunnelForm.domain.trim(),
      tunnelName: tunnelForm.tunnelName.trim(),
      reuse: tunnelForm.reuse,
    });
    tunnelEditingId.value = "";
  } catch (error) {
    ui.toast("配置实例 Tunnel 失败", errorMessage(error), "danger");
  }
}

async function repairTunnelDNS(instance: MCPInstance) {
  try {
    await app.repairInstanceTunnelDNS(instance.id);
  } catch (error) {
    ui.toast("修复 DNS 失败", errorMessage(error), "danger");
  }
}

async function copyValue(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value);
    ui.toast(`${label}已复制`, value, "success");
  } catch (error) {
    ui.toast("复制失败", errorMessage(error), "danger");
  }
}

async function openLogs(instance: MCPInstance) {
  logInstanceId.value = instance.id;
  logSource.value = "manager";
  await loadLogs();
}

async function loadLogs() {
  if (!logInstanceId.value) return;
  logLoading.value = true;
  try {
    const result = await app.loadInstanceLog(logInstanceId.value, logSource.value, 100);
    logLines.value = result.lines;
    logPath.value = result.path;
  } catch (error) {
    logLines.value = [];
    logPath.value = "";
    ui.toast("读取实例日志失败", errorMessage(error), "danger");
  } finally {
    logLoading.value = false;
  }
}

function instanceTone(instance: MCPInstance): "success" | "warning" | "danger" | "neutral" {
  if (!instance.configurationOk) return "danger";
  if (!instance.mcp.running) return "neutral";
  if (instance.tunnelId && !instance.tunnel.running) return "warning";
  return "success";
}

function instanceLabel(instance: MCPInstance) {
  if (!instance.configurationOk) return "配置不完整";
  if (!instance.mcp.running) return "已停止";
  if (instance.tunnelId && !instance.tunnel.running) return "MCP 已运行，Tunnel 未连接";
  return instance.tunnel.running ? "MCP 与 Tunnel 运行中" : "MCP 运行中";
}

onMounted(async () => {
  try {
    await Promise.all([app.loadInstances(), app.loadProjects()]);
  } catch (error) {
    ui.toast("读取 MCP 实例失败", errorMessage(error), "danger");
  }
});
</script>

<template>
  <div class="page-stack instances-page">
    <PageHeader eyebrow="Multi-instance runtime" title="MCP 实例" description="一个桌面管理器同时运行多个独立 MCP；每个实例绑定一个项目、一个端口，并可配置独立 Tunnel 域名。">
      <template #actions>
        <AppButton tone="primary" icon="services" @click="showCreate = !showCreate">新建实例</AppButton>
      </template>
    </PageHeader>

    <section class="instance-summary-grid">
      <AppCard><span>实例总数</span><strong>{{ app.instances.length }}</strong><small>包含主实例</small></AppCard>
      <AppCard><span>运行中</span><strong>{{ runningCount }}</strong><small>独立 MCP 进程</small></AppCard>
      <AppCard><span>Tunnel 在线</span><strong>{{ tunnelCount }}</strong><small>独立 cloudflared 连接</small></AppCard>
    </section>

    <div class="instance-info-banner">
      <AppIcon name="info" :size="18" />
      <div>
        <strong>实例之间相互隔离</strong>
        <span>工作目录、端口、运行进程和日志独立；当前使用独立 Tunnel 模式，OAuth 客户端凭据由管理器统一维护。</span>
      </div>
    </div>

    <AppCard v-if="showCreate" class="instance-create-card">
      <div class="card-heading">
        <div><span class="eyebrow">Create runtime</span><h3>新建 MCP 实例</h3></div>
        <AppButton tone="quiet" @click="showCreate = false; resetCreate()">取消</AppButton>
      </div>
      <form class="stack-form" @submit.prevent="createInstance">
        <div class="field-grid">
          <label class="field"><span>实例名称</span><input v-model="createForm.name" placeholder="例如 API 后端" /></label>
          <label class="field"><span>关联已有项目</span><select v-model="createForm.projectId" @change="selectCreateProject"><option value="">不关联，直接选择目录</option><option v-for="project in app.projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
          <label class="field span-2">
            <span>项目目录</span>
            <div class="path-picker-row"><input v-model="createForm.workspace" placeholder="D:\Projects\api" /><AppButton tone="secondary" icon="folder" :loading="browsing" @click="browseWorkspace('create')">浏览</AppButton></div>
          </label>
          <label class="field"><span>MCP 端口</span><input v-model.number="createForm.mcpPort" type="number" min="0" max="65535" placeholder="0 = 自动分配" /><small>填写 0 时自动选择空闲端口。</small></label>
          <label class="field"><span>核心</span><select v-model="createForm.coreMode"><option value="go">Go 核心</option><option value="legacy">Python 兼容核心</option></select></label>
          <label class="field"><span>权限模式</span><select v-model="createForm.permissionMode"><option value="safe">安全</option><option value="trusted">受信任</option><option value="dangerous">高权限</option></select></label>
          <label class="field"><span>工具配置</span><select v-model="createForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>
        </div>
        <div class="instance-toggle-grid">
          <ToggleSwitch v-model="createForm.autoStart" label="自动启动" description="打开管理器后自动启动此实例。" />
          <ToggleSwitch v-model="createForm.watchdog" label="运行守护" description="异常退出后自动重启。" />
          <ToggleSwitch v-model="createForm.loggingEnabled" label="记录日志" description="每类日志最多保留 100 条。" />
          <ToggleSwitch v-model="createForm.allowNetwork" label="允许网络" description="允许该实例的工具访问网络。" />
        </div>
        <div class="form-footer"><small>创建后可单独配置 Cloudflare 域名，不会影响现有实例。</small><AppButton type="submit" tone="primary" :loading="app.actionPending === 'create-instance'">创建实例</AppButton></div>
      </form>
    </AppCard>

    <section class="instance-list">
      <AppCard v-for="instance in app.instances" :key="instance.id" class="instance-card" :class="{ 'is-primary': instance.primary }">
        <div class="instance-card-head">
          <div class="instance-icon"><AppIcon name="server" :size="22" /></div>
          <div class="instance-title">
            <div><h3>{{ instance.name }}</h3><StatusPill v-if="instance.primary" tone="info">主实例</StatusPill><StatusPill :tone="instanceTone(instance)">{{ instanceLabel(instance) }}</StatusPill></div>
            <p class="mono">{{ instance.workspace }}</p>
          </div>
          <div class="instance-card-actions">
            <AppButton v-if="!instance.mcp.running" tone="primary" icon="play" :loading="app.actionPending === `start-instance-${instance.id}`" @click="runInstanceAction(instance, 'start')">启动</AppButton>
            <AppButton v-else tone="secondary" icon="restart" :loading="app.actionPending === `restart-instance-${instance.id}`" @click="runInstanceAction(instance, 'restart')">重启</AppButton>
            <AppButton v-if="instance.mcp.running" tone="quiet" icon="stop" :loading="app.actionPending === `stop-instance-${instance.id}`" @click="runInstanceAction(instance, 'stop')">停止</AppButton>
          </div>
        </div>

        <div class="instance-facts">
          <div><span>端口</span><strong>{{ instance.mcpPort }}</strong></div>
          <div><span>核心</span><strong>{{ instance.coreMode === 'go' ? 'Go' : 'Python' }}</strong></div>
          <div><span>权限</span><strong>{{ instance.permissionMode }}</strong></div>
          <div><span>Tunnel</span><strong>{{ instance.tunnelId ? '已配置' : '未配置' }}</strong></div>
        </div>

        <div class="instance-endpoints">
          <div><span>本地地址</span><code>{{ instance.localMcpUrl }}</code><AppButton tone="quiet" icon="copy" compact @click="copyValue(instance.localMcpUrl, '本地地址')">复制</AppButton></div>
          <div v-if="instance.remoteMcpUrl"><span>公网地址</span><code>{{ instance.remoteMcpUrl }}</code><AppButton tone="quiet" icon="copy" compact @click="copyValue(instance.remoteMcpUrl, '公网地址')">复制</AppButton></div>
          <div v-else><span>公网地址</span><em>尚未配置独立域名</em></div>
        </div>

        <div v-if="!instance.configurationOk" class="instance-warning"><AppIcon name="warning" :size="16" /><span>{{ instance.configurationMessage }}</span></div>

        <div class="instance-secondary-actions">
          <AppButton v-if="!instance.primary" tone="secondary" icon="settings" @click="startEdit(instance)">编辑配置</AppButton>
          <AppButton tone="secondary" icon="copy" :loading="app.actionPending === `clone-instance-${instance.id}`" @click="cloneWithOtherCore(instance)">复制到另一核心</AppButton>
          <AppButton tone="secondary" icon="cloud" @click="startTunnelEdit(instance)">配置 Tunnel</AppButton>
          <AppButton v-if="instance.domain && instance.tunnelId" tone="secondary" icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instance.id}`" @click="repairTunnelDNS(instance)">修复 DNS</AppButton>
          <AppButton tone="secondary" icon="logs" @click="openLogs(instance)">实例日志</AppButton>
          <AppButton v-if="!instance.primary" tone="quiet" @click="deleteInstance(instance)">删除</AppButton>
        </div>

        <form v-if="editingId === instance.id" class="instance-inline-editor" @submit.prevent="saveEdit(instance)">
          <div class="card-heading"><div><span class="eyebrow">Runtime settings</span><h3>编辑实例配置</h3></div><AppButton tone="quiet" @click="editingId = ''">取消</AppButton></div>
          <div class="field-grid">
            <label class="field"><span>实例名称</span><input v-model="editForm.name" /></label>
            <label class="field"><span>关联项目</span><select v-model="editForm.projectId" @change="selectEditProject"><option value="">不关联</option><option v-for="project in app.projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
            <label class="field span-2"><span>项目目录</span><div class="path-picker-row"><input v-model="editForm.workspace" /><AppButton tone="secondary" icon="folder" :loading="browsing" @click="browseWorkspace('edit')">浏览</AppButton></div></label>
            <label class="field"><span>MCP 端口</span><input v-model.number="editForm.mcpPort" type="number" min="1024" max="65535" /></label>
            <label class="field"><span>核心</span><select v-model="editForm.coreMode" :disabled="instance.mcp.running || instance.tunnel.running"><option value="go">Go 核心</option><option value="legacy">Python 兼容核心</option></select><small v-if="instance.mcp.running || instance.tunnel.running">切换核心前必须先停止实例；也可以复制到另一核心。</small></label>
            <label class="field"><span>权限模式</span><select v-model="editForm.permissionMode"><option value="safe">安全</option><option value="trusted">受信任</option><option value="dangerous">高权限</option></select></label>
            <label class="field"><span>工具配置</span><select v-model="editForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>
          </div>
          <div class="instance-toggle-grid">
            <ToggleSwitch v-model="editForm.autoStart" label="自动启动" />
            <ToggleSwitch v-model="editForm.watchdog" label="运行守护" />
            <ToggleSwitch v-model="editForm.loggingEnabled" label="记录日志" />
            <ToggleSwitch v-model="editForm.allowNetwork" label="允许网络" />
          </div>
          <div class="form-footer"><small>运行中的实例保存后会单独重启，不影响其他实例。</small><AppButton type="submit" tone="primary" :loading="app.actionPending === `update-instance-${instance.id}`">保存配置</AppButton></div>
        </form>

        <form v-if="tunnelEditingId === instance.id" class="instance-inline-editor" @submit.prevent="configureTunnel(instance)">
          <div class="card-heading"><div><span class="eyebrow">Cloudflare Tunnel</span><h3>配置独立域名</h3><small>保存后会自动创建并验证公网 DNS；失败会显示 cloudflared 原始错误。</small></div><AppButton tone="quiet" @click="tunnelEditingId = ''">取消</AppButton></div>
          <div class="field-grid">
            <label class="field"><span>完整域名</span><input v-model="tunnelForm.domain" placeholder="api-mcp.example.com" /></label>
            <label class="field"><span>Tunnel 名称</span><input v-model="tunnelForm.tunnelName" placeholder="mcp-devdesk-api" /></label>
          </div>
          <ToggleSwitch v-model="tunnelForm.reuse" label="复用同名 Tunnel" description="已存在同名 Tunnel 时复用其凭据。" />
          <div class="form-footer"><small>需要先在 Cloudflare 页面完成账户授权。</small><AppButton type="submit" tone="primary" :loading="app.actionPending === `configure-instance-tunnel-${instance.id}`">配置域名</AppButton></div>
        </form>
      </AppCard>
    </section>

    <AppCard v-if="logInstanceId" class="instance-log-card">
      <div class="card-heading">
        <div><span class="eyebrow">Instance logs</span><h3>{{ app.instances.find((item) => item.id === logInstanceId)?.name }} 日志</h3></div>
        <div class="instance-log-actions"><select v-model="logSource" @change="loadLogs"><option value="manager">管理器</option><option value="mcp-out">MCP 输出</option><option value="mcp-error">MCP 错误</option><option value="tunnel-out">Tunnel 输出</option><option value="tunnel-error">Tunnel 错误</option><option value="audit">MCP 审计</option></select><AppButton tone="secondary" icon="refresh" :loading="logLoading" @click="loadLogs">刷新</AppButton><AppButton tone="quiet" @click="logInstanceId = ''">关闭</AppButton></div>
      </div>
      <code class="instance-log-path">{{ logPath }}</code>
      <pre class="instance-log-output">{{ logLines.length ? logLines.join('\n') : '当前没有日志记录。' }}</pre>
    </AppCard>

    <RemoteFolderPicker
      :open="remotePickerOpen"
      :initial-path="remotePickerInitialPath"
      :title="remotePickerTitle"
      @close="remotePickerOpen = false"
      @select="applyRemoteWorkspace"
    />
  </div>
</template>
