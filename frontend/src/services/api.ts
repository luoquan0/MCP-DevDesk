export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message);
  }
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const request: RequestInit = {
    ...options,
    headers: { ...(options.headers ?? {}) },
  };

  if (request.body && typeof request.body !== "string") {
    (request.headers as Record<string, string>)["Content-Type"] = "application/json";
    request.body = JSON.stringify(request.body);
  }

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
}
