<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import RemoteFolderPicker from "@/components/ui/RemoteFolderPicker.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { MCPInstance, Project } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();

const search = ref("");
const selectedFolder = ref("__all__");
const folderPaneCollapsed = ref(localStorage.getItem("mcp-devdesk-project-folders-collapsed") === "1");
const showAddProject = ref(false);
const showAddFolder = ref(false);
const newFolderName = ref("");
const addProjectName = ref("");
const addProjectPath = ref("");
const addProjectFolder = ref("");
const remotePickerOpen = ref(false);
const remotePickerTarget = ref<"add" | "project-path" | "runtime">("add");
const remotePickerInitialPath = ref("");
const remotePickerTitle = ref("");
const runtimeProject = ref<Project | null>(null);
const pathProject = ref<Project | null>(null);
const pathDraft = ref("");
const promptProject = ref<Project | null>(null);
const promptDraft = ref("");
const selectedProjectIds = ref<string[]>([]);
const selectionAnchorId = ref("");
const draggedProjectIds = ref<string[]>([]);
const dragTargetFolder = ref("");
const folderContextMenu = ref<{ folder: string; x: number; y: number } | null>(null);

const runtimeForm = reactive({
  name: "",
  workspace: "",
  mcpPort: 0,
  coreMode: "go" as "go" | "legacy",
  permissionMode: "trusted" as "safe" | "trusted" | "dangerous",
  toolProfile: "full" as "full" | "read-only" | "compat-readonly-all",
  allowNetwork: true,
  autoStart: false,
  watchdog: true,
  loggingEnabled: true,
  domain: "",
  tunnelName: "",
  reuseTunnel: false,
});

const serviceForm = reactive({
  mcpPort: 8770,
  coreMode: "go" as "go" | "legacy",
  toolProfile: "full" as "full" | "read-only" | "compat-readonly-all",
  autoStart: false,
  watchdog: true,
});

const normalizedSearch = computed(() => search.value.trim().toLocaleLowerCase());
const runningProjectInstances = computed(() => app.instances.filter((instance) => !instance.primary && instance.mcp.running).length);
const promptBytes = computed(() => new TextEncoder().encode(promptDraft.value).length);
const selectedProjectIdSet = computed(() => new Set(selectedProjectIds.value));

function toggleFolderPane() {
  folderPaneCollapsed.value = !folderPaneCollapsed.value;
  localStorage.setItem("mcp-devdesk-project-folders-collapsed", folderPaneCollapsed.value ? "1" : "0");
}

const visibleFolders = computed(() => {
  const query = normalizedSearch.value;
  if (!query) return app.projectFolders;
  return app.projectFolders.filter((folder) => folder.toLocaleLowerCase().includes(query));
});

const filteredProjects = computed(() => {
  const query = normalizedSearch.value;
  return app.projects.filter((project) => {
    if (selectedFolder.value === "__unfiled__" && project.folder) return false;
    if (selectedFolder.value !== "__all__" && selectedFolder.value !== "__unfiled__" && project.folder !== selectedFolder.value) return false;
    if (!query) return true;
    return [project.name, project.path, project.folder || ""].some((value) => value.toLocaleLowerCase().includes(query));
  });
});

function normalizePath(value = "") {
  return value.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLocaleLowerCase();
}

function isActiveProject(project: Project) {
  return normalizePath(project.path) === normalizePath(app.config?.workspace);
}

function instanceForProject(project: Project) {
  return app.instances.find((instance) => !instance.primary && instance.projectId === project.id)
    || app.instances.find((instance) => !instance.primary && normalizePath(instance.workspace) === normalizePath(project.path));
}

function runtimeStatus(project: Project) {
  const instance = instanceForProject(project);
  if (!instance) return { label: "未配置", tone: "neutral" as const };
  if (instance.mcp.running) return { label: `运行中 · ${instance.mcpPort}`, tone: "success" as const };
  return { label: `已配置 · ${instance.mcpPort}`, tone: "info" as const };
}

