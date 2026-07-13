<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from "vue";
import { RouterLink, RouterView, useRoute } from "vue-router";
import AppButton from "@/components/ui/AppButton.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import CommandPalette from "./CommandPalette.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const route = useRoute();
const app = useAppStore();
const ui = useUiStore();

const navigation = [
  { to: "/", label: "概览", icon: "overview" },
  { to: "/projects", label: "项目", icon: "projects" },
  { to: "/instances", label: "MCP 实例", icon: "server" },
  { to: "/services", label: "服务", icon: "services" },
  { to: "/cloudflare", label: "Cloudflare", icon: "cloud" },
  { to: "/logs", label: "日志与诊断", icon: "logs" },
  { to: "/security", label: "权限与安全", icon: "security" },
  { to: "/settings", label: "设置", icon: "settings" },
];

const connectionTone = computed(() => app.connectionError ? "danger" : app.healthy ? "success" : "warning");
const connectionLabel = computed(() => app.connectionError ? "管理器离线" : app.healthy ? "系统正常" : "需要处理");
const lastUpdated = computed(() => app.lastUpdatedAt?.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" }) ?? "--:--:--");
const runningInstances = computed(() => app.instances.filter((instance) => instance.mcp.running).length);
const runningTunnels = computed(() => app.instances.filter((instance) => instance.tunnel.running).length);

function handleShortcut(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    ui.commandPaletteOpen = !ui.commandPaletteOpen;
  }
  if (event.key === "Escape") ui.mobileSidebarOpen = false;
}

onMounted(() => window.addEventListener("keydown", handleShortcut));
onBeforeUnmount(() => window.removeEventListener("keydown", handleShortcut));
</script>

<template>
  <div class="app-shell" :class="{ 'sidebar-compact': ui.sidebarCompact }">
    <aside class="app-sidebar" :class="{ 'is-open': ui.mobileSidebarOpen }">
      <div class="brand-block">
        <div class="brand-mark">
          <img class="brand-logo-image" src="/brand-logo.png" alt="" />
        </div>
        <div class="brand-copy">
          <strong>MCP DevDesk</strong>
          <span>{{ app.status?.version ?? 'Starting…' }}</span>
        </div>
      </div>

      <nav class="primary-nav" aria-label="主要导航">
        <RouterLink
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          :class="{ 'is-active': route.path === item.to }"
          @click="ui.mobileSidebarOpen = false"
        >
          <AppIcon :name="item.icon" :size="18" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>

      <div class="sidebar-project">
        <span class="sidebar-label">当前工作区</span>
        <div class="project-avatar"><AppIcon name="folder" :size="17" /></div>
        <div class="project-copy">
          <strong>{{ app.config?.workspace?.split(/[\\/]/).filter(Boolean).at(-1) ?? '尚未配置' }}</strong>
          <span>{{ app.config?.workspace ?? '选择本地项目目录' }}</span>
        </div>
        <AppIcon name="chevron-right" :size="15" />
      </div>

      <div class="sidebar-footer">
        <div class="sidebar-health">
          <span class="health-orb" :class="`is-${connectionTone}`" />
          <div>
            <strong>{{ connectionLabel }}</strong>
            <span>最后更新 {{ lastUpdated }}</span>
          </div>
        </div>
        <button class="sidebar-collapse" type="button" @click="ui.sidebarCompact = !ui.sidebarCompact">
          <AppIcon name="chevron-right" :size="16" />
        </button>
      </div>
    </aside>

    <div v-if="ui.mobileSidebarOpen" class="mobile-sidebar-backdrop" @click="ui.mobileSidebarOpen = false" />

    <section class="app-stage">
      <header class="app-topbar">
        <button class="mobile-menu-button" type="button" @click="ui.mobileSidebarOpen = true">
          <AppIcon name="menu" :size="20" />
        </button>
        <button class="command-trigger" type="button" @click="ui.commandPaletteOpen = true">
          <AppIcon name="search" :size="16" />
          <span>搜索或执行命令</span>
          <kbd>Ctrl K</kbd>
        </button>
        <div class="topbar-actions">
          <span class="topbar-endpoint">
            <AppIcon name="globe" :size="15" />
            {{ app.status?.remoteMcpUrl?.replace(/^https?:\/\//, '') || '本地模式' }}
          </span>
          <AppButton tone="quiet" icon="refresh" compact :loading="app.refreshing" @click="app.refreshStatus()">刷新</AppButton>
        </div>
      </header>

      <main class="app-workspace">
        <div v-if="app.connectionError" class="global-alert is-danger">
          <AppIcon name="warning" :size="18" />
          <div><strong>无法连接本地管理服务</strong><span>{{ app.connectionError }}</span></div>
        </div>
        <RouterView v-slot="{ Component }">
          <Transition name="page" mode="out-in">
            <component :is="Component" />
          </Transition>
        </RouterView>
      </main>

      <footer class="app-statusbar">
        <div class="statusbar-group">
          <span><i :class="runningInstances ? 'is-success' : 'is-muted'" /> MCP 实例 {{ runningInstances }}/{{ app.instances.length || 1 }} 运行中</span>
          <span><i :class="runningTunnels ? 'is-success' : 'is-muted'" /> Tunnel {{ runningTunnels }} 个在线</span>
          <span v-if="app.status?.tunnelInventory.duplicateCount" class="is-warning-text">重复 {{ app.status.tunnelInventory.duplicateCount }} 个</span>
        </div>
        <div class="statusbar-group">
          <span class="mono">{{ app.status?.localMcpUrl ?? 'http://127.0.0.1:--/mcp' }}</span>
          <span>WebView2</span>
        </div>
      </footer>
    </section>
  </div>
  <CommandPalette />
</template>
