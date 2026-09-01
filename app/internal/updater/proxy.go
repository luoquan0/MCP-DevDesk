package updater

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// updateHTTPClient returns a client dedicated to GitHub update traffic. The
// proxy configured here never affects MCP Core, cloudflared, or project access.
func (m *Manager) updateHTTPClient() *http.Client {
	settings := m.Settings()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = metadataRequestTimeout
	transport.TLSHandshakeTimeout = 15 * time.Second
	// Empty fields explicitly mean direct access instead of inheriting an
	// unrelated HTTP_PROXY/HTTPS_PROXY environment variable.
	transport.Proxy = nil
	if host := strings.TrimSpace(settings.ProxyHost); host != "" && settings.ProxyPort > 0 {
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(host, strconv.Itoa(settings.ProxyPort)),
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport}
}

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
