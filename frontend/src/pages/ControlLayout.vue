<script setup lang="ts">
import { computed, onMounted } from "vue";
import { RouterLink, RouterView, useRouter } from "vue-router";
import AppButton from "@/components/ui/AppButton.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { useControlStore } from "@/stores/control";

const control = useControlStore();
const router = useRouter();
const ready = computed(() => !control.loading && Boolean(control.overview));

onMounted(async () => {
  const auth = await control.loadAuth();
  if (auth.required && !auth.authenticated) {
    await router.replace({ name: "control-login" });
    return;
  }
  await control.bootstrap();
});

async function logout() {
  await control.logout();
  await router.replace({ name: "control-login" });
}
</script>

<template>
  <div class="mobile-control-shell">
    <header class="mobile-control-header">
      <div class="mobile-control-brand">
        <span class="mobile-control-logo"><AppIcon name="network" :size="20" /></span>
        <div>
          <strong>MCP DevDesk</strong>
          <small>{{ control.activeProject?.name || '网页控制台' }}</small>
        </div>
      </div>
      <div class="mobile-control-header-actions">
        <StatusPill :tone="control.overview?.mcpRunning ? 'success' : 'neutral'">
          {{ control.overview?.mcpRunning ? 'MCP 在线' : 'MCP 停止' }}
        </StatusPill>
        <AppButton v-if="control.auth?.required" tone="quiet" @click="logout">退出</AppButton>
      </div>
    </header>

    <div v-if="!ready" class="mobile-control-loading">正在读取电脑状态…</div>
    <main v-else class="mobile-control-content">
      <RouterView />
    </main>

    <nav class="mobile-control-nav" aria-label="网页控制导航">
      <RouterLink :to="{ name: 'control-projects' }"><AppIcon name="folder" :size="18" /><span>项目</span></RouterLink>
      <RouterLink :to="{ name: 'control-prompts' }"><AppIcon name="settings" :size="18" /><span>提示词</span></RouterLink>
      <RouterLink :to="{ name: 'control-services' }"><AppIcon name="network" :size="18" /><span>服务</span></RouterLink>
    </nav>
  </div>
</template>
