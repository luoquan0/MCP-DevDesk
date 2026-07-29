import { defineStore } from "pinia";
import { api } from "@/services/api";
import { useUiStore } from "@/stores/ui";
import type {
  Config,
  ConfigureTunnelRequest,
  ConfigureTunnelResult,
  DesktopStatus,
  Diagnostics,
  FolderPickerResult,
  GitHistory,
  GitRollbackResult,
  LogResponse,
  MCPInstance,
  MCPInstanceCloneRequest,
  MCPInstanceCreateRequest,
  MCPInstanceUpdateRequest,
  Project,
  ProjectDetails,
  ProjectDiff,
  SecretSummary,
  SecretSaveResult,
  SecretUpdateRequest,
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
    projectHistories: {} as Record<string, GitHistory>,
    instances: [] as MCPInstance[],
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
          this.loadInstances(),
        ]);
      } finally {
        this.loading = false;
      }
    },
    async refreshStatus(silent = false) {
      if (this.refreshing) return;
      this.refreshing = true;
      try {
        if (silent) {
          this.status = await api<ServiceStatus>("/api/status");
        } else {
          const [status, instances] = await Promise.all([
            api<ServiceStatus>("/api/status"),
            api<MCPInstance[]>("/api/instances"),
          ]);
          this.status = status;
          this.instances = instances;
        }
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
    async loadInstances() {
      this.instances = await api<MCPInstance[]>("/api/instances");
      return this.instances;
    },
    async createInstance(request: MCPInstanceCreateRequest) {
      const ui = useUiStore();
      const instance = await this.runAction("create-instance", () => api<MCPInstance>("/api/instances", {
        method: "POST",
        body: request as unknown as BodyInit,
      }));
      await this.loadInstances();
      ui.toast("MCP 实例已创建", `${instance.name} · 端口 ${instance.mcpPort}`, "success");
      return instance;
    },
    async updateInstance(id: string, request: MCPInstanceUpdateRequest) {
      const ui = useUiStore();
      const instance = await this.runAction(`update-instance-${id}`, () => api<MCPInstance>(`/api/instances/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: request as unknown as BodyInit,
      }));
      await this.loadInstances();
      ui.toast("MCP 实例已更新", instance.name, "success");
      return instance;
    },
    async cloneInstance(id: string, request: MCPInstanceCloneRequest) {
      const ui = useUiStore();
      const instance = await this.runAction(`clone-instance-${id}`, () => api<MCPInstance>(`/api/instances/${encodeURIComponent(id)}/clone`, {
        method: "POST",
        body: request as unknown as BodyInit,
      }));
      await this.loadInstances();
      ui.toast("已复制为独立 MCP 实例", `${instance.name} · 端口 ${instance.mcpPort}`, "success");
      return instance;
    },
    async deleteInstance(id: string) {
      const ui = useUiStore();
      await this.runAction(`delete-instance-${id}`, () => api(`/api/instances/${encodeURIComponent(id)}`, { method: "DELETE" }));
      await this.loadInstances();
      ui.toast("MCP 实例已删除", "项目文件不会被删除。", "success");
    },
    async instanceAction(id: string, action: "start" | "stop" | "restart") {
      const ui = useUiStore();
      const instance = await this.runAction(`${action}-instance-${id}`, () => api<MCPInstance>(`/api/instances/${encodeURIComponent(id)}/${action}`, { method: "POST" }));
      await Promise.all([this.loadInstances(), id === "primary" ? this.refreshStatus(true) : Promise.resolve()]);
      ui.toast(
        action === "stop" ? "实例已停止" : action === "restart" ? "实例已重启" : "实例已启动",
        instance.name,
        "success",
      );
      return instance;
    },
    async configureInstanceTunnel(id: string, request: ConfigureTunnelRequest) {
      const ui = useUiStore();
      const result = await this.runAction(`configure-instance-tunnel-${id}`, () => api<ConfigureTunnelResult>(`/api/instances/${encodeURIComponent(id)}/cloudflare/configure`, {
        method: "POST",
        body: request as unknown as BodyInit,
      }));
      await this.loadInstances();
      ui.toast("实例 Tunnel 已配置", result.remoteMcpUrl, "success");
      return result;
    },
    async loadInstanceLog(id: string, name: string, limit = 100) {
      return api<LogResponse>(`/api/instances/${encodeURIComponent(id)}/logs?name=${encodeURIComponent(name)}&limit=${limit}`);
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
    async updateProjectPath(id: string, path: string) {
      const ui = useUiStore();
      const project = await this.runAction(`update-${id}`, () => api<Project>(`/api/projects/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: { path } as unknown as BodyInit,
      }));
      delete this.projectDetails[id];
      delete this.projectDiffs[id];
      delete this.projectHistories[id];
      await Promise.all([this.loadProjects(), this.loadConfig(), this.refreshStatus(true)]);
      ui.toast("项目路径已更新", project.path, "success");
      return project;
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
    async loadProjectHistory(id: string, limit = 200) {
      this.projectHistories[id] = await api<GitHistory>(`/api/projects/${encodeURIComponent(id)}/git/history?limit=${limit}`);
      return this.projectHistories[id];
    },
    async inspectProject(id: string) {
      return this.runAction(`inspect-${id}`, async () => {
        const details = await api<ProjectDetails>(`/api/projects/${encodeURIComponent(id)}/details`);
        this.projectDetails[id] = details;
        if (details.git) {
          const [diff, history] = await Promise.all([
            api<ProjectDiff>(`/api/projects/${encodeURIComponent(id)}/diff`),
            api<GitHistory>(`/api/projects/${encodeURIComponent(id)}/git/history?limit=200`),
          ]);
          this.projectDiffs[id] = diff;
          this.projectHistories[id] = history;
        } else {
          this.projectDiffs[id] = { text: "", truncated: false };
          this.projectHistories[id] = { commits: [], truncated: false };
        }
        return details;
      });
    },
    async rollbackProject(id: string, commit: string) {
      const result = await this.runAction(`rollback-${id}`, () => api<GitRollbackResult>(`/api/projects/${encodeURIComponent(id)}/git/rollback`, {
        method: "POST",
        body: { commit } as unknown as BodyInit,
      }));
      try {
        await this.inspectProject(id);
      } catch {
        delete this.projectDetails[id];
        delete this.projectDiffs[id];
        delete this.projectHistories[id];
      }
      return result;
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
    async pickFolder(initialPath = "", title = "选择本地项目文件夹") {
      return api<FolderPickerResult>("/api/system/pick-folder", {
        method: "POST",
        body: { initialPath, title } as unknown as BodyInit,
      });
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
    async saveConfig(update: Partial<Config> & { confirmCoreSwitch?: boolean }) {
      const ui = useUiStore();
      this.config = await this.runAction("save-config", () => api<Config>("/api/config", {
        method: "PUT",
        body: update as BodyInit,
      }));
      await this.refreshStatus(true);
      ui.toast("设置已保存", "新的配置已经写入本地数据目录。", "success");
    },
    async updateLogging(enabled: boolean) {
      const ui = useUiStore();
      this.config = await this.runAction("update-logging", () => api<Config>("/api/config", {
        method: "PUT",
        body: { loggingEnabled: enabled } as unknown as BodyInit,
      }));
      await this.loadDiagnostics();
      ui.toast(
        enabled ? "日志记录已开启" : "日志记录已关闭",
        enabled ? "各日志文件最多保留最新 100 条记录。" : "运行中的服务将停止写入新的日志记录。",
        "success",
      );
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
    async changeWorkspace(path: string) {
      const ui = useUiStore();
      this.status = await this.runAction("change-workspace", () => api<ServiceStatus>("/api/services/change-workspace", {
        method: "POST",
        body: { path } as unknown as BodyInit,
      }));
      await Promise.all([this.loadConfig(), this.loadProjects()]);
      ui.toast("工作目录已切换", "MCP 已安全重启；如果新目录启动失败，系统会自动恢复原目录。", "success");
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
    async loadLog(name: string, limit = 100) {
      return api<LogResponse>(`/api/logs?name=${encodeURIComponent(name)}&limit=${limit}`);
    },
    async revealSecrets() {
      return api<SecretSummary>("/api/secrets?reveal=true");
    },
    async generateSecret(field: "ownerPassword" | "clientId" | "clientSecret" | "tokenSecret" | "all") {
      return api<SecretSummary>("/api/secrets/generate", {
        method: "POST",
        body: { field } as unknown as BodyInit,
      });
    },
    async saveSecrets(update: SecretUpdateRequest) {
      return this.runAction("save-secrets", () => api<SecretSaveResult>("/api/secrets", {
        method: "PUT",
        body: update as unknown as BodyInit,
      }));
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
