<script setup lang="ts">
import AppIcon from "./AppIcon.vue";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <div class="toast-stack" aria-live="polite">
    <TransitionGroup name="toast">
      <article v-for="toast in ui.toasts" :key="toast.id" class="toast-item" :class="`is-${toast.tone}`">
        <div class="toast-icon">
          <AppIcon :name="toast.tone === 'danger' ? 'warning' : toast.tone === 'success' ? 'check' : 'info'" :size="17" />
        </div>
        <div class="toast-copy">
          <strong>{{ toast.title }}</strong>
          <span v-if="toast.message">{{ toast.message }}</span>
        </div>
        <button class="icon-button" type="button" aria-label="关闭通知" @click="ui.dismissToast(toast.id)">
          <AppIcon name="x" :size="15" />
        </button>
      </article>
    </TransitionGroup>
  </div>
</template>
