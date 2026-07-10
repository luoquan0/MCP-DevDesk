"use strict";

const state = {
  status: null,
  config: null,
  diagnostics: null,
  activeLog: "mcp-error",
  secretsRevealed: false,
  lastAuthenticated: null,
  pollTimer: null,
};

const pages = {
  dashboard: ["OVERVIEW", "开发环境控制台"],
  cloudflare: ["FIXED DOMAIN", "Cloudflare 固定域名"],
  workspace: ["WORKSPACE", "项目与服务"],
  permissions: ["SECURITY", "权限设置"],
  logs: ["OBSERVABILITY", "运行日志"],
  diagnostics: ["DIAGNOSTICS", "环境诊断"],
};

const diagnosticLabels = {
  workspaceExists: ["工作区目录", "配置的项目路径是否存在"],
  coreExists: ["MCP 核心", "coding-tools-mcp.exe 是否存在"],
  cloudflaredExists: ["Cloudflare 客户端", "cloudflared.exe 是否存在"],
  cloudflareAuthenticated: ["Cloudflare 授权", "是否已生成 cert.pem"],
  credentialsExist: ["Tunnel 凭据", "当前 Tunnel 凭据文件是否存在"],
  mcpPortAvailable: ["MCP 端口", "端口当前是否可用于启动服务"],
  adminHostLoopback: ["管理后台绑定", "管理界面是否仅绑定本机"],
};

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

document.addEventListener("DOMContentLoaded", () => {
  bindNavigation();
  bindActions();
  initialize().catch((error) => toast("初始化失败", error.message, "error"));
});

async function initialize() {
  await Promise.all([loadConfig(), refreshStatus(), runDiagnostics()]);
  await loadLog(state.activeLog);
  state.pollTimer = window.setInterval(() => refreshStatus(true), 3000);
}

function bindNavigation() {
  $$(".nav-item").forEach((button) => {
    button.addEventListener("click", () => navigate(button.dataset.section));
  });
  $$('[data-goto]').forEach((button) => {
    button.addEventListener("click", () => navigate(button.dataset.goto));
  });
  $("#mobileMenu").addEventListener("click", () => $("#sidebar").classList.toggle("open"));
}

function navigate(section) {
  if (!pages[section]) return;
  $$(".nav-item").forEach((button) => button.classList.toggle("active", button.dataset.section === section));
  $$(".page").forEach((page) => page.classList.toggle("active", page.id === `page-${section}`));
  $("#pageEyebrow").textContent = pages[section][0];
  $("#pageTitle").textContent = pages[section][1];
  $("#sidebar").classList.remove("open");

  if (section === "logs") loadLog(state.activeLog).catch(handleError);
  if (section === "diagnostics") runDiagnostics().catch(handleError);
}

function bindActions() {
  $("#refreshButton").addEventListener("click", async (event) => {
    await withBusy(event.currentTarget, async () => {
      await Promise.all([refreshStatus(), loadConfig(), runDiagnostics()]);
      toast("状态已刷新", "已重新读取配置和服务状态。", "success");
    });
  });

  $("#startAllButton").addEventListener("click", (event) => serviceAction(event.currentTarget, "start", "服务已启动"));
  $("#stopAllButton").addEventListener("click", (event) => serviceAction(event.currentTarget, "stop", "服务已停止"));
  $("#restartAllButton").addEventListener("click", (event) => serviceAction(event.currentTarget, "restart", "服务已重新启动"));
  $("#testAndStartButton").addEventListener("click", (event) => serviceAction(event.currentTarget, "start", "服务已启动，请使用最终 URL 连接"));

  $("#cloudflareLoginButton").addEventListener("click", async (event) => {
    await withBusy(event.currentTarget, async () => {
      await api("/api/cloudflare/login", { method: "POST" });
      toast("浏览器授权已启动", "请在打开的 Cloudflare 页面登录并选择域名。", "success");
      await refreshStatus();
    });
  });

  $("#cloudflareForm").addEventListener("submit", configureCloudflare);
  $("#workspaceForm").addEventListener("submit", saveWorkspaceSettings);
  $("#permissionForm").addEventListener("submit", savePermissionSettings);

  $$('input[name="permissionMode"]').forEach((input) => input.addEventListener("change", updatePermissionCards));
  $$('input[name="fileScope"]').forEach((input) => input.addEventListener("change", updateDangerWarning));

  $("#revealSecretsButton").addEventListener("click", revealSecrets);

  $$(".copy-button").forEach((button) => {
    button.addEventListener("click", () => copyElementText(button.dataset.copyTarget));
  });

  $$(".log-tabs button").forEach((button) => {
    button.addEventListener("click", () => {
      state.activeLog = button.dataset.log;
      $$(".log-tabs button").forEach((tab) => tab.classList.toggle("active", tab === button));
      loadLog(state.activeLog).catch(handleError);
    });
  });
  $("#refreshLogsButton").addEventListener("click", (event) => withBusy(event.currentTarget, () => loadLog(state.activeLog)));
  $("#runDiagnosticsButton").addEventListener("click", (event) => withBusy(event.currentTarget, runDiagnostics));
}

