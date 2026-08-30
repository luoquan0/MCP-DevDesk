<script setup lang="ts">
import { computed, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { GitCommit, Project } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const projectName = computed(() => app.config?.workspace?.split(/[\\/]/).filter(Boolean).at(-1) ?? "当前项目");
const projectPath = ref("");
const projectLabel = ref("");
const showAdd = ref(false);
const browsingProject = ref(false);
const editingId = ref("");
const editingPath = ref("");
const browsingEditId = ref("");
const promptEditingId = ref("");
const promptDraft = ref("");
const globalPromptDraft = ref("");
const selectedId = ref("");
const worktreePath = ref("");
const worktreeBranch = ref("");
const normalizePath = (value = "") => value.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLocaleLowerCase();
const isActive = (path: string) => normalizePath(path) === normalizePath(app.config?.workspace);
const currentProject = computed(() => app.projects.find((project) => isActive(project.path)));
const errorMessage = (error: unknown) => error instanceof Error ? error.message : String(error);
const completionPromptTemplate = `执行要求：
1. 收到用户任务后，先完整理解并持续执行，直到当前任务全部完成，或遇到必须由用户提供信息、授权或外部条件才能继续的真实阻塞点。
2. 不要把同一个任务拆成“下一阶段再做”并提前向用户汇报；只要当前环境允许继续，就直接继续执行，不要停下来询问是否继续。
3. 在所有可执行步骤完成前，不要发送仅表示进度、计划、“接下来我会……”或“进入下一阶段”的最终回复。
4. 任务需要读取项目、修改代码、调用工具、测试、构建、打包或验证时，应连续完成这些步骤后再与用户对话。
5. 最终回复只总结已经完成的事项、验证结果和真实存在的阻塞项。没有阻塞时，不要把本轮可以完成的工作留到下一轮。
6. 用户明确要求只分析、只评估或不要修改时，以用户当前指令为准。`;
const globalPromptBytes = computed(() => new TextEncoder().encode(globalPromptDraft.value).length);
const projectPromptBytes = computed(() => new TextEncoder().encode(promptDraft.value).length);

watch(() => app.projectPromptSettings?.globalPrompt, (value) => {
  globalPromptDraft.value = value || "";
}, { immediate: true });

async function saveGlobalPrompt() {
  try {
    await app.saveGlobalProjectPrompt(globalPromptDraft.value);
  } catch (error) {
    ui.toast("保存全局项目提示词失败", errorMessage(error), "danger");
  }
}

function useGlobalCompletionTemplate() {
  globalPromptDraft.value = completionPromptTemplate;
}

function startPromptEdit(project: Project) {
  promptEditingId.value = project.id;
  promptDraft.value = project.prompt || "";
  editingId.value = "";
}

function cancelPromptEdit() {
  promptEditingId.value = "";
  promptDraft.value = "";
}

async function saveProjectPrompt(project: Project) {
  try {
    await app.updateProjectPrompt(project.id, promptDraft.value);
    cancelPromptEdit();
  } catch (error) {
    ui.toast("保存项目提示词失败", errorMessage(error), "danger");
  }
}

function useProjectCompletionTemplate() {
  promptDraft.value = completionPromptTemplate;
}

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

async function browseProject() {
  browsingProject.value = true;
  try {
    const result = await app.pickFolder(projectPath.value || app.config?.workspace || "", "选择要添加的本地项目");
    if (!result.canceled && result.path) projectPath.value = result.path;
  } catch (error) {
    ui.toast("无法打开文件夹选择器", errorMessage(error), "danger");
  } finally {
    browsingProject.value = false;
  }
}

function cancelAddProject() {
  projectPath.value = "";
  projectLabel.value = "";
  showAdd.value = false;
}

function startEditProject(project: Project) {
  editingId.value = project.id;
  editingPath.value = project.path;
}

function cancelEditProject() {
  editingId.value = "";
  editingPath.value = "";
}

async function browseEditProject(project: Project) {
  browsingEditId.value = project.id;
  try {
    const result = await app.pickFolder(editingPath.value || project.path, `选择“${project.name}”的新目录`);
    if (!result.canceled && result.path) editingPath.value = result.path;
  } catch (error) {
    ui.toast("无法打开文件夹选择器", errorMessage(error), "danger");
  } finally {
    browsingEditId.value = "";
  }
}

async function saveProjectPath(project: Project) {
  const path = editingPath.value.trim();
  if (!path) {
    ui.toast("项目路径不能为空", "请选择一个存在的本地文件夹。", "danger");
    return;
  }
  if (normalizePath(path) === normalizePath(project.path)) {
    cancelEditProject();
    return;
  }

  const active = isActive(project.path);
  const accepted = await ui.ask({
    title: active ? "修改当前项目路径" : "修改项目路径",
    message: active
      ? `当前 MCP 工作目录将切换为 ${path}。MCP 会安全重启，启动失败时会自动恢复原目录。`
      : `项目“${project.name}”的目录引用将修改为 ${path}，不会移动或修改磁盘中的文件。`,
    confirmLabel: active ? "修改并切换" : "修改路径",
  });
  if (!accepted) return;

  try {
    const wasSelected = selectedId.value === project.id;
    const updated = await app.updateProjectPath(project.id, path);
    cancelEditProject();
    if (wasSelected) {
      await app.inspectProject(updated.id);
      selectedId.value = updated.id;
    }
  } catch (error) {
    ui.toast("项目路径修改失败", errorMessage(error), "danger");
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

async function copyCommit(hash: string) {
  try {
    await navigator.clipboard.writeText(hash);
    ui.toast("提交 ID 已复制", hash, "success");
  } catch (error) {
    ui.toast("复制提交 ID 失败", errorMessage(error), "danger");
  }
}

function formatCommitTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN");
}

async function rollbackCommit(commit: GitCommit) {
  if (!selectedId.value || commit.current) return;
  const accepted = await ui.ask({
    title: "回档到此 Git 提交",
    message: `当前分支将回退到 ${commit.shortHash}（${commit.subject}）。存在未提交修改时系统会拒绝回档；执行前会自动创建备份分支保存当前 HEAD。`,
    confirmLabel: "创建备份并回档",
    danger: true,
  });
  if (!accepted) return;
  try {
    const result = await app.rollbackProject(selectedId.value, commit.hash);
    ui.toast(
      "Git 回档完成",
      result.backupBranch ? `当前已到 ${commit.shortHash}；原版本保存在 ${result.backupBranch}` : `当前已是 ${commit.shortHash}`,
      "success",
    );
  } catch (error) {
    ui.toast("Git 回档失败", errorMessage(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack projects-page">
    <PageHeader eyebrow="Workspaces" title="项目" description="保存多个本地项目，并在不中断公网地址的情况下热切换 MCP 工作目录。">
      <template #actions><AppButton tone="primary" icon="projects" @click="showAdd = !showAdd">添加项目</AppButton></template>
    </PageHeader>

    <AppCard class="project-prompt-global-card">
      <div class="card-heading">
        <div>
          <span class="eyebrow">Global AI instructions</span>
          <h3>全局项目提示词</h3>
          <p>对所有项目生效。单项目提示词会在此基础上继续叠加，适合统一限定 AI 的执行方式、完成标准和沟通规则。</p>
        </div>
        <StatusPill :tone="globalPromptDraft.trim() ? 'success' : 'neutral'">{{ globalPromptDraft.trim() ? '已启用' : '未设置' }}</StatusPill>
      </div>
      <label class="field project-prompt-field">
        <span>提示词内容</span>
        <textarea v-model="globalPromptDraft" rows="8" spellcheck="false" placeholder="例如：任务未完成前不要只汇报进度，能继续执行时直接完成所有步骤后再回复用户。" />
        <small>{{ globalPromptBytes }} / {{ app.projectPromptSettings?.maxPromptBytes || 32768 }} bytes · Go 核心会把保存后的内容注入 MCP initialize instructions。</small>
      </label>
      <div class="form-footer">
        <small>保存后会立即同步到所有 Go MCP 实例；运行中的核心检测到变化后会刷新旧会话，让客户端重新握手并加载新提示词。Legacy/Python 兼容核心不支持该注入机制。</small>
        <div class="form-footer-actions">
          <AppButton tone="secondary" @click="useGlobalCompletionTemplate">使用“任务完成后再回复”模板</AppButton>
          <AppButton tone="primary" :loading="app.actionPending === 'save-global-project-prompt'" @click="saveGlobalPrompt">保存全局提示词</AppButton>
        </div>
      </div>
    </AppCard>

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
        <label class="field">
          <span>项目目录</span>
          <div class="path-picker-row">
            <input v-model="projectPath" placeholder="D:\Projects\my-app" />
            <AppButton tone="secondary" icon="folder" :loading="browsingProject" @click="browseProject">浏览</AppButton>
          </div>
        </label>
        <label class="field"><span>显示名称（可选）</span><input v-model="projectLabel" placeholder="默认使用文件夹名称" /></label>
        <div class="form-footer">
          <small>只保存目录引用，不会移动或修改项目文件。</small>
          <div class="form-footer-actions">
            <AppButton tone="quiet" @click="cancelAddProject">取消添加</AppButton>
            <AppButton type="submit" tone="primary" :loading="app.actionPending === 'add-project'">添加</AppButton>
          </div>
        </div>
      </form>
    </AppCard>

    <section class="project-list">
      <AppCard v-for="project in app.projects" :key="project.id" class="project-list-card">
        <div class="current-project-icon"><AppIcon name="folder" :size="24" /></div>
        <div class="current-project-copy">
          <div class="current-project-title"><h3>{{ project.name }}</h3><StatusPill v-if="isActive(project.path)" tone="success">当前活动</StatusPill><StatusPill v-if="project.prompt" tone="info">已设置提示词</StatusPill></div>
          <div v-if="editingId === project.id" class="project-path-editor">
            <div class="path-picker-row">
              <input v-model="editingPath" :aria-label="`${project.name} 项目路径`" spellcheck="false" />
              <AppButton tone="secondary" icon="folder" :loading="browsingEditId === project.id" @click="browseEditProject(project)">浏览</AppButton>
            </div>
            <small>修改当前活动项目时会同步切换 MCP 工作目录；其他项目只更新目录引用。</small>
          </div>
          <p v-else class="mono">{{ project.path }}</p>
          <small>最近打开 {{ new Date(project.lastOpenedAt).toLocaleString() }}</small>
        </div>
        <div class="project-row-actions">
          <template v-if="editingId === project.id">
            <AppButton tone="quiet" @click="cancelEditProject">取消</AppButton>
            <AppButton tone="primary" :loading="app.actionPending === `update-${project.id}`" @click="saveProjectPath(project)">保存路径</AppButton>
          </template>
          <template v-else>
            <AppButton tone="secondary" icon="folder" @click="startEditProject(project)">修改路径</AppButton>
            <AppButton tone="secondary" icon="settings" @click="startPromptEdit(project)">提示词</AppButton>
            <AppButton tone="secondary" icon="info" :loading="app.actionPending === `inspect-${project.id}`" @click="inspect(project.id)">详情</AppButton>
            <AppButton v-if="!isActive(project.path)" tone="primary" icon="restart" :loading="app.actionPending === `activate-${project.id}`" @click="switchProject(project.id)">切换</AppButton>
            <AppButton v-if="!isActive(project.path)" tone="quiet" :loading="app.actionPending === `remove-${project.id}`" @click="removeProject(project.id, project.name)">移除</AppButton>
          </template>
        </div>
        <div v-if="promptEditingId === project.id" class="project-prompt-editor">
          <div class="card-heading">
            <div><span class="eyebrow">Project AI instructions</span><h3>{{ project.name }} · 项目提示词</h3><p>只对该项目生效，并叠加在全局项目提示词之后。</p></div>
          </div>
          <label class="field project-prompt-field">
            <span>项目专属提示词</span>
            <textarea v-model="promptDraft" rows="8" spellcheck="false" placeholder="留空表示只使用全局项目提示词。" />
            <small>{{ projectPromptBytes }} / {{ app.projectPromptSettings?.maxPromptBytes || 32768 }} bytes</small>
          </label>
          <div class="form-footer">
            <small>保存后不会修改项目目录中的任何文件。对应 Go MCP 实例会立即同步，并在自动重新握手后加载“全局提示词 + 当前项目提示词”。</small>
            <div class="form-footer-actions">
              <AppButton tone="quiet" @click="cancelPromptEdit">取消</AppButton>
              <AppButton tone="secondary" @click="useProjectCompletionTemplate">使用任务完成模板</AppButton>
              <AppButton tone="primary" :loading="app.actionPending === `prompt-${project.id}`" @click="saveProjectPrompt(project)">保存项目提示词</AppButton>
            </div>
          </div>
        </div>
      </AppCard>
    </section>

    <AppCard v-if="selectedId && app.projectDetails[selectedId]" class="project-inspector-card">
      <div class="card-heading"><div><span class="eyebrow">Developer context</span><h3>项目开发信息</h3></div><AppButton tone="secondary" icon="refresh" :loading="app.actionPending === `inspect-${selectedId}`" @click="inspect(selectedId)">刷新</AppButton></div>
      <div class="project-inspector-grid">
        <div><span>Git</span><strong>{{ app.projectDetails[selectedId].git ? app.projectDetails[selectedId].branch || 'Detached' : '不是 Git 仓库' }}</strong></div>
        <div><span>当前提交</span><strong class="mono">{{ app.projectDetails[selectedId].currentShort || '--' }}</strong></div>
        <div><span>文件变化</span><strong>{{ app.projectDetails[selectedId].changedFiles }}</strong></div>
        <div><span>AGENTS.md</span><strong>{{ app.projectDetails[selectedId].hasAgents ? '已检测' : '未检测' }}</strong></div>
        <div><span>Skills</span><strong>{{ app.projectDetails[selectedId].skills.length }}</strong></div>
      </div>
      <div v-if="app.projectDetails[selectedId].skills.length" class="skill-chip-list"><span v-for="skill in app.projectDetails[selectedId].skills" :key="skill">{{ skill }}</span></div>
      <div v-if="app.projectDetails[selectedId].git" class="git-history-panel">
        <div class="card-heading">
          <div><span class="eyebrow">Git history</span><h3>提交历史记录</h3><p>显示当前分支最近 200 条记录；在列表内使用鼠标滚轮查看。</p></div>
          <StatusPill tone="neutral">{{ app.projectHistories[selectedId]?.commits.length || 0 }} 条</StatusPill>
        </div>
        <div class="git-history-scroll" tabindex="0" aria-label="Git 提交历史记录">
          <article
            v-for="commit in app.projectHistories[selectedId]?.commits || []"
            :key="commit.hash"
            class="git-history-row"
            :class="{ 'is-current': commit.current }"
          >
            <span class="git-history-node"><i /></span>
            <div class="git-history-copy">
              <div class="git-history-subject">
                <strong>{{ commit.subject || '无提交说明' }}</strong>
                <StatusPill v-if="commit.current" tone="success">当前 HEAD</StatusPill>
                <span v-for="decoration in commit.decorations" :key="decoration" class="git-decoration">{{ decoration }}</span>
              </div>
              <div class="git-history-meta">
                <span>{{ commit.author }}</span>
                <span>{{ formatCommitTime(commit.timestamp) }}</span>
                <code :title="commit.hash">{{ commit.shortHash }}</code>
              </div>
              <code class="git-full-hash" :title="commit.hash">{{ commit.hash }}</code>
            </div>
            <div class="git-history-actions">
              <AppButton tone="quiet" icon="copy" compact @click="copyCommit(commit.hash)">复制 ID</AppButton>
              <AppButton
                v-if="!commit.current"
                tone="quiet"
                icon="restart"
                compact
                :loading="app.actionPending === `rollback-${selectedId}`"
                @click="rollbackCommit(commit)"
              >回档</AppButton>
            </div>
          </article>
          <div v-if="!(app.projectHistories[selectedId]?.commits.length)" class="git-history-empty">当前仓库还没有提交记录。</div>
        </div>
        <small v-if="app.projectHistories[selectedId]?.truncated">历史记录超过 200 条，仅显示最近记录。</small>
      </div>
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
