package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxReleaseAssetBytes   int64 = 512 << 20
	metadataRequestTimeout       = 45 * time.Second
	downloadRetryAttempts        = 3
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Settings struct {
	Repository     string `json:"repository"`
	Channel        string `json:"channel"`
	CheckOnStartup bool   `json:"checkOnStartup"`
}

type SettingsUpdate struct {
	Repository     *string `json:"repository"`
	Channel        *string `json:"channel"`
	CheckOnStartup *bool   `json:"checkOnStartup"`
}

type Release struct {
	CurrentVersion   string    `json:"currentVersion"`
	LatestVersion    string    `json:"latestVersion"`
	UpdateAvailable  bool      `json:"updateAvailable"`
	TagName          string    `json:"tagName"`
	Name             string    `json:"name"`
	Notes            string    `json:"notes"`
	PublishedAt      time.Time `json:"publishedAt"`
	PageURL          string    `json:"pageUrl"`
	AssetName        string    `json:"assetName"`
	AssetURL         string    `json:"-"`
	ChecksumAssetURL string    `json:"-"`
}

type PreparedUpdate struct {
	Release     Release `json:"release"`
	PackagePath string  `json:"-"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type downloadHTTPError struct {
	status int
}

func (e *downloadHTTPError) Error() string {
	return fmt.Sprintf("update package download returned HTTP %d", e.status)
}

type Manager struct {
	mu           sync.RWMutex
	settingsPath string
	updatesDir   string
	current      Settings
	version      string
	apiBaseURL   string
	client       *http.Client
}

func NewManager(dataDir, currentVersion string, defaultRepository ...string) (*Manager, error) {
	updatesDir := filepath.Join(dataDir, "updates")
	if err := os.MkdirAll(updatesDir, 0o700); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = metadataRequestTimeout
	transport.TLSHandshakeTimeout = 15 * time.Second
	m := &Manager{
		settingsPath: filepath.Join(dataDir, "update-settings.json"),
		updatesDir:   updatesDir,
		version:      strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
		apiBaseURL:   "https://api.github.com",
		// Do not set http.Client.Timeout here. Release metadata/checksum requests
		// get their own short deadlines, while the package body is allowed to
		// stream until the install request context expires.
		client: &http.Client{Transport: transport},
		current: Settings{
			Channel:        "stable",
			CheckOnStartup: true,
		},
	}
	if raw, err := os.ReadFile(m.settingsPath); err == nil {
		if err := json.Unmarshal(raw, &m.current); err != nil {
			return nil, fmt.Errorf("parse update settings: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read update settings: %w", err)
	}
	m.normalizeLocked()
	if m.current.Repository == "" && len(defaultRepository) > 0 {
		candidate := strings.TrimSpace(defaultRepository[0])
		if repositoryPattern.MatchString(candidate) {
			m.current.Repository = candidate
		}
	}
	if err := validateSettings(m.current); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *Manager) UpdateSettings(update SettingsUpdate) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.current
	if update.Repository != nil {
		next.Repository = strings.TrimSpace(*update.Repository)
	}
	if update.Channel != nil {
		next.Channel = strings.ToLower(strings.TrimSpace(*update.Channel))
	}
	if update.CheckOnStartup != nil {
		next.CheckOnStartup = *update.CheckOnStartup
	}
	if err := validateSettings(next); err != nil {
		return Settings{}, err
	}
	previous := m.current
	m.current = next
	if err := m.saveLocked(); err != nil {
		m.current = previous
		return Settings{}, err
	}
	return m.current, nil
}

func (m *Manager) Check(ctx context.Context) (Release, error) {
	settings := m.Settings()
	if strings.TrimSpace(settings.Repository) == "" {
		return Release{CurrentVersion: m.version}, errors.New("GitHub repository is not configured")
	}

	releases, err := m.fetchReleases(ctx, settings)
	if err != nil {
		return Release{}, err
	}
	if len(releases) == 0 {
		return Release{CurrentVersion: m.version}, errors.New("no matching GitHub release was found")
	}

	best := releases[0]
	for _, candidate := range releases[1:] {
		if compareVersions(candidate.TagName, best.TagName) > 0 {
			best = candidate
		}
	}

	assetName := fmt.Sprintf("MCP-DevDesk-Portable-%s.zip", runtime.GOARCH)
	checksumName := assetName + ".sha256"
	var assetURL, checksumURL string
	for _, asset := range best.Assets {
		switch asset.Name {
		case assetName:
			assetURL = asset.BrowserDownloadURL
		case checksumName:
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return Release{}, fmt.Errorf("release %s does not contain %s", best.TagName, assetName)
	}
	if checksumURL == "" {
		return Release{}, fmt.Errorf("release %s does not contain %s", best.TagName, checksumName)
	}
	if err := validateGitHubDownloadURL(assetURL); err != nil {
		return Release{}, err
	}
	if err := validateGitHubDownloadURL(checksumURL); err != nil {
		return Release{}, err
	}

	latest := strings.TrimPrefix(strings.TrimSpace(best.TagName), "v")
	return Release{
		CurrentVersion:   m.version,
		LatestVersion:    latest,
		UpdateAvailable:  compareVersions(latest, m.version) > 0,
		TagName:          best.TagName,
		Name:             strings.TrimSpace(best.Name),
		Notes:            truncate(best.Body, 20000),
		PublishedAt:      best.PublishedAt,
		PageURL:          best.HTMLURL,
		AssetName:        assetName,
		AssetURL:         assetURL,
		ChecksumAssetURL: checksumURL,
	}, nil
}

func (m *Manager) Download(ctx context.Context, release Release) (PreparedUpdate, error) {
	if !release.UpdateAvailable {
		return PreparedUpdate{}, errors.New("no newer update is available")
	}
	if release.AssetURL == "" || release.ChecksumAssetURL == "" {
		return PreparedUpdate{}, errors.New("release download metadata is incomplete")
	}

	expected, err := m.downloadChecksum(ctx, release.ChecksumAssetURL, release.AssetName)
	if err != nil {
		return PreparedUpdate{}, err
	}
	packageName := sanitizeFilename(release.TagName) + "-" + release.AssetName
	packagePath := filepath.Join(m.updatesDir, packageName)
	tmp := packagePath + ".tmp"
	if err := m.downloadFile(ctx, release.AssetURL, tmp); err != nil {
		// Keep a bounded partial package so a later retry can resume instead of
		// throwing away tens of megabytes after a short network interruption.
		if info, statErr := os.Stat(tmp); statErr == nil && info.Size() > maxReleaseAssetBytes {
			_ = os.Remove(tmp)
		}
		return PreparedUpdate{}, err
	}
	actual, err := fileSHA256(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return PreparedUpdate{}, err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(tmp)
		return PreparedUpdate{}, fmt.Errorf("SHA256 mismatch for %s", release.AssetName)
	}
	if err := replaceFile(tmp, packagePath); err != nil {
		_ = os.Remove(tmp)
		return PreparedUpdate{}, err
	}
	return PreparedUpdate{Release: release, PackagePath: packagePath}, nil
}

func (m *Manager) fetchReleases(ctx context.Context, settings Settings) ([]githubRelease, error) {
	var endpoint string
	if settings.Channel == "stable" {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(m.apiBaseURL, "/"), settings.Repository)
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/releases?per_page=30", strings.TrimRight(m.apiBaseURL, "/"), settings.Repository)
	}
	requestCtx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check GitHub release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release check returned HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 4<<20)
	if settings.Channel == "stable" {
		var item githubRelease
		if err := json.NewDecoder(limited).Decode(&item); err != nil {
			return nil, err
		}
		if item.Draft || item.Prerelease {
			return nil, errors.New("latest stable release is not eligible")
		}
		return []githubRelease{item}, nil
	}
	var items []githubRelease
	if err := json.NewDecoder(limited).Decode(&items); err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		if !item.Draft {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (m *Manager) downloadChecksum(ctx context.Context, downloadURL, assetName string) (string, error) {
	var lastErr error
	attempts := 0
	for attempt := 1; attempt <= downloadRetryAttempts; attempt++ {
		attempts = attempt
		value, err := m.downloadChecksumAttempt(ctx, downloadURL, assetName)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !retryableDownloadError(err) || attempt == downloadRetryAttempts {
			break
		}
		if err := waitForRetry(ctx, attempt); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("download checksum failed after %d attempt(s): %w", attempts, lastErr)
}

func (m *Manager) downloadChecksumAttempt(ctx context.Context, downloadURL, assetName string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, metadataRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &downloadHTTPError{status: resp.StatusCode}
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 16<<10))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err != nil {
				continue
			}
			if len(fields) == 1 || strings.EqualFold(strings.TrimPrefix(fields[len(fields)-1], "*"), assetName) {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("release checksum file does not contain a valid SHA256")
}

func (m *Manager) downloadFile(ctx context.Context, downloadURL, target string) error {
	var lastErr error
	attempts := 0
	for attempt := 1; attempt <= downloadRetryAttempts; attempt++ {
		attempts = attempt
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.downloadFileAttempt(ctx, downloadURL, target); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if !retryableDownloadError(lastErr) || attempt == downloadRetryAttempts {
			break
		}
		if err := waitForRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("download update package failed after %d attempt(s): %w", attempts, lastErr)
}

func (m *Manager) downloadFileAttempt(ctx context.Context, downloadURL, target string) error {
	offset := int64(0)
	if info, err := os.Stat(target); err == nil {
		offset = info.Size()
		if offset > maxReleaseAssetBytes {
			return errors.New("partial update package exceeds the maximum allowed size")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MCP-DevDesk/"+m.version)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download update package: %w", err)
	}
	defer resp.Body.Close()

	appendMode := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// The local partial file may be stale or already complete. Restart from
		// zero on the next retry so checksum verification can decide the result.
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return &downloadHTTPError{status: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return &downloadHTTPError{status: resp.StatusCode}
	}
	if !appendMode {
		offset = 0
	}

	if resp.ContentLength > 0 && offset+resp.ContentLength > maxReleaseAssetBytes {
		return errors.New("update package is unexpectedly large")
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(target, flags, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxReleaseAssetBytes-offset+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if offset+written > maxReleaseAssetBytes {
		return errors.New("update package exceeds the maximum allowed size")
	}
	return nil
}

func retryableDownloadError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var statusErr *downloadHTTPError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusRequestTimeout || statusErr.status == http.StatusTooManyRequests || statusErr.status == http.StatusRequestedRangeNotSatisfiable || statusErr.status >= 500
	}
	return true
}

func waitForRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt) * time.Second
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (m *Manager) normalizeLocked() {
	m.current.Repository = strings.TrimSpace(m.current.Repository)
	m.current.Channel = strings.ToLower(strings.TrimSpace(m.current.Channel))
	if m.current.Channel == "" {
		m.current.Channel = "stable"
	}
}

func validateSettings(settings Settings) error {
	if settings.Repository != "" && !repositoryPattern.MatchString(settings.Repository) {
		return errors.New("repository must use owner/repo format")
	}
	switch settings.Channel {
	case "stable", "prerelease":
	default:
		return errors.New("update channel must be stable or prerelease")
	}
	return nil
}

func validateGitHubDownloadURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" {
		return errors.New("release asset URL must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".githubusercontent.com")
	if !allowed {
		return fmt.Errorf("release asset host %q is not a GitHub host", host)
	}
	return nil
}

func compareVersions(left, right string) int {
	leftParts := parseVersion(left)
	rightParts := parseVersion(right)
	for i := 0; i < 3; i++ {
		if leftParts.numbers[i] < rightParts.numbers[i] {
			return -1
		}
		if leftParts.numbers[i] > rightParts.numbers[i] {
			return 1
		}
	}
	if leftParts.prerelease == rightParts.prerelease {
		return 0
	}
	if leftParts.prerelease == "" {
		return 1
	}
	if rightParts.prerelease == "" {
		return -1
	}
	return strings.Compare(leftParts.prerelease, rightParts.prerelease)
}

type parsedVersion struct {
	numbers    [3]int
	prerelease string
}

func parseVersion(value string) parsedVersion {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	main := value
	pre := ""
	if index := strings.IndexAny(main, "-+"); index >= 0 {
		pre = main[index+1:]
		main = main[:index]
	}
	parts := strings.Split(main, ".")
	var result parsedVersion
	for i := 0; i < len(parts) && i < 3; i++ {
		result.numbers[i], _ = strconv.Atoi(parts[i])
	}
	result.prerelease = pre
	return result
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func replaceFile(source, target string) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
	if value == "" {
		return "update"
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (m *Manager) saveLocked() error {
	raw, err := json.MarshalIndent(m.current, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.settingsPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return replaceFile(tmp, m.settingsPath)
}

func sortedReleaseTags(releases []githubRelease) []string {
	result := make([]string, 0, len(releases))
	for _, release := range releases {
		result = append(result, release.TagName)
	}
	sort.Slice(result, func(i, j int) bool { return compareVersions(result[i], result[j]) > 0 })
	return result
}