async function api(path, options = {}) {
  const request = { ...options, headers: { ...(options.headers || {}) } };
  if (request.body && typeof request.body !== "string") {
    request.headers["Content-Type"] = "application/json";
    request.body = JSON.stringify(request.body);
  }
  const response = await fetch(path, request);
  const contentType = response.headers.get("Content-Type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = typeof payload === "object" && payload?.message ? payload.message : String(payload || response.statusText);
    throw new Error(message);
  }
  return payload;
}

async function loadConfig() {
  state.config = await api("/api/config");
  renderConfig(state.config);
}

async function refreshStatus(silent = false) {
  try {
    const previousAuth = state.status?.cloudflare?.authenticated ?? state.lastAuthenticated;
    state.status = await api("/api/status");
    renderStatus(state.status);
    if (previousAuth === false && state.status.cloudflare.authenticated) {
      toast("Cloudflare 授权成功", "现在可以填写固定子域名并自动创建 Tunnel。", "success");
      navigate("cloudflare");
    }
    state.lastAuthenticated = state.status.cloudflare.authenticated;
  } catch (error) {
    renderDisconnected();
    if (!silent) throw error;
  }
}

function renderConfig(config) {
  $("#workspaceInput").value = config.workspace || "";
  $("#toolProfileInput").value = config.toolProfile || "full";
  $("#mcpPortInput").value = config.mcpPort || 8765;
  $("#adminPortInput").value = config.adminPort || 17860;
  $("#openBrowserInput").checked = Boolean(config.openBrowserOnStart);
  $("#autoStartInput").checked = Boolean(config.autoStart);
  $("#watchdogInput").checked = Boolean(config.watchdog);
  $("#domainInput").value = config.domain || "";
  $("#tunnelNameInput").value = config.tunnelName || "mcp-devdesk";

  const modeInput = $(`input[name="permissionMode"][value="${escapeSelector(config.permissionMode)}"]`);
  if (modeInput) modeInput.checked = true;
  const scopeInput = $(`input[name="fileScope"][value="${escapeSelector(config.fileScope)}"]`);
  if (scopeInput) scopeInput.checked = true;
  $("#allowNetworkInput").checked = Boolean(config.allowNetwork);
  updatePermissionCards();
}

function renderStatus(status) {
  $("#sidebarVersion").textContent = status.version;
  $("#rootDirectory").textContent = status.rootDirectory;
  $("#dataDirectory").textContent = status.dataDirectory;

  setServiceBadge("mcpBadge", status.mcp.running, status.mcp.running ? "运行中" : "已停止");
  $("#mcpDetail").textContent = status.mcp.running
    ? `PID ${status.mcp.pid} · MCP 端点已启动`
    : status.mcp.lastError || "服务当前未运行";
  $("#mcpPort").textContent = status.localMcpUrl.replace(/^.*:/, "").replace("/mcp", "");

  setServiceBadge("tunnelBadge", status.tunnel.running, status.tunnel.running ? "已连接" : "未连接");
  $("#tunnelDetail").textContent = status.tunnel.running
    ? `PID ${status.tunnel.pid} · 固定域名通道在线`
    : status.tunnel.lastError || (status.cloudflare.tunnelId ? "Tunnel 当前未启动" : "尚未配置 Tunnel");
  $("#tunnelDomain").textContent = status.cloudflare.domain || "未配置";

  const permissionNames = { safe: "安全", trusted: "信任", dangerous: "危险" };
  $("#permissionBadge").textContent = `${permissionNames[status.permissionMode] || status.permissionMode}模式`;
  $("#permissionBadge").className = `badge ${status.permissionMode === "dangerous" ? "offline" : status.permissionMode === "trusted" ? "online" : "neutral"}`;
  $("#permissionDetail").textContent = permissionDescription(status.permissionMode, status.fileScope);
  $("#networkStatus").textContent = status.allowNetwork ? "允许" : "禁止";

  $("#remoteUrl").textContent = status.remoteMcpUrl || "尚未配置域名";
  $("#authorizeUrl").textContent = status.authorizeUrl || "尚未配置域名";

  const bothRunning = status.mcp.running && (!status.cloudflare.tunnelId || status.tunnel.running);
  const partlyRunning = status.mcp.running || status.tunnel.running;
  const sidebarDot = $("#sidebarStatusDot");
  sidebarDot.className = `status-dot ${bothRunning ? "online" : partlyRunning ? "warning" : "offline"}`;
  $("#sidebarStatusText").textContent = bothRunning ? "服务运行中" : partlyRunning ? "部分服务运行" : "服务已停止";

  const heroBadge = $("#heroBadge");
  if (bothRunning) {
    heroBadge.textContent = "All systems operational";
    heroBadge.className = "hero-badge success";
    $("#heroMessage").textContent = "本地 MCP 与固定域名通道正在运行，可以直接从 ChatGPT 连接和操作项目。";
  } else if (!status.configurationOk) {
    heroBadge.textContent = "Setup required";
    heroBadge.className = "hero-badge warning";
    $("#heroMessage").textContent = status.configurationMessage || "请先完成工作区和 Cloudflare 配置。";
  } else {
    heroBadge.textContent = "Ready to launch";
    heroBadge.className = "hero-badge";
    $("#heroMessage").textContent = "配置已经就绪，点击启动即可运行 MCP 服务和 Cloudflare Tunnel。";
  }

  $("#startAllButton").disabled = status.mcp.running && (status.tunnel.running || !status.cloudflare.tunnelId);
  $("#stopAllButton").disabled = !status.mcp.running && !status.tunnel.running;

  renderCloudflareStatus(status.cloudflare, status);
}

