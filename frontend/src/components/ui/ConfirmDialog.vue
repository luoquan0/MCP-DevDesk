<script setup lang="ts">
import AppButton from "./AppButton.vue";
import AppIcon from "./AppIcon.vue";
import { useUiStore } from "@/stores/ui";

const ui = useUiStore();
</script>

<template>
  <Transition name="fade">
    <div v-if="ui.confirm.open" class="modal-backdrop" @click.self="ui.resolveConfirm(false)">
      <section class="confirm-dialog" role="alertdialog" aria-modal="true">
        <div class="confirm-icon" :class="{ 'is-danger': ui.confirm.danger }">
          <AppIcon :name="ui.confirm.danger ? 'warning' : 'info'" :size="22" />
        </div>
        <div class="confirm-copy">
          <h2>{{ ui.confirm.title }}</h2>
          <p>{{ ui.confirm.message }}</p>
        </div>
        <div class="confirm-actions">
          <AppButton tone="quiet" @click="ui.resolveConfirm(false)">取消</AppButton>
          <AppButton :tone="ui.confirm.danger ? 'danger' : 'primary'" @click="ui.resolveConfirm(true)">
            {{ ui.confirm.confirmLabel }}
          </AppButton>
        </div>
      </section>
    </div>
  </Transition>
</template>
