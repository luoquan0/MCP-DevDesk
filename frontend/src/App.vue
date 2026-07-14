<script setup lang="ts">
import { onBeforeUnmount, onMounted } from "vue";
import AppShell from "@/components/layout/AppShell.vue";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ToastStack from "@/components/ui/ToastStack.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const appStore = useAppStore();
const uiStore = useUiStore();
let statusPollingTimer: number | undefined;
let instancePollingTimer: number | undefined;
let instanceRefreshPending = false;

function refreshStatusWhenVisible() {
  if (document.visibilityState === "visible") void appStore.refreshStatus(true);
}

async function refreshInstancesWhenVisible() {
  if (document.visibilityState !== "visible" || instanceRefreshPending) return;
  instanceRefreshPending = true;
  try {
    await appStore.loadInstances();
  } catch (error) {
    appStore.connectionError = error instanceof Error ? error.message : String(error);
  } finally {
    instanceRefreshPending = false;
  }
}

function refreshAfterVisibilityChange() {
  refreshStatusWhenVisible();
  void refreshInstancesWhenVisible();
}

onMounted(async () => {
  uiStore.initializeTheme();
  await appStore.bootstrap();
  statusPollingTimer = window.setInterval(refreshStatusWhenVisible, 5000);
  instancePollingTimer = window.setInterval(() => void refreshInstancesWhenVisible(), 15000);
  document.addEventListener("visibilitychange", refreshAfterVisibilityChange);
});

onBeforeUnmount(() => {
  if (statusPollingTimer) window.clearInterval(statusPollingTimer);
  if (instancePollingTimer) window.clearInterval(instancePollingTimer);
  document.removeEventListener("visibilitychange", refreshAfterVisibilityChange);
});
</script>

<template>
  <AppShell />
  <ToastStack />
  <ConfirmDialog />
</template>
