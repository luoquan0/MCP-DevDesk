package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Folder       string    `json:"folder,omitempty"`
	Prompt       string    `json:"prompt,omitempty"`
	AddedAt      time.Time `json:"addedAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type Store struct {
	mu                  sync.RWMutex
	path                string
	foldersPath         string
	promptSettingsPath  string
	globalPromptEnabled bool
	globalPrompt        string
	folders             []string
	data                []Project
}

const (
	maxProjects    = 256
	MaxPromptBytes = 32 * 1024
)

type PromptSettings struct {
	Enabled      bool   `json:"enabled"`
	GlobalPrompt string `json:"globalPrompt"`
}

func NewStore(dataDir, initialWorkspace string) (*Store, error) {
	s := &Store{
		path:               filepath.Join(dataDir, "projects.json"),
		foldersPath:        filepath.Join(dataDir, "project-folders.json"),
		promptSettingsPath: filepath.Join(dataDir, "project-prompts.json"),
	}
	changed := false
	if raw, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("parse projects: %w", err)
		}
		for _, project := range s.data {
			if len([]byte(project.Prompt)) > MaxPromptBytes {
				return nil, fmt.Errorf("project %q prompt exceeds %d bytes", project.Name, MaxPromptBytes)
			}
		}
		original := append([]Project(nil), s.data...)
		s.data = normalizeProjects(s.data)
		changed = !reflect.DeepEqual(original, s.data)
		for i := range s.data {
			legacyPrompt := strings.TrimSpace(s.data[i].Prompt)
			if legacyPrompt == "" {
				continue
			}
			if err := migrateLegacyPromptToAgents(s.data[i].Path, legacyPrompt); err != nil {
				return nil, fmt.Errorf("migrate project %q prompt to AGENTS.md: %w", s.data[i].Name, err)
			}
			s.data[i].Prompt = ""
			changed = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read projects: %w", err)
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, fmt.Errorf("migrate projects: %w", err)
		}
	}
	if raw, err := os.ReadFile(s.foldersPath); err == nil {
		if err := json.Unmarshal(raw, &s.folders); err != nil {
			return nil, fmt.Errorf("parse project folders: %w", err)
		}
		s.folders = normalizeFolders(s.folders)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read project folders: %w", err)
	}
	if raw, err := os.ReadFile(s.promptSettingsPath); err == nil {
		var settings struct {
			Enabled      *bool  `json:"enabled"`
			GlobalPrompt string `json:"globalPrompt"`
		}
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, fmt.Errorf("parse project prompts: %w", err)
		}
		if len([]byte(settings.GlobalPrompt)) > MaxPromptBytes {
			return nil, fmt.Errorf("global project prompt exceeds %d bytes", MaxPromptBytes)
		}
		s.globalPrompt = strings.TrimSpace(settings.GlobalPrompt)
		if settings.Enabled != nil {
			s.globalPromptEnabled = *settings.Enabled
		} else {
			s.globalPromptEnabled = s.globalPrompt != ""
			if err := s.savePromptSettingsLocked(); err != nil {
				return nil, fmt.Errorf("migrate global prompt settings: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read project prompts: %w", err)
	}
	if initialWorkspace != "" {
		if _, err := s.Add("", initialWorkspace); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) List() []Project {
	s.mu.RLock()
	items := append([]Project(nil), s.data...)
	s.mu.RUnlock()
	for i := range items {
		items[i].Prompt, _ = readProjectAgents(items[i].Path)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LastOpenedAt.After(items[j].LastOpenedAt) })
	return items
}

func (s *Store) Folders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.folders...)
}

func (s *Store) AddFolder(name string) (string, error) {
	name = normalizeFolder(name)
	if name == "" {
		return "", errors.New("folder name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.folders {
		if strings.EqualFold(existing, name) {
			return existing, nil
		}
	}
	previous := append([]string(nil), s.folders...)
	s.folders = normalizeFolders(append(s.folders, name))
	if err := s.saveFoldersLocked(); err != nil {
		s.folders = previous
		return "", err
	}
	return name, nil
}

func (s *Store) SetFolder(id, folder string) (Project, error) {
	updated, err := s.SetFolderMany([]string{id}, folder)
	if err != nil {
		return Project{}, err
	}
	if len(updated) == 0 {
		return Project{}, errors.New("project not found")
	}
	return updated[0], nil
}

func (s *Store) SetFolderMany(ids []string, folder string) ([]Project, error) {
	folder = normalizeFolder(folder)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) == 0 {
		return nil, errors.New("project ids are required")
	}
	if folder != "" {
		matched := ""
		for _, existing := range s.folders {
			if strings.EqualFold(existing, folder) {
				matched = existing
				break
			}
		}
		if matched == "" {
			return nil, errors.New("project folder does not exist")
		}
		folder = matched
	}
	requested := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			requested[id] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil, errors.New("project ids are required")
	}
	indices := make([]int, 0, len(requested))
	for i := range s.data {
		if _, ok := requested[s.data[i].ID]; ok {
			indices = append(indices, i)
			delete(requested, s.data[i].ID)
		}
	}
	if len(requested) > 0 {
		return nil, errors.New("one or more projects were not found")
	}
	previous := append([]Project(nil), s.data...)
	updated := make([]Project, 0, len(indices))
	for _, index := range indices {
		s.data[index].Folder = folder
		updated = append(updated, s.data[index])
	}
	if err := s.saveLocked(); err != nil {
		s.data = previous
		return nil, err
	}
	return updated, nil
}

func (s *Store) RemoveFolder(name string) error {
	name = normalizeFolder(name)
	if name == "" {
		return errors.New("folder name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := ""
	for _, existing := range s.folders {
		if strings.EqualFold(existing, name) {
			matched = existing
			break
		}
	}
	if matched == "" {
		return errors.New("project folder does not exist")
	}
	previousFolders := append([]string(nil), s.folders...)
	previousProjects := append([]Project(nil), s.data...)
	nextFolders := make([]string, 0, len(s.folders)-1)
	for _, existing := range s.folders {
		if !strings.EqualFold(existing, matched) {
			nextFolders = append(nextFolders, existing)
		}
	}
	s.folders = nextFolders
	projectsChanged := false
	for i := range s.data {
		if strings.EqualFold(s.data[i].Folder, matched) {
			s.data[i].Folder = ""
			projectsChanged = true
		}
	}
	if projectsChanged {
		if err := s.saveLocked(); err != nil {
			s.folders = previousFolders
			s.data = previousProjects
			return err
		}
	}
	if err := s.saveFoldersLocked(); err != nil {
		s.folders = previousFolders
		s.data = previousProjects
		if projectsChanged {
			if rollbackErr := s.saveLocked(); rollbackErr != nil {
				return fmt.Errorf("delete project folder: %w; rollback projects: %v", err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func (s *Store) Add(name, path string) (Project, error) {
	abs, err := canonicalPath(path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Project{}, errors.New("project path must be an existing directory")
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(abs)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = abs
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.data {
		if samePath(item.Path, abs) {
			return item, nil
		}
	}
	if len(s.data) >= maxProjects {
		return Project{}, fmt.Errorf("no more than %d projects are allowed", maxProjects)
	}
	now := time.Now().UTC()
	project := Project{ID: projectID(abs), Name: strings.TrimSpace(name), Path: abs, AddedAt: now, LastOpenedAt: now}
	s.data = append(s.data, project)
	if err := s.saveLocked(); err != nil {
		s.data = s.data[:len(s.data)-1]
		return Project{}, err
	}
	return project, nil
}

func (s *Store) Get(id string) (Project, bool) {
	s.mu.RLock()
	var result Project
	found := false
	for _, item := range s.data {
		if item.ID == id {
			result = item
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return Project{}, false
	}
	result.Prompt, _ = readProjectAgents(result.Path)
	return result, true
}

func (s *Store) GlobalPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.globalPrompt
}

func (s *Store) GlobalPromptEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.globalPromptEnabled
}

func (s *Store) SetPromptSettings(enabled bool, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if len([]byte(prompt)) > MaxPromptBytes {
		return fmt.Errorf("global project prompt exceeds %d bytes", MaxPromptBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previousEnabled := s.globalPromptEnabled
	previous := s.globalPrompt
	s.globalPromptEnabled = enabled
	s.globalPrompt = prompt
	if err := s.savePromptSettingsLocked(); err != nil {
		s.globalPromptEnabled = previousEnabled
		s.globalPrompt = previous
		return err
	}
	return nil
}

func (s *Store) UpdatePrompt(id, prompt string) (Project, error) {
	prompt = strings.TrimSpace(prompt)
	if len([]byte(prompt)) > MaxPromptBytes {
		return Project{}, fmt.Errorf("project prompt exceeds %d bytes", MaxPromptBytes)
	}
	s.mu.RLock()
	var project Project
	found := false
	for _, item := range s.data {
		if item.ID == id {
			project = item
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return Project{}, errors.New("project not found")
	}
	if err := writeProjectAgents(project.Path, prompt); err != nil {
		return Project{}, err
	}
	project.Prompt = prompt
	return project, nil
}

func (s *Store) EffectivePrompt(workspace string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.globalPromptEnabled {
		return ""
	}
	global := strings.TrimSpace(s.globalPrompt)
	if global == "" {
		return ""
	}
	return "# MCP DevDesk 全局提示词\n\n" + global
}

func (s *Store) PreparePathUpdate(id, path string) (Project, error) {
	abs, err := canonicalPath(path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Project{}, errors.New("project path must be an existing directory")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preparePathUpdateLocked(id, abs)
}

func (s *Store) UpdatePath(id, path string) (Project, error) {
	abs, err := canonicalPath(path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Project{}, errors.New("project path must be an existing directory")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.preparePathUpdateLocked(id, abs)
	if err != nil {
		return Project{}, err
	}
	for i := range s.data {
		if s.data[i].ID != id {
			continue
		}
		if samePath(s.data[i].Path, updated.Path) {
			return s.data[i], nil
		}
		previous := s.data[i]
		s.data[i] = updated
		if err := s.saveLocked(); err != nil {
			s.data[i] = previous
			return Project{}, err
		}
		return updated, nil
	}
	return Project{}, errors.New("project not found")
}

func (s *Store) preparePathUpdateLocked(id, abs string) (Project, error) {
	var current Project
	found := false
	for _, item := range s.data {
		if item.ID == id {
			current = item
			found = true
			continue
		}
		if samePath(item.Path, abs) {
			return Project{}, errors.New("another project already uses this path")
		}
	}
	if !found {
		return Project{}, errors.New("project not found")
	}
	current.Path = abs
	current.ID = projectID(abs)
	return current, nil
}

func (s *Store) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data {
		if s.data[i].ID == id {
			previous := s.data[i]
			s.data[i].LastOpenedAt = time.Now().UTC()
			if err := s.saveLocked(); err != nil {
				s.data[i] = previous
				return err
			}
			return nil
		}
	}
	return errors.New("project not found")
}

func (s *Store) Remove(id, activePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.data {
		if item.ID == id {
			if samePath(item.Path, activePath) {
				return errors.New("cannot remove the active project")
			}
			previous := append([]Project(nil), s.data...)
			s.data = append(s.data[:i], s.data[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.data = previous
				return err
			}
			return nil
		}
	}
	return errors.New("project not found")
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) saveFoldersLocked() error {
	raw, err := json.MarshalIndent(s.folders, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.foldersPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.foldersPath)
}

func (s *Store) savePromptSettingsLocked() error {
	raw, err := json.MarshalIndent(PromptSettings{Enabled: s.globalPromptEnabled, GlobalPrompt: s.globalPrompt}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.promptSettingsPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.promptSettingsPath)
}

func projectAgentsPath(projectPath string) string {
	return filepath.Join(projectPath, "AGENTS.md")
}

func readProjectAgents(projectPath string) (string, error) {
	raw, err := os.ReadFile(projectAgentsPath(projectPath))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read AGENTS.md: %w", err)
	}
	if len(raw) > MaxPromptBytes {
		return "", fmt.Errorf("AGENTS.md exceeds %d bytes", MaxPromptBytes)
	}
	if !utf8.Valid(raw) {
		return "", errors.New("AGENTS.md must be valid UTF-8")
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeProjectAgents(projectPath, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	path := projectAgentsPath(projectPath)
	if prompt == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove AGENTS.md: %w", err)
		}
		return nil
	}
	if len([]byte(prompt)) > MaxPromptBytes {
		return fmt.Errorf("AGENTS.md exceeds %d bytes", MaxPromptBytes)
	}
	if err := os.WriteFile(path, []byte(prompt+"\n"), 0o600); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}
	return nil
}

func migrateLegacyPromptToAgents(projectPath, legacyPrompt string) error {
	legacyPrompt = strings.TrimSpace(legacyPrompt)
	if legacyPrompt == "" {
		return nil
	}
	existing, err := readProjectAgents(projectPath)
	if err != nil {
		return err
	}
	if existing == "" {
		return writeProjectAgents(projectPath, legacyPrompt)
	}
	if strings.Contains(existing, legacyPrompt) {
		return nil
	}
	combined := existing + "\n\n## MCP DevDesk migrated instructions\n\n" + legacyPrompt
	if len([]byte(combined)) > MaxPromptBytes {
		return fmt.Errorf("existing AGENTS.md plus migrated prompt exceeds %d bytes", MaxPromptBytes)
	}
	return writeProjectAgents(projectPath, combined)
}

func projectID(path string) string {
	value := uint64(1469598103934665603)
	for _, b := range []byte(strings.ToLower(path)) {
		value = (value ^ uint64(b)) * 1099511628211
	}
	return fmt.Sprintf("project-%016x", value)
}

func canonicalPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("project path is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func samePath(left, right string) bool {
	leftPath, leftErr := canonicalPath(left)
	rightPath, rightErr := canonicalPath(right)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return strings.EqualFold(leftPath, rightPath)
}

func normalizeProjects(items []Project) []Project {
	result := make([]Project, 0, len(items))
	indexes := make(map[string]int, len(items))
	for _, item := range items {
		path, err := canonicalPath(item.Path)
		if err != nil {
			continue
		}
		item.Path = path
		item.ID = projectID(path)
		item.Name = strings.TrimSpace(item.Name)
		item.Folder = normalizeFolder(item.Folder)
		item.Prompt = strings.TrimSpace(item.Prompt)
		if item.Name == "" {
			item.Name = filepath.Base(path)
		}
		key := strings.ToLower(path)
		if index, exists := indexes[key]; exists {
			existing := &result[index]
			if item.LastOpenedAt.After(existing.LastOpenedAt) {
				existing.LastOpenedAt = item.LastOpenedAt
			}
			if existing.AddedAt.IsZero() || (!item.AddedAt.IsZero() && item.AddedAt.Before(existing.AddedAt)) {
				existing.AddedAt = item.AddedAt
			}
			continue
		}
		indexes[key] = len(result)
		result = append(result, item)
	}
	return result
}

func normalizeFolder(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' })
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/")
}

func normalizeFolders(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = normalizeFolder(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}
