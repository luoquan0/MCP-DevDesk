import { defineStore } from "pinia";
import { api } from "@/services/api";
import type {
  ControlDirectoryListing,
  Project,
  ProjectPromptSettings,
  ServiceStatus,
  WebControlAuthStatus,
  WebControlOverview,
} from "@/types/api";

const normalizePath = (value = "") => value.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLocaleLowerCase();

export const useControlStore = defineStore("control", {
  state: () => ({
    auth: null as WebControlAuthStatus | null,
    overview: null as WebControlOverview | null,
    projects: [] as Project[],
    promptSettings: null as ProjectPromptSettings | null,
    loading: false,
    actionPending: "",
  }),
  getters: {
    activeProject(state): Project | undefined {
      return state.projects.find((project) => normalizePath(project.path) === normalizePath(state.overview?.workspace));
    },
  },
  actions: {
    async runAction<T>(name: string, task: () => Promise<T>): Promise<T> {
      this.actionPending = name;
      try {
        return await task();
      } finally {
        if (this.actionPending === name) this.actionPending = "";
      }
    },
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
    async bootstrap() {
      this.loading = true;
      try {
        await Promise.all([this.loadOverview(), this.loadProjects(), this.loadPromptSettings()]);
      } finally {
        this.loading = false;
      }
    },
    async loadOverview() {
      this.overview = await api<WebControlOverview>("/api/control/overview");
      return this.overview;
    },
    async loadProjects() {
      this.projects = await api<Project[]>("/api/control/projects");
      return this.projects;
    },
    async loadPromptSettings() {
      this.promptSettings = await api<ProjectPromptSettings>("/api/control/prompt-settings");
      return this.promptSettings;
    },
    async browseDirectories(path = "") {
      const query = path ? `?path=${encodeURIComponent(path)}` : "";
      return api<ControlDirectoryListing>(`/api/control/directories${query}`);
    },
    async addProject(path: string, name = "") {
      const project = await this.runAction("add-project", () => api<Project>("/api/control/projects", {
        method: "POST",
        body: { name, path } as unknown as BodyInit,
      }));
      await this.loadProjects();
      return project;
    },
    async updateProjectPath(id: string, path: string) {
      const project = await this.runAction(`path-${id}`, () => api<Project>(`/api/control/projects/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: { path } as unknown as BodyInit,
      }));
      await Promise.all([this.loadProjects(), this.loadOverview()]);
      return project;
    },
    async removeProject(id: string) {
      await this.runAction(`remove-${id}`, () => api(`/api/control/projects/${encodeURIComponent(id)}`, { method: "DELETE" }));
      await this.loadProjects();
    },
    async activateProject(id: string) {
      await this.runAction(`activate-${id}`, () => api<ServiceStatus>(`/api/control/projects/${encodeURIComponent(id)}/activate`, {
        method: "POST",
        body: {} as unknown as BodyInit,
      }));
      await Promise.all([this.loadOverview(), this.loadProjects()]);
    },
    async saveProjectPrompt(id: string, prompt: string) {
      const project = await this.runAction(`prompt-${id}`, () => api<Project>(`/api/control/projects/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: { prompt } as unknown as BodyInit,
      }));
      await this.loadProjects();
      return project;
    },
    async savePromptSettings(enabled: boolean, globalPrompt: string) {
      this.promptSettings = await this.runAction("global-prompt", () => api<ProjectPromptSettings>("/api/control/prompt-settings", {
        method: "PUT",
        body: { enabled, globalPrompt } as unknown as BodyInit,
      }));
      return this.promptSettings;
    },
    async serviceAction(action: "start" | "stop" | "restart") {
      await this.runAction(`service-${action}`, () => api<ServiceStatus>(`/api/control/services/${action}`, {
        method: "POST",
        body: {} as unknown as BodyInit,
      }));
      await this.loadOverview();
    },
  },
});
