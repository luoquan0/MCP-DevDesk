package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStableChannelAllowsPrereleaseReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/mcp-devdesk/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
          "tag_name":"v0.12.33",
          "name":"MCP DevDesk v0.12.33",
          "body":"stable",
          "html_url":"https://github.com/example/mcp-devdesk/releases/tag/v0.12.33",
          "draft":false,
          "prerelease":false,
          "published_at":"2026-09-01T00:00:00Z",
          "assets":[
            {"name":"MCP-DevDesk-Portable-amd64.zip","browser_download_url":"https://github.com/example/mcp-devdesk/releases/download/v0.12.33/MCP-DevDesk-Portable-amd64.zip"},
            {"name":"MCP-DevDesk-Portable-amd64.zip.sha256","browser_download_url":"https://github.com/example/mcp-devdesk/releases/download/v0.12.33/MCP-DevDesk-Portable-amd64.zip.sha256"}
          ]
        }`)
	}))
	defer server.Close()

	manager, err := NewManager(t.TempDir(), "0.12.34-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	repository := "example/mcp-devdesk"
	channel := "stable"
	if _, err := manager.UpdateSettings(SettingsUpdate{Repository: &repository, Channel: &channel}); err != nil {
		t.Fatal(err)
	}
	manager.apiBaseURL = server.URL
	release, err := manager.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !release.UpdateAvailable || release.LatestVersion != "0.12.33" {
		t.Fatalf("prerelease return release = %+v", release)
	}
}

func TestStableChannelDoesNotDowngradeStableBuild(t *testing.T) {
	available, _ := updateAvailability("0.12.34", "0.12.33", "stable")
	if available {
		t.Fatal("stable builds must not be offered arbitrary stable downgrades")
	}
}
