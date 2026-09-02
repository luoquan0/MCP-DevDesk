package selfupdate

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Options struct {
	PackagePath       string
	RootDir           string
	CurrentExe        string
	GoCoreTarget      string
	LegacyCoreTarget  string
	CloudflaredTarget string
	UpdaterTarget     string
	WaitPID           int
	RestartArgs       []string
	LogPath           string
}

type replacement struct {
	archiveName string
	target      string
}

type backup struct {
	target string
	path   string
	exists bool
}

var restartProcess = restart

func Install(options Options) error {
	if options.PackagePath == "" || options.RootDir == "" || options.CurrentExe == "" {
		return errors.New("update package, root directory and current executable are required")
	}
	root, err := filepath.Abs(options.RootDir)
	if err != nil {
		return err
	}
	currentExe, err := filepath.Abs(options.CurrentExe)
	if err != nil {
		return err
	}
	if !inside(root, currentExe) {
		return errors.New("current executable is outside the application root")
	}
	if options.WaitPID > 0 {
		if err := waitForProcessExit(options.WaitPID, 90*time.Second); err != nil {
			return err
		}
	}

	workDir, err := os.MkdirTemp("", "mcp-devdesk-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	extractDir := filepath.Join(workDir, "payload")
	if err := extractPackage(options.PackagePath, extractDir); err != nil {
		return err
	}

	replacements, err := buildReplacementPlan(options, root, currentExe)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(workDir, "backup")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	backups := make([]backup, 0, len(replacements))
	completed := make([]string, 0, len(replacements))
	rollback := func() {
		for i := len(completed) - 1; i >= 0; i-- {
			_ = os.Remove(completed[i])
		}
		for i := len(backups) - 1; i >= 0; i-- {
			item := backups[i]
			if item.exists {
				_ = copyFileAtomic(item.path, item.target)
			}
		}
	}

	for index, item := range replacements {
		source := filepath.Join(extractDir, item.archiveName)
		if info, err := os.Stat(source); err != nil || info.IsDir() {
			rollback()
			return fmt.Errorf("update package is missing %s", item.archiveName)
		}
		if !inside(root, item.target) {
			rollback()
			return fmt.Errorf("refusing to update file outside application root: %s", item.target)
		}
		entry := backup{target: item.target, path: filepath.Join(backupDir, fmt.Sprintf("%03d.bak", index))}
		if info, err := os.Stat(item.target); err == nil && !info.IsDir() {
			entry.exists = true
			if err := copyFile(item.target, entry.path); err != nil {
				rollback()
				return fmt.Errorf("backup %s: %w", item.target, err)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			rollback()
			return err
		}
		backups = append(backups, entry)
		if err := copyFileAtomic(source, item.target); err != nil {
			rollback()
			return fmt.Errorf("replace %s: %w", item.target, err)
		}
		completed = append(completed, item.target)
	}

	if err := restartProcess(options.CurrentExe, options.RestartArgs); err != nil {
		rollback()
		_ = restartProcess(options.CurrentExe, options.RestartArgs)
		return fmt.Errorf("restart updated application: %w", err)
	}
	// The archive is only needed through extraction. Once the updated manager
	// has been launched successfully, remove the verified package immediately;
	// startup housekeeping remains a fallback for interrupted/older updates.
	_ = os.Remove(options.PackagePath)
	return nil
}

func buildReplacementPlan(options Options, root, currentExe string) ([]replacement, error) {
	portable := strings.EqualFold(filepath.Dir(currentExe), root) && strings.EqualFold(filepath.Base(currentExe), "MCP-DevDesk.exe")
	if portable {
		return []replacement{
			{archiveName: "MCP-DevDesk.exe", target: currentExe},
			{archiveName: "devdeskctl.exe", target: filepath.Join(root, "devdeskctl.exe")},
			{archiveName: "mcp-core.exe", target: filepath.Join(root, "mcp-core.exe")},
			{archiveName: "devdesk-updater.exe", target: filepath.Join(root, "devdesk-updater.exe")},
			{archiveName: "cloudflared.exe", target: filepath.Join(root, "cloudflared.exe")},
			{archiveName: "coding-tools-mcp.exe", target: filepath.Join(root, "coding-tools-mcp.exe")},
			{archiveName: "README.md", target: filepath.Join(root, "README.md")},
		}, nil
	}

	managerBase := filepath.Base(currentExe)
	arch := ""
	lowerManagerBase := strings.ToLower(managerBase)
	if strings.HasPrefix(lowerManagerBase, "mcp-devdesk-") && strings.HasSuffix(lowerManagerBase, ".exe") {
		arch = strings.TrimSuffix(strings.TrimPrefix(lowerManagerBase, "mcp-devdesk-"), ".exe")
	}
	if arch == "" {
		arch = "amd64"
	}
	distDir := filepath.Dir(currentExe)
	plan := []replacement{
		{archiveName: "MCP-DevDesk.exe", target: currentExe},
		{archiveName: "devdeskctl.exe", target: filepath.Join(distDir, "devdeskctl-"+arch+".exe")},
		{archiveName: "devdesk-updater.exe", target: filepath.Join(distDir, "devdesk-updater-"+arch+".exe")},
	}
	appendTarget := func(archiveName, target string) {
		if strings.TrimSpace(target) == "" {
			return
		}
		absolute, err := filepath.Abs(target)
		if err == nil && inside(root, absolute) {
			for _, existing := range plan {
				if strings.EqualFold(existing.target, absolute) {
					return
				}
			}
			plan = append(plan, replacement{archiveName: archiveName, target: absolute})
		}
	}
	appendTarget("mcp-core.exe", options.GoCoreTarget)
	appendTarget("mcp-core.exe", filepath.Join(distDir, "mcp-core.exe"))
	appendTarget("mcp-core.exe", filepath.Join(distDir, "mcp-core-"+arch+".exe"))
	appendTarget("coding-tools-mcp.exe", options.LegacyCoreTarget)
	appendTarget("cloudflared.exe", options.CloudflaredTarget)
	if options.UpdaterTarget != "" {
		appendTarget("devdesk-updater.exe", options.UpdaterTarget)
	}
	return plan, nil
}

func extractPackage(packagePath, destination string) error {
	reader, err := zip.OpenReader(packagePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	allowed := map[string]bool{
		"MCP-DevDesk.exe":      true,
		"devdeskctl.exe":       true,
		"mcp-core.exe":         true,
		"devdesk-updater.exe":  true,
		"cloudflared.exe":      true,
		"coding-tools-mcp.exe": true,
		"README.md":            true,
		"启动 MCP DevDesk.cmd":   true,
	}
	var total uint64
	for _, file := range reader.File {
		clean := filepath.Clean(strings.ReplaceAll(file.Name, "/", string(filepath.Separator)))
		if clean == "." || strings.Contains(clean, "..") || filepath.IsAbs(clean) {
			return errors.New("update package contains an unsafe path")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(clean)
		if !allowed[base] {
			continue
		}
		total += file.UncompressedSize64
		if total > 768<<20 {
			return errors.New("update package expands beyond the allowed size")
		}
		target := filepath.Join(destination, base)
		input, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, io.LimitReader(input, int64(file.UncompressedSize64)+1))
		closeErr := output.Close()
		input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func copyFileAtomic(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + fmt.Sprintf(".update-%d.tmp", os.Getpid())
	_ = os.Remove(tmp)
	if err := copyFile(source, tmp); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func restart(executable string, args []string) error {
	command := exec.Command(executable, args...)
	command.Dir = filepath.Dir(executable)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
