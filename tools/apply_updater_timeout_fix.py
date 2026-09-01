from pathlib import Path


def replace(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected snippet not found in {path}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")


p = Path("frontend/src/services/api.ts")
text = p.read_text(encoding="utf-8")
old = '''export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const request: RequestInit = {
    ...options,
    headers: { ...(options.headers ?? {}) },
  };
'''
new = '''export interface ApiRequestInit extends RequestInit {
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
'''
if old not in text:
    raise SystemExit("api function header not found")
text = text.replace(old, new, 1)
old = '''    return payload as T;
  } finally {
    if (stopProgressPolling) await stopProgressPolling();
  }
}
'''
new = '''    return payload as T;
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
'''
if old not in text:
    raise SystemExit("api finally block not found")
p.write_text(text.replace(old, new, 1), encoding="utf-8")

replace(
    "frontend/src/pages/SettingsPage.vue",
    '''  const proxy = updateProxyPayload(!auto);
  if (!proxy || updateSettingsSaving.value) return;
  updateSettingsSaving.value = true;
''',
    '''  const proxy = updateProxyPayload(!auto);
  if (!proxy) return;
  if (updateSettingsSaving.value) {
    updateActionFeedback.value = "上一轮更新设置仍在保存，最多 6 秒会自动结束";
    return;
  }
  updateSettingsSaving.value = true;
''',
)

replace(
    "frontend/src/pages/SettingsPage.vue",
    '''async function testUpdateProxy() {
  markUpdateAction("测试代理");
  const proxy = updateProxyPayload();
''',
    '''async function testUpdateProxy() {
  if (updateProxyTesting.value) {
    updateActionFeedback.value = "上一轮代理测试仍在进行，最多 14 秒会自动结束";
    return;
  }
  markUpdateAction("测试代理");
  const proxy = updateProxyPayload();
''',
)
replace(
    "frontend/src/pages/SettingsPage.vue",
    '''  if (updateProxyTesting.value) return;
  updateProxyTesting.value = true;
''',
    '''  updateProxyTesting.value = true;
''',
)

replace(
    "frontend/src/pages/SettingsPage.vue",
    '''async function checkForUpdate() {
  markUpdateAction("检查更新");
  const proxy = updateProxyPayload();
  if (!proxy || updateChecking.value) return;
''',
    '''async function checkForUpdate() {
  if (updateChecking.value) {
    updateActionFeedback.value = "上一轮检查仍在进行，最多 19 秒会自动结束";
    return;
  }
  markUpdateAction("检查更新");
  const proxy = updateProxyPayload();
  if (!proxy) return;
''',
)

replace(
    "app/internal/web/server.go",
    "context.WithTimeout(r.Context(), 13*time.Second)",
    "context.WithTimeout(r.Context(), 11*time.Second)",
)
replace(
    "app/internal/web/server.go",
    "context.WithTimeout(r.Context(), 20*time.Second)",
    "context.WithTimeout(r.Context(), 16*time.Second)",
)
replace(
    "app/internal/updater/proxy.go",
    "proxyConnectTimeout        = 3 * time.Second",
    "proxyConnectTimeout        = 2 * time.Second",
)
replace(
    "app/internal/updater/proxy.go",
    "proxyProbeTimeout          = 1200 * time.Millisecond",
    "proxyProbeTimeout          = 900 * time.Millisecond",
)
replace(
    "app/internal/updater/proxy.go",
    "proxyResponseHeaderTimeout = 6 * time.Second",
    "proxyResponseHeaderTimeout = 4 * time.Second",
)
replace(
    "app/internal/updater/proxy.go",
    "proxyFallbackTimeout       = 6 * time.Second",
    "proxyFallbackTimeout       = 4 * time.Second",
)

p = Path("app/internal/updater/proxy_test.go")
text = p.read_text(encoding="utf-8")
marker = "func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {"
test = '''func TestProxyTestHonorsContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buffer := make([]byte, 32)
				_, _ = c.Read(buffer)
				<-time.After(5 * time.Second)
			}(conn)
		}
	}()

	manager, err := NewManager(t.TempDir(), "0.12.18", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port}); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if _, err := manager.TestProxy(ctx); err == nil {
		t.Fatal("expected proxy test to fail on context deadline")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("proxy test ignored context deadline: %s", elapsed)
	}
	_ = listener.Close()
	<-done
}

'''
if "TestProxyTestHonorsContextDeadline" not in text:
    if marker not in text:
        raise SystemExit("proxy test insertion marker missing")
    text = text.replace(marker, test + marker, 1)
    text = text.replace('"net"\n\t"testing"', '"net"\n\t"strconv"\n\t"testing"', 1)
    p.write_text(text, encoding="utf-8")

Path("tools/apply_updater_timeout_fix.py").unlink()
