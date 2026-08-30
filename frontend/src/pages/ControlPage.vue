<script setup lang="ts">
import { computed, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();
const selectedProjectId = ref("");
const projectPromptDraft = ref("");
const globalPromptEnabled = ref(false);
const globalPromptDraft = ref("");

const normalizePath = (value = "") => value.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLocaleLowerCase();
const activeProject = computed(() => app.projects.find((project) => normalizePath(project.path) === normalizePath(app.config?.workspace)));
const selectedProject = computed(() => app.projects.find((project) => project.id === selectedProjectId.value));
const mcpRunning = computed(() => Boolean(app.status?.mcp.running));

watch([() => app.projects, () => app.config?.workspace], () => {
  if (!selectedProjectId.value || !app.projects.some((project) => project.id === selectedProjectId.value)) {
    selectedProjectId.value = activeProject.value?.id || app.projects[0]?.id || "";
  }
}, { immediate: true, deep: true });

watch(selectedProject, (project) => {
  projectPromptDraft.value = project?.prompt || "";
}, { immediate: true });

watch(() => app.projectPromptSettings, (settings) => {
  globalPromptEnabled.value = Boolean(settings?.enabled);
  globalPromptDraft.value = settings?.globalPrompt || "";
}, { immediate: true });

async function refreshPage() {
  try {
    await Promise.all([
      app.refreshStatus(true),
      app.loadConfig(),
      app.loadProjects(),
      app.loadProjectPromptSettings(),
    ]);
  } catch (error) {
    ui.toast("刷新失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function switchProject() {
  if (!selectedProject.value || selectedProject.value.id === activeProject.value?.id) return;
  try {
    await app.activateProject(selectedProject.value.id);
  } catch (error) {
    ui.toast("切换项目失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveProjectPrompt() {
  if (!selectedProject.value) return;
  try {
    await app.updateProjectPrompt(selectedProject.value.id, projectPromptDraft.value);
  } catch (error) {
    ui.toast("保存 AGENTS.md 失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveGlobalPrompt() {
  try {
    await app.saveGlobalProjectPrompt(globalPromptEnabled.value, globalPromptDraft.value);
  } catch (error) {
    ui.toast("保存全局提示词失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function runServiceAction(action: "start" | "stop" | "restart") {
  try {
    await app.serviceAction(action);
  } catch (error) {
    ui.toast("服务操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <main class="web-control-page">
    <header class="web-control-hero">
      <div class="web-control-brand">
        <span class="web-control-logo"><AppIcon name="network" :size="24" /></span>
        <div>
          <span class="eyebrow">MCP DevDesk Web Control</span>
          <h1>网页控制台</h1>
          <p>项目切换、提示词和 MCP 服务控制。</p>
        </div>
      </div>
      <div class="web-control-hero-actions">
        <StatusPill :tone="mcpRunning ? 'success' : 'neutral'">{{ mcpRunning ? 'MCP 运行中' : 'MCP 已停止' }}</StatusPill>
        <AppButton tone="secondary" icon="refresh" :loading="app.refreshing" @click="refreshPage">刷新</AppButton>
      </div>
    </header>

    <section class="web-control-summary">
      <div><span>当前项目</span><strong>{{ activeProject?.name || '未匹配项目' }}</strong></div>
      <div><span>工作目录</span><code>{{ app.config?.workspace || '--' }}</code></div>
      <div><span>MCP 地址</span><code>{{ app.status?.remoteMcpUrl || app.status?.localMcpUrl || '--' }}</code></div>
    </section>

    <section class="web-control-grid">
      <AppCard class="web-control-card web-control-project-card">
        <div class="card-heading">
          <div><span class="eyebrow">Workspace</span><h3>切换项目目录</h3><p>从 DevDesk 已保存的项目中选择工作目录。</p></div>
          <StatusPill v-if="selectedProject?.id === activeProject?.id" tone="success">当前活动</StatusPill>
        </div>
        <label class="field">
          <span>项目</span>
          <select v-model="selectedProjectId">
            <option v-for="project in app.projects" :key="project.id" :value="project.id">{{ project.name }}</option>
          </select>
        </label>
        <div class="web-control-path-box">
          <span>目录</span>
          <code>{{ selectedProject?.path || '--' }}</code>
        </div>
        <div class="form-footer">
          <small>切换后 MCP 会使用新的 workspace，公网 Tunnel 地址保持不变。</small>
          <AppButton
            tone="primary"
            icon="restart"
            :disabled="!selectedProject || selectedProject.id === activeProject?.id"
            :loading="app.actionPending === `activate-${selectedProjectId}`"
            @click="switchProject"
          >切换到此项目</AppButton>
        </div>
      </AppCard>

      <AppCard class="web-control-card">
        <div class="card-heading">
          <div><span class="eyebrow">Service</span><h3>MCP 服务</h3><p>快速启动、停止或重新启动当前主实例。</p></div>
          <StatusPill :tone="mcpRunning ? 'success' : 'neutral'">{{ mcpRunning ? 'Running' : 'Stopped' }}</StatusPill>
        </div>
        <div class="web-control-service-actions">
          <AppButton tone="primary" icon="play" :disabled="mcpRunning" :loading="app.actionPending === 'start'" @click="runServiceAction('start')">启动</AppButton>
          <AppButton tone="secondary" :disabled="!mcpRunning" :loading="app.actionPending === 'stop'" @click="runServiceAction('stop')">停止</AppButton>
          <AppButton tone="secondary" icon="restart" :loading="app.actionPending === 'restart'" @click="runServiceAction('restart')">重启</AppButton>
        </div>
        <div class="detail-list compact top-divider">
          <div><span>核心</span><strong>{{ app.config?.coreMode === 'go' ? 'Go Core' : 'Legacy' }}</strong></div>
          <div><span>端口</span><strong>{{ app.config?.mcpPort || '--' }}</strong></div>
          <div><span>Tunnel</span><strong>{{ app.status?.tunnel.running ? '运行中' : '已停止' }}</strong></div>
        </div>
      </AppCard>
    </section>

    <section class="web-control-grid web-control-prompts-grid">
      <AppCard class="web-control-card">
        <div class="card-heading">
          <div>
            <span class="eyebrow">Project instructions</span>
            <h3>项目 AGENTS.md</h3>
            <p>内容直接保存到所选项目根目录，项目复制后仍可复用。</p>
          </div>
          <StatusPill :tone="projectPromptDraft.trim() ? 'info' : 'neutral'">{{ projectPromptDraft.trim() ? '已设置' : '未设置' }}</StatusPill>
        </div>
        <label class="field project-prompt-field">
          <span>{{ selectedProject?.path || '请选择项目' }}\AGENTS.md</span>
          <textarea v-model="projectPromptDraft" rows="12" spellcheck="false" :disabled="!selectedProject" placeholder="# 项目 Agent 指令&#10;&#10;## 开发规则&#10;- ...&#10;&#10;## 验证要求&#10;- ..." />
        </label>
        <div class="form-footer">
          <small>留空保存会删除该项目根目录的 AGENTS.md。</small>
          <AppButton tone="primary" :disabled="!selectedProject" :loading="app.actionPending === `prompt-${selectedProjectId}`" @click="saveProjectPrompt">保存 AGENTS.md</AppButton>
        </div>
      </AppCard>

      <AppCard class="web-control-card">
        <div class="card-heading">
          <div>
            <span class="eyebrow">Global instructions</span>
            <h3>全局提示词</h3>
            <p>只存于 DevDesk 设置；开关关闭时内容保留但不会注入模型。</p>
          </div>
          <StatusPill :tone="globalPromptEnabled && globalPromptDraft.trim() ? 'success' : 'neutral'">
            {{ globalPromptEnabled && globalPromptDraft.trim() ? '已生效' : '未生效' }}
          </StatusPill>
        </div>
        <div class="toggle-list">
          <ToggleSwitch v-model="globalPromptEnabled" label="启用全局提示词" description="项目自己的 AGENTS.md 仍然保持独立。" />
        </div>
        <label class="field project-prompt-field top-divider">
          <span>全局提示词内容</span>
          <textarea v-model="globalPromptDraft" rows="10" spellcheck="false" placeholder="# 全局 Agent 规则&#10;&#10;只写所有项目共同遵守的规则。" />
        </label>
        <div class="form-footer">
          <small>实际生效条件：开关开启且内容非空。</small>
          <AppButton tone="primary" :loading="app.actionPending === 'save-global-project-prompt'" @click="saveGlobalPrompt">保存全局提示词</AppButton>
        </div>
      </AppCard>
    </section>
  </main>
</template>
