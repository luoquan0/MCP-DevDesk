package instances

import (
	"crypto/rand"
	"encoding/hex"
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

const currentVersion = 1

type Record struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ProjectID string    `json:"projectId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type persisted struct {
	Version   int      `json:"version"`
	Instances []Record `json:"instances"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	instancesDir string
	records      map[string]Record
}

func NewStore(dataDir string) (*Store, error) {
	instancesDir := filepath.Join(dataDir, "instances")
	if err := os.MkdirAll(instancesDir, 0o700); err != nil {
		return nil, fmt.Errorf("create instances directory: %w", err)
	}
	s := &Store{
		path:         filepath.Join(dataDir, "instances.json"),
		instancesDir: instancesDir,
		records:      map[string]Record{},
	}
	if raw, err := os.ReadFile(s.path); err == nil {
		var stored persisted
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, fmt.Errorf("parse instances index: %w", err)
		}
		if stored.Version != currentVersion {
			return nil, fmt.Errorf("unsupported instances index version %d", stored.Version)
		}
		for _, record := range stored.Instances {
			if err := validateRecord(record); err != nil {
				return nil, fmt.Errorf("invalid instance %q: %w", record.ID, err)
			}
			if _, exists := s.records[record.ID]; exists {
				return nil, fmt.Errorf("duplicate instance id %q", record.ID)
			}
			s.records[record.ID] = record
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read instances index: %w", err)
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	return record, ok
}

func (s *Store) Add(name, projectID string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" {
		return Record{}, errors.New("instance name is required")
	}
	for _, existing := range s.records {
		if strings.EqualFold(existing.Name, name) {
			return Record{}, errors.New("instance name is already in use")
		}
	}
	id, err := randomID()
	if err != nil {
		return Record{}, err
	}
	now := time.Now()
	record := Record{ID: id, Name: name, ProjectID: projectID, CreatedAt: now, UpdatedAt: now}
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	s.records[id] = record
	if err := os.MkdirAll(s.DataDir(id), 0o700); err != nil {
		delete(s.records, id)
		return Record{}, fmt.Errorf("create instance data directory: %w", err)
	}
	if err := s.saveLocked(); err != nil {
		delete(s.records, id)
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Update(id, name, projectID string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, errors.New("instance not found")
	}
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" {
		return Record{}, errors.New("instance name is required")
	}
	for otherID, existing := range s.records {
		if otherID != id && strings.EqualFold(existing.Name, name) {
			return Record{}, errors.New("instance name is already in use")
		}
	}
	record.Name = name
	record.ProjectID = projectID
	record.UpdatedAt = time.Now()
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	s.records[id] = record
	if err := s.saveLocked(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Touch(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, errors.New("instance not found")
	}
	record.UpdatedAt = time.Now()
	s.records[id] = record
	if err := s.saveLocked(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return errors.New("instance not found")
	}
	delete(s.records, id)
	return s.saveLocked()
}

func (s *Store) DataDir(id string) string {
	return filepath.Join(s.instancesDir, id)
}

func (s *Store) saveLocked() error {
	items := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		items = append(items, record)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	raw, err := json.MarshalIndent(persisted{Version: currentVersion, Instances: items}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instances index: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write instances index: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace instances index: %w", err)
	}
	return nil
}

func validateRecord(record Record) error {
	if len(record.ID) < 8 || strings.ContainsAny(record.ID, "\\/\r\n\t ") {
		return errors.New("invalid instance id")
	}
	name := strings.TrimSpace(record.Name)
	if name == "" || len([]rune(name)) > 80 || strings.ContainsAny(name, "\r\n\t") {
		return errors.New("instance name must be 1 to 80 characters")
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("instance timestamps are required")
	}
	return nil
}

func randomID() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate instance id: %w", err)
	}
	return "mcp-" + hex.EncodeToString(buffer), nil
}
