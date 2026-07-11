package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	AddedAt      time.Time `json:"addedAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data []Project
}

func NewStore(dataDir, initialWorkspace string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "projects.json")}
	if raw, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("parse projects: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read projects: %w", err)
	}
	if len(s.data) == 0 && initialWorkspace != "" {
		if _, err := s.Add("", initialWorkspace); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) List() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]Project(nil), s.data...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].LastOpenedAt.After(items[j].LastOpenedAt) })
	return items
}

func (s *Store) Add(name, path string) (Project, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	abs = filepath.Clean(abs)
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
		if strings.EqualFold(item.Path, abs) {
			return item, nil
		}
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
	defer s.mu.RUnlock()
	for _, item := range s.data {
		if item.ID == id {
			return item, true
		}
	}
	return Project{}, false
}

func (s *Store) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data {
		if s.data[i].ID == id {
			s.data[i].LastOpenedAt = time.Now().UTC()
			return s.saveLocked()
		}
	}
	return errors.New("project not found")
}

func (s *Store) Remove(id, activePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.data {
		if item.ID == id {
			if strings.EqualFold(item.Path, activePath) {
				return errors.New("cannot remove the active project")
			}
			s.data = append(s.data[:i], s.data[i+1:]...)
			return s.saveLocked()
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

func projectID(path string) string {
	value := uint64(1469598103934665603)
	for _, b := range []byte(strings.ToLower(path)) {
		value = (value ^ uint64(b)) * 1099511628211
	}
	return fmt.Sprintf("project-%016x", value)
}
