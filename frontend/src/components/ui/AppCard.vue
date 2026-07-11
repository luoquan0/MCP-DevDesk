<script setup lang="ts">
withDefaults(defineProps<{
  padded?: boolean;
  interactive?: boolean;
}>(), {
  padded: true,
  interactive: false,
});
</script>

<template>
  <section class="app-card" :class="{ 'is-padded': padded, 'is-interactive': interactive }">
    <slot />
  </section>
</template>
