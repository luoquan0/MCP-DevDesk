<script setup lang="ts">
withDefaults(defineProps<{
  tone?: "success" | "warning" | "danger" | "neutral" | "info";
  dot?: boolean;
}>(), {
  tone: "neutral",
  dot: true,
});
</script>

<template>
  <span class="status-pill" :class="`is-${tone}`">
    <span v-if="dot" class="status-pill-dot" />
    <slot />
  </span>
</template>
