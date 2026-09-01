export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message);
  }
}

export interface UpdateDownloadProgress {
  active: boolean;
  stage: "idle" | "checksum" | "download" | "retrying" | "verifying" | "ready" | "error" | string;
  bytesDownloaded: number;
  totalBytes: number;
  percent: number;
  attempt: number;
  maxAttempts: number;
  message: string;
  updatedAt?: string;
}

const updateProgressEvent = "mcp-devdesk:update-progress";

async function readUpdateProgress(): Promise<UpdateDownloadProgress | null> {
  try {
    const response = await fetch("/api/update/settings", {
      cache: "no-store",
      credentials: "same-origin",
    });
    if (!response.ok) return null;
    const payload = await response.json() as { progress?: UpdateDownloadProgress };
    return payload.progress ?? null;
  } catch {
    return null;
  }
}

function publishUpdateProgress(progress: UpdateDownloadProgress) {
  window.dispatchEvent(new CustomEvent<UpdateDownloadProgress>(updateProgressEvent, { detail: progress }));
}

function startUpdateProgressPolling() {
  let timer: number | undefined;
  let inFlight = false;

  const poll = async () => {
    if (inFlight) return;
    inFlight = true;
    try {
      const progress = await readUpdateProgress();
      if (progress) publishUpdateProgress(progress);
    } finally {
      inFlight = false;
    }
  };

  void poll();
  timer = window.setInterval(() => void poll(), 500);

  return async () => {
    if (timer) window.clearInterval(timer);
    timer = undefined;
    await poll();
  };
}

export interface ApiRequestInit extends RequestInit {
  timeoutMs?: number;
  timeoutMessage?: string;
}

function requestTimeoutFor(path: string, method: string): number {
  if (path === "/api/update/settings") return 6000;
  if (path === "/api/update/proxy-test" && method === "POST") return 14000;
  if (path === "/api/update/check" && method === "POST") return 19000;
  return 0;
}

export async function api<T>(path: string, options: ApiRequestInit = {}): Promise<T> {
  const method = String(options.method || "GET").toUpperCase();
  const { timeoutMs: configuredTimeout, timeoutMessage, ...fetchOptions } = options;
  const timeoutMs = configuredTimeout ?? requestTimeoutFor(path, method);
  const timeoutController = timeoutMs > 0 && !fetchOptions.signal ? new AbortController() : null;
  let timeoutHandle: number | undefined;
  if (timeoutController) {
    timeoutHandle = window.setTimeout(() => timeoutController.abort(), timeoutMs);
  }
  const request: RequestInit = {
    ...fetchOptions,
    signal: timeoutController?.signal ?? fetchOptions.signal,
    headers: { ...(fetchOptions.headers ?? {}) },
  };

  if (request.body && typeof request.body !== "string") {
    (request.headers as Record<string, string>)["Content-Type"] = "application/json";
    request.body = JSON.stringify(request.body);
  }

  const installingUpdate = path === "/api/update/install" && String(request.method || "GET").toUpperCase() === "POST";
  const stopProgressPolling = installingUpdate ? startUpdateProgressPolling() : null;

  try {
    const response = await fetch(path, request);
    const contentType = response.headers.get("Content-Type") ?? "";
    const payload = contentType.includes("application/json")
      ? await response.json()
      : await response.text();

    if (!response.ok) {
      if (response.status === 401 && !path.startsWith("/api/control/auth/")) {
        window.dispatchEvent(new CustomEvent("mcp-devdesk:web-auth-required"));
      }
      const message = typeof payload === "object" && payload && "message" in payload
        ? String(payload.message)
        : String(payload || response.statusText);
      throw new ApiError(message, response.status);
    }

    return payload as T;
  } catch (error) {
    if (timeoutController?.signal.aborted) {
      throw new ApiError(
        timeoutMessage || `请求超时（${Math.ceil(timeoutMs / 1000)} 秒），请检查本机服务或代理连接。`,
        408,
      );
    }
    throw error;
  } finally {
    if (timeoutHandle !== undefined) window.clearTimeout(timeoutHandle);
    if (stopProgressPolling) await stopProgressPolling();
  }
}
