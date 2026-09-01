package updater

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestUpdateProxySettingsPersist(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.14", "owner/repository")
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
	reloaded, err := NewManager(dataDir, "0.12.14", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.ProxyHost != host || got.ProxyPort != port {
		t.Fatalf("reloaded proxy = %+v", got)
	}
}

func TestAutoProxyRoundTripperFallsBackToSOCKS5(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	primaryCalls := 0
	secondaryCalls := 0
	transport := &autoProxyRoundTripper{manager: manager,
		httpProxy: roundTripFunc(func(*http.Request) (*http.Response, error) {
			primaryCalls++
			return nil, errors.New("not an HTTP proxy")
		}),
		socks5: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			secondaryCalls++
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header), Request: request}, nil
		}),
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://github.com/", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if primaryCalls != 1 || secondaryCalls != 1 {
		t.Fatalf("calls http=%d socks=%d", primaryCalls, secondaryCalls)
	}
	if manager.cachedProxyMode() != "socks5" {
		t.Fatalf("cached proxy mode = %q", manager.cachedProxyMode())
	}
}

func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
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
	manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
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
	manager, err := NewManager(t.TempDir(), "0.12.14", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.TestProxy(context.Background()); err == nil {
		t.Fatal("expected missing proxy to fail")
	}
}