function renderCloudflareStatus(cloudflare, status) {
  const statusBox = $("#cloudflareAuthStatus");
  const dot = $(".status-dot", statusBox);
  const text = $("span:last-child", statusBox);
  const loginButton = $("#cloudflareLoginButton");

  if (cloudflare.authenticated) {
    dot.className = "status-dot online";
    text.textContent = "已授权，可以自动配置域名";
    loginButton.textContent = "重新授权 Cloudflare";
    $("#loginCard").classList.add("complete");
    $("#domainCard").classList.add("active");
    $("#cloudflareStep").textContent = cloudflare.tunnelId ? "3" : "2";
  } else if (cloudflare.loginInProgress) {
    dot.className = "status-dot warning";
    text.textContent = "等待浏览器授权完成";
    loginButton.textContent = "授权进行中……";
    loginButton.disabled = true;
    $("#cloudflareStep").textContent = "1";
  } else {
    dot.className = "status-dot offline";
    text.textContent = cloudflare.installed ? "尚未授权" : "未找到 cloudflared.exe";
    loginButton.textContent = "打开 Cloudflare 登录";
    loginButton.disabled = !cloudflare.installed;
    $("#loginCard").classList.remove("complete");
    $("#domainCard").classList.remove("active");
    $("#cloudflareStep").textContent = "1";
  }

  if (!cloudflare.loginInProgress && cloudflare.installed) loginButton.disabled = false;
  $("#resultMcpUrl").textContent = status.remoteMcpUrl || "等待配置";
  $("#resultTunnelId").textContent = cloudflare.tunnelId || "等待配置";
  if (cloudflare.tunnelId) {
    $("#resultCard").classList.add("complete", "active");
    $("#cloudflareResultText").textContent = status.tunnel.running
      ? "固定域名已配置，Tunnel 当前在线。"
      : "固定域名已配置，点击下方按钮启动服务。";
  } else {
    $("#resultCard").classList.remove("complete", "active");
  }
}

function renderDisconnected() {
  $("#sidebarStatusDot").className = "status-dot offline";
  $("#sidebarStatusText").textContent = "管理器离线";
  $("#heroBadge").textContent = "Disconnected";
  $("#heroBadge").className = "hero-badge warning";
  $("#heroMessage").textContent = "无法连接本地管理 API，请确认程序仍在运行。";
}

function setServiceBadge(id, running, text) {
  const badge = $(`#${id}`);
  badge.textContent = text;
  badge.className = `badge ${running ? "online" : "offline"}`;
}

function permissionDescription(mode, scope) {
  const modeText = {
    safe: "限制脚本和高风险命令",
    trusted: "允许常用开发与联网命令",
    dangerous: "应用层命令门控已关闭",
  }[mode] || "自定义权限";
  const scopeText = { workspace: "当前工作区", roots: "授权根目录", computer: "整台电脑" }[scope] || scope;
  return `${modeText} · ${scopeText}`;
}

