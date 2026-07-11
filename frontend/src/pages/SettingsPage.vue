<script setup lang="ts">
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import PageHeader from "@/components/ui/PageHeader.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import ToggleSwitch from "@/components/ui/ToggleSwitch.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore, type ThemeMode } from "@/stores/ui";

const app = useAppStore();
const ui = useUiStore();

const themes: Array<{ id: ThemeMode; label: string; description: string; icon: string }> = [
  { id: "system", label: "跟随系统", description: "根据 Windows 外观自动切换", icon: "monitor" },
  { id: "light", label: "浅色", description: "明亮、克制的 Apple 风格", icon: "sun" },
  { id: "dark", label: "深色", description: "适合夜间和长时间运行", icon: "moon" },
];

async function setStartup(enabled: boolean) {
  try {
    await app.updateStartup(enabled);
  } catch (error) {
    ui.toast("开机启动设置失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <div class="page-stack settings-page">
    <PageHeader
      eyebrow="Application preferences"
      title="设置"
      description="调整界面外观、Windows 集成和本地数据位置。"
    />

    <AppCard>
      <div class="card-heading">
        <div><span class="eyebrow">Appearance</span><h3>外观</h3></div>
      </div>
      <div class="theme-grid">
        <button v-for="theme in themes" :key="theme.id" type="button" class="theme-option" :class="{ 'is-selected': ui.theme === theme.id }" @click="ui.setTheme(theme.id)">
          <div class="theme-preview" :class="`theme-${theme.id}`">
            <span class="theme-preview-sidebar" />
            <span class="theme-preview-card one" />
            <span class="theme-preview-card two" />
          </div>
          <div class="theme-option-copy">
            <span class="theme-option-icon"><AppIcon :name="theme.icon" :size="16" /></span>
            <div><strong>{{ theme.label }}</strong><small>{{ theme.description }}</small></div>
            <span class="radio-mark"><i /></span>
          </div>
        </button>
      </div>
    </AppCard>

    <section class="settings-grid equal">
      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Windows integration</span><h3>桌面集成</h3></div>
          <StatusPill :tone="app.desktop?.nativeWindow ? 'success' : 'neutral'">{{ app.desktop?.nativeWindow ? 'Native' : 'Unavailable' }}</StatusPill>
        </div>
        <div class="toggle-list">
          <ToggleSwitch
            :model-value="Boolean(app.desktop?.startupEnabled)"
            label="Windows 登录时后台运行"
            description="在当前用户登录后启动托盘与本地管理服务。"
            @update:model-value="setStartup"
          />
        </div>
        <div class="detail-list compact top-divider">
          <div><span>窗口模式</span><strong>{{ app.desktop?.windowModeLabel || '--' }}</strong></div>
          <div><span>渲染引擎</span><strong>{{ app.desktop?.renderEngine || 'WebView2' }}</strong></div>
          <div><span>WebView2</span><strong>{{ app.desktop?.runtimeVersion || '--' }}</strong></div>
          <div><span>系统托盘</span><strong>{{ app.desktop?.trayAvailable ? '可用' : '不可用' }}</strong></div>
          <div><span>单实例</span><strong>{{ app.desktop?.singleInstance ? '启用' : '关闭' }}</strong></div>
        </div>
      </AppCard>

      <AppCard>
        <div class="card-heading">
          <div><span class="eyebrow">Local storage</span><h3>数据位置</h3></div>
        </div>
        <div class="path-list settings-paths">
          <div><span>程序根目录</span><code>{{ app.status?.rootDirectory || '--' }}</code></div>
          <div><span>数据目录</span><code>{{ app.status?.dataDirectory || '--' }}</code></div>
          <div><span>管理地址</span><code>{{ app.status?.adminUrl || '--' }}</code></div>
        </div>
      </AppCard>
    </section>

    <AppCard class="about-card">
      <div class="about-mark">
        <span class="brand-node node-a" />
        <span class="brand-node node-b" />
        <span class="brand-line" />
      </div>
      <div class="about-copy">
        <h3>MCP DevDesk</h3>
        <p>Windows 本地 MCP、Cloudflare Tunnel 与开发工作区管理器。</p>
        <span>版本 {{ app.status?.version || '--' }}</span>
      </div>
      <AppButton tone="secondary" icon="info" disabled>检查更新</AppButton>
    </AppCard>
  </div>
</template>
