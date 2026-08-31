<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useRouter } from "vue-router";
import AppIcon from "@/components/ui/AppIcon.vue";
import { useAppStore } from "@/stores/app";
import { useUiStore } from "@/stores/ui";

const router = useRouter();
const app = useAppStore();
const ui = useUiStore();
const query = ref("");
const searchInput = ref<HTMLInputElement | null>(null);

const commands = computed(() => [
  { label: "打开概览", hint: "页面", icon: "overview", run: () => router.push("/") },
  { label: "打开项目与运行", hint: "页面", icon: "projects", run: () => router.push("/workspace") },
  { label: "打开 Cloudflare", hint: "页面", icon: "cloud", run: () => router.push("/cloudflare") },
  { label: "查看日志与诊断", hint: "页面", icon: "logs", run: () => router.push("/logs") },
  { label: "启动全部服务", hint: "动作", icon: "play", run: () => app.serviceAction("start") },
  { label: "重新启动服务", hint: "动作", icon: "restart", run: () => app.serviceAction("restart") },
  { label: "同步 Tunnel 端口", hint: "动作", icon: "network", run: () => app.syncTunnelPort() },
]);

const filtered = computed(() => {
  const normalized = query.value.trim().toLowerCase();
  return normalized
    ? commands.value.filter((command) => command.label.toLowerCase().includes(normalized))
    : commands.value;
});

watch(() => ui.commandPaletteOpen, async (open) => {
  if (open) {
    query.value = "";
    await nextTick();
    searchInput.value?.focus();
  }
});

async function execute(command: (typeof commands.value)[number]) {
  ui.commandPaletteOpen = false;
  try {
    await command.run();
  } catch (error) {
    ui.toast("操作失败", error instanceof Error ? error.message : String(error), "danger");
  }
}
</script>

<template>
  <Transition name="fade">
    <div v-if="ui.commandPaletteOpen" class="command-backdrop" @click.self="ui.commandPaletteOpen = false">
      <section class="command-palette">
        <div class="command-search">
          <AppIcon name="search" :size="18" />
          <input ref="searchInput" v-model="query" placeholder="搜索页面或执行操作…" @keydown.esc="ui.commandPaletteOpen = false" />
          <kbd>Esc</kbd>
        </div>
        <div class="command-list">
          <button v-for="command in filtered" :key="command.label" type="button" @click="execute(command)">
            <span class="command-icon"><AppIcon :name="command.icon" :size="17" /></span>
            <span>{{ command.label }}</span>
            <small>{{ command.hint }}</small>
          </button>
          <div v-if="!filtered.length" class="command-empty">没有找到匹配的命令</div>
        </div>
      </section>
    </div>
  </Transition>
</template>
