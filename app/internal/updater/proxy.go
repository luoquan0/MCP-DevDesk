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

const proxyConnectTimeout = 4 * time.Second

type ProxyTestResult struct {
	OK        bool   `json:"ok"`
	Protocol  string `json:"protocol"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

type autoProxyRoundTripper struct {
	manager   *Manager
	httpProxy http.RoundTripper
	socks5    http.RoundTripper
}

func (t *autoProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if mode := t.manager.cachedProxyMode(); mode != "" {
		if mode == "socks5" {
			return t.socks5.RoundTrip(req)
		}
		return t.httpProxy.RoundTrip(req)
	}
	first := req.Clone(req.Context())
	response, httpErr := t.httpProxy.RoundTrip(first)
	if httpErr == nil {
		t.manager.setCachedProxyMode("http")
		return response, nil
	}
	second := req.Clone(req.Context())
	response, socksErr := t.socks5.RoundTrip(second)
	if socksErr == nil {
		t.manager.setCachedProxyMode("socks5")
		return response, nil
	}
	return nil, fmt.Errorf("proxy connection failed (HTTP: %v; SOCKS5: %v)", httpErr, socksErr)
}

func (m *Manager) updateHTTPClient() *http.Client {
	settings := m.Settings()
	if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
		return &http.Client{Transport: directUpdateTransport()}
	}
	return &http.Client{Transport: &autoProxyRoundTripper{
		manager:   m,
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

func httpProxyTransport(settings Settings) *http.Transport {
	transport := directUpdateTransport()
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort))}
	transport.Proxy = http.ProxyURL(proxyURL)
	return transport
}

func socks5ProxyTransport(settings Settings) *http.Transport {
	transport := directUpdateTransport()
	transport.DialContext = socks5DialContext(net.JoinHostPort(strings.TrimSpace(settings.ProxyHost), strconv.Itoa(settings.ProxyPort)))
	return transport
}

func (m *Manager) TestProxy(ctx context.Context) (ProxyTestResult, error) {
	settings := m.Settings()
	if strings.TrimSpace(settings.ProxyHost) == "" || settings.ProxyPort <= 0 {
		return ProxyTestResult{}, errors.New("请先填写代理 IP 和端口")
	}
	modes := []struct {
		name      string
		transport http.RoundTripper
	}{{"HTTP", httpProxyTransport(settings)}, {"SOCKS5", socks5ProxyTransport(settings)}}
	var failures []string
	for _, mode := range modes {
		attemptCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		start := time.Now()
		request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, "https://github.com/", nil)
		if err != nil {
			cancel()
			return ProxyTestResult{}, err
		}
		request.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)
		response, err := (&http.Client{Transport: mode.transport}).Do(request)
		latency := time.Since(start)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
			_ = response.Body.Close()
		}
		cancel()
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 400 {
			m.setCachedProxyMode(strings.ToLower(mode.name))
			return ProxyTestResult{OK: true, Protocol: mode.name, LatencyMS: latency.Milliseconds(), Message: fmt.Sprintf("代理可用 · %s · %d ms", mode.name, latency.Milliseconds())}, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", mode.name, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: GitHub HTTP %d", mode.name, response.StatusCode))
		}
	}
	return ProxyTestResult{}, fmt.Errorf("代理测试失败（已尝试 HTTP 和 SOCKS5）：%s", strings.Join(failures, "; "))
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
