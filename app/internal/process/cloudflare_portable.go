package process

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func PortableCredentialsPath(rootDir, tunnelID string) string {
	rootDir = strings.TrimSpace(rootDir)
	tunnelID = strings.ToLower(strings.TrimSpace(tunnelID))
	if rootDir == "" || tunnelID == "" {
		return ""
	}
	return filepath.Join(rootDir, "data", "devdesk", "cloudflare", tunnelID+".json")
}

// LegacyCredentialsPath remains as the compatibility entrypoint used by older
// call sites, but now resolves to MCP DevDesk's portable file location. When a
// legacy cloudflared file exists it is copied into portable storage first.
func LegacyCredentialsPath(tunnelID string) string {
	rootDir := defaultPortableRoot()
	if rootDir == "" {
		return userCloudflaredCredentialsPath(tunnelID)
	}
	if path, err := EnsurePortableCredentials(rootDir, tunnelID); err == nil {
		return path
	}
	return PortableCredentialsPath(rootDir, tunnelID)
}

func userCloudflaredCredentialsPath(tunnelID string) string {
	home := UserHomeDir()
	tunnelID = strings.ToLower(strings.TrimSpace(tunnelID))
	if home == "" || tunnelID == "" {
		return ""
	}
	return filepath.Join(home, ".cloudflared", tunnelID+".json")
}

func defaultPortableRoot() string {
	if configured := strings.TrimSpace(os.Getenv("MCP_DEVDESK_ROOT")); configured != "" {
		if absolute, err := filepath.Abs(configured); err == nil {
			return absolute
		}
	}

	candidates := make([]string, 0, 4)
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd, filepath.Dir(cwd))
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if info, err := os.Stat(filepath.Join(candidate, "cloudflared.exe")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return filepath.Clean(candidates[0])
	}
	return ""
}

func EnsurePortableCredentials(rootDir, tunnelID string) (string, error) {
	dst := PortableCredentialsPath(rootDir, tunnelID)
	if dst == "" {
		return "", errors.New("cannot determine portable Cloudflare path")
	}
	if info, err := os.Stat(dst); err == nil && !info.IsDir() {
		return dst, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return dst, err
	}
	src := userCloudflaredCredentialsPath(tunnelID)
	if src == "" {
		return dst, errors.New("legacy Cloudflare path is unavailable")
	}
	if err := copyPortableFile(src, dst); err != nil {
		return dst, fmt.Errorf("migrate Cloudflare tunnel file: %w", err)
	}
	return dst, nil
}

func StorePortableCredentials(rootDir, tunnelID, src string) (string, error) {
	dst := PortableCredentialsPath(rootDir, tunnelID)
	if dst == "" {
		return "", errors.New("cannot determine portable Cloudflare path")
	}
	if err := copyPortableFile(src, dst); err != nil {
		return dst, err
	}
	if filepath.Clean(src) != filepath.Clean(dst) {
		_ = os.Remove(src)
	}
	return dst, nil
}

func copyPortableFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".cloudflare-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}
