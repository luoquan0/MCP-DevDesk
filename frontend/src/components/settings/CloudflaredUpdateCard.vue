<script setup lang="ts">
import { onMounted, ref } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppCard from "@/components/ui/AppCard.vue";
import StatusPill from "@/components/ui/StatusPill.vue";
import { api } from "@/services/api";
import { useUiStore } from "@/stores/ui";

interface CloudflaredUpdateStatus {
  installed: boolean;
  executable: string;
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
  assetName: string;
  pageUrl: string;
}
interface CloudflaredUpdateResult {
  status: CloudflaredUpdateStatus;
  previousVersion: string;
  restartedTunnels: number;
  restartErrors?: string[];
  message: string;
}

const ui = useUiStore();
const status = ref<CloudflaredUpdateStatus | null>(null);
const checking = ref(false);
const installing = ref(false);
const feedback = ref("");

async function check(showToast = true) {
  if (checking.value) return;
  checking.value = true;
  feedback.value = "正在检查 Cloudflare 官方 Release…";
  try {
    status.value = await api<CloudflaredUpdateStatus>("/api/cloudflared/update/check", { method: "POST" });
    feedback.value = status.value.updateAvailable
      ? `发现 cloudflared ${status.value.latestVersion}`
      : `cloudflared ${status.value.currentVersion || status.value.latestVersion} 已是最新`;
    if (showToast) ui.toast(status.value.updateAvailable ? "发现 cloudflared 更新" : "cloudflared 已是最新", feedback.value, status.value.updateAvailable ? "info" : "success");
  } catch (error) {
    feedback.value = error instanceof Error ? error.message : String(error);
    if (showToast) ui.toast("检查 cloudflared 更新失败", feedback.value, "danger");
  } finally {
    checking.value = false;
  }
}

async function install() {
  if (installing.value) return;
  if (!status.value?.updateAvailable) await check(false);
  if (!status.value?.updateAvailable) return;
  const accepted = await ui.ask({
    title: "更新 cloudflared",
    message: `将把 ${status.value.currentVersion || "当前版本"} 更新到 ${status.value.latestVersion}。更新时会短暂停止使用同一 cloudflared.exe 的 Tunnel，校验 SHA256 后替换文件，再自动恢复原来运行中的 Tunnel。`,
    confirmLabel: "更新 cloudflared",
  });
  if (!accepted) return;
  installing.value = true;
  feedback.value = "正在下载、校验并替换 cloudflared.exe…";
  try {
    const result = await api<CloudflaredUpdateResult>("/api/cloudflared/update/install", { method: "POST" });
    status.value = result.status;
    if (result.restartErrors?.length) {
      feedback.value = `${result.message}；${result.restartErrors.length} 个 Tunnel 恢复失败`;
      ui.toast("cloudflared 已更新，但部分 Tunnel 未恢复", result.restartErrors.join("；"), "warning");
    } else {
      feedback.value = result.message;
      ui.toast("cloudflared 更新完成", result.message, "success");
    }
  } catch (error) {
    feedback.value = error instanceof Error ? error.message : String(error);
    ui.toast("cloudflared 更新失败", feedback.value, "danger");
  } finally {
    installing.value = false;
  }
}

onMounted(() => { void check(false); });
</script>

<template>
  <AppCard class="cloudflared-update-card">
    <div class="card-heading">
      <div>
        <span class="eyebrow">Cloudflare Tunnel Runtime</span>
        <h3>cloudflared 更新</h3>
        <p>独立维护隧道程序版本。使用上方“软件更新”的代理设置连接 GitHub，并校验 Cloudflare Release 提供的 SHA256 digest。</p>
      </div>
      <StatusPill :tone="status?.updateAvailable ? 'info' : (status?.installed ? 'success' : 'warning')">
        {{ status?.updateAvailable ? `可更新 ${status.latestVersion}` : (status?.currentVersion ? `当前 ${status.currentVersion}` : '检测中') }}
      </StatusPill>
    </div>

    <div class="cloudflared-version-grid">
      <div><span>当前版本</span><strong>{{ status?.currentVersion || '--' }}</strong></div>
      <div><span>官方最新</span><strong>{{ status?.latestVersion || '--' }}</strong></div>
      <div class="cloudflared-path"><span>程序位置</span><code>{{ status?.executable || 'cloudflared.exe' }}</code></div>
    </div>

    <div class="form-footer top-divider">
      <small>{{ feedback || '进入软件设置时会自动检查一次；更新过程只重启 Tunnel，不会停止 MCP 核心。' }}</small>
      <div class="form-footer-actions">
        <AppButton tone="secondary" icon="refresh" :loading="checking" :disabled="installing" @click="check(true)">检查 cloudflared</AppButton>
        <AppButton v-if="status?.updateAvailable" tone="primary" icon="cloud" :loading="installing" :disabled="checking" @click="install">立即更新</AppButton>
      </div>
    </div>
  </AppCard>
</template>

<style scoped>
.cloudflared-version-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 16px; }
.cloudflared-version-grid > div { min-width: 0; padding: 14px 16px; border: 1px solid var(--border-subtle); border-radius: 14px; background: color-mix(in srgb, var(--surface-card) 88%, transparent); }
.cloudflared-version-grid span { display: block; color: var(--text-tertiary); font-size: 12px; margin-bottom: 6px; }
.cloudflared-version-grid strong, .cloudflared-version-grid code { display: block; overflow-wrap: anywhere; }
.cloudflared-path { grid-column: 1 / -1; }
@media (max-width: 720px) { .cloudflared-version-grid { grid-template-columns: 1fr; } .cloudflared-path { grid-column: auto; } }
</style>
