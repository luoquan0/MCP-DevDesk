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
  webControlEnabled: boolean;
  webControlPort: number;
  webControlLanEnabled: boolean;
  webControlAuthEnabled: boolean;
  coreMode: CoreMode;
  coreExecutable: string;
  goCoreExecutable: string;
  cloudflaredExecutable: string;
  toolProfile: ToolProfile;
  permissionMode: PermissionMode;
  allowNetwork: boolean;
  screenCaptureEnabled: boolean;
  fileScope: FileScope;
  domain: string;
  tunnelName: string;
  tunnelId: string;
  openBrowserOnStart: boolean;
  autoStart: boolean;
  watchdog: boolean;
  loggingEnabled: boolean;
}

export interface AppearanceSettings {
  theme: "system" | "light" | "dark";
  customColorsEnabled: boolean;
  primaryColor: string;
  secondaryColor: string;
  backgroundOpacity: number;
  hasBackgroundImage: boolean;
  backgroundRevision: number;
}

export interface UpdateSettings {
  repository: string;
  channel: "stable" | "prerelease";
  checkOnStartup: boolean;
  proxyHost: string;
  proxyPort: number;
}

export interface UpdateProxyTestResult {
  ok: boolean;
  protocol: "HTTP" | "SOCKS5" | string;
  latencyMs: number;
  message: string;
}

export interface UpdateRelease {
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
  tagName: string;
  name: string;
  notes: string;
  publishedAt?: string;
  pageUrl?: string;
  assetName: string;
}

export interface UpdateInstallResult {
  started: boolean;
  release: UpdateRelease;
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
  folder?: string;
  prompt?: string;
  addedAt: string;
  lastOpenedAt: string;
}

export interface ProjectPromptSettings {
  enabled: boolean;
  globalPrompt: string;
  maxPromptBytes: number;
}

export interface WebControlStatus {
  enabled: boolean;
  running: boolean;
  port: number;
  lanEnabled: boolean;
  authEnabled: boolean;
  passwordConfigured: boolean;
  url?: string;
  lanUrls?: string[];
  lastError?: string;
}

export interface WebControlAuthStatus {
  required: boolean;
  authenticated: boolean;
}

export interface WebControlOverview {
  version: string;
  workspace: string;
  coreMode: CoreMode;
  mcpPort: number;
  mcpRunning: boolean;
  tunnelRunning: boolean;
  localMcpUrl?: string;
  remoteMcpUrl?: string;
}

export interface ControlDirectoryEntry {
  name: string;
  path: string;
}

export interface ControlDirectoryListing {
  path: string;
  parent?: string;
  roots?: ControlDirectoryEntry[];
  directories: ControlDirectoryEntry[];
}

export interface Worktree { path: string; head: string; branch?: string; bare: boolean; detached: boolean; }
export interface ProjectDetails {
  path: string; git: boolean; branch?: string; currentCommit?: string; currentShort?: string; changedFiles: number; ahead: number; behind: number;
  hasAgents: boolean; agentsPath?: string; skills: string[]; worktrees: Worktree[];
}
export interface ProjectDiff { text: string; truncated: boolean; }
export interface GitCommit {
  hash: string;
  shortHash: string;
  author: string;
  authorEmail?: string;
  timestamp: string;
  subject: string;
  decorations: string[];
  current: boolean;
}
export interface GitHistory {
  branch?: string;
  currentCommit?: string;
  currentShort?: string;
  commits: GitCommit[];
  truncated: boolean;
}
export interface GitRollbackResult {
  previousCommit: string;
  currentCommit: string;
  backupBranch?: string;
}

export interface MCPInstance {
  id: string;
  name: string;
  projectId?: string;
  primary: boolean;
  tunnelMode: "independent";
  workspace: string;
  mcpHost: string;
  mcpPort: number;
  localMcpUrl: string;
  remoteMcpUrl?: string;
  authorizeUrl?: string;
  domain?: string;
  tunnelName?: string;
  tunnelId?: string;
  coreMode: CoreMode;
  permissionMode: PermissionMode;
  fileScope: FileScope;
  toolProfile: ToolProfile;
  allowNetwork: boolean;
  autoStart: boolean;
  watchdog: boolean;
  loggingEnabled: boolean;
  dataDirectory: string;
  mcp: ProcessStatus;
  tunnel: ProcessStatus;
  mcpPortOwner: PortOwner;
  configurationOk: boolean;
  configurationMessage?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface MCPInstanceCreateRequest {
  name: string;
  projectId?: string;
  workspace: string;
  mcpPort?: number;
  domain?: string;
  tunnelName?: string;
  coreMode?: CoreMode;
  permissionMode?: PermissionMode;
  fileScope?: FileScope;
  toolProfile?: ToolProfile;
  allowNetwork?: boolean;
  autoStart?: boolean;
  watchdog?: boolean;
  loggingEnabled?: boolean;
}

export interface MCPInstanceUpdateRequest {
  name?: string;
  projectId?: string;
  workspace?: string;
  mcpPort?: number;
  domain?: string;
  tunnelName?: string;
  coreMode?: CoreMode;
  permissionMode?: PermissionMode;
  fileScope?: FileScope;
  toolProfile?: ToolProfile;
  allowNetwork?: boolean;
  autoStart?: boolean;
  watchdog?: boolean;
  loggingEnabled?: boolean;
  confirmCoreSwitch?: boolean;
}

export interface MCPInstanceCloneRequest {
  name?: string;
  coreMode: CoreMode;
}

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
