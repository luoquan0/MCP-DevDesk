package updater

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestUpdateProxySettingsPersist(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.15", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	port := 10808
	settings, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port})
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProxyHost != host || settings.ProxyPort != port {
		t.Fatalf("proxy settings = %+v", settings)
	}
	reloaded, err := NewManager(dataDir, "0.12.15", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.ProxyHost != host || got.ProxyPort != port {
		t.Fatalf("reloaded proxy = %+v", got)
	}
}

func TestDetectSOCKS5ProxyProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		greeting := make([]byte, 3)
		if _, err := io.ReadFull(conn, greeting); err != nil {
			done <- err
			return
		}
		if greeting[0] != 0x05 {
			done <- io.ErrUnexpectedEOF
			return
		}
		_, err = conn.Write([]byte{0x05, 0x00})
		done <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mode, err := detectProxyProtocol(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if mode != "socks5" {
		t.Fatalf("mode = %q", mode)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDetectHTTPProxyProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		buffer := make([]byte, 3)
		if _, err := io.ReadFull(conn, buffer); err != nil {
			done <- err
			return
		}
		_, err = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		done <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mode, err := detectProxyProtocol(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if mode != "http" {
		t.Fatalf("mode = %q", mode)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDetectUnavailableProxyFailsQuickly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := detectProxyProtocol(ctx, address); err == nil {
		t.Fatal("expected unavailable proxy to fail")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("unavailable proxy took too long: %s", elapsed)
	}
}

func TestProxyTestHonorsContextDeadline(t *testing.T) {
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

func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.15", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	zero := 0
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &zero}); err == nil {
		t.Fatal("expected proxy without port to be rejected")
	}
	empty := ""
	port := 10808
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &port}); err == nil {
		t.Fatal("expected proxy port without host to be rejected")
	}
}

func TestUpdateProxyCanBeDisabledAgain(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.15", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	port := 10808
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port}); err != nil {
		t.Fatal(err)
	}
	manager.setCachedProxyMode("socks5")
	empty := ""
	zero := 0
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &zero}); err != nil {
		t.Fatal(err)
	}
	if manager.cachedProxyMode() != "" {
		t.Fatalf("proxy mode should reset, got %q", manager.cachedProxyMode())
	}
}

func TestProxyTestRequiresConfiguredProxy(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.15", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.TestProxy(context.Background()); err == nil {
		t.Fatal("expected missing proxy to fail")
	}
}

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
