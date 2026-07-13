export type PermissionMode = "safe" | "trusted" | "dangerous";
export type FileScope = "workspace" | "roots" | "computer";
export type ToolProfile = "full" | "read-only" | "compat-readonly-all";
export type CoreMode = "legacy" | "go";

export interface ProcessStatus {
  name: string;
  running: boolean;
  managed?: boolean;
  pid?: number;
  startedAt?: string;
  stoppedAt?: string;
  exitCode?: number;
  lastError?: string;
  stdoutPath?: string;
  stderrPath?: string;
}

export interface PortOwner {
  occupied: boolean;
  pid?: number;
  parentPid?: number;
  processName?: string;
  processPath?: string;
  managed: boolean;
}

export interface TunnelProcess {
  pid: number;
  parentPid?: number;
  processPath?: string;
  commandLine?: string;
  tunnelName?: string;
  tunnelId?: string;
  credentialsPath?: string;
  localUrl?: string;
  localHost?: string;
  localPort?: number;
  managed: boolean;
  matchesConfig: boolean;
  duplicate: boolean;
}

export interface TunnelInventory {
  count: number;
  matchingCount: number;
  duplicateCount: number;
  expectedLocalUrl: string;
  processes: TunnelProcess[];
}

export interface CloudflareStatus {
  installed: boolean;
  authenticated: boolean;
  loginInProgress: boolean;
  certificatePath?: string;
  credentialsPath?: string;
  domain?: string;
  tunnelName?: string;
  tunnelId?: string;
}

export interface ServiceStatus {
  version: string;
  rootDirectory: string;
  dataDirectory: string;
  adminUrl: string;
  localMcpUrl: string;
  remoteMcpUrl?: string;
  authorizeUrl?: string;
  oauthClientId: string;
  oauthClientType: string;
  oauthTokenAuth: string;
  coreMode: CoreMode;
  mcp: ProcessStatus;
  mcpPortOwner: PortOwner;
  tunnel: ProcessStatus;
  tunnelInventory: TunnelInventory;
  cloudflare: CloudflareStatus;
  permissionMode: PermissionMode;
  fileScope: FileScope;
  allowNetwork: boolean;
  toolProfile: ToolProfile;
  configurationOk: boolean;
  configurationMessage?: string;
}

export interface Config {
  workspace: string;
  allowedRoots: string[];
  mcpHost: string;
  mcpPort: number;
  adminHost: string;
  adminPort: number;
  coreMode: CoreMode;
  coreExecutable: string;
  goCoreExecutable: string;
  cloudflaredExecutable: string;
  toolProfile: ToolProfile;
  permissionMode: PermissionMode;
  allowNetwork: boolean;
  fileScope: FileScope;
  domain: string;
  tunnelName: string;
  tunnelId: string;
  openBrowserOnStart: boolean;
  autoStart: boolean;
  watchdog: boolean;
}

export interface DesktopStatus {
  available: boolean;
  appMode?: boolean;
  nativeWindow?: boolean;
  renderEngine?: string;
  runtimeVersion?: string;
  edgePath?: string;
  startupEnabled: boolean;
  trayAvailable: boolean;
  singleInstance: boolean;
  dashboardUrl: string;
  windowModeLabel: string;
}

export interface FolderPickerResult {
  path: string;
  canceled: boolean;
}

export interface Project {
  id: string;
  name: string;
  path: string;
  addedAt: string;
  lastOpenedAt: string;
}

export interface Worktree { path: string; head: string; branch?: string; bare: boolean; detached: boolean; }
export interface ProjectDetails {
  path: string; git: boolean; branch?: string; changedFiles: number; ahead: number; behind: number;
  hasAgents: boolean; agentsPath?: string; skills: string[]; worktrees: Worktree[];
}
export interface ProjectDiff { text: string; truncated: boolean; }

export interface LogResponse {
  name: string;
  path: string;
  lines: string[];
  truncated: boolean;
}

export interface SecretSummary {
  ownerPassword?: string;
  clientId?: string;
  clientSecret?: string;
  tokenSecret?: string;
  configured: boolean;
  encryptedAtRest: boolean;
  redirectUris?: string[];
}

export interface SecretUpdateRequest {
  ownerPassword?: string;
  clientId?: string;
  clientSecret?: string;
  tokenSecret?: string;
  redirectUris?: string[];
  restart: boolean;
}

export interface SecretSaveResult {
  secrets: SecretSummary;
  restarted: boolean;
  restartRequired: boolean;
  restartError?: string;
}

export type Diagnostics = Record<string, string | number | boolean | null>;

export interface ConfigureTunnelRequest {
  domain: string;
  tunnelName: string;
  reuse: boolean;
}

export interface ConfigureTunnelResult {
  message: string;
  domain: string;
  tunnelName: string;
  tunnelId: string;
  remoteMcpUrl: string;
}
