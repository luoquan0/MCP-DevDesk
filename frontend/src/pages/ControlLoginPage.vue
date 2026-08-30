<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import AppButton from "@/components/ui/AppButton.vue";
import AppIcon from "@/components/ui/AppIcon.vue";
import { useControlStore } from "@/stores/control";

const control = useControlStore();
const router = useRouter();
const password = ref("");
const error = ref("");
const loading = ref(false);

onMounted(async () => {
  try {
    const status = await control.loadAuth();
    if (!status.required || status.authenticated) {
      await router.replace({ name: "overview" });
    }
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause);
  }
});

async function login() {
  error.value = "";
  loading.value = true;
  try {
    await control.login(password.value);
    await router.replace({ name: "overview" });
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="mobile-login-page">
    <section class="mobile-login-card">
      <span class="mobile-login-logo"><AppIcon name="network" :size="26" /></span>
      <div class="mobile-login-heading">
        <span class="eyebrow">MCP DevDesk LAN</span>
        <h1>网页控制登录</h1>
        <p>输入电脑端设置的网页控制密码。</p>
      </div>
      <form class="mobile-login-form" @submit.prevent="login">
        <label class="field">
          <span>密码</span>
          <input v-model="password" type="password" autocomplete="current-password" autofocus placeholder="请输入网页控制密码" />
        </label>
        <p v-if="error" class="mobile-control-error">{{ error }}</p>
        <AppButton tone="primary" :loading="loading" :disabled="!password" type="submit">登录</AppButton>
      </form>
    </section>
  </main>
</template>
