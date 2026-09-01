package updater

import (
	"net/http"
	"testing"
)

func TestUpdateProxySettingsPersistAndConfigureTransport(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.13", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	port := 7890
	settings, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port})
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProxyHost != host || settings.ProxyPort != port {
		t.Fatalf("proxy settings = %+v", settings)
	}

	transport, ok := manager.updateHTTPClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport type = %T", manager.updateHTTPClient().Transport)
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repository/releases/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy URL = %v", proxyURL)
	}

	reloaded, err := NewManager(dataDir, "0.12.13", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.ProxyHost != host || got.ProxyPort != port {
		t.Fatalf("reloaded proxy = %+v", got)
	}
}

func TestUpdateProxySettingsRequireCompleteAddress(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.13", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	zero := 0
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &zero}); err == nil {
		t.Fatal("expected proxy without port to be rejected")
	}
	empty := ""
	port := 7890
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &port}); err == nil {
		t.Fatal("expected proxy port without host to be rejected")
	}
	invalid := "http://127.0.0.1"
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &invalid, ProxyPort: &port}); err == nil {
		t.Fatal("expected proxy host with scheme to be rejected")
	}
}

func TestUpdateProxyCanBeDisabledAgain(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.13", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1"
	port := 7890
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &host, ProxyPort: &port}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	zero := 0
	if _, err := manager.UpdateSettings(SettingsUpdate{ProxyHost: &empty, ProxyPort: &zero}); err != nil {
		t.Fatal(err)
	}
	transport := manager.updateHTTPClient().Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("expected direct update transport after clearing proxy")
	}
}
