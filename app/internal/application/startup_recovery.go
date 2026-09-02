package application

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcp-devdesk/internal/appearance"
	instancestore "mcp-devdesk/internal/instances"
	projectstore "mcp-devdesk/internal/projects"
	"mcp-devdesk/internal/statefiles"
	appupdater "mcp-devdesk/internal/updater"
)

func loadAppearanceRecovering(dataDir string) (*appearance.Store, error) {
	store, err := appearance.NewStore(dataDir)
	if err == nil {
		return store, nil
	}
	path := filepath.Join(dataDir, "appearance.json")
	if _, statErr := os.Stat(path); statErr == nil {
		if _, backupErr := statefiles.Quarantine(path, "appearance-startup"); backupErr != nil {
			return nil, fmt.Errorf("load appearance: %w; quarantine failed: %v", err, backupErr)
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, err
	}
	return appearance.NewStore(dataDir)
}

func loadProjectsRecovering(dataDir, initialWorkspace string) (*projectstore.Store, error) {
	store, err := projectstore.NewStore(dataDir, initialWorkspace)
	if err == nil {
		return store, nil
	}

	// A portable workspace may have moved or no longer exist. That must not
	// prevent the control app from opening; the user can choose a new path.
	store, retryErr := projectstore.NewStore(dataDir, "")
	if retryErr == nil {
		return store, nil
	}
	err = retryErr

	for attempt := 0; attempt < 3; attempt++ {
		path := projectStatePathForError(dataDir, err)
		if path == "" {
			return nil, err
		}
		if _, backupErr := statefiles.Quarantine(path, "projects-startup"); backupErr != nil {
			return nil, fmt.Errorf("load project state: %w; quarantine failed: %v", err, backupErr)
		}
		store, err = projectstore.NewStore(dataDir, "")
		if err == nil {
			return store, nil
		}
	}
	return nil, err
}

func projectStatePathForError(dataDir string, err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "project folder") || strings.Contains(text, "project folders"):
		return filepath.Join(dataDir, "project-folders.json")
	case strings.Contains(text, "project prompt") || strings.Contains(text, "global project prompt"):
		return filepath.Join(dataDir, "project-prompts.json")
	case strings.Contains(text, "parse projects") || strings.Contains(text, "migrate project") || strings.Contains(text, "prompt exceeds"):
		return filepath.Join(dataDir, "projects.json")
	default:
		return ""
	}
}

func loadInstancesRecovering(dataDir string) (*instancestore.Store, error) {
	store, err := instancestore.NewStore(dataDir)
	if err == nil {
		return store, nil
	}
	path := filepath.Join(dataDir, "instances.json")
	if _, statErr := os.Stat(path); statErr == nil {
		if _, backupErr := statefiles.Quarantine(path, "instances-startup"); backupErr != nil {
			return nil, fmt.Errorf("load instances: %w; quarantine failed: %v", err, backupErr)
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, err
	}
	return instancestore.NewStore(dataDir)
}

func loadUpdaterRecovering(dataDir, currentVersion, repository string) (*appupdater.Manager, error) {
	manager, err := appupdater.NewManager(dataDir, currentVersion, repository)
	if err == nil {
		return manager, nil
	}
	path := filepath.Join(dataDir, "update-settings.json")
	if _, statErr := os.Stat(path); statErr == nil {
		if _, backupErr := statefiles.Quarantine(path, "updater-startup"); backupErr != nil {
			return nil, fmt.Errorf("load updater settings: %w; quarantine failed: %v", err, backupErr)
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, err
	}
	return appupdater.NewManager(dataDir, currentVersion, repository)
}