async function createFolder() {
  const name = newFolderName.value.trim();
  if (!name) return;
  try {
    const folder = await app.addProjectFolder(name);
    selectedFolder.value = folder;
    newFolderName.value = "";
    showAddFolder.value = false;
  } catch (error) {
    ui.toast("创建项目文件夹失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function addProject() {
  const path = addProjectPath.value.trim();
  if (!path) return;
  try {
    const project = await app.addProject(path, addProjectName.value.trim());
    if (addProjectFolder.value) await app.updateProjectFolder(project.id, addProjectFolder.value);
    addProjectName.value = "";
    addProjectPath.value = "";
    addProjectFolder.value = "";
    showAddProject.value = false;
  } catch (error) {
    ui.toast("添加项目失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function browseProjectPath() {
  if (app.webControlClient) {
    remotePickerTarget.value = "add";
    remotePickerInitialPath.value = addProjectPath.value || app.config?.workspace || "";
    remotePickerTitle.value = "选择要添加的电脑项目目录";
    remotePickerOpen.value = true;
    return;
  }
  void (async () => {
    try {
      const result = await app.pickFolder(addProjectPath.value || app.config?.workspace || "", "选择要添加的本地项目");
      if (!result.canceled && result.path) addProjectPath.value = result.path;
    } catch (error) {
      ui.toast("无法打开文件夹选择器", error instanceof Error ? error.message : String(error), "danger");
    }
  })();
}

async function moveProject(project: Project, folder: string) {
  try {
    await app.updateProjectFolder(project.id, folder);
  } catch (error) {
    ui.toast("移动项目失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function moveProjects(ids: string[], folder: string) {
  if (!ids.length) return;
  try {
    await app.updateProjectsFolder(ids, folder);
  } catch (error) {
    ui.toast("移动项目失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function handleMobileProjectFolderChange(project: Project, event: Event) {
  const target = event.target as HTMLSelectElement | null;
  if (target) void moveProject(project, target.value);
}

function selectProject(project: Project, event: MouseEvent) {
  const ids = filteredProjects.value.map((item) => item.id);
  if (event.shiftKey && selectionAnchorId.value) {
    const anchorIndex = ids.indexOf(selectionAnchorId.value);
    const currentIndex = ids.indexOf(project.id);
    if (anchorIndex >= 0 && currentIndex >= 0) {
      const start = Math.min(anchorIndex, currentIndex);
      const end = Math.max(anchorIndex, currentIndex);
      selectedProjectIds.value = ids.slice(start, end + 1);
      return;
    }
  }
  if (event.ctrlKey || event.metaKey) {
    selectedProjectIds.value = selectedProjectIdSet.value.has(project.id)
      ? selectedProjectIds.value.filter((id) => id !== project.id)
      : [...selectedProjectIds.value, project.id];
  } else {
    selectedProjectIds.value = [project.id];
  }
  selectionAnchorId.value = project.id;
}

function beginProjectDrag(project: Project, event: DragEvent) {
  if (!selectedProjectIdSet.value.has(project.id)) {
    selectedProjectIds.value = [project.id];
    selectionAnchorId.value = project.id;
  }
  draggedProjectIds.value = [...selectedProjectIds.value];
  dragTargetFolder.value = "";
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", draggedProjectIds.value.join(","));
  }
}

function endProjectDrag() {
  draggedProjectIds.value = [];
  dragTargetFolder.value = "";
}

function allowFolderDrop(folder: string, event: DragEvent) {
  if (!draggedProjectIds.value.length) return;
  event.preventDefault();
  dragTargetFolder.value = folder;
  if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
}

async function dropProjectIntoFolder(folder: string, event: DragEvent) {
  event.preventDefault();
  const ids = [...draggedProjectIds.value];
  dragTargetFolder.value = "";
  draggedProjectIds.value = [];
  if (!ids.length) return;
  await moveProjects(ids, folder);
}

function openFolderContextMenu(folder: string, event: MouseEvent) {
  event.preventDefault();
  const menuWidth = 170;
  const menuHeight = 54;
  folderContextMenu.value = {
    folder,
    x: Math.max(8, Math.min(event.clientX, window.innerWidth - menuWidth - 8)),
    y: Math.max(8, Math.min(event.clientY, window.innerHeight - menuHeight - 8)),
  };
}

async function deleteFolder(folder: string) {
  folderContextMenu.value = null;
  const count = app.projects.filter((project) => project.folder === folder).length;
  const accepted = await ui.ask({
    title: "删除项目文件夹",
    message: count
      ? `删除“${folder}”？其中 ${count} 个项目会移回“未归类”，不会删除任何磁盘文件。`
      : `删除空文件夹“${folder}”？不会删除任何磁盘文件。`,
    confirmLabel: "删除文件夹",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.deleteProjectFolder(folder);
    if (selectedFolder.value === folder) selectedFolder.value = "__unfiled__";
  } catch (error) {
    ui.toast("删除项目文件夹失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function switchProject(project: Project) {
  try {
    await app.activateProject(project.id);
  } catch (error) {
    ui.toast("项目切换失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function openPathEdit(project: Project) {
  pathProject.value = project;
  pathDraft.value = project.path;
}

async function browseEditedProjectPath() {
  const project = pathProject.value;
  if (!project) return;
  if (app.webControlClient) {
    remotePickerTarget.value = "project-path";
    remotePickerInitialPath.value = pathDraft.value || project.path;
    remotePickerTitle.value = `选择“${project.name}”的新目录`;
    remotePickerOpen.value = true;
    return;
  }
  try {
    const result = await app.pickFolder(pathDraft.value || project.path, `选择“${project.name}”的新目录`);
    if (!result.canceled && result.path) pathDraft.value = result.path;
  } catch (error) {
    ui.toast("无法打开文件夹选择器", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveProjectPath() {
  const project = pathProject.value;
  const path = pathDraft.value.trim();
  if (!project || !path) return;
  try {
    await app.updateProjectPath(project.id, path);
    pathProject.value = null;
  } catch (error) {
    ui.toast("修改项目路径失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function openProjectPrompt(project: Project) {
  promptProject.value = project;
  promptDraft.value = project.prompt || "";
}

async function saveProjectPrompt() {
  const project = promptProject.value;
  if (!project) return;
  try {
    await app.updateProjectPrompt(project.id, promptDraft.value);
    promptProject.value = null;
  } catch (error) {
    ui.toast("保存 AGENTS.md 失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function openRuntimeConfig(project: Project) {
  const instance = instanceForProject(project);
  runtimeProject.value = project;
  runtimeForm.name = instance?.name || `${project.name} MCP`;
  runtimeForm.workspace = project.path;
  runtimeForm.mcpPort = instance?.mcpPort || 0;
  runtimeForm.coreMode = instance?.coreMode || (app.config?.coreMode === "legacy" ? "legacy" : "go");
  runtimeForm.permissionMode = instance?.permissionMode || "trusted";
  runtimeForm.toolProfile = instance?.toolProfile || "full";
  runtimeForm.allowNetwork = instance?.allowNetwork ?? true;
  runtimeForm.autoStart = instance?.autoStart ?? false;
  runtimeForm.watchdog = instance?.watchdog ?? true;
  runtimeForm.loggingEnabled = instance?.loggingEnabled ?? true;
  runtimeForm.domain = instance?.domain || "";
  runtimeForm.tunnelName = instance?.tunnelName || "";
  runtimeForm.reuseTunnel = Boolean(instance?.tunnelId);
}

async function browseRuntimeWorkspace() {
  const project = runtimeProject.value;
  if (!project) return;
  if (app.webControlClient) {
    remotePickerTarget.value = "runtime";
    remotePickerInitialPath.value = runtimeForm.workspace || project.path;
    remotePickerTitle.value = `选择“${project.name}”的项目目录`;
    remotePickerOpen.value = true;
    return;
  }
  try {
    const result = await app.pickFolder(runtimeForm.workspace || project.path, `选择“${project.name}”的项目目录`);
    if (!result.canceled && result.path) runtimeForm.workspace = result.path;
  } catch (error) {
    ui.toast("无法打开文件夹选择器", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveRuntimeConfig() {
  const project = runtimeProject.value;
  if (!project) return;
  let targetProject = project;
  let existing = instanceForProject(project);
  try {
    const desiredWorkspace = runtimeForm.workspace.trim();
    if (!desiredWorkspace) throw new Error("请选择项目目录。");
    if (normalizePath(desiredWorkspace) !== normalizePath(project.path)) {
      targetProject = await app.updateProjectPath(project.id, desiredWorkspace);
      runtimeProject.value = targetProject;
      runtimeForm.workspace = targetProject.path;
      if (existing) existing = app.instances.find((instance) => instance.id === existing?.id) || existing;
    }
    let savedInstance: MCPInstance;
    if (existing) {
      savedInstance = await app.updateInstance(existing.id, {
        name: runtimeForm.name.trim(),
        projectId: targetProject.id,
        workspace: targetProject.path,
        mcpPort: runtimeForm.mcpPort,
        coreMode: runtimeForm.coreMode,
        permissionMode: runtimeForm.permissionMode,
        toolProfile: runtimeForm.toolProfile,
        allowNetwork: runtimeForm.allowNetwork,
        autoStart: runtimeForm.autoStart,
        watchdog: runtimeForm.watchdog,
        loggingEnabled: runtimeForm.loggingEnabled,
      });
    } else {
      savedInstance = await app.createInstance({
        name: runtimeForm.name.trim(),
        projectId: targetProject.id,
        workspace: targetProject.path,
        mcpPort: runtimeForm.mcpPort > 0 ? runtimeForm.mcpPort : undefined,
        coreMode: runtimeForm.coreMode,
        permissionMode: runtimeForm.permissionMode,
        toolProfile: runtimeForm.toolProfile,
        allowNetwork: runtimeForm.allowNetwork,
        autoStart: runtimeForm.autoStart,
        watchdog: runtimeForm.watchdog,
        loggingEnabled: runtimeForm.loggingEnabled,
      });
    }
    const domain = runtimeForm.domain.trim().toLocaleLowerCase();
    const tunnelName = runtimeForm.tunnelName.trim();
    if (domain) {
      const tunnelNeedsUpdate = !savedInstance.tunnelId
        || domain !== (savedInstance.domain || "").toLocaleLowerCase()
        || (tunnelName && tunnelName !== (savedInstance.tunnelName || ""));
      if (tunnelNeedsUpdate) {
        await app.configureInstanceTunnel(savedInstance.id, {
          domain,
          tunnelName,
          reuse: runtimeForm.reuseTunnel,
        });
      }
    } else if (savedInstance.domain) {
      await app.updateInstance(savedInstance.id, { domain: "" });
    }
    runtimeProject.value = null;
  } catch (error) {
    ui.toast("保存项目 MCP 配置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function repairProjectTunnelDNS(project: Project | null) {
  if (!project) return;
  const instance = instanceForProject(project);
  if (!instance?.domain || !instance.tunnelId) {
    openRuntimeConfig(project);
    ui.toast("暂时无法修复 DNS", "请先保存公网域名并成功创建或复用 Tunnel。", "info");
    return;
  }
  try {
    await app.repairInstanceTunnelDNS(instance.id);
  } catch (error) {
    ui.toast("修复 DNS 失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function runProject(project: Project, action: "start" | "stop" | "restart") {
  const instance = instanceForProject(project);
  if (!instance) {
    openRuntimeConfig(project);
    ui.toast("请先配置项目 MCP", "保存配置后即可独立启动这个项目。", "info");
    return;
  }
  try {
    await app.instanceAction(instance.id, action);
  } catch (error) {
    ui.toast("项目 MCP 操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function removeProject(project: Project) {
  const active = isActiveProject(project);
  const linkedInstance = instanceForProject(project);
  const accepted = await ui.ask({
    title: "移除项目",
    message: active
      ? `移除当前项目“${project.name}”？程序会先自动切换到下一个项目${linkedInstance ? "，并停止和移除它的独立 MCP 配置" : ""}。不会删除磁盘中的文件。`
      : `从项目库移除“${project.name}”？${linkedInstance ? "对应的独立 MCP 配置会同时停止并移除；" : ""}不会删除磁盘中的文件。`,
    confirmLabel: "移除",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.removeProject(project.id);
  } catch (error) {
    ui.toast("移除项目失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

function applyRemoteFolder(path: string) {
  if (remotePickerTarget.value === "add") addProjectPath.value = path;
  else if (remotePickerTarget.value === "project-path") pathDraft.value = path;
  else runtimeForm.workspace = path;
  remotePickerOpen.value = false;
}

async function runPrimary(action: "start" | "stop" | "restart") {
  try {
    await app.serviceAction(action);
  } catch (error) {
    ui.toast("运行操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveServiceSettings() {
  try {
    const oldPort = app.config?.mcpPort;
    const oldCoreMode = app.config?.coreMode;
    const requestedPort = Number(serviceForm.mcpPort);
    if (!Number.isInteger(requestedPort) || requestedPort < 1024 || requestedPort > 65535) {
      throw new Error("MCP 端口必须是 1024 到 65535 之间的整数。");
    }
    const coreChanged = Boolean(oldCoreMode && oldCoreMode !== serviceForm.coreMode);
    if (coreChanged && (app.status?.mcp.running || app.status?.tunnel.running)) {
      throw new Error("切换主 MCP 核心前请先停止主服务。");
    }
    let confirmCoreSwitch = false;
    if (coreChanged) {
      const accepted = await ui.ask({
        title: "切换主 MCP 核心",
        message: "切换核心可能让已连接的客户端重新授权。确定继续保存吗？",
        confirmLabel: "切换核心",
        danger: Boolean(app.config?.domain),
      });
      if (!accepted) {
        serviceForm.coreMode = oldCoreMode || serviceForm.coreMode;
        return;
      }
      confirmCoreSwitch = Boolean(app.config?.domain);
    }
    await app.saveConfig({
      coreMode: serviceForm.coreMode,
      confirmCoreSwitch,
      toolProfile: serviceForm.toolProfile,
      autoStart: serviceForm.autoStart,
      watchdog: serviceForm.watchdog,
    });
    if (oldPort && oldPort !== requestedPort) await app.changePort(requestedPort);
  } catch (error) {
    ui.toast("保存运行设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

watch(() => app.config, (config) => {
  if (!config) return;
  serviceForm.mcpPort = config.mcpPort;
  serviceForm.coreMode = config.coreMode;
  serviceForm.toolProfile = config.toolProfile;
  serviceForm.autoStart = config.autoStart;
  serviceForm.watchdog = config.watchdog;
}, { immediate: true });

onMounted(async () => {
  try {
    await Promise.all([app.loadProjects(), app.loadProjectFolders(), app.loadInstances()]);
  } catch (error) {
    ui.toast("读取项目与运行数据失败", error instanceof Error ? error.message : String(error), "danger");
  }
});
</script>

<template>
  <div class="page-stack workspace-hub-page">
    <PageHeader eyebrow="Projects & runtimes" title="项目与运行" description="项目库、独立 MCP 运行实例和基础服务设置集中在一个工作区中。">
      <template #actions>
        <AppButton tone="secondary" icon="folder" @click="showAddFolder = !showAddFolder">新建文件夹</AppButton>
        <AppButton tone="primary" icon="projects" @click="showAddProject = !showAddProject">添加项目</AppButton>
      </template>
    </PageHeader>

    <AppCard v-if="showAddFolder" class="workspace-quick-form">
      <div class="workspace-quick-form-copy"><strong>新建项目文件夹</strong><span>这是 DevDesk 内的分类，不会创建或移动 Windows 磁盘目录。</span></div>
      <input v-model="newFolderName" placeholder="例如：客户项目 / 内部工具" @keyup.enter="createFolder" />
      <AppButton tone="primary" :disabled="!newFolderName.trim()" :loading="app.actionPending === 'add-project-folder'" @click="createFolder">创建</AppButton>
    </AppCard>

    <AppCard v-if="showAddProject" class="workspace-add-project-card">
      <div class="card-heading"><div><span class="eyebrow">Add project</span><h3>添加项目</h3></div><AppButton tone="quiet" @click="showAddProject = false">取消</AppButton></div>
      <div class="field-grid">
        <label class="field"><span>显示名称</span><input v-model="addProjectName" placeholder="默认使用目录名称" /></label>
        <label class="field"><span>归类文件夹</span><select v-model="addProjectFolder"><option value="">未归类</option><option v-for="folder in app.projectFolders" :key="folder" :value="folder">{{ folder }}</option></select></label>
        <label class="field span-2"><span>项目目录</span><div class="path-picker-row"><input v-model="addProjectPath" placeholder="D:\Projects\my-app" /><AppButton tone="secondary" icon="folder" @click="browseProjectPath">浏览</AppButton></div></label>
      </div>
      <div class="form-footer"><small>只保存项目引用，不移动磁盘文件。</small><AppButton tone="primary" :disabled="!addProjectPath.trim()" :loading="app.actionPending === 'add-project'" @click="addProject">添加项目</AppButton></div>
    </AppCard>

    <AppCard class="workspace-runtime-summary">
      <div class="card-heading">
        <div><span class="eyebrow">Runtime</span><h3>运行控制</h3><p>主工作区与各项目独立实例可以同时运行。</p></div>
        <StatusPill :tone="app.status?.mcp.running ? 'success' : 'neutral'">{{ app.status?.mcp.running ? '主服务运行中' : '主服务已停止' }}</StatusPill>
      </div>
      <div class="workspace-runtime-facts">
        <div><span>当前工作目录</span><code>{{ app.config?.workspace || '--' }}</code></div>
        <div><span>本地 MCP</span><code>{{ app.status?.localMcpUrl || '--' }}</code></div>
        <div><span>独立项目实例</span><strong>{{ runningProjectInstances }} 个运行中</strong></div>
      </div>
      <div class="workspace-runtime-actions">
        <AppButton v-if="!app.status?.mcp.running" tone="primary" icon="play" @click="runPrimary('start')">启动主服务</AppButton>
        <template v-else><AppButton tone="secondary" icon="restart" @click="runPrimary('restart')">重启主服务</AppButton><AppButton tone="danger" icon="stop" @click="runPrimary('stop')">停止主服务</AppButton></template>
      </div>
    </AppCard>

    <section class="workspace-explorer" :class="{ 'is-folder-pane-collapsed': folderPaneCollapsed }">
      <aside class="workspace-folder-pane">
        <button class="workspace-folder-collapse" type="button" :title="folderPaneCollapsed ? '展开项目文件夹' : '收起项目文件夹'" @click="toggleFolderPane">
          <AppIcon name="chevron-right" :size="15" />
          <span>{{ folderPaneCollapsed ? '展开分类' : '收起分类' }}</span>
        </button>
        <div class="workspace-search"><AppIcon name="search" :size="16" /><input v-model="search" placeholder="搜索项目或文件夹" /></div>
        <button type="button" :class="{ 'is-active': selectedFolder === '__all__' }" @click="selectedFolder = '__all__'"><AppIcon name="projects" :size="16" /><span>全部项目</span><em>{{ app.projects.length }}</em></button>
        <button
          type="button"
          :class="{ 'is-active': selectedFolder === '__unfiled__', 'is-drop-target': dragTargetFolder === '__unfiled__' }"
          @click="selectedFolder = '__unfiled__'"
          @dragover="allowFolderDrop('__unfiled__', $event)"
          @dragleave="dragTargetFolder = ''"
          @drop="dropProjectIntoFolder('', $event)"
        ><AppIcon name="folder" :size="16" /><span>未归类</span><em>{{ app.projects.filter((item) => !item.folder).length }}</em></button>
        <div class="workspace-folder-divider"><span>文件夹</span></div>
        <button
          v-for="folder in visibleFolders"
          :key="folder"
          type="button"
          :class="{ 'is-active': selectedFolder === folder, 'is-drop-target': dragTargetFolder === folder }"
          @click="selectedFolder = folder"
          @contextmenu="openFolderContextMenu(folder, $event)"
          @dragover="allowFolderDrop(folder, $event)"
          @dragleave="dragTargetFolder = ''"
          @drop="dropProjectIntoFolder(folder, $event)"
        >
          <AppIcon name="folder" :size="16" /><span>{{ folder }}</span><em>{{ app.projects.filter((item) => item.folder === folder).length }}</em>
        </button>
      </aside>

      <div class="workspace-project-pane">
        <div class="workspace-project-header">
          <span>名称</span><span>MCP</span><span>操作</span>
        </div>
        <article v-for="project in filteredProjects" :key="project.id" class="workspace-project-row" :class="{ 'is-active': isActiveProject(project), 'is-selected': selectedProjectIdSet.has(project.id) }">
          <div class="workspace-project-name" draggable="true" title="点击选择；Shift 连选；拖到左侧文件夹可批量归类" @click="selectProject(project, $event)" @dragstart="beginProjectDrag(project, $event)" @dragend="endProjectDrag">
            <span class="workspace-row-icon"><AppIcon name="folder" :size="17" /></span>
            <div><strong>{{ project.name }}</strong><small>{{ project.folder || '未归类' }}</small></div>
          </div>
          <div class="workspace-runtime-state"><StatusPill :tone="runtimeStatus(project).tone">{{ runtimeStatus(project).label }}</StatusPill></div>
          <div class="workspace-project-actions">
            <AppButton tone="quiet" compact icon="folder" @click="openPathEdit(project)">路径</AppButton>
            <AppButton tone="quiet" compact icon="settings" @click="openProjectPrompt(project)">AGENTS.md</AppButton>
            <AppButton tone="secondary" compact icon="settings" @click="openRuntimeConfig(project)">配置</AppButton>
            <AppButton v-if="instanceForProject(project)?.domain && instanceForProject(project)?.tunnelId" tone="quiet" compact icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instanceForProject(project)?.id}`" @click="repairProjectTunnelDNS(project)">修复DNS</AppButton>
            <AppButton v-if="!instanceForProject(project)?.mcp.running" tone="primary" compact icon="play" @click="runProject(project, 'start')">启动</AppButton>
            <AppButton v-else tone="secondary" compact icon="restart" @click="runProject(project, 'restart')">重启</AppButton>
            <AppButton v-if="instanceForProject(project)?.mcp.running" tone="quiet" compact icon="stop" @click="runProject(project, 'stop')">停止</AppButton>
            <AppButton v-if="!isActiveProject(project)" tone="quiet" compact icon="restart" @click="switchProject(project)">切换</AppButton>
            <AppButton tone="quiet" compact @click="removeProject(project)">移除</AppButton>
          </div>
          <select class="workspace-folder-mobile-select" :value="project.folder || ''" aria-label="项目归类" @change="handleMobileProjectFolderChange(project, $event)">
            <option value="">未归类</option>
            <option v-for="folder in app.projectFolders" :key="folder" :value="folder">{{ folder }}</option>
          </select>
        </article>
        <div v-if="!filteredProjects.length" class="workspace-project-empty">没有匹配的项目。</div>
      </div>
    </section>

    <Teleport to="body">
      <div v-if="folderContextMenu" class="workspace-folder-context-backdrop" @pointerdown.self="folderContextMenu = null" @contextmenu.prevent="folderContextMenu = null">
        <div class="workspace-folder-context-menu" :style="{ left: `${folderContextMenu.x}px`, top: `${folderContextMenu.y}px` }">
          <button type="button" class="is-danger" @click="deleteFolder(folderContextMenu.folder)">
            <AppIcon name="trash" :size="16" />
            <span>删除文件夹</span>
          </button>
        </div>
      </div>
    </Teleport>

    <section class="workspace-service-section">
      <form class="workspace-service-settings" @submit.prevent="saveServiceSettings">
        <AppCard>
          <div class="card-heading"><div><span class="eyebrow">Basic settings</span><h3>基础设置</h3></div></div>
          <div class="field-grid">
            <label class="field span-2"><span>工作目录</span><input :value="app.config?.workspace || ''" readonly /></label>
            <label class="field"><span>MCP 端口</span><input v-model.number="serviceForm.mcpPort" type="number" min="1024" max="65535" /></label>
            <label class="field"><span>MCP 核心</span><select v-model="serviceForm.coreMode" :disabled="Boolean(app.status?.mcp.running || app.status?.tunnel.running)"><option value="go">Go 核心</option><option value="legacy">Python 兼容核心</option></select></label>
            <label class="field span-2"><span>工具配置</span><select v-model="serviceForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>
          </div>
        </AppCard>
        <AppCard>
          <div class="card-heading"><div><span class="eyebrow">Behavior</span><h3>运行方式</h3></div></div>
          <div class="toggle-list"><ToggleSwitch v-model="serviceForm.autoStart" label="自动启动服务" /><ToggleSwitch v-model="serviceForm.watchdog" label="运行守护" /></div>
          <div class="settings-card-footer"><AppButton type="submit" tone="primary">保存运行设置</AppButton></div>
        </AppCard>
      </form>

      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Paths</span><h3>程序路径</h3></div></div>
        <div class="path-list"><div><span>当前核心</span><code>{{ app.config?.coreMode === 'go' ? app.config?.goCoreExecutable : app.config?.coreExecutable || '--' }}</code></div><div><span>Cloudflare 客户端</span><code>{{ app.config?.cloudflaredExecutable || '--' }}</code></div></div>
      </AppCard>
    </section>

    <div v-if="pathProject" class="workspace-runtime-backdrop" @click.self="pathProject = null">
      <AppCard class="workspace-runtime-dialog workspace-small-dialog">
        <div class="card-heading"><div><span class="eyebrow">Project path</span><h3>{{ pathProject.name }} · 修改路径</h3><p>只修改 DevDesk 引用；不会移动或复制磁盘文件。</p></div><AppButton tone="quiet" @click="pathProject = null">关闭</AppButton></div>
        <label class="field"><span>项目目录</span><div class="path-picker-row"><input v-model="pathDraft" spellcheck="false" /><AppButton tone="secondary" icon="folder" @click="browseEditedProjectPath">浏览</AppButton></div></label>
        <div class="form-footer"><small>当前活动项目修改后会同步切换主 MCP 工作目录。</small><AppButton tone="primary" :disabled="!pathDraft.trim()" @click="saveProjectPath">保存路径</AppButton></div>
      </AppCard>
    </div>

    <div v-if="promptProject" class="workspace-runtime-backdrop" @click.self="promptProject = null">
      <AppCard class="workspace-runtime-dialog">
        <div class="card-heading"><div><span class="eyebrow">Project instructions</span><h3>{{ promptProject.name }} · AGENTS.md</h3><p>直接编辑项目根目录的 Agent 指令文件。</p></div><AppButton tone="quiet" @click="promptProject = null">关闭</AppButton></div>
        <label class="field"><span>AGENTS.md 内容</span><textarea v-model="promptDraft" rows="14" spellcheck="false" placeholder="# 项目说明&#10;&#10;## 开发规则&#10;- ..." /><small>{{ promptBytes }} / {{ app.projectPromptSettings?.maxPromptBytes || 32768 }} bytes</small></label>
        <div class="form-footer"><small>留空保存会删除项目根目录的 AGENTS.md。</small><AppButton tone="primary" @click="saveProjectPrompt">保存 AGENTS.md</AppButton></div>
      </AppCard>
    </div>

    <div v-if="runtimeProject" class="workspace-runtime-backdrop" @click.self="runtimeProject = null">
      <AppCard class="workspace-runtime-dialog">
        <div class="card-heading"><div><span class="eyebrow">Project runtime</span><h3>{{ runtimeProject.name }} · MCP 配置</h3><p>每个项目使用独立端口和进程，因此可与其他项目同时运行。</p></div><AppButton tone="quiet" @click="runtimeProject = null">关闭</AppButton></div>
        <div class="field-grid">
          <label class="field"><span>实例名称</span><input v-model="runtimeForm.name" /></label>
          <label class="field"><span>MCP 端口</span><input v-model.number="runtimeForm.mcpPort" type="number" min="0" max="65535" /><small>新配置填写 0 自动分配。</small></label>
          <label class="field span-2"><span>项目目录</span><div class="path-picker-row"><input v-model="runtimeForm.workspace" spellcheck="false" /><AppButton tone="secondary" icon="folder" @click="browseRuntimeWorkspace">浏览</AppButton></div><small>这里修改目录会同步更新项目本身的路径引用。</small></label>
          <label class="field"><span>核心</span><select v-model="runtimeForm.coreMode"><option value="go">Go 核心</option><option value="legacy">Python 兼容核心</option></select></label>
          <label class="field"><span>权限</span><select v-model="runtimeForm.permissionMode"><option value="safe">安全</option><option value="trusted">受信任</option><option value="dangerous">高权限</option></select></label>
          <label class="field span-2"><span>工具配置</span><select v-model="runtimeForm.toolProfile"><option value="full">完整工具</option><option value="read-only">只读</option><option value="compat-readonly-all">兼容只读</option></select></label>
          <div class="workspace-tunnel-config span-2">
            <div class="workspace-tunnel-heading">
              <div><strong>Cloudflare 穿透</strong><small>可选。填写域名后，保存配置时会为该项目创建或复用独立 Tunnel。</small></div>
              <StatusPill v-if="instanceForProject(runtimeProject)?.remoteMcpUrl" tone="success">已配置</StatusPill>
            </div>
            <div class="field-grid">
              <label class="field"><span>公网域名</span><input v-model="runtimeForm.domain" placeholder="例如 mcp-project.example.com" spellcheck="false" /></label>
              <label class="field"><span>Tunnel 名称</span><input v-model="runtimeForm.tunnelName" placeholder="留空自动生成" spellcheck="false" /></label>
            </div>
            <ToggleSwitch v-model="runtimeForm.reuseTunnel" label="复用已有 Tunnel" />
            <small v-if="instanceForProject(runtimeProject)?.remoteMcpUrl" class="workspace-tunnel-url">{{ instanceForProject(runtimeProject)?.remoteMcpUrl }}</small>
            <div v-if="instanceForProject(runtimeProject)?.domain && instanceForProject(runtimeProject)?.tunnelId" class="form-footer">
              <small>Tunnel 正常但域名没有 DNS 时，可重新绑定当前 Tunnel UUID 并验证公网解析。</small>
              <AppButton tone="secondary" icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instanceForProject(runtimeProject)?.id}`" @click="repairProjectTunnelDNS(runtimeProject)">修复 DNS</AppButton>
            </div>
          </div>
        </div>
        <div class="instance-toggle-grid"><ToggleSwitch v-model="runtimeForm.autoStart" label="自动启动" /><ToggleSwitch v-model="runtimeForm.watchdog" label="运行守护" /><ToggleSwitch v-model="runtimeForm.loggingEnabled" label="记录日志" /><ToggleSwitch v-model="runtimeForm.allowNetwork" label="允许网络" /></div>
        <div class="form-footer"><small>填写 Cloudflare 域名时会同时配置穿透；项目已运行时 Tunnel 会立即启动。</small><AppButton tone="primary" @click="saveRuntimeConfig">保存配置</AppButton></div>
      </AppCard>
    </div>

    <RemoteFolderPicker :open="remotePickerOpen" :initial-path="remotePickerInitialPath" :title="remotePickerTitle" @close="remotePickerOpen = false" @select="applyRemoteFolder" />
  </div>
</template>