async function serviceAction(button, action, successMessage) {
  await withBusy(button, async () => {
    await api(`/api/services/${action}`, { method: "POST" });
    await refreshStatus();
    toast(successMessage, action === "stop" ? "所有受管理的子进程已停止。" : "状态已更新，可在仪表盘查看连接信息。", "success");
  });
}

async function configureCloudflare(event) {
  event.preventDefault();
  const button = $("#configureTunnelButton");
  await withBusy(button, async () => {
    const result = await api("/api/cloudflare/configure", {
      method: "POST",
      body: {
        domain: $("#domainInput").value.trim(),
        tunnelName: $("#tunnelNameInput").value.trim(),
        reuse: $("#reuseTunnelInput").checked,
      },
    });
    $("#resultMcpUrl").textContent = result.remoteMcpUrl;
    $("#resultTunnelId").textContent = result.tunnelId;
    $("#cloudflareResultText").textContent = result.message;
    toast("固定域名配置完成", `${result.domain} 已指向 Tunnel ${result.tunnelName}。`, "success");
    await Promise.all([loadConfig(), refreshStatus(), runDiagnostics()]);
  });
}

async function saveWorkspaceSettings(event) {
  event.preventDefault();
  const button = $("button[type=submit]", event.currentTarget);
  await withBusy(button, async () => {
    await api("/api/config", {
      method: "PUT",
      body: {
        workspace: $("#workspaceInput").value.trim(),
        toolProfile: $("#toolProfileInput").value,
        mcpPort: Number($("#mcpPortInput").value),
        adminPort: Number($("#adminPortInput").value),
        openBrowserOnStart: $("#openBrowserInput").checked,
        autoStart: $("#autoStartInput").checked,
        watchdog: $("#watchdogInput").checked,
      },
    });
    await Promise.all([loadConfig(), refreshStatus(), runDiagnostics()]);
    toast("项目设置已保存", "运行中的服务请重新启动以应用新参数。", "success");
  });
}

async function savePermissionSettings(event) {
  event.preventDefault();
  const mode = $('input[name="permissionMode"]:checked')?.value || "safe";
  const fileScope = $('input[name="fileScope"]:checked')?.value || "workspace";

  if (mode === "dangerous" || fileScope === "computer") {
    const accepted = await confirmAction(
      "确认启用高风险权限",
      mode === "dangerous" && fileScope === "computer"
        ? "危险模式与整台电脑访问同时启用后，远程会话可以执行当前用户有权限运行的命令。兼容核心中的直接文件工具仍以工作区为根，但 Shell 可以访问工作区外路径。"
        : mode === "dangerous"
          ? "危险模式会关闭应用层命令门控，并允许联网、删除、安装软件和启动子进程。Shell 可能访问工作区外路径。"
          : "兼容核心中的直接文件工具仍以工作区为根；整台电脑能力将在新版 Go MCP 核心中完整实现。"
    );
    if (!accepted) return;
  }

  const button = $("button[type=submit]", event.currentTarget);
  await withBusy(button, async () => {
    await api("/api/config", {
      method: "PUT",
      body: {
        permissionMode: mode,
        fileScope,
        allowNetwork: $("#allowNetworkInput").checked,
      },
    });
    await Promise.all([loadConfig(), refreshStatus()]);
    toast("权限设置已保存", "重启 MCP 服务后，新权限策略将生效。", "success");
  });
}

function updatePermissionCards() {
  const mode = $('input[name="permissionMode"]:checked')?.value || "safe";
  $$(".permission-card").forEach((card) => card.classList.toggle("selected", card.dataset.mode === mode));
  const networkInput = $("#allowNetworkInput");
  if (mode === "trusted" || mode === "dangerous") {
    networkInput.checked = true;
    networkInput.disabled = true;
  } else {
    networkInput.disabled = false;
  }
  updateDangerWarning();
}

function updateDangerWarning() {
  const mode = $('input[name="permissionMode"]:checked')?.value;
  const scope = $('input[name="fileScope"]:checked')?.value;
  $("#dangerWarning").classList.toggle("visible", mode === "dangerous" || scope === "computer");
}

async function revealSecrets(event) {
  const button = event.currentTarget;
  await withBusy(button, async () => {
    if (state.secretsRevealed) {
      state.secretsRevealed = false;
      $("#ownerPassword").textContent = "••••••••••••••••";
      $("#revealSecretsButton").textContent = "显示凭证";
      return;
    }
    const secrets = await api("/api/secrets?reveal=true");
    state.secretsRevealed = true;
    $("#ownerPassword").textContent = secrets.ownerPassword || "未生成";
    $("#clientId").textContent = secrets.clientId || "mcp-devdesk";
    $("#revealSecretsButton").textContent = "隐藏凭证";
  });
}

