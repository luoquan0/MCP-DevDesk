package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	proxyConnectTimeout        = 3 * time.Second
	proxyProbeTimeout          = 1500 * time.Millisecond
	proxyResponseHeaderTimeout = 7 * time.Second
)

type ProxyTestResult struct {
	OK        bool   `json:"ok"`
	Protocol  string `json:"protocol"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

type autoProxyRoundTripper struct {
	manager   *Manager
	address   string
	httpProxy http.RoundTripper
	socks5    http.RoundTripper
}

func (t *autoProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	mode := t.manager.cachedProxyMode()
	if mode == "" {
		detected, err := detectProxyProtocol(req.Context(), t.address)
		if err != nil {
			return nil, fmt.Errorf("update proxy unavailable: %w", err)
		}
		mode = detected
		t.manager.setCachedProxyMode(mode)
	}

	primary, fallback := t.httpProxy, t.socks5
	fallbackMode := "socks5"
	if mode == "socks5" {
		primary, fallback = t.socks5, t.httpProxy
		fallbackMode = "http"
	}

	response, primaryErr := primary.RoundTrip(req.Clone(req.Context()))
	if primaryErr == nil {
		return response, nil
	}

	// RoundTrip returns before callers read Response.Body. The fallback must
	// keep the original request context alive; canceling a short-lived context
	// here makes a successful fallback response unreadable.
	response, fallbackErr := fallback.RoundTrip(req.Clone(req.Context()))
	if fallbackErr == nil {
		t.manager.setCachedProxyMode(fallbackMode)
		return response, nil
	}
	return nil, fmt.Errorf("proxy request failed (%s: %v; %s fallback: %v)", strings.ToUpper(mode), primaryErr, strings.ToUpper(fallbackMode), fallbackErr)
}

func (m *Manager) updateHTTPClient() *http.Client {
	settings := m.Settings()
	if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
		return &http.Client{Transport: directUpdateTransport()}
	}
	address := net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort))
	return &http.Client{Transport: &autoProxyRoundTripper{
		manager:   m,
		address:   address,
		httpProxy: httpProxyTransport(settings),
		socks5:    socks5ProxyTransport(settings),
	}}
}

func directUpdateTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: proxyConnectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = metadataRequestTimeout
	transport.TLSHandshakeTimeout = 15 * time.Second
	return transport
}

func proxyBaseTransport() *http.Transport {
	transport := directUpdateTransport()
	transport.ResponseHeaderTimeout = proxyResponseHeaderTimeout
	transport.TLSHandshakeTimeout = proxyResponseHeaderTimeout
	return transport
}

func httpProxyTransport(settings Settings) *http.Transport {
	transport := proxyBaseTransport()
	proxyURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)),
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport
}

func socks5ProxyTransport(settings Settings) *http.Transport {
	transport := proxyBaseTransport()
	proxyURL := &url.URL{
		Scheme: "socks5h",
		Host:   net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)),
	}
	// Use net/http's native SOCKS5 support so context cancellation, remote DNS,
	// connection reuse, and response lifetimes are handled by the standard transport.
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport
}

func detectProxyProtocol(ctx context.Context, proxyAddress string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, proxyProbeTimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: proxyProbeTimeout}
	conn, err := dialer.DialContext(probeCtx, "tcp", proxyAddress)
	if err != nil {
		return "", fmt.Errorf("cannot connect to %s: %w", proxyAddress, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(proxyProbeTimeout)
	if value, ok := probeCtx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return "", fmt.Errorf("probe %s: %w", proxyAddress, err)
	}

	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err == nil {
		if reply[0] == 0x05 {
			if reply[1] == 0x00 {
				return "socks5", nil
			}
			return "", fmt.Errorf("SOCKS5 proxy requires an unsupported authentication method (0x%02x)", reply[1])
		}
		return "http", nil
	}

	// An HTTP CONNECT proxy commonly waits for a full HTTP request and therefore
	// does not answer the short SOCKS greeting. A successful TCP connection with
	// no SOCKS5 greeting is enough to classify it as HTTP; the real request below
	// still has a strict response-header timeout and will report a useful error.
	return "http", nil
}

func (m *Manager) TestProxy(ctx context.Context) (ProxyTestResult, error) {
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

func (m *Manager) testProxyMode(ctx context.Context, settings Settings, mode string) error {
	requestCtx, cancel := context.WithTimeout(ctx, proxyResponseHeaderTimeout)
	defer cancel()

	endpoint := strings.TrimRight(m.apiBaseURL, "/") + "/rate_limit"
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)

	var transport http.RoundTripper = httpProxyTransport(settings)
	if mode == "socks5" {
		transport = socks5ProxyTransport(settings)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (m *Manager) cachedProxyMode() string {
	m.proxyModeMu.RLock()
	defer m.proxyModeMu.RUnlock()
	return m.proxyMode
}

func (m *Manager) setCachedProxyMode(mode string) {
	m.proxyModeMu.Lock()
	m.proxyMode = mode
	m.proxyModeMu.Unlock()
}

func (m *Manager) resetCachedProxyMode() { m.setCachedProxyMode("") }

func validateProxySettings(settings Settings) error {
	host := strings.TrimSpace(settings.ProxyHost)
	port := settings.ProxyPort
	if host == "" && port == 0 {
		return nil
	}
	if host == "" {
		return errors.New("update proxy IP/host is required when a proxy port is set")
	}
	if port < 1 || port > 65535 {
		return errors.New("update proxy port must be between 1 and 65535")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/\\@ \t\r\n") {
		return errors.New("update proxy must be an IP address or hostname without scheme, path, or credentials")
	}
	return nil
}
