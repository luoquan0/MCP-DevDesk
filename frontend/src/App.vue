<script setup lang="ts">
import { onBeforeUnmount, onMounted } from "vue";
import AppShell from "@/components/layout/AppShell.vue";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ToastStack from "@/components/ui/ToastStack.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const appStore = useAppStore();
const uiStore = useUiStore();
let pollingTimer: number | undefined;

onMounted(async () => {
  uiStore.initializeTheme();
  await appStore.bootstrap();
  pollingTimer = window.setInterval(() => appStore.refreshStatus(true), 4000);
});

onBeforeUnmount(() => {
  if (pollingTimer) window.clearInterval(pollingTimer);
});
</script>

<template>
  <AppShell />
  <ToastStack />
  <ConfirmDialog />
</template>
