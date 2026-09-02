package startup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcp-devdesk/internal/appearance"
	"mcp-devdesk/internal/buildinfo"
	"mcp-devdesk/internal/config"
	instancestore "mcp-devdesk/internal/instances"
	devlogging "mcp-devdesk/internal/logging"
	projectstore "mcp-devdesk/internal/projects"
	"mcp-devdesk/internal/statefiles"
	appupdater "mcp-devdesk/internal/updater"
)

type Report struct {
	Recovered []string
}

func Prepare(rootDir, dataDir string) (Report, error) {
	var report Report
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o700); err != nil {
		return report, err
	}

	cfgStore, err := config.NewRecoveringStore(rootDir, dataDir)
	if err != nil {
		return report, err
	}
	cfg := cfgStore.Get()
	if !directoryExists(cfg.Workspace) {
		oldWorkspace := cfg.Workspace
		_, _ = statefiles.Backup(filepath.Join(dataDir, "config.json"), "workspace-missing")
		cfg.Workspace = filepath.Clean(rootDir)
		cfg.AllowedRoots = []string{cfg.Workspace}
		cfg.AutoStart = false
		if _, err := cfgStore.Replace(cfg); err != nil {
			return report, fmt.Errorf("recover missing workspace: %w", err)
		}
		report.Recovered = append(report.Recovered, fmt.Sprintf("主工作目录不存在，已从 %q 临时回退到程序目录并关闭自动启动", oldWorkspace))
	}

	if recovered, err := recoverAppearance(dataDir); err != nil {
		return report, err
	} else if recovered {
		report.Recovered = append(report.Recovered, "appearance.json 已损坏，已隔离并恢复默认外观")
	}

	if messages, err := recoverProjects(dataDir, cfgStore.Get().Workspace); err != nil {
		return report, err
	} else {
		report.Recovered = append(report.Recovered, messages...)
	}

	instanceStore, recoveredIndex, err := recoverInstancesIndex(dataDir)
	if err != nil {
		return report, err
	}
	if recoveredIndex {
		report.Recovered = append(report.Recovered, "instances.json 已损坏，已隔离并重建实例索引")
	}
	if messages := recoverInstanceConfigs(rootDir, instanceStore); len(messages) > 0 {
		report.Recovered = append(report.Recovered, messages...)
	}

	if recovered, err := recoverUpdateSettings(dataDir); err != nil {
		return report, err
	} else if recovered {
		report.Recovered = append(report.Recovered, "update-settings.json 已损坏，已隔离并恢复更新默认设置")
	}

	for _, message := range report.Recovered {
		_ = devlogging.AppendLine(filepath.Join(dataDir, "logs", "recovery.log"), []byte(fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), message)))
	}
	return report, nil
}

func recoverAppearance(dataDir string) (bool, error) {
	if _, err := appearance.NewStore(dataDir); err == nil {
		return false, nil
	}
	path := filepath.Join(dataDir, "appearance.json")
	if _, err := statefiles.Quarantine(path, "appearance-startup"); err != nil {
		return false, err
	}
	_, err := appearance.NewStore(dataDir)
	return true, err
}

func recoverProjects(dataDir, workspace string) ([]string, error) {
	if _, err := projectstore.NewStore(dataDir, workspace); err == nil {
		return nil, nil
	}
	// Retry without auto-adding the workspace first. This distinguishes a moved
	// project directory from corrupt project-library state.
	if _, err := projectstore.NewStore(dataDir, ""); err == nil {
		return []string{"项目库可读取，但旧工作目录不可用；已跳过自动添加"}, nil
	}

	var messages []string
	for attempt := 0; attempt < 3; attempt++ {
		_, err := projectstore.NewStore(dataDir, "")
		if err == nil {
			return messages, nil
		}
		path := projectStatePathForError(dataDir, err)
		if path == "" {
			return messages, err
		}
		if _, backupErr := statefiles.Quarantine(path, "projects-startup"); backupErr != nil {
			return messages, fmt.Errorf("recover project state after %v: %w", err, backupErr)
		}
		messages = append(messages, filepath.Base(path)+" 已损坏，已隔离")
	}
	_, err := projectstore.NewStore(dataDir, "")
	return messages, err
}

func projectStatePathForError(dataDir string, err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "project folder"):
		return filepath.Join(dataDir, "project-folders.json")
	case strings.Contains(text, "project prompt") || strings.Contains(text, "global project prompt"):
		return filepath.Join(dataDir, "project-prompts.json")
	case strings.Contains(text, "parse projects") || strings.Contains(text, "migrate project") || strings.Contains(text, "prompt exceeds"):
		return filepath.Join(dataDir, "projects.json")
	default:
		return ""
	}
}

func recoverInstancesIndex(dataDir string) (*instancestore.Store, bool, error) {
	store, err := instancestore.NewStore(dataDir)
	if err == nil {
		return store, false, nil
	}
	path := filepath.Join(dataDir, "instances.json")
	if _, backupErr := statefiles.Quarantine(path, "instances-startup"); backupErr != nil {
		return nil, false, fmt.Errorf("recover instances index after %v: %w", err, backupErr)
	}
	store, retryErr := instancestore.NewStore(dataDir)
	return store, true, retryErr
}

func recoverInstanceConfigs(rootDir string, store *instancestore.Store) []string {
	if store == nil {
		return nil
	}
	var messages []string
	for _, record := range store.List() {
		dataDir := store.DataDir(record.ID)
		if _, err := config.NewStore(rootDir, dataDir); err == nil {
			continue
		}
		path := filepath.Join(dataDir, "config.json")
		if _, backupErr := statefiles.Quarantine(path, "instance-config-startup"); backupErr != nil {
			messages = append(messages, fmt.Sprintf("实例 %q 配置损坏且无法隔离: %v", record.Name, backupErr))
			continue
		}
		if _, retryErr := config.NewStore(rootDir, dataDir); retryErr != nil {
			messages = append(messages, fmt.Sprintf("实例 %q 配置恢复失败: %v", record.Name, retryErr))
			continue
		}
		messages = append(messages, fmt.Sprintf("实例 %q 的 config.json 已损坏，已隔离并以安全默认值重建（自动启动关闭）", record.Name))
	}
	return messages
}

func recoverUpdateSettings(dataDir string) (bool, error) {
	if _, err := appupdater.NewManager(dataDir, buildinfo.Version, buildinfo.Repository); err == nil {
		return false, nil
	}
	path := filepath.Join(dataDir, "update-settings.json")
	if _, err := statefiles.Quarantine(path, "updater-startup"); err != nil {
		return false, err
	}
	_, err := appupdater.NewManager(dataDir, buildinfo.Version, buildinfo.Repository)
	return true, err
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func IsRecoverableMissingFile(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
