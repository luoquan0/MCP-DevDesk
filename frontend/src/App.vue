<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterView, useRoute, useRouter } from "vue-router";
import AppShell from "@/components/layout/AppShell.vue";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import ToastStack from "@/components/ui/ToastStack.vue";
import type { UpdateDownloadProgress } from "@/services/api";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const appStore = useAppStore();
const uiStore = useUiStore();
const route = useRoute();
const router = useRouter();
const standalonePage = computed(() => route.meta.standalone === true);
const updateProgress = ref<UpdateDownloadProgress | null>(null);
let statusPollingTimer: number | undefined;
let sharedStatePollingTimer: number | undefined;
let updateProgressHideTimer: number | undefined;
let stateEvents: EventSource | undefined;
let sharedSyncPending = false;
let appStarted = false;

const updateProgressVisible = computed(() => {
  const progress = updateProgress.value;
  return Boolean(progress && progress.stage !== "idle" && (progress.active || progress.stage === "ready" || progress.stage === "error"));
});

const updateProgressTitle = computed(() => {
  switch (updateProgress.value?.stage) {
    case "checksum": return "正在准备更新";
    case "download": return "正在下载更新";
    case "retrying": return "网络波动，正在重试";
    case "verifying": return "正在校验更新包";
    case "ready": return "更新包准备完成";
    case "error": return "更新下载失败";
    default: return "正在处理更新";
  }
});

const updateProgressMeta = computed(() => {
  const progress = updateProgress.value;
  if (!progress) return "";
  const parts: string[] = [];
  if (progress.totalBytes > 0) {
    parts.push(`${formatBytes(progress.bytesDownloaded)} / ${formatBytes(progress.totalBytes)}`);
    parts.push(`${Math.max(0, Math.min(100, progress.percent))}%`);
  } else if (progress.bytesDownloaded > 0) {
    parts.push(`已下载 ${formatBytes(progress.bytesDownloaded)}`);
  }
  if (progress.attempt > 0 && progress.maxAttempts > 0) {
    parts.push(`第 ${progress.attempt}/${progress.maxAttempts} 次`);
  }
  return parts.join(" · ");
});

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit >= 2 ? 1 : unit === 1 ? 0 : 0;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

function handleUpdateProgress(event: Event) {
  const detail = (event as CustomEvent<UpdateDownloadProgress>).detail;
  if (!detail) return;
  updateProgress.value = detail;
  if (updateProgressHideTimer) window.clearTimeout(updateProgressHideTimer);
  updateProgressHideTimer = undefined;
  if (!detail.active && (detail.stage === "ready" || detail.stage === "error")) {
    const revision = detail.updatedAt;
    updateProgressHideTimer = window.setTimeout(() => {
      if (updateProgress.value?.updatedAt === revision) updateProgress.value = null;
    }, detail.stage === "error" ? 10000 : 5000);
  }
}

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
  if (appStore.updateSettings?.checkOnStartup && appStore.updateSettings.repository) {
    void appStore.checkForUpdate(true).catch(() => undefined);
  }
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
  window.addEventListener("mcp-devdesk:update-progress", handleUpdateProgress);
  if (!standalonePage.value && !await webControlAccessReady()) return;
  await startAppForCurrentRoute();
});

watch(() => route.fullPath, () => void startAppForCurrentRoute());
watch(() => appStore.appearance, (appearance) => {
  if (appearance) uiStore.applyAppearance(appearance);
}, { deep: true });

onBeforeUnmount(() => {
  if (statusPollingTimer) window.clearInterval(statusPollingTimer);
  if (sharedStatePollingTimer) window.clearInterval(sharedStatePollingTimer);
  if (updateProgressHideTimer) window.clearTimeout(updateProgressHideTimer);
  closeStateEvents();
  document.removeEventListener("visibilitychange", refreshAfterVisibilityChange);
  window.removeEventListener("mcp-devdesk:web-auth-required", handleWebAuthRequired);
  window.removeEventListener("mcp-devdesk:update-progress", handleUpdateProgress);
});
</script>

