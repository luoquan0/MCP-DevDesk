from pathlib import Path


def replace(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected snippet not found in {path}: {old[:120]!r}")
    file.write_text(text.replace(old, new, 1), encoding="utf-8")


proxy = Path("app/internal/updater/proxy.go")
text = proxy.read_text(encoding="utf-8")

text = text.replace(
    '''const (
	proxyConnectTimeout        = 2 * time.Second
	proxyProbeTimeout          = 900 * time.Millisecond
	proxyResponseHeaderTimeout = 4 * time.Second
	proxyFallbackTimeout       = 4 * time.Second
)''',
    '''const (
	proxyConnectTimeout        = 3 * time.Second
	proxyProbeTimeout          = 1500 * time.Millisecond
	proxyResponseHeaderTimeout = 7 * time.Second
)''',
    1,
)

text = text.replace(
    '''	fallbackCtx, cancel := context.WithTimeout(req.Context(), proxyFallbackTimeout)
	defer cancel()
	response, fallbackErr := fallback.RoundTrip(req.Clone(fallbackCtx))
	if fallbackErr == nil {
		t.manager.setCachedProxyMode(fallbackMode)
		return response, nil
	}''',
    '''	// RoundTrip returns before callers read Response.Body. The fallback must
	// keep the original request context alive; canceling a short-lived context
	// here makes a successful fallback response unreadable.
	response, fallbackErr := fallback.RoundTrip(req.Clone(req.Context()))
	if fallbackErr == nil {
		t.manager.setCachedProxyMode(fallbackMode)
		return response, nil
	}''',
    1,
)

text = text.replace(
    '''func socks5ProxyTransport(settings Settings) *http.Transport {
	transport := proxyBaseTransport()
	transport.DialContext = socks5DialContext(net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)))
	return transport
}''',
    '''func socks5ProxyTransport(settings Settings) *http.Transport {
	transport := proxyBaseTransport()
	proxyURL := &url.URL{
		Scheme: "socks5h",
		Host:   net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)),
	}
	// Use net/http's native SOCKS5 support so context cancellation, remote DNS,
	// connection reuse, and response lifetimes are handled by the standard transport.
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport
}''',
    1,
)

start = text.index("func (m *Manager) TestProxy(ctx context.Context) (ProxyTestResult, error) {")
end = text.index("\nfunc (m *Manager) testProxyMode", start)
new_test_proxy = '''func (m *Manager) TestProxy(ctx context.Context) (ProxyTestResult, error) {
	settings := m.Settings()
	if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
		return ProxyTestResult{}, errors.New("请先填写代理 IP 和端口")
	}

	type result struct {
		mode string
		err  error
	}

	started := time.Now()
	testCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan result, 2)
	for _, mode := range []string{"socks5", "http"} {
		mode := mode
		go func() {
			results <- result{mode: mode, err: m.testProxyMode(testCtx, settings, mode)}
		}()
	}

	failures := make(map[string]error, 2)
	for i := 0; i < 2; i++ {
		select {
		case current := <-results:
			if current.err == nil {
				cancel()
				m.setCachedProxyMode(current.mode)
				latency := time.Since(started).Milliseconds()
				protocol := strings.ToUpper(current.mode)
				return ProxyTestResult{
					OK:        true,
					Protocol:  protocol,
					LatencyMS: latency,
					Message:   fmt.Sprintf("已使用代理模式 · %s · %d ms", protocol, latency),
				}, nil
			}
			failures[current.mode] = current.err
		case <-ctx.Done():
			return ProxyTestResult{}, fmt.Errorf("代理测试超时: %w", ctx.Err())
		}
	}

	return ProxyTestResult{}, fmt.Errorf(
		"代理测试失败（SOCKS5: %v；HTTP: %v）",
		failures["socks5"],
		failures["http"],
	)
}
'''
text = text[:start] + new_test_proxy + text[end:]

custom_dialer = text.find("\nfunc socks5DialContext(")
if custom_dialer == -1:
    raise SystemExit("custom SOCKS5 dialer not found")
text = text[:custom_dialer].rstrip() + "\n"
proxy.write_text(text, encoding="utf-8")


tests = Path("app/internal/updater/proxy_test.go")
test_text = tests.read_text(encoding="utf-8")
test_text = test_text.replace(
    '''import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)''',
    '''import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)''',
    1,
)

extra = r'''

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type requestContextBody struct {
	ctx  context.Context
	sent bool
}

func (b *requestContextBody) Read(buffer []byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	default:
	}
	if b.sent {
		return 0, io.EOF
	}
	b.sent = true
	return copy(buffer, "ok"), nil
}

func (b *requestContextBody) Close() error { return nil }

func TestAutoProxyFallbackKeepsResponseBodyUsable(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.20", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	manager.setCachedProxyMode("http")

	transport := &autoProxyRoundTripper{
		manager: manager,
		address: "127.0.0.1:10808",
		httpProxy: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("not an HTTP proxy")
		}),
		socks5: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       &requestContextBody{ctx: request.Context()},
				Request:    request,
			}, nil
		}),
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/rate_limit", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("fallback response body became unreadable: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("fallback body = %q", string(body))
	}
	if manager.cachedProxyMode() != "socks5" {
		t.Fatalf("cached proxy mode = %q", manager.cachedProxyMode())
	}
}

func TestSOCKS5TransportUsesNativeProxyURL(t *testing.T) {
	settings := Settings{ProxyHost: "127.0.0.1", ProxyPort: 10808}
	transport := socks5ProxyTransport(settings)
	request, err := http.NewRequest(http.MethodGet, "https://api.github.com/rate_limit", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil {
		t.Fatal("expected SOCKS5 proxy URL")
	}
	if proxyURL.Scheme != "socks5h" {
		t.Fatalf("proxy scheme = %q", proxyURL.Scheme)
	}
	if proxyURL.Host != "127.0.0.1:10808" {
		t.Fatalf("proxy host = %q", proxyURL.Host)
	}
}
'''
if "TestAutoProxyFallbackKeepsResponseBodyUsable" not in test_text:
    test_text = test_text.rstrip() + extra + "\n"
tests.write_text(test_text, encoding="utf-8")
