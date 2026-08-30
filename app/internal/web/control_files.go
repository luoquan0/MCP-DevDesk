package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const maxControlDirectoryEntries = 500

type controlDirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type controlDirectoryListing struct {
	Path        string                  `json:"path"`
	Parent      string                  `json:"parent,omitempty"`
	Roots       []controlDirectoryEntry `json:"roots,omitempty"`
	Directories []controlDirectoryEntry `json:"directories"`
}

func listControlDirectories(requested string) (controlDirectoryListing, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		roots := controlFilesystemRoots()
		return controlDirectoryListing{Roots: roots, Directories: roots}, nil
	}

	abs, err := filepath.Abs(requested)
	if err != nil {
		return controlDirectoryListing{}, fmt.Errorf("resolve directory: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlDirectoryListing{}, errors.New("directory does not exist")
		}
		return controlDirectoryListing{}, fmt.Errorf("open directory: %w", err)
	}
	if !info.IsDir() {
		return controlDirectoryListing{}, errors.New("path is not a directory")
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return controlDirectoryListing{}, fmt.Errorf("read directory: %w", err)
	}
	directories := make([]controlDirectoryEntry, 0, min(len(entries), maxControlDirectoryEntries))
	for _, entry := range entries {
		if len(directories) >= maxControlDirectoryEntries {
			break
		}
		full := filepath.Join(abs, entry.Name())
		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			if target, statErr := os.Stat(full); statErr == nil {
				isDir = target.IsDir()
			}
		}
		if !isDir {
			continue
		}
		directories = append(directories, controlDirectoryEntry{Name: entry.Name(), Path: full})
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.ToLower(directories[i].Name) < strings.ToLower(directories[j].Name)
	})

	parent := filepath.Dir(abs)
	if sameFilesystemPath(parent, abs) {
		parent = ""
	}
	return controlDirectoryListing{
		Path:        abs,
		Parent:      parent,
		Roots:       controlFilesystemRoots(),
		Directories: directories,
	}, nil
}

func controlFilesystemRoots() []controlDirectoryEntry {
	if runtime.GOOS != "windows" {
		return []controlDirectoryEntry{{Name: "/", Path: "/"}}
	}
	roots := make([]controlDirectoryEntry, 0, 8)
	seen := make(map[string]struct{})
	appendRoot := func(name, path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if _, exists := seen[key]; exists {
			return
		}
		if info, err := os.Stat(clean); err != nil || !info.IsDir() {
			return
		}
		seen[key] = struct{}{}
		roots = append(roots, controlDirectoryEntry{Name: name, Path: clean})
	}
	if home, err := os.UserHomeDir(); err == nil {
		appendRoot("桌面", filepath.Join(home, "Desktop"))
		appendRoot("用户目录", home)
	}
	for letter := 'A'; letter <= 'Z'; letter++ {
		path := string(letter) + `:\`
		appendRoot(string(letter)+":", path)
	}
	return roots
}

func sameFilesystemPath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