async function runDiagnostics() {
  state.diagnostics = await api("/api/diagnostics");
  renderDiagnostics(state.diagnostics);
  renderQuickChecks(state.diagnostics);
}

function renderDiagnostics(values) {
  const grid = $("#diagnosticGrid");
  grid.replaceChildren();
  Object.entries(diagnosticLabels).forEach(([key, [title, description]]) => {
    const passed = Boolean(values[key]);
    const card = element("article", `panel diagnostic-card${passed ? "" : " fail"}`);
    const icon = element("span", "diagnostic-icon", passed ? "✓" : "!");
    const copy = element("div");
    copy.append(element("b", "", title), element("small", "", description));
    const result = element("span", "diagnostic-value", passed ? "通过" : "需要处理");
    card.append(icon, copy, result);
    grid.append(card);
  });
}

function renderQuickChecks(values) {
  const checks = [
    ["coreExists", "MCP 核心程序", "可执行文件"],
    ["cloudflaredExists", "Cloudflare 客户端", "隧道组件"],
    ["workspaceExists", "项目工作区", "本地目录"],
    ["cloudflareAuthenticated", "Cloudflare 授权", "浏览器登录"],
  ];
  const root = $("#quickChecks");
  root.replaceChildren();
  checks.forEach(([key, title, detail]) => {
    const passed = Boolean(values[key]);
    const item = element("div", "check-item");
    item.append(
      element("span", `check-symbol${passed ? "" : " fail"}`, passed ? "✓" : "!"),
      element("b", "", title),
      element("small", "", passed ? `${detail}正常` : `${detail}待处理`)
    );
    root.append(item);
  });
}

async function loadLog(name) {
  const response = await api(`/api/logs?name=${encodeURIComponent(name)}&limit=600`);
  $("#logPath").textContent = response.path;
  $("#logCount").textContent = `${response.lines.length} 行${response.truncated ? " · 仅显示末尾" : ""}`;
  $("#logView").textContent = response.lines.length ? response.lines.join("\n") : "日志文件尚未产生内容。";
  $("#logView").scrollTop = $("#logView").scrollHeight;
}

async function copyElementText(id) {
  const value = $(`#${id}`)?.textContent?.trim();
  if (!value || value.includes("尚未") || value === "••••••••••••••••") {
    toast("无法复制", "请先完成配置或显示凭证。", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    toast("已复制", value, "success");
  } catch {
    const textarea = document.createElement("textarea");
    textarea.value = value;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.append(textarea);
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
    toast("已复制", value, "success");
  }
}

async function withBusy(button, task) {
  if (button?.disabled) return;
  const originalText = button?.textContent;
  if (button) {
    button.disabled = true;
    button.textContent = "处理中……";
  }
  try {
    return await task();
  } catch (error) {
    handleError(error);
  } finally {
    if (button) {
      button.disabled = false;
      button.textContent = originalText;
    }
  }
}

function handleError(error) {
  console.error(error);
  toast("操作失败", error?.message || String(error), "error");
}

function toast(title, message, type = "success") {
  const node = element("div", `toast ${type}`);
  const icon = element("span", "toast-icon", type === "error" ? "!" : "✓");
  const copy = element("div");
  copy.append(element("b", "", title), element("span", "", message));
  node.append(icon, copy);
  $("#toastStack").append(node);
  window.setTimeout(() => {
    node.style.opacity = "0";
    node.style.transform = "translateY(6px)";
    window.setTimeout(() => node.remove(), 180);
  }, 4200);
}

function confirmAction(title, message) {
  const modal = $("#confirmModal");
  $("#confirmTitle").textContent = title;
  $("#confirmMessage").textContent = message;
  modal.hidden = false;
  return new Promise((resolve) => {
    const accept = $("#confirmAccept");
    const cancel = $("#confirmCancel");
    const finish = (value) => {
      modal.hidden = true;
      accept.removeEventListener("click", onAccept);
      cancel.removeEventListener("click", onCancel);
      resolve(value);
    };
    const onAccept = () => finish(true);
    const onCancel = () => finish(false);
    accept.addEventListener("click", onAccept);
    cancel.addEventListener("click", onCancel);
  });
}

function element(tag, className = "", text = "") {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== "") node.textContent = text;
  return node;
}

function escapeSelector(value) {
  return window.CSS?.escape ? CSS.escape(String(value)) : String(value).replace(/["\\]/g, "\\$&");
}

