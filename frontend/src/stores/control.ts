import { defineStore } from "pinia";
import { api } from "@/services/api";
import type { WebControlAuthStatus } from "@/types/api";

export const useControlStore = defineStore("control", {
  state: () => ({
    auth: null as WebControlAuthStatus | null,
  }),
  actions: {
    async loadAuth() {
      this.auth = await api<WebControlAuthStatus>("/api/control/auth/status");
      return this.auth;
    },
    async login(password: string) {
      this.auth = await api<WebControlAuthStatus>("/api/control/auth/login", {
        method: "POST",
        body: { password } as unknown as BodyInit,
      });
      return this.auth;
    },
    async logout() {
      await api("/api/control/auth/logout", { method: "POST", body: {} as unknown as BodyInit });
      this.auth = { required: true, authenticated: false };
    },
  },
});
