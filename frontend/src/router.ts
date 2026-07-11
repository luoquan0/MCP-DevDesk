import { createRouter, createWebHashHistory } from "vue-router";

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", name: "overview", component: () => import("@/pages/OverviewPage.vue") },
    { path: "/projects", name: "projects", component: () => import("@/pages/ProjectsPage.vue") },
    { path: "/services", name: "services", component: () => import("@/pages/ServicesPage.vue") },
    { path: "/cloudflare", name: "cloudflare", component: () => import("@/pages/CloudflarePage.vue") },
    { path: "/logs", name: "logs", component: () => import("@/pages/LogsPage.vue") },
    { path: "/security", name: "security", component: () => import("@/pages/SecurityPage.vue") },
    { path: "/settings", name: "settings", component: () => import("@/pages/SettingsPage.vue") },
  ],
});
