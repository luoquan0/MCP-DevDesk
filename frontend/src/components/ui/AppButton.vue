<script setup lang="ts">
import AppIcon from "./AppIcon.vue";

withDefaults(defineProps<{
  tone?: "primary" | "secondary" | "quiet" | "danger";
  icon?: string;
  loading?: boolean;
  disabled?: boolean;
  type?: "button" | "submit";
  compact?: boolean;
}>(), {
  tone: "secondary",
  type: "button",
  loading: false,
  disabled: false,
  compact: false,
});
</script>

<template>
  <button
    class="app-button"
    :class="[`is-${tone}`, { 'is-loading': loading, 'is-compact': compact }]"
    :type="type"
    :disabled="disabled || loading"
  >
    <span v-if="loading" class="button-spinner" />
    <AppIcon v-else-if="icon" :name="icon" :size="16" />
    <span><slot /></span>
  </button>
</template>
