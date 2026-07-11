<script setup lang="ts">
defineProps<{
  modelValue: boolean;
  disabled?: boolean;
  label?: string;
  description?: string;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();
</script>

<template>
  <label class="toggle-row" :class="{ 'is-disabled': disabled }">
    <span v-if="label || description" class="toggle-copy">
      <strong v-if="label">{{ label }}</strong>
      <small v-if="description">{{ description }}</small>
    </span>
    <button
      class="toggle-switch"
      type="button"
      role="switch"
      :aria-checked="modelValue"
      :disabled="disabled"
      @click="emit('update:modelValue', !modelValue)"
    >
      <span />
    </button>
  </label>
</template>
