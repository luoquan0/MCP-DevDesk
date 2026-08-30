<script setup lang="ts">
import { computed, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useControlStore } from "@/stores/control";
import { useUiStore } from "@/stores/ui";
import type { ControlDirectoryListing, Project } from "@/types/api";

const control = useControlStore();
const ui = useUiStore();
const browserOpen = ref(false);
const browserMode = ref<"add" | "edit">("add");
const editingProjectId = ref("");
const projectName = ref("");
const listing = ref<ControlDirectoryListing | null>(null);
const browserLoading = ref(false);
const selectedPath = computed(() => listing.value?.path || "");

async function browse(path = "") {
  browserLoading.value = true;
  try {
    listing.value = await control.browseDirectories(path);
  } catch (error) {
    ui.toast("读取目录失败", error instanceof Error ? error.message : String(error), "danger");
  } finally {
    browserLoading.value = false;
  }
}

async function openAddBrowser() {
  browserMode.value = "add";
  editingProjectId.value = "";
  projectName.value = "";
  browserOpen.value = true;
  await browse("");
}

async function openEditBrowser(project: Project) {
  browserMode.value = "edit";
  editingProjectId.value = project.id;
  projectName.value = project.name;
  browserOpen.value = true;
  await browse(project.path);
}

async function chooseCurrentDirectory() {
  if (!selectedPath.value) {
    ui.toast("请选择目录", "先进入一个具体文件夹，再选择当前目录。", "info");
    return;
  }
  try {
    if (browserMode.value === "add") {
      const project = await control.addProject(selectedPath.value, projectName.value);
      ui.toast("项目已添加", project.path, "success");
    } else {
      const project = await control.updateProjectPath(editingProjectId.value, selectedPath.value);
      ui.toast("项目目录已修改", project.path, "success");
    }
    browserOpen.value = false;
  } catch (error) {
    ui.toast("保存项目目录失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function activate(project: Project) {
  try {
    await control.activateProject(project.id);
    ui.toast("项目已切换", project.path, "success");
  } catch (error) {
    ui.toast("切换失败", error instanceof Error ? error.message : String(error), "danger");
  }
}

async function remove(project: Project) {
  try {
    await control.removeProject(project.id);
    ui.toast("项目已移除", "电脑上的项目文件不会被删除。", "success");
  } catch (error) {
    ui.toast("移除失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <section class="mobile-control-page-stack">
    <div class="mobile-control-page-title">
      <div><span class="eyebrow">Projects</span><h1>项目目录</h1><p>在手机上浏览电脑文件夹、添加项目并切换 MCP 工作目录。</p></div>
      <AppButton tone="primary" icon="folder" @click="openAddBrowser">添加项目</AppButton>
    </div>

    <AppCard class="mobile-current-project-card">
      <span>当前 MCP 工作目录</span>
      <strong>{{ control.activeProject?.name || '未匹配项目' }}</strong>
      <code>{{ control.overview?.workspace || '--' }}</code>
    </AppCard>

    <div class="mobile-project-list">
      <AppCard v-for="project in control.projects" :key="project.id" class="mobile-project-card">
        <div class="mobile-project-main">
          <span class="mobile-project-icon"><AppIcon name="folder" :size="20" /></span>
          <div><div class="mobile-project-name"><strong>{{ project.name }}</strong><StatusPill v-if="project.id === control.activeProject?.id" tone="success">当前</StatusPill></div><code>{{ project.path }}</code></div>
        </div>
        <div class="mobile-project-actions">
          <AppButton tone="secondary" @click="openEditBrowser(project)">修改目录</AppButton>
          <AppButton tone="primary" :disabled="project.id === control.activeProject?.id" :loading="control.actionPending === `activate-${project.id}`" @click="activate(project)">切换</AppButton>
          <AppButton v-if="project.id !== control.activeProject?.id" tone="quiet" @click="remove(project)">移除</AppButton>
        </div>
      </AppCard>
    </div>

    <section v-if="browserOpen" class="mobile-directory-browser">
      <div class="mobile-directory-browser-header">
        <div><span class="eyebrow">Computer folders</span><h2>{{ browserMode === 'add' ? '选择新项目目录' : '修改项目目录' }}</h2></div>
        <AppButton tone="quiet" @click="browserOpen = false">关闭</AppButton>
      </div>

      <label v-if="browserMode === 'add'" class="field">
        <span>项目名称（可选）</span>
        <input v-model="projectName" placeholder="留空时自动使用文件夹名称" />
      </label>

      <div class="mobile-directory-path">
        <span>当前目录</span>
        <code>{{ selectedPath || '请选择磁盘' }}</code>
      </div>

      <div class="mobile-directory-toolbar">
        <AppButton tone="secondary" :loading="browserLoading" @click="browse('')">磁盘</AppButton>
        <AppButton tone="secondary" :disabled="!listing?.parent" @click="browse(listing?.parent || '')">上一级</AppButton>
        <AppButton tone="primary" :disabled="!selectedPath" @click="chooseCurrentDirectory">选择当前目录</AppButton>
      </div>

      <div class="mobile-directory-list" :class="{ 'is-loading': browserLoading }">
        <button v-for="root in (!listing?.path ? listing?.roots || [] : [])" :key="root.path" type="button" class="mobile-directory-row" @click="browse(root.path)">
          <AppIcon name="folder" :size="18" /><strong>{{ root.name }}</strong><code>{{ root.path }}</code><span>›</span>
        </button>
        <button v-for="directory in (listing?.path ? listing?.directories || [] : [])" :key="directory.path" type="button" class="mobile-directory-row" @click="browse(directory.path)">
          <AppIcon name="folder" :size="18" /><strong>{{ directory.name }}</strong><span>›</span>
        </button>
        <div v-if="!browserLoading && listing?.path && !(listing?.directories?.length)" class="mobile-directory-empty">这个目录下没有可进入的子文件夹。</div>
      </div>
    </section>
  </section>
</template>
