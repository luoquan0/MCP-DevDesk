import { defineStore } from "pinia";
import { api } from "@/services/api";
import { useUiStore } from "@/stores/ui";
import type {
  Config,
  ConfigureTunnelRequest,
  ConfigureTunnelResult,
  DesktopStatus,
  Diagnostics,
  LogResponse,
  Project,
  ProjectDetails,
  ProjectDiff,
  SecretSummary,
  ServiceStatus,
  TunnelInventory,
} from "@/types/api";

export const useAppStore = defineStore("app", {
  state: () => ({
    status: null as ServiceStatus | null,
    config: null as Config | null,
    desktop: null as DesktopStatus | null,
    diagnostics: null as Diagnostics | null,
    projects: [] as Project[],
    projectDetails: {} as Record<string, ProjectDetails>,
    projectDiffs: {} as Record<string, ProjectDiff>,
    loading: true,
    refreshing: false,
    actionPending: "" as string,
    lastUpdatedAt: null as Date | null,
    connectionError: "" as string,
  }),
  getters: {
    mcpOnline: (state) => Boolean(state.status?.mcp.running),
    tunnelOnline: (state) => Boolean(state.status?.tunnel.running),
    healthy(state): boolean {
      return Boolean(state.status?.mcp.running && (!state.status.cloudflare.tunnelId || state.status.tunnel.running));
    },
  },
  actions: {
    async bootstrap() {
      this.loading = true;
      try {
        await Promise.all([
          this.refreshStatus(true),
          this.loadConfig(),
          this.loadDesktop(),
          this.loadDiagnostics(),
          this.loadProjects(),
        ]);
      } finally {
        this.loading = false;
      }
    },
    async refreshStatus(silent = false) {
      if (!silent) this.refreshing = true;
      try {
        this.status = await api<ServiceStatus>("/api/status");
        this.lastUpdatedAt = new Date();
        this.connectionError = "";
      } catch (error) {
        this.connectionError = error instanceof Error ? error.message : String(error);
        if (!silent) useUiStore().toast("刷新状态失败", this.connectionError, "danger");
      } finally {
        this.refreshing = false;
      }
    },
    async loadConfig() {
      this.config = await api<Config>("/api/config");
    },
    async loadProjects() {
      this.projects = await api<Project[]>("/api/projects");
    },
    async addProject(path: string, name = "") {
      const ui = useUiStore();
      await this.runAction("add-project", () => api<Project>("/api/projects", {
        method: "POST",
        body: { path, name } as unknown as BodyInit,
      }));
      await this.loadProjects();
      ui.toast("项目已添加", path, "success");
    },
    async activateProject(id: string) {
      const ui = useUiStore();
      this.status = await this.runAction(`activate-${id}`, () => api<ServiceStatus>(`/api/projects/${encodeURIComponent(id)}/activate`, { method: "POST" }));
      await Promise.all([this.loadConfig(), this.loadProjects()]);
      ui.toast("项目已切换", "MCP 已使用新的工作目录，Tunnel 地址保持不变。", "success");
    },
    async removeProject(id: string) {
      const ui = useUiStore();
      await this.runAction(`remove-${id}`, () => api(`/api/projects/${encodeURIComponent(id)}`, { method: "DELETE" }));
      await this.loadProjects();
      ui.toast("项目已移除", "项目文件没有被删除。", "success");
    },
    async loadProjectDetails(id: string) {
      this.projectDetails[id] = await api<ProjectDetails>(`/api/projects/${encodeURIComponent(id)}/details`);
      return this.projectDetails[id];
    },
    async loadProjectDiff(id: string) {
      this.projectDiffs[id] = await api<ProjectDiff>(`/api/projects/${encodeURIComponent(id)}/diff`);
      return this.projectDiffs[id];
    },
    async inspectProject(id: string) {
      const [details, diff] = await this.runAction(`inspect-${id}`, () => Promise.all([
        api<ProjectDetails>(`/api/projects/${encodeURIComponent(id)}/details`),
        api<ProjectDiff>(`/api/projects/${encodeURIComponent(id)}/diff`),
      ]));
      this.projectDetails[id] = details;
      this.projectDiffs[id] = diff;
      return details;
    },
    async createWorktree(id: string, path: string, branch: string, base = "HEAD") {
      this.projectDetails[id] = await this.runAction("create-worktree", () => api<ProjectDetails>(`/api/projects/${encodeURIComponent(id)}/worktrees`, {
        method: "POST", body: { path, branch, base } as unknown as BodyInit,
      }));
    },
    async removeWorktree(id: string, path: string) {
      await this.runAction("remove-worktree", () => api(`/api/projects/${encodeURIComponent(id)}/worktrees?path=${encodeURIComponent(path)}`, { method: "DELETE" }));
      await this.loadProjectDetails(id);
    },
    async loadDesktop() {
      this.desktop = await api<DesktopStatus>("/api/system/desktop");
    },
    async loadDiagnostics() {
      this.diagnostics = await api<Diagnostics>("/api/diagnostics");
    },
    async runAction<T>(name: string, action: () => Promise<T>): Promise<T> {
      this.actionPending = name;
      try {
        return await action();
      } finally {
        this.actionPending = "";
      }
    },
    async serviceAction(action: "start" | "stop" | "restart" | "takeover") {
      const ui = useUiStore();
      await this.runAction(action, async () => {
        this.status = await api<ServiceStatus>(`/api/services/${action}`, { method: "POST" });
      });
      ui.toast(
        action === "stop" ? "服务已停止" : action === "restart" ? "服务已重新启动" : "服务已启动",
        "MCP DevDesk 已更新当前运行状态。",
      );
    },
    async saveConfig(update: Partial<Config>) {
      const ui = useUiStore();
      this.config = await this.runAction("save-config", () => api<Config>("/api/config", {
        method: "PUT",
        body: update as BodyInit,
      }));
      await this.refreshStatus(true);
      ui.toast("设置已保存", "新的配置已经写入本地数据目录。", "success");
    },
    async changePort(port: number) {
      const ui = useUiStore();
      this.status = await this.runAction("change-port", () => api<ServiceStatus>("/api/services/change-port", {
        method: "POST",
        body: { port } as unknown as BodyInit,
      }));
      await this.loadConfig();
      ui.toast("端口已切换", `MCP 与 Cloudflare Tunnel 已同步到端口 ${port}。`, "success");
    },
    async startCloudflareLogin() {
      const ui = useUiStore();
      await this.runAction("cloudflare-login", () => api("/api/cloudflare/login", { method: "POST" }));
      ui.toast("Cloudflare 授权已启动", "请在系统打开的授权页面完成登录。", "info");
    },
    async configureTunnel(request: ConfigureTunnelRequest) {
      const ui = useUiStore();
      const result = await this.runAction("configure-tunnel", () => api<ConfigureTunnelResult>("/api/cloudflare/configure", {
        method: "POST",
        body: request as unknown as BodyInit,
      }));
      await Promise.all([this.loadConfig(), this.refreshStatus(true)]);
      ui.toast("固定域名配置完成", result.remoteMcpUrl, "success");
      return result;
    },
    async syncTunnelPort() {
      const ui = useUiStore();
      this.status = await this.runAction("sync-tunnel", () => api<ServiceStatus>("/api/tunnels/sync-port", { method: "POST" }));
      ui.toast("Tunnel 已同步", this.status.tunnelInventory.expectedLocalUrl, "success");
    },
    async stopTunnelProcess(pid: number) {
      const ui = useUiStore();
      const inventory = await this.runAction(`stop-tunnel-${pid}`, () => api<TunnelInventory>(`/api/tunnels/processes/${pid}`, { method: "DELETE" }));
      if (this.status) this.status.tunnelInventory = inventory;
      ui.toast("隧道进程已关闭", `cloudflared PID ${pid} 已结束。`, "success");
    },
    async loadLog(name: string, limit = 800) {
      return api<LogResponse>(`/api/logs?name=${encodeURIComponent(name)}&limit=${limit}`);
    },
    async revealSecrets() {
      return api<SecretSummary>("/api/secrets?reveal=true");
    },
    async updateStartup(enabled: boolean) {
      const ui = useUiStore();
      this.desktop = await api<DesktopStatus>("/api/system/startup", {
        method: "PUT",
        body: { enabled } as unknown as BodyInit,
      });
      ui.toast(enabled ? "已启用登录时启动" : "已关闭登录时启动", "该设置仅影响当前 Windows 用户。", "success");
    },
  },
});
