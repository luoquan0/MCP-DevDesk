package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	devlogging "mcp-devdesk/internal/logging"
)

const (
	Interval          = 6 * time.Hour
	LogMaxAge         = 14 * 24 * time.Hour
	RecoveryMaxAge    = 30 * 24 * time.Hour
	TempMaxAge        = 24 * time.Hour
	MaxRecoveryPerDir = 20
)

type Report struct {
	FilesRemoved int
	BytesFreed   int64
}

// Cleanup is intended for process startup, when no update download is active.
// It removes every completed/partial update package left from previous runs.
func Cleanup(dataDir string) Report {
	return cleanup(dataDir, true)
}

func RunPeriodic(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup(dataDir, false)
		}
	}
}

func cleanup(dataDir string, startup bool) Report {
	var report Report
	cleanupUpdates(filepath.Join(dataDir, "updates"), startup, &report)
	cleanupLogs(dataDir, &report)
	cleanupRecovery(dataDir, &report)
	cleanupUpdaterTemp(&report)
	return report
}

func cleanupUpdates(dir string, startup bool, report *Report) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".zip") && !strings.HasSuffix(name, ".tmp") && !strings.HasSuffix(name, ".sha256") {
			continue
		}
		if !startup {
			info, err := entry.Info()
			if err != nil || now.Sub(info.ModTime()) <= TempMaxAge {
				continue
			}
		}
		removeFile(filepath.Join(dir, entry.Name()), report)
	}
}

func cleanupLogs(root string, report *Report) {
	now := time.Now()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !isLogPath(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if now.Sub(info.ModTime()) > LogMaxAge {
			removeFile(path, report)
			return nil
		}
		_ = devlogging.TrimFile(path)
		return nil
	})
}

func cleanupRecovery(root string, report *Report) {
	now := time.Now()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() || !strings.EqualFold(entry.Name(), "recovery") {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return filepath.SkipDir
		}
		type candidate struct {
			path string
			info os.FileInfo
		}
		files := make([]candidate, 0, len(entries))
		for _, item := range entries {
			if item.IsDir() {
				continue
			}
			info, err := item.Info()
			if err == nil {
				files = append(files, candidate{path: filepath.Join(path, item.Name()), info: info})
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().After(files[j].info.ModTime()) })
		for index, file := range files {
			if index >= MaxRecoveryPerDir || now.Sub(file.info.ModTime()) > RecoveryMaxAge {
				removeFile(file.path, report)
			}
		}
		return filepath.SkipDir
	})
}

func cleanupUpdaterTemp(report *Report) {
	dir := filepath.Join(os.TempDir(), "MCP-DevDesk-Updater")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= TempMaxAge {
			continue
		}
		removeFile(filepath.Join(dir, entry.Name()), report)
	}
	_ = os.Remove(dir)
}

func isLogPath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	separator := string(filepath.Separator)
	if !strings.Contains(clean, separator+"logs"+separator) {
		return false
	}
	return strings.HasSuffix(clean, ".log") || strings.HasSuffix(clean, ".jsonl")
}

func removeFile(path string, report *Report) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	report.FilesRemoved++
	report.BytesFreed += info.Size()
}
