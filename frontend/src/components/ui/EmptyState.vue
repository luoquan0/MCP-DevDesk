<script setup lang="ts">
import AppIcon from "./AppIcon.vue";

withDefaults(defineProps<{
  icon?: string;
  title: string;
  description: string;
}>(), {
  icon: "folder",
});
</script>

<template>
  <div class="empty-state">
    <div class="empty-state-icon"><AppIcon :name="icon" :size="24" /></div>
    <h3>{{ title }}</h3>
    <p>{{ description }}</p>
    <div class="empty-state-actions"><slot /></div>
  </div>
</template>
