import { createRouter, createWebHashHistory } from "vue-router";

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", name: "overview", component: () => import("@/pages/OverviewPage.vue") },
    { path: "/workspace", name: "workspace", component: () => import("@/pages/WorkspaceHubPage.vue") },
    { path: "/projects", redirect: "/workspace" },
    { path: "/instances", redirect: "/workspace" },
    { path: "/services", redirect: "/workspace" },
    { path: "/cloudflare", name: "cloudflare", component: () => import("@/pages/CloudflarePage.vue") },
    { path: "/logs", name: "logs", component: () => import("@/pages/LogsPage.vue") },
    { path: "/security", name: "security", component: () => import("@/pages/SecurityPage.vue") },
    { path: "/settings", name: "settings", component: () => import("@/pages/SettingsPage.vue") },
    { path: "/control/login", name: "control-login", component: () => import("@/pages/ControlLoginPage.vue"), meta: { standalone: true, control: true } },
    { path: "/control", redirect: "/" },
    { path: "/control/projects", redirect: "/workspace" },
    { path: "/control/prompts", redirect: "/workspace" },
    { path: "/control/services", redirect: "/workspace" },
  ],
});
