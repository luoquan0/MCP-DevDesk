package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSettingsPersistAndValidate(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.7")
	if err != nil {
		t.Fatal(err)
	}
	repository := "example/mcp-devdesk"
	channel := "prerelease"
	startup := false
	settings, err := manager.UpdateSettings(SettingsUpdate{Repository: &repository, Channel: &channel, CheckOnStartup: &startup})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Repository != repository || settings.Channel != channel || settings.CheckOnStartup {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	reloaded, err := NewManager(dataDir, "0.12.7")
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Settings()
	if got.Repository != settings.Repository || got.Channel != settings.Channel || got.CheckOnStartup != settings.CheckOnStartup {
		t.Fatalf("reloaded settings = %+v, want configuration %+v", got, settings)
	}
	if got.Progress == nil || got.Progress.Stage != "idle" {
		t.Fatalf("runtime progress = %+v", got.Progress)
	}
	bad := "not-a-repository"
	if _, err := manager.UpdateSettings(SettingsUpdate{Repository: &bad}); err == nil {
		t.Fatal("expected invalid repository to be rejected")
	}
}

func TestSettingsExposeRuntimeProgressWithoutPersistingIt(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.11")
	if err != nil {
		t.Fatal(err)
	}
	repository := "example/mcp-devdesk"
	if _, err := manager.UpdateSettings(SettingsUpdate{Repository: &repository}); err != nil {
		t.Fatal(err)
	}
	manager.setProgress(DownloadProgress{
		Active:          true,
		Stage:           "download",
		BytesDownloaded: 25,
		TotalBytes:      100,
		Attempt:         2,
		Message:         "downloading",
	})
	settings := manager.Settings()
	if settings.Progress == nil || settings.Progress.Percent != 25 || settings.Progress.Attempt != 2 {
		t.Fatalf("settings progress = %+v", settings.Progress)
	}
	raw, err := os.ReadFile(manager.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "progress") || strings.Contains(string(raw), "bytesDownloaded") {
		t.Fatalf("runtime progress leaked into settings file: %s", raw)
	}
}

func TestCheckSelectsGitHubRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/mcp-devdesk/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
          "tag_name":"v0.13.0",
          "name":"0.13.0",
          "body":"new update",
          "html_url":"https://github.com/example/mcp-devdesk/releases/tag/v0.13.0",
          "draft":false,
          "prerelease":false,
          "published_at":"2026-08-31T00:00:00Z",
          "assets":[
            {"name":"MCP-DevDesk-Portable-amd64.zip","browser_download_url":"https://github.com/example/mcp-devdesk/releases/download/v0.13.0/MCP-DevDesk-Portable-amd64.zip"},
            {"name":"MCP-DevDesk-Portable-amd64.zip.sha256","browser_download_url":"https://github.com/example/mcp-devdesk/releases/download/v0.13.0/MCP-DevDesk-Portable-amd64.zip.sha256"}
          ]
        }`)
	}))
	defer server.Close()
	manager, err := NewManager(t.TempDir(), "0.12.7")
	if err != nil {
		t.Fatal(err)
	}
	repository := "example/mcp-devdesk"
	if _, err := manager.UpdateSettings(SettingsUpdate{Repository: &repository}); err != nil {
		t.Fatal(err)
	}
	manager.apiBaseURL = server.URL
	release, err := manager.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !release.UpdateAvailable || release.LatestVersion != "0.13.0" || release.AssetName != "MCP-DevDesk-Portable-amd64.zip" {
		t.Fatalf("unexpected release: %+v", release)
	}
}

func TestDownloadRequiresMatchingSHA256(t *testing.T) {
	payload := []byte("portable update payload")
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/package.zip":
			_, _ = w.Write(payload)
		case "/package.zip.sha256":
			fmt.Fprintf(w, "%s  MCP-DevDesk-Portable-amd64.zip\n", hash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manager, err := NewManager(t.TempDir(), "0.12.7")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.Download(context.Background(), Release{
		UpdateAvailable:  true,
		TagName:          "v0.13.0",
		AssetName:        "MCP-DevDesk-Portable-amd64.zip",
		AssetURL:         server.URL + "/package.zip",
		ChecksumAssetURL: server.URL + "/package.zip.sha256",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.PackagePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(payload) {
		t.Fatalf("downloaded payload = %q", raw)
	}
	progress := manager.Progress()
	if progress.Active || progress.Stage != "ready" || progress.Percent != 100 {
		t.Fatalf("successful download progress = %+v", progress)
	}

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprint(w, strings.Repeat("0", 64))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer badServer.Close()
	_, err = manager.Download(context.Background(), Release{
		UpdateAvailable:  true,
		TagName:          "v0.13.1",
		AssetName:        "MCP-DevDesk-Portable-amd64.zip",
		AssetURL:         badServer.URL + "/package.zip",
		ChecksumAssetURL: badServer.URL + "/package.zip.sha256",
	})
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("expected SHA256 mismatch, got %v", err)
	}
	progress = manager.Progress()
	if progress.Active || progress.Stage != "error" || !strings.Contains(progress.Message, "SHA256") {
		t.Fatalf("failed download progress = %+v", progress)
	}
}

func TestPackageDownloadUsesCallerDeadlineInsteadOfGlobalClientTimeout(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.11")
	if err != nil {
		t.Fatal(err)
	}
	if manager.client.Timeout != 0 {
		t.Fatalf("HTTP client timeout = %s, want no global timeout", manager.client.Timeout)
	}
	transport, ok := manager.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport type = %T", manager.client.Transport)
	}
	if transport.ResponseHeaderTimeout != metadataRequestTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, metadataRequestTimeout)
	}
}

func TestDownloadRetriesAndResumesPartialPackage(t *testing.T) {
	payload := []byte(strings.Repeat("portable-update-payload-", 128))
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	split := len(payload) / 2
	packageRequests := 0
	resumedRange := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/package.zip.sha256":
			fmt.Fprintf(w, "%s  MCP-DevDesk-Portable-amd64.zip\n", hash)
		case "/package.zip":
			packageRequests++
			if packageRequests == 1 {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(payload[:split])
				return
			}
			resumedRange = r.Header.Get("Range")
			if resumedRange != fmt.Sprintf("bytes=%d-", split) {
				http.Error(w, "unexpected range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)-split))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", split, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[split:])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager, err := NewManager(t.TempDir(), "0.12.11")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.Download(context.Background(), Release{
		UpdateAvailable:  true,
		TagName:          "v0.12.12",
		AssetName:        "MCP-DevDesk-Portable-amd64.zip",
		AssetURL:         server.URL + "/package.zip",
		ChecksumAssetURL: server.URL + "/package.zip.sha256",
	})
	if err != nil {
		t.Fatal(err)
	}
	if packageRequests != 2 {
		t.Fatalf("package requests = %d, want 2", packageRequests)
	}
	raw, err := os.ReadFile(prepared.PackagePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(payload) {
		t.Fatalf("resumed payload length = %d, want %d", len(raw), len(payload))
	}
	progress := manager.Progress()
	if progress.Stage != "ready" || progress.Percent != 100 || progress.BytesDownloaded != int64(len(payload)) || progress.TotalBytes != int64(len(payload)) {
		t.Fatalf("resumed progress = %+v", progress)
	}
}

func TestDownloadRestartsWhenPartialRangeIsRejected(t *testing.T) {
	payload := []byte("fresh portable package")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	manager, err := NewManager(t.TempDir(), "0.12.11")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "package.tmp")
	if err := os.WriteFile(target, []byte("stale-partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.downloadFile(context.Background(), server.URL, target); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(payload) {
		t.Fatalf("downloaded payload = %q, want %q", raw, payload)
	}
}

func TestPackageDownloadDoesNotRetryPermanentHTTPError(t *testing.T) {
	payload := []byte("payload")
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	packageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%s  MCP-DevDesk-Portable-amd64.zip\n", hash)
			return
		}
		packageRequests++
		http.NotFound(w, r)
	}))
	defer server.Close()
	manager, err := NewManager(t.TempDir(), "0.12.11")
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Download(context.Background(), Release{
		UpdateAvailable:  true,
		TagName:          "v0.12.12",
		AssetName:        "MCP-DevDesk-Portable-amd64.zip",
		AssetURL:         server.URL + "/package.zip",
		ChecksumAssetURL: server.URL + "/package.zip.sha256",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected HTTP 404, got %v", err)
	}
	if packageRequests != 1 {
		t.Fatalf("package requests = %d, want 1", packageRequests)
	}
}

func TestContentRangeTotal(t *testing.T) {
	if got := contentRangeTotal("bytes 2048-4095/8192"); got != 8192 {
		t.Fatalf("content range total = %d, want 8192", got)
	}
	if got := contentRangeTotal("bytes */*"); got != 0 {
		t.Fatalf("unknown content range total = %d", got)
	}
}

func TestVersionComparison(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"0.13.0", "0.12.7", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"2.0.0", "10.0.0", -1},
	}
	for _, test := range cases {
		got := compareVersions(test.left, test.right)
		if (got < 0 && test.want >= 0) || (got > 0 && test.want <= 0) || (got == 0 && test.want != 0) {
			t.Fatalf("compareVersions(%q,%q)=%d want sign %d", test.left, test.right, got, test.want)
		}
	}
}

func TestSettingsFileLivesUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	manager, err := NewManager(dataDir, "0.12.7")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(manager.settingsPath) != dataDir {
		t.Fatalf("settings path = %q", manager.settingsPath)
	}
}

func TestReleaseBuildDefaultRepository(t *testing.T) {
	manager, err := NewManager(t.TempDir(), "0.12.8", "owner/repository")
	if err != nil {
		t.Fatal(err)
	}
	if manager.Settings().Repository != "owner/repository" {
		t.Fatalf("default repository = %q", manager.Settings().Repository)
	}
}
