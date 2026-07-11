<script setup lang="ts">
import { computed, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();
const projectName = computed(() => app.config?.workspace?.split(/[\\/]/).filter(Boolean).at(-1) ?? "当前项目");
const projectPath = ref("");
const projectLabel = ref("");
const showAdd = ref(false);
const selectedId = ref("");
const worktreePath = ref("");
const worktreeBranch = ref("");
const normalizePath = (value = "") => value.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLocaleLowerCase();
const isActive = (path: string) => normalizePath(path) === normalizePath(app.config?.workspace);
const currentProject = computed(() => app.projects.find((project) => isActive(project.path)));
const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error);

async function addProject() {
  const path = projectPath.value.trim();
  if (!path) return;
  try {
    await app.addProject(path, projectLabel.value.trim());
    projectPath.value = "";
    projectLabel.value = "";
    showAdd.value = false;
  } catch (error) {
    ui.toast("添加项目失败", errorMessage(error), "danger");
  }
}

async function inspect(id: string) {
  try {
    await app.inspectProject(id);
    selectedId.value = id;
  } catch (error) {
    ui.toast("读取项目详情失败", errorMessage(error), "danger");
  }
}

async function switchProject(id: string) {
  try {
    await app.activateProject(id);
  } catch (error) {
    ui.toast("项目切换失败", errorMessage(error), "danger");
  }
}

