<script setup lang="ts">
import { computed } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import EmptyState from "@/components/ui/EmptyState.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useAppStore } from "@/stores/app";

const app = useAppStore();
const projectName = computed(() => app.config?.workspace?.split(/[\\/]/).filter(Boolean).at(-1) ?? "当前项目");
</script>

<template>
  <div class="page-stack projects-page">
    <PageHeader
      eyebrow="Workspaces"
      title="项目"
      description="管理当前工作区，并为下一阶段的多项目切换做好准备。"
    >
      <template #actions>
        <AppButton tone="primary" icon="projects" disabled>添加项目</AppButton>
      </template>
    </PageHeader>

    <AppCard class="current-project-card">
      <div class="current-project-icon"><AppIcon name="folder" :size="30" /></div>
      <div class="current-project-copy">
        <div class="current-project-title">
          <h2>{{ projectName }}</h2>
          <StatusPill tone="success">当前活动</StatusPill>
        </div>
        <p class="mono">{{ app.config?.workspace || '尚未配置工作目录' }}</p>
        <div class="project-facts">
          <span><AppIcon name="services" :size="14" /> 端口 {{ app.config?.mcpPort || '--' }}</span>
          <span><AppIcon name="shield" :size="14" /> {{ app.status?.permissionMode || 'safe' }}</span>
          <span><AppIcon name="activity" :size="14" /> {{ app.status?.mcp.running ? 'MCP 运行中' : 'MCP 已停止' }}</span>
        </div>
      </div>
      <AppButton tone="secondary" icon="settings" disabled>项目设置</AppButton>
    </AppCard>

    <section class="project-roadmap-grid">
      <AppCard>
        <div class="roadmap-icon is-blue"><AppIcon name="projects" :size="20" /></div>
        <h3>多项目列表</h3>
        <p>保存多个本地目录、最近打开时间、Git 状态和独立配置。</p>
      </AppCard>
      <AppCard>
        <div class="roadmap-icon is-purple"><AppIcon name="restart" :size="20" /></div>
        <h3>无感切换</h3>
        <p>切换工作区时自动重启 MCP，公网地址与 OAuth 配置保持不变。</p>
      </AppCard>
      <AppCard>
        <div class="roadmap-icon is-mint"><AppIcon name="network" :size="20" /></div>
        <h3>项目独立策略</h3>
        <p>为每个项目保存权限、启动命令、端口和环境变量。</p>
      </AppCard>
    </section>

    <AppCard>
      <EmptyState
        icon="projects"
        title="多项目能力将在下一阶段启用"
        description="新版界面已经预留项目列表、项目详情与快速切换区域。你确认本轮视觉框架后，再继续接入完整功能。"
      >
        <AppButton tone="secondary" disabled>等待下一阶段</AppButton>
      </EmptyState>
    </AppCard>
  </div>
</template>
