import { createRouter, createWebHashHistory } from "vue-router";

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", name: "overview", component: () => import("@/pages/OverviewPage.vue") },
    { path: "/projects", name: "projects", component: () => import("@/pages/ProjectsPage.vue") },
    { path: "/instances", name: "instances", component: () => import("@/pages/InstancesPage.vue") },
    { path: "/services", name: "services", component: () => import("@/pages/ServicesPage.vue") },
    { path: "/cloudflare", name: "cloudflare", component: () => import("@/pages/CloudflarePage.vue") },
    { path: "/logs", name: "logs", component: () => import("@/pages/LogsPage.vue") },
    { path: "/security", name: "security", component: () => import("@/pages/SecurityPage.vue") },
    { path: "/settings", name: "settings", component: () => import("@/pages/SettingsPage.vue") },
    { path: "/control/login", name: "control-login", component: () => import("@/pages/ControlLoginPage.vue"), meta: { standalone: true, control: true } },
    {
      path: "/control",
      component: () => import("@/pages/ControlLayout.vue"),
      meta: { standalone: true, control: true },
      children: [
        { path: "", redirect: { name: "control-projects" } },
        { path: "projects", name: "control-projects", component: () => import("@/pages/ControlProjectsPage.vue") },
        { path: "prompts", name: "control-prompts", component: () => import("@/pages/ControlPromptsPage.vue") },
        { path: "services", name: "control-services", component: () => import("@/pages/ControlServicesPage.vue") },
      ],
    },
  ],
});