async function removeProject(id: string, name: string) {
  const accepted = await ui.ask({
    title: "移除项目",
    message: `确定从列表移除“${name}”吗？磁盘中的项目文件不会被删除。`,
    confirmLabel: "移除",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.removeProject(id);
    if (selectedId.value === id) selectedId.value = "";
  } catch (error) {
    ui.toast("移除项目失败", errorMessage(error), "danger");
  }
}

async function createWorktree() {
  if (!selectedId.value || !worktreePath.value.trim() || !worktreeBranch.value.trim()) return;
  try {
    await app.createWorktree(selectedId.value, worktreePath.value.trim(), worktreeBranch.value.trim());
    worktreePath.value = "";
    worktreeBranch.value = "";
    ui.toast("Worktree 已创建", "新的并行工作区已经准备完成。", "success");
  } catch (error) {
    ui.toast("创建 Worktree 失败", errorMessage(error), "danger");
  }
}

async function removeWorktree(path: string) {
  if (!selectedId.value) return;
  const accepted = await ui.ask({
    title: "移除 Worktree",
    message: `确定移除 ${path} 吗？请先确认其中没有需要保留的未提交修改。`,
    confirmLabel: "移除",
    danger: true,
  });
  if (!accepted) return;
  try {
    await app.removeWorktree(selectedId.value, path);
    ui.toast("Worktree 已移除", path, "success");
  } catch (error) {
    ui.toast("移除 Worktree 失败", errorMessage(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack projects-page">
    <PageHeader eyebrow="Workspaces" title="项目" description="保存多个本地项目，并在不中断公网地址的情况下热切换 MCP 工作目录。">
      <template #actions><AppButton tone="primary" icon="projects" @click="showAdd = !showAdd">添加项目</AppButton></template>
    </PageHeader>

    <AppCard class="current-project-card">
      <div class="current-project-icon"><AppIcon name="folder" :size="30" /></div>
      <div class="current-project-copy">
        <div class="current-project-title"><h2>{{ projectName }}</h2><StatusPill tone="success">当前活动</StatusPill></div>
        <p class="mono">{{ app.config?.workspace || '尚未配置工作目录' }}</p>
        <div class="project-facts">
          <span><AppIcon name="services" :size="14" /> 端口 {{ app.config?.mcpPort || '--' }}</span>
          <span><AppIcon name="shield" :size="14" /> {{ app.status?.permissionMode || 'safe' }}</span>
          <span><AppIcon name="activity" :size="14" /> {{ app.status?.mcp.running ? 'MCP 运行中' : 'MCP 已停止' }}</span>
        </div>
      </div>
      <div class="project-row-actions">
        <AppButton v-if="currentProject" tone="secondary" icon="info" :loading="app.actionPending === `inspect-${currentProject.id}`" @click="inspect(currentProject.id)">详情</AppButton>
        <StatusPill tone="info">支持热切换</StatusPill>
      </div>
    </AppCard>

    <AppCard v-if="showAdd" class="project-add-card">
      <div class="card-heading"><div><span class="eyebrow">Add workspace</span><h3>添加本地项目</h3></div></div>
      <form class="stack-form" @submit.prevent="addProject">
        <label class="field"><span>项目目录</span><input v-model="projectPath" placeholder="D:\Projects\my-app" /></label>
        <label class="field"><span>显示名称（可选）</span><input v-model="projectLabel" placeholder="默认使用文件夹名称" /></label>
        <div class="form-footer"><small>只保存目录引用，不会移动或修改项目文件。</small><AppButton type="submit" tone="primary" :loading="app.actionPending === 'add-project'">添加</AppButton></div>
      </form>
    </AppCard>

    <section class="project-list">
      <AppCard v-for="project in app.projects" :key="project.id" class="project-list-card">
        <div class="current-project-icon"><AppIcon name="folder" :size="24" /></div>
        <div class="current-project-copy">
          <div class="current-project-title"><h3>{{ project.name }}</h3><StatusPill v-if="isActive(project.path)" tone="success">当前活动</StatusPill></div>
          <p class="mono">{{ project.path }}</p>
          <small>最近打开 {{ new Date(project.lastOpenedAt).toLocaleString() }}</small>
        </div>
        <div class="project-row-actions">
          <AppButton tone="secondary" icon="info" :loading="app.actionPending === `inspect-${project.id}`" @click="inspect(project.id)">详情</AppButton>
          <AppButton v-if="!isActive(project.path)" tone="primary" icon="restart" :loading="app.actionPending === `activate-${project.id}`" @click="switchProject(project.id)">切换</AppButton>
          <AppButton v-if="!isActive(project.path)" tone="quiet" :loading="app.actionPending === `remove-${project.id}`" @click="removeProject(project.id, project.name)">移除</AppButton>
        </div>
      </AppCard>
    </section>

    <AppCard v-if="selectedId && app.projectDetails[selectedId]" class="project-inspector-card">
      <div class="card-heading"><div><span class="eyebrow">Developer context</span><h3>项目开发信息</h3></div><AppButton tone="secondary" icon="refresh" :loading="app.actionPending === `inspect-${selectedId}`" @click="inspect(selectedId)">刷新</AppButton></div>
      <div class="project-inspector-grid">
        <div><span>Git</span><strong>{{ app.projectDetails[selectedId].git ? app.projectDetails[selectedId].branch || 'Detached' : '不是 Git 仓库' }}</strong></div>
        <div><span>文件变化</span><strong>{{ app.projectDetails[selectedId].changedFiles }}</strong></div>
        <div><span>AGENTS.md</span><strong>{{ app.projectDetails[selectedId].hasAgents ? '已检测' : '未检测' }}</strong></div>
        <div><span>Skills</span><strong>{{ app.projectDetails[selectedId].skills.length }}</strong></div>
      </div>
      <div v-if="app.projectDetails[selectedId].skills.length" class="skill-chip-list"><span v-for="skill in app.projectDetails[selectedId].skills" :key="skill">{{ skill }}</span></div>
      <div v-if="app.projectDetails[selectedId].git" class="worktree-panel">
        <div class="card-heading"><div><span class="eyebrow">Git worktree</span><h3>并行工作区</h3></div></div>
        <div v-for="tree in app.projectDetails[selectedId].worktrees" :key="tree.path" class="worktree-row"><div><strong>{{ tree.branch || 'Detached' }}</strong><code>{{ tree.path }}</code></div><AppButton v-if="!isActive(tree.path) && normalizePath(tree.path) !== normalizePath(app.projectDetails[selectedId].path)" tone="quiet" :loading="app.actionPending === 'remove-worktree'" @click="removeWorktree(tree.path)">移除</AppButton></div>
        <form class="worktree-form" @submit.prevent="createWorktree"><input v-model="worktreePath" placeholder="Worktree 目录" /><input v-model="worktreeBranch" placeholder="新分支名称" /><AppButton type="submit" tone="primary" :loading="app.actionPending === 'create-worktree'">创建</AppButton></form>
      </div>
      <div v-if="app.projectDetails[selectedId].git" class="diff-panel"><div class="card-heading"><div><span class="eyebrow">Working tree diff</span><h3>未提交修改</h3></div></div><pre>{{ app.projectDiffs[selectedId]?.text || '当前没有未提交的文本差异。' }}</pre><small v-if="app.projectDiffs[selectedId]?.truncated">差异过大，已截断显示。</small></div>
    </AppCard>

    <section class="project-roadmap-grid">
      <AppCard><div class="roadmap-icon is-blue"><AppIcon name="projects" :size="20" /></div><h3>多项目列表</h3><p>项目记录保存在本机数据目录，重启软件后仍然保留。</p></AppCard>
      <AppCard><div class="roadmap-icon is-purple"><AppIcon name="restart" :size="20" /></div><h3>安全热切换</h3><p>自动重启 MCP；启动失败时恢复旧目录和旧服务。</p></AppCard>
      <AppCard><div class="roadmap-icon is-mint"><AppIcon name="network" :size="20" /></div><h3>Tunnel 保持连接</h3><p>端口和域名不变，切换目录时无需重建 Cloudflare Tunnel。</p></AppCard>
    </section>
  </div>
</template>
