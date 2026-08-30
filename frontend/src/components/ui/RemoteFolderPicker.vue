<script setup lang="ts">
import { ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import { api } from "@/services/api";
import type { ControlDirectoryListing } from "@/types/api";

const props = defineProps<{
  open: boolean;
  initialPath?: string;
  title?: string;
}>();

const emit = defineEmits<{
  close: [];
  select: [path: string];
}>();

const listing = ref<ControlDirectoryListing | null>(null);
const loading = ref(false);
const error = ref("");

async function load(path = "") {
  loading.value = true;
  error.value = "";
  try {
    const query = path ? `?path=${encodeURIComponent(path)}` : "";
    listing.value = await api<ControlDirectoryListing>(`/api/control/directories${query}`);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

watch(() => props.open, (open) => {
  if (open) void load(props.initialPath || "");
}, { immediate: true });

function selectCurrent() {
  if (listing.value?.path) emit("select", listing.value.path);
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="remote-folder-backdrop" @click.self="emit('close')">
      <section class="remote-folder-dialog" role="dialog" aria-modal="true" :aria-label="title || '选择电脑目录'">
        <header class="remote-folder-header">
          <div>
            <span class="eyebrow">Computer folders</span>
            <h3>{{ title || '选择电脑目录' }}</h3>
          </div>
          <button type="button" class="remote-folder-close" aria-label="关闭" @click="emit('close')">×</button>
        </header>

        <div class="remote-folder-current">
          <span>当前位置</span>
          <code>{{ listing?.path || '此电脑' }}</code>
        </div>

        <div class="remote-folder-actions">
          <AppButton tone="secondary" icon="chevron-right" :disabled="!listing?.parent || loading" @click="load(listing?.parent || '')">上一级</AppButton>
          <AppButton tone="quiet" icon="refresh" :loading="loading" @click="load(listing?.path || '')">刷新</AppButton>
        </div>

        <div v-if="error" class="global-alert is-danger remote-folder-error">
          <AppIcon name="warning" :size="17" />
          <div><strong>无法读取目录</strong><span>{{ error }}</span></div>
        </div>

        <div class="remote-folder-list" :class="{ 'is-loading': loading }">
          <button
            v-for="entry in listing?.roots || []"
            :key="`root-${entry.path}`"
            type="button"
            class="remote-folder-row is-root"
            @click="load(entry.path)"
          >
            <span class="remote-folder-icon"><AppIcon name="folder" :size="18" /></span>
            <span><strong>{{ entry.name }}</strong><small>{{ entry.path }}</small></span>
            <AppIcon name="chevron-right" :size="16" />
          </button>
          <button
            v-for="entry in listing?.directories || []"
            :key="entry.path"
            type="button"
            class="remote-folder-row"
            @click="load(entry.path)"
          >
            <span class="remote-folder-icon"><AppIcon name="folder" :size="18" /></span>
            <span><strong>{{ entry.name }}</strong><small>{{ entry.path }}</small></span>
            <AppIcon name="chevron-right" :size="16" />
          </button>
          <div v-if="!loading && !(listing?.roots?.length || listing?.directories?.length)" class="remote-folder-empty">当前目录没有可进入的子文件夹。</div>
        </div>

        <footer class="remote-folder-footer">
          <AppButton tone="secondary" @click="emit('close')">取消</AppButton>
          <AppButton tone="primary" icon="folder" :disabled="!listing?.path" @click="selectCurrent">选择当前目录</AppButton>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
