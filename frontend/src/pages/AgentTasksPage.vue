<script setup lang="ts">
import { computed } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { AgentTask } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const reviewCount = computed(() => app.agentTasks.filter((task) => task.status === "review").length);

function tone(task: AgentTask) {
  if (task.status === "accepted") return "success";
  if (task.status === "review") return "warning";
  if (task.status === "rejected") return "neutral";
  return "info";
}

function statusLabel(task: AgentTask) {
  return ({ editing: "AI 修改中", review: "等待你验收", accepted: "已接受", rejected: "已放弃" } as Record<string, string>)[task.status] || task.status;
}

async function accept(task: AgentTask) {
  const ok = await ui.ask({
    title: "接受 AI 任务",
    message: `将把 ${task.branch} 的修改以 fast-forward 方式应用到原项目。只有原项目仍停留在任务开始时的提交且工作区干净时才会成功。`,
    confirmLabel: "接受并应用",
  });
  if (!ok) return;
  try { await app.acceptAgentTask(task.id); }
  catch (error) { ui.toast("接受失败", error instanceof Error ? error.message : String(error), "danger"); }
}

async function reject(task: AgentTask) {
  const ok = await ui.ask({
    title: "放弃 AI 任务",
    message: `将删除 ${task.branch} 和对应隔离 Worktree，原项目不会被修改。`,
    confirmLabel: "放弃并清理",
    danger: true,
  });
  if (!ok) return;
  try { await app.rejectAgentTask(task.id); }
  catch (error) { ui.toast("放弃失败", error instanceof Error ? error.message : String(error), "danger"); }
}
</script>

<template>
  <div class="page-stack agent-tasks-page">
    <PageHeader eyebrow="Human approval boundary" title="AI 隔离任务" description="AI 写入会在独立 Git Worktree 中完成。Task ID 会持久化；AI 完成后只能由本机 DevDesk 接受或放弃。">
      <template #actions>
        <StatusPill :tone="reviewCount ? 'warning' : 'neutral'">{{ reviewCount }} 个待验收</StatusPill>
        <AppButton tone="secondary" icon="refresh" @click="app.loadAgentTasks()">刷新</AppButton>
      </template>
    </PageHeader>

    <AppCard>
      <div class="task-policy">
        <AppIcon name="shield" :size="22" />
        <div><strong>AI 不能自行接受自己的修改</strong><span>MCP 只提供 task_start / task_finish / task_resume 等工具；accept / reject 仅存在于本机管理界面。</span></div>
      </div>
    </AppCard>

    <div v-if="app.agentTasks.length" class="task-list">
      <AppCard v-for="task in app.agentTasks" :key="task.id" class="task-card">
        <div class="task-heading">
          <div class="task-title"><strong>{{ task.title }}</strong><code>{{ task.id }}</code></div>
          <StatusPill :tone="tone(task)">{{ statusLabel(task) }}</StatusPill>
        </div>
        <p v-if="task.summary" class="task-summary">{{ task.summary }}</p>
        <div class="task-meta">
          <span><b>Branch</b><code>{{ task.branch }}</code></span>
          <span><b>Worktree</b><code>{{ task.worktreePath }}</code></span>
          <span><b>Base</b><code>{{ task.baseCommit.slice(0, 12) }}</code></span>
          <span><b>更新</b>{{ new Date(task.updatedAt).toLocaleString('zh-CN') }}</span>
        </div>
        <div v-if="task.status === 'review'" class="task-actions">
          <AppButton tone="danger" :loading="app.actionPending === `reject-agent-task-${task.id}`" @click="reject(task)">放弃修改</AppButton>
          <AppButton tone="primary" icon="check" :loading="app.actionPending === `accept-agent-task-${task.id}`" @click="accept(task)">接受并应用</AppButton>
        </div>
      </AppCard>
    </div>
    <AppCard v-else>
      <div class="inline-empty large"><AppIcon name="command" :size="26" /><div><strong>还没有 AI 隔离任务</strong><span>ChatGPT 调用 task_start 后，这里会出现持久化 Task ID 和独立 Worktree。</span></div></div>
    </AppCard>
  </div>
</template>

<style scoped>
.task-list { display: grid; gap: 12px; }
.task-card { display: grid; gap: 14px; }
.task-heading, .task-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.task-title { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; min-width: 0; }
.task-title code, .task-meta code { overflow-wrap: anywhere; }
.task-summary { margin: 0; color: var(--text-secondary); white-space: pre-wrap; }
.task-meta { display: grid; gap: 7px; color: var(--text-tertiary); font-size: 12px; }
.task-meta span { display: grid; grid-template-columns: 70px minmax(0, 1fr); gap: 8px; }
.task-meta b { color: var(--text-secondary); font-weight: 600; }
.task-policy { display: flex; align-items: flex-start; gap: 12px; }
.task-policy > div { display: grid; gap: 4px; }
.task-policy span { color: var(--text-tertiary); font-size: 13px; }
.task-actions { justify-content: flex-end; border-top: 1px solid var(--border-subtle); padding-top: 12px; }
@media (max-width: 700px) { .task-actions { flex-direction: column-reverse; align-items: stretch; } .task-meta span { grid-template-columns: 1fr; } }
</style>
