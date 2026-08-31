<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from "vue";
import { RouterView, useRoute, useRouter } from "vue-router";
import AppShell from "@/components/layout/AppShell.vue";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ToastStack from "@/components/ui/ToastStack.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const appStore = useAppStore();
const uiStore = useUiStore();
const route = useRoute();
const router = useRouter();
const standalonePage = computed(() => route.meta.standalone === true);
let statusPollingTimer: number | undefined;
let sharedStatePollingTimer: number | undefined;
let stateEvents: EventSource | undefined;
let sharedSyncPending = false;
let appStarted = false;

async function syncSharedStateWhenVisible() {
  if (document.visibilityState !== "visible" || sharedSyncPending || standalonePage.value) return;
  sharedSyncPending = true;
  try {
    await appStore.syncSharedState();
  } catch (error) {
    appStore.connectionError = error instanceof Error ? error.message : String(error);
  } finally {
    sharedSyncPending = false;
  }
}

function refreshAfterVisibilityChange() {
  if (document.visibilityState === "visible") void syncSharedStateWhenVisible();
}

function closeStateEvents() {
  stateEvents?.close();
  stateEvents = undefined;
}

function connectStateEvents() {
  closeStateEvents();
  stateEvents = new EventSource("/api/events");
  stateEvents.addEventListener("state", () => void syncSharedStateWhenVisible());
  stateEvents.onerror = async () => {
    if (!appStore.webControlClient) return;
    try {
      const response = await fetch("/api/control/auth/status", { cache: "no-store", credentials: "same-origin" });
      if (!response.ok) return;
      const status = await response.json() as { required?: boolean; authenticated?: boolean };
      if (status.required && !status.authenticated) window.dispatchEvent(new CustomEvent("mcp-devdesk:web-auth-required"));
    } catch {
      // EventSource reconnects automatically; the periodic sync remains the fallback.
    }
  };
}

function handleWebAuthRequired() {
  if (!appStore.webControlClient || route.name === "control-login") return;
  appStarted = false;
  if (statusPollingTimer) window.clearInterval(statusPollingTimer);
  if (sharedStatePollingTimer) window.clearInterval(sharedStatePollingTimer);
  statusPollingTimer = undefined;
  sharedStatePollingTimer = undefined;
  closeStateEvents();
  document.removeEventListener("visibilitychange", refreshAfterVisibilityChange);
  void router.replace({ name: "control-login" });
}

async function webControlAccessReady() {
  try {
    const response = await fetch("/api/control/auth/status", { cache: "no-store", credentials: "same-origin" });
    const contentType = response.headers.get("Content-Type") || "";
    if (!response.ok || !contentType.includes("application/json")) return true;
    const status = await response.json() as { required?: boolean; authenticated?: boolean };
    if (typeof status.required !== "boolean" || typeof status.authenticated !== "boolean") return true;
    appStore.webControlClient = true;
    if (status.required && !status.authenticated) {
      if (route.name !== "control-login") await router.replace({ name: "control-login" });
      return false;
    }
    return true;
  } catch {
    return true;
  }
}

async function startAppForCurrentRoute() {
  if (appStarted || standalonePage.value) return;
  if (!await webControlAccessReady()) return;
  appStarted = true;
  await appStore.bootstrap();
  connectStateEvents();
  statusPollingTimer = window.setInterval(() => {
    if (document.visibilityState === "visible") void appStore.refreshStatus(true);
  }, 5000);
  sharedStatePollingTimer = window.setInterval(() => void syncSharedStateWhenVisible(), 30000);
  document.addEventListener("visibilitychange", refreshAfterVisibilityChange);
}

onMounted(async () => {
  uiStore.initializeTheme();
  window.addEventListener("mcp-devdesk:web-auth-required", handleWebAuthRequired);
  if (!standalonePage.value && !await webControlAccessReady()) return;
  await startAppForCurrentRoute();
});

watch(() => route.fullPath, () => void startAppForCurrentRoute());

onBeforeUnmount(() => {
  if (statusPollingTimer) window.clearInterval(statusPollingTimer);
  if (sharedStatePollingTimer) window.clearInterval(sharedStatePollingTimer);
  closeStateEvents();
  document.removeEventListener("visibilitychange", refreshAfterVisibilityChange);
  window.removeEventListener("mcp-devdesk:web-auth-required", handleWebAuthRequired);
});
</script>

<template>
  <RouterView v-if="standalonePage" />
  <AppShell v-else />
  <ToastStack />
  <ConfirmDialog />
</template>
