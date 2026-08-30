<script setup lang="ts">
import { computed, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useControlStore } from "@/stores/control";
import { useUiStore } from "@/stores/ui";

const control = useControlStore();
const ui = useUiStore();
const selectedProjectId = ref("");
const projectPrompt = ref("");
const globalEnabled = ref(false);
const globalPrompt = ref("");
const selectedProject = computed(() => control.projects.find((project) => project.id === selectedProjectId.value));

watch(() => control.projects, (projects) => {
  if (!selectedProjectId.value || !projects.some((project) => project.id === selectedProjectId.value)) {
    selectedProjectId.value = control.activeProject?.id || projects[0]?.id || "";
  }
}, { immediate: true, deep: true });

watch(selectedProject, (project) => {
  projectPrompt.value = project?.prompt || "";
}, { immediate: true });

watch(() => control.promptSettings, (settings) => {
  globalEnabled.value = Boolean(settings?.enabled);
  globalPrompt.value = settings?.globalPrompt || "";
}, { immediate: true });

async function saveProjectPrompt() {
  if (!selectedProject.value) return;
  try {
    await control.saveProjectPrompt(selectedProject.value.id, projectPrompt.value);
    ui.toast("AGENTS.md 已保存", selectedProject.value.path, "success");
  } catch (error) {
    ui.toast("保存失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function saveGlobalPrompt() {
  try {
    await control.savePromptSettings(globalEnabled.value, globalPrompt.value);
    ui.toast("全局提示词已保存", globalEnabled.value && globalPrompt.value.trim() ? "已生效" : "当前未启用", "success");
  } catch (error) {
    ui.toast("保存失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <section class="mobile-control-page-stack">
    <div class="mobile-control-page-title"><div><span class="eyebrow">Instructions</span><h1>提示词</h1><p>项目规则写入项目自己的 AGENTS.md；全局规则单独控制。</p></div></div>

    <AppCard class="mobile-prompt-card">
      <div class="card-heading"><div><h3>项目 AGENTS.md</h3><p>选择项目后直接编辑电脑中该项目根目录的指令文件。</p></div><StatusPill :tone="projectPrompt.trim() ? 'info' : 'neutral'">{{ projectPrompt.trim() ? '已设置' : '未设置' }}</StatusPill></div>
      <label class="field">
        <span>项目</span>
        <select v-model="selectedProjectId"><option v-for="project in control.projects" :key="project.id" :value="project.id">{{ project.name }}</option></select>
      </label>
      <label class="field project-prompt-field">
        <span>{{ selectedProject?.path || '请选择项目' }}\AGENTS.md</span>
        <textarea v-model="projectPrompt" rows="15" spellcheck="false" :disabled="!selectedProject" placeholder="# 项目 Agent 指令" />
      </label>
      <AppButton tone="primary" :disabled="!selectedProject" :loading="control.actionPending === `prompt-${selectedProjectId}`" @click="saveProjectPrompt">保存 AGENTS.md</AppButton>
    </AppCard>

    <AppCard class="mobile-prompt-card">
      <div class="card-heading"><div><h3>全局提示词</h3><p>只保存在 DevDesk，不会复制到项目文件夹。</p></div><StatusPill :tone="globalEnabled && globalPrompt.trim() ? 'success' : 'neutral'">{{ globalEnabled && globalPrompt.trim() ? '已生效' : '未生效' }}</StatusPill></div>
      <ToggleSwitch v-model="globalEnabled" label="启用全局提示词" description="开关关闭时保留内容但不注入。" />
      <label class="field project-prompt-field"><span>全局提示词内容</span><textarea v-model="globalPrompt" rows="12" spellcheck="false" placeholder="# 全局 Agent 规则" /></label>
      <AppButton tone="primary" :loading="control.actionPending === 'global-prompt'" @click="saveGlobalPrompt">保存全局提示词</AppButton>
    </AppCard>
  </section>
</template>
