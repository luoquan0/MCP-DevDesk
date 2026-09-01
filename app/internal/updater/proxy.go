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
	proxyProbeTimeout          = 1200 * time.Millisecond
	proxyResponseHeaderTimeout = 6 * time.Second
	proxyFallbackTimeout       = 6 * time.Second
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

	fallbackCtx, cancel := context.WithTimeout(req.Context(), proxyFallbackTimeout)
	defer cancel()
	response, fallbackErr := fallback.RoundTrip(req.Clone(fallbackCtx))
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
	transport.DialContext = socks5DialContext(net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)))
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

	address := net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort))
	mode, err := detectProxyProtocol(ctx, address)
	if err != nil {
		return ProxyTestResult{}, err
	}

	start := time.Now()
	if err := m.testProxyMode(ctx, settings, mode); err != nil {
		fallbackMode := "socks5"
		if mode == "socks5" {
			fallbackMode = "http"
		}
		fallbackCtx, cancel := context.WithTimeout(ctx, proxyFallbackTimeout)
		defer cancel()
		if fallbackErr := m.testProxyMode(fallbackCtx, settings, fallbackMode); fallbackErr != nil {
			return ProxyTestResult{}, fmt.Errorf("代理测试失败（%s: %v；%s: %v）", strings.ToUpper(mode), err, strings.ToUpper(fallbackMode), fallbackErr)
		}
		mode = fallbackMode
	}

	m.setCachedProxyMode(mode)
	latency := time.Since(start).Milliseconds()
	protocol := strings.ToUpper(mode)
	return ProxyTestResult{
		OK:        true,
		Protocol:  protocol,
		LatencyMS: latency,
		Message:   fmt.Sprintf("已使用代理模式 · %s · %d ms", protocol, latency),
	}, nil
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

func socks5DialContext(proxyAddress string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, targetAddress string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: proxyConnectTimeout, KeepAlive: 30 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", proxyAddress)
		if err != nil {
			return nil, err
		}
		success := false
		defer func() {
			if !success {
				_ = conn.Close()
			}
		}()

		deadline := time.Now().Add(proxyConnectTimeout)
		if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
			deadline = value
		}
		_ = conn.SetDeadline(deadline)

		if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			return nil, err
		}
		greeting := make([]byte, 2)
		if _, err := io.ReadFull(conn, greeting); err != nil {
			return nil, err
		}
		if greeting[0] != 0x05 || greeting[1] != 0x00 {
			return nil, fmt.Errorf("SOCKS5 proxy does not allow unauthenticated connections")
		}

		host, portText, err := net.SplitHostPort(targetAddress)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid target port %q", portText)
		}
		request := []byte{0x05, 0x01, 0x00}
		if ip := net.ParseIP(host); ip != nil {
			if ipv4 := ip.To4(); ipv4 != nil {
				request = append(request, 0x01)
				request = append(request, ipv4...)
			} else {
				request = append(request, 0x04)
				request = append(request, ip.To16()...)
			}
		} else {
			if len(host) == 0 || len(host) > 255 {
				return nil, errors.New("SOCKS5 target hostname is invalid")
			}
			request = append(request, 0x03, byte(len(host)))
			request = append(request, host...)
		}
		request = append(request, byte(port>>8), byte(port))
		if _, err := conn.Write(request); err != nil {
			return nil, err
		}

		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			return nil, err
		}
		if header[0] != 0x05 || header[1] != 0x00 {
			return nil, fmt.Errorf("SOCKS5 connect failed with code 0x%02x", header[1])
		}
		if err := discardSOCKS5Address(conn, header[3]); err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		success = true
		return conn, nil
	}
}

func discardSOCKS5Address(reader io.Reader, addressType byte) error {
	var length int
	switch addressType {
	case 0x01:
		length = 4
	case 0x04:
		length = 16
	case 0x03:
		size := []byte{0}
		if _, err := io.ReadFull(reader, size); err != nil {
			return err
		}
		length = int(size[0])
	default:
		return fmt.Errorf("unsupported SOCKS5 address type 0x%02x", addressType)
	}
	buffer := make([]byte, length+2)
	_, err := io.ReadFull(reader, buffer)
	return err
}
