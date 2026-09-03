<script setup lang="ts">
import { reactive, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";
import type { FileScope, PermissionMode } from "@/types/api";

const app = useAppStore();
const ui = useUiStore();
const form = reactive({
  permissionMode: "safe" as PermissionMode,
  fileScope: "workspace" as FileScope,
  allowNetwork: false,
  screenCaptureEnabled: false,
  allowedRootsText: "",
});

watch(() => app.config, (config) => {
  if (!config) return;
  form.permissionMode = config.permissionMode;
  form.fileScope = config.fileScope;
  form.allowNetwork = config.allowNetwork;
  form.screenCaptureEnabled = config.screenCaptureEnabled ?? false;
  form.allowedRootsText = (config.allowedRoots ?? []).join("\n");
}, { immediate: true, deep: true });

const modes = [
  { id: "safe" as const, title: "安全模式", subtitle: "默认推荐", icon: "shield", tone: "success", description: "限制内联脚本、Shell 展开和高风险命令。联网可单独开启。" },
  { id: "trusted" as const, title: "信任模式", subtitle: "日常开发", icon: "key", tone: "warning", description: "允许常用开发命令、包管理器、联网和脚本执行。" },
  { id: "dangerous" as const, title: "危险模式", subtitle: "完全控制", icon: "warning", tone: "danger", description: "关闭应用层命令门控。远程会话可以执行当前 Windows 用户能够执行的操作。" },
];

async function savePermissions() {
  if (form.screenCaptureEnabled && form.permissionMode === "safe") {
    ui.toast("无法启用屏幕视觉", "屏幕画面可能包含敏感信息，请先选择“信任模式”或“危险模式”。", "warning");
    return;
  }
  if (form.screenCaptureEnabled && !app.config?.screenCaptureEnabled) {
    const accepted = await ui.ask({
      title: "启用屏幕视觉（测试）",
      message: "启用后，已连接并获得授权的 MCP 客户端可以按需获取当前窗口或桌面截图。截图仅在工具被调用时生成，不持续录屏、不保存历史；但画面仍可能包含聊天、账号、文件名等敏感内容。",
      confirmLabel: "确认启用",
      danger: true,
    });
    if (!accepted) return;
  }
  if (form.permissionMode === "dangerous" || form.fileScope === "computer") {
    const accepted = await ui.ask({
      title: "应用高风险权限",
      message: "危险模式或整台电脑范围可能允许远程会话访问工作区外文件、安装软件和删除数据。请只在完全可信的连接中使用。",
      confirmLabel: "确认应用",
      danger: true,
    });
    if (!accepted) return;
  }
  try {
    await app.saveConfig({
      permissionMode: form.permissionMode,
      fileScope: form.fileScope,
      allowNetwork: form.permissionMode !== "safe" ? true : form.allowNetwork,
      screenCaptureEnabled: form.screenCaptureEnabled,
      allowedRoots: form.allowedRootsText.split(/\r?\n/).map((value) => value.trim()).filter(Boolean),
    });
    if (app.status?.mcp.running) await app.serviceAction("restart");
  } catch (error) {
    ui.toast("权限保存失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <section class="settings-security-section">
    <div class="settings-section-heading">
      <div>
        <span class="eyebrow">Permission & security</span>
        <h2>权限与安全</h2>
        <p>控制远程 MCP 会话能够运行的命令、访问的路径、网络能力和屏幕视觉。</p>
      </div>
      <AppButton tone="primary" icon="shield" :loading="app.actionPending === 'save-config'" @click="savePermissions">保存权限</AppButton>
    </div>

    <div v-if="form.permissionMode === 'dangerous' || form.fileScope === 'computer'" class="inline-alert is-danger">
      <AppIcon name="warning" :size="20" />
      <div><strong>高风险权限已选择</strong><span>当前会话可能访问工作区以外的文件并执行不可逆命令。保存前请再次确认。</span></div>
    </div>

    <AppCard>
      <div class="card-heading">
        <div><span class="eyebrow">Execution policy</span><h3>权限模式</h3></div>
        <StatusPill :tone="form.permissionMode === 'dangerous' ? 'danger' : form.permissionMode === 'trusted' ? 'warning' : 'success'">
          {{ form.permissionMode }}
        </StatusPill>
      </div>
      <div class="permission-mode-grid">
        <button v-for="mode in modes" :key="mode.id" type="button" class="permission-mode-card" :class="{ 'is-selected': form.permissionMode === mode.id, [`is-${mode.tone}`]: true }" @click="form.permissionMode = mode.id">
          <div class="permission-mode-top">
            <span class="permission-mode-icon"><AppIcon :name="mode.icon" :size="21" /></span>
            <span class="radio-mark"><i /></span>
          </div>
          <strong>{{ mode.title }}</strong>
          <small>{{ mode.subtitle }}</small>
          <p>{{ mode.description }}</p>
        </button>
      </div>
    </AppCard>

    <section class="security-grid">
      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Filesystem</span><h3>文件访问范围</h3></div></div>
        <div class="segmented-control vertical">
          <label :class="{ 'is-selected': form.fileScope === 'workspace' }"><input v-model="form.fileScope" type="radio" value="workspace" /><span><strong>当前工作区</strong><small>仅允许当前项目目录。</small></span></label>
          <label :class="{ 'is-selected': form.fileScope === 'roots' }"><input v-model="form.fileScope" type="radio" value="roots" /><span><strong>授权根目录</strong><small>允许多个指定目录。</small></span></label>
          <label :class="{ 'is-selected': form.fileScope === 'computer' }"><input v-model="form.fileScope" type="radio" value="computer" /><span><strong>整台电脑</strong><small>Shell 可访问当前 Windows 用户拥有权限的位置。</small></span></label>
        </div>
        <label class="field security-roots-field">
          <span>授权根目录</span>
          <textarea v-model="form.allowedRootsText" rows="4" spellcheck="false" placeholder="每行填写一个目录，例如 D:\\Projects" />
          <small>仅在“授权根目录”范围下生效；当前工作区始终自动允许。</small>
        </label>
      </AppCard>

      <AppCard>
        <div class="card-heading"><div><span class="eyebrow">Network</span><h3>联网能力</h3></div></div>
        <ToggleSwitch v-model="form.allowNetwork" :disabled="form.permissionMode !== 'safe'" label="允许联网命令" :description="form.permissionMode === 'safe' ? '允许 npm、pip、Git、curl 等工具访问网络。' : '信任模式和危险模式默认允许联网。'" />
        <div class="capability-list">
          <div><AppIcon name="network" :size="16" /><span>包管理器与远程 Git</span><StatusPill :tone="form.permissionMode !== 'safe' || form.allowNetwork ? 'success' : 'neutral'">{{ form.permissionMode !== 'safe' || form.allowNetwork ? '允许' : '禁止' }}</StatusPill></div>
          <div><AppIcon name="terminal" :size="16" /><span>内联脚本与 Shell 展开</span><StatusPill :tone="form.permissionMode === 'safe' ? 'neutral' : 'success'">{{ form.permissionMode === 'safe' ? '限制' : '允许' }}</StatusPill></div>
          <div><AppIcon name="warning" :size="16" /><span>高风险命令</span><StatusPill :tone="form.permissionMode === 'dangerous' ? 'danger' : 'neutral'">{{ form.permissionMode === 'dangerous' ? '不拦截' : '受保护' }}</StatusPill></div>
        </div>
      </AppCard>

      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Screen Vision · Experimental</span><h3>屏幕视觉（测试）</h3></div>
          <StatusPill :tone="form.screenCaptureEnabled ? 'warning' : 'neutral'">{{ form.screenCaptureEnabled ? '已启用' : '已关闭' }}</StatusPill>
        </div>
        <ToggleSwitch
          v-model="form.screenCaptureEnabled"
          label="允许 AI 按需读取窗口画面"
          :description="form.permissionMode === 'safe' ? '需要先切换到信任模式或危险模式。' : '仅在 MCP 工具主动调用时截图；空闲时不录屏、不启动持续捕获。'"
        />
        <div class="capability-list">
          <div><AppIcon name="monitor" :size="16" /><span>指定窗口 / 当前窗口 / 整个桌面</span><StatusPill :tone="form.screenCaptureEnabled ? 'warning' : 'neutral'">{{ form.screenCaptureEnabled ? '按需可读' : '不可读' }}</StatusPill></div>
          <div><AppIcon name="shield" :size="16" /><span>截图历史</span><StatusPill tone="success">不保存</StatusPill></div>
          <div><AppIcon name="terminal" :size="16" /><span>鼠标与键盘控制</span><StatusPill tone="neutral">本测试版未开放</StatusPill></div>
        </div>
        <p class="field-hint">当前测试功能仅由 Go MCP Core 提供。部分受保护、DRM、最小化或硬件加速窗口可能返回黑屏；关闭本开关后不会暴露这些视觉工具。</p>
      </AppCard>
    </section>
  </section>
</template>