<template>
  <RouterView v-if="standalonePage" />
  <AppShell v-else />
  <Transition name="update-progress">
    <aside
      v-if="updateProgressVisible && updateProgress"
      class="update-progress-card"
      :class="{ 'is-error': updateProgress.stage === 'error', 'is-ready': updateProgress.stage === 'ready' }"
      role="status"
      aria-live="polite"
    >
      <div class="update-progress-heading">
        <span class="update-progress-orb" />
        <div>
          <strong>{{ updateProgressTitle }}</strong>
          <small>{{ updateProgress.message }}</small>
        </div>
        <b v-if="updateProgress.totalBytes > 0">{{ Math.max(0, Math.min(100, updateProgress.percent)) }}%</b>
      </div>
      <div
        class="update-progress-track"
        :class="{ 'is-indeterminate': updateProgress.active && updateProgress.totalBytes <= 0 }"
        aria-hidden="true"
      >
        <span :style="{ width: updateProgress.totalBytes > 0 ? `${Math.max(0, Math.min(100, updateProgress.percent))}%` : '38%' }" />
      </div>
      <div v-if="updateProgressMeta" class="update-progress-meta">{{ updateProgressMeta }}</div>
    </aside>
  </Transition>
  <ToastStack />
  <ConfirmDialog />
</template>

<style scoped>
.update-progress-card {
  position: fixed;
  right: 20px;
  bottom: 44px;
  z-index: 120;
  display: grid;
  width: min(390px, calc(100vw - 32px));
  gap: 10px;
  padding: 14px 15px 12px;
  border: 1px solid var(--glass-card-border);
  border-radius: 16px;
  background: var(--glass-card-bg-strong);
  box-shadow: var(--glass-card-shadow);
  backdrop-filter: blur(24px) saturate(150%);
  -webkit-backdrop-filter: blur(24px) saturate(150%);
}

.update-progress-heading {
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}

.update-progress-heading > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.update-progress-heading strong {
  font-size: 13px;
  font-weight: 660;
}

.update-progress-heading small,
.update-progress-meta {
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.update-progress-heading b {
  color: var(--accent);
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 650;
}

.update-progress-orb {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 4px var(--accent-soft);
}

.update-progress-track {
  position: relative;
  height: 5px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--surface-muted);
}

.update-progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--accent);
  transition: width 160ms ease-out;
}

.update-progress-track.is-indeterminate span {
  animation: update-progress-slide 1.15s ease-in-out infinite;
}

.update-progress-card.is-ready .update-progress-orb,
.update-progress-card.is-ready .update-progress-track span {
  background: var(--success);
}

.update-progress-card.is-ready .update-progress-orb {
  box-shadow: 0 0 0 4px var(--success-soft);
}

.update-progress-card.is-error .update-progress-orb,
.update-progress-card.is-error .update-progress-track span {
  background: var(--danger);
}

.update-progress-card.is-error .update-progress-orb {
  box-shadow: 0 0 0 4px var(--danger-soft);
}

.update-progress-meta {
  font-family: var(--font-mono);
}

.update-progress-enter-active,
.update-progress-leave-active {
  transition: opacity 180ms ease-out, transform 180ms ease-out;
}

.update-progress-enter-from,
.update-progress-leave-to {
  opacity: 0;
  transform: translateY(8px) scale(0.98);
}

@keyframes update-progress-slide {
  0% { transform: translateX(-120%); }
  55% { transform: translateX(115%); }
  100% { transform: translateX(260%); }
}

@media (prefers-reduced-motion: reduce) {
  .update-progress-track span,
  .update-progress-enter-active,
  .update-progress-leave-active {
    transition: none;
  }

  .update-progress-track.is-indeterminate span {
    animation: none;
  }
}

@media (max-width: 720px) {
  .update-progress-card {
    right: 12px;
    bottom: 40px;
    width: calc(100vw - 24px);
  }
}
</style>
