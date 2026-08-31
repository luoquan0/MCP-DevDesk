package appearance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxBackgroundBytes = 15 << 20

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type Settings struct {
	Theme               string `json:"theme"`
	CustomColorsEnabled bool   `json:"customColorsEnabled"`
	PrimaryColor        string `json:"primaryColor"`
	SecondaryColor      string `json:"secondaryColor"`
	BackgroundOpacity   int    `json:"backgroundOpacity"`
	HasBackgroundImage  bool   `json:"hasBackgroundImage"`
	BackgroundRevision  int64  `json:"backgroundRevision"`
}

type Update struct {
	Theme               *string `json:"theme"`
	CustomColorsEnabled *bool   `json:"customColorsEnabled"`
	PrimaryColor        *string `json:"primaryColor"`
	SecondaryColor      *string `json:"secondaryColor"`
	BackgroundOpacity   *int    `json:"backgroundOpacity"`
}

type Store struct {
	mu             sync.RWMutex
	settingsPath   string
	backgroundPath string
	current        Settings
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		settingsPath:   filepath.Join(dataDir, "appearance.json"),
		backgroundPath: filepath.Join(dataDir, "appearance-background.bin"),
		current: Settings{
			Theme:             "system",
			PrimaryColor:      "#007aff",
			SecondaryColor:    "#5856d6",
			BackgroundOpacity: 30,
		},
	}
	if raw, err := os.ReadFile(s.settingsPath); err == nil {
		if err := json.Unmarshal(raw, &s.current); err != nil {
			return nil, fmt.Errorf("parse appearance settings: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read appearance settings: %w", err)
	}
	s.normalizeLocked()
	if info, err := os.Stat(s.backgroundPath); err == nil && !info.IsDir() {
		s.current.HasBackgroundImage = true
		if s.current.BackgroundRevision == 0 {
			s.current.BackgroundRevision = info.ModTime().UnixMilli()
		}
	} else {
		s.current.HasBackgroundImage = false
	}
	if err := validate(s.current); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *Store) Update(update Update) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.current
	if update.Theme != nil {
		next.Theme = *update.Theme
	}
	if update.CustomColorsEnabled != nil {
		next.CustomColorsEnabled = *update.CustomColorsEnabled
	}
	if update.PrimaryColor != nil {
		next.PrimaryColor = *update.PrimaryColor
	}
	if update.SecondaryColor != nil {
		next.SecondaryColor = *update.SecondaryColor
	}
	if update.BackgroundOpacity != nil {
		next.BackgroundOpacity = *update.BackgroundOpacity
	}
	next.Theme = strings.ToLower(strings.TrimSpace(next.Theme))
	next.PrimaryColor = strings.ToLower(strings.TrimSpace(next.PrimaryColor))
	next.SecondaryColor = strings.ToLower(strings.TrimSpace(next.SecondaryColor))
	if err := validate(next); err != nil {
		return Settings{}, err
	}
	previous := s.current
	s.current = next
	if err := s.saveLocked(); err != nil {
		s.current = previous
		return Settings{}, err
	}
	return s.current, nil
}

func (s *Store) SaveBackground(data []byte) (Settings, error) {
	if len(data) == 0 {
		return Settings{}, errors.New("background image is empty")
	}
	if len(data) > MaxBackgroundBytes {
		return Settings{}, fmt.Errorf("background image exceeds %d bytes", MaxBackgroundBytes)
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return Settings{}, errors.New("background image must be PNG, JPEG, GIF, or WebP")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := s.backgroundPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return Settings{}, fmt.Errorf("write background image: %w", err)
	}
	if err := os.Rename(tmp, s.backgroundPath); err != nil {
		_ = os.Remove(tmp)
		return Settings{}, fmt.Errorf("replace background image: %w", err)
	}
	previous := s.current
	s.current.HasBackgroundImage = true
	s.current.BackgroundRevision = time.Now().UnixMilli()
	if err := s.saveLocked(); err != nil {
		s.current = previous
		return Settings{}, err
	}
	return s.current, nil
}

func (s *Store) RemoveBackground() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.backgroundPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Settings{}, fmt.Errorf("remove background image: %w", err)
	}
	previous := s.current
	s.current.HasBackgroundImage = false
	s.current.BackgroundRevision = time.Now().UnixMilli()
	if err := s.saveLocked(); err != nil {
		s.current = previous
		return Settings{}, err
	}
	return s.current, nil
}

func (s *Store) BackgroundPath() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backgroundPath, s.current.HasBackgroundImage
}

func (s *Store) normalizeLocked() {
	if strings.TrimSpace(s.current.Theme) == "" {
		s.current.Theme = "system"
	}
	s.current.Theme = strings.ToLower(strings.TrimSpace(s.current.Theme))
	if strings.TrimSpace(s.current.PrimaryColor) == "" {
		s.current.PrimaryColor = "#007aff"
	}
	if strings.TrimSpace(s.current.SecondaryColor) == "" {
		s.current.SecondaryColor = "#5856d6"
	}
	s.current.PrimaryColor = strings.ToLower(strings.TrimSpace(s.current.PrimaryColor))
	s.current.SecondaryColor = strings.ToLower(strings.TrimSpace(s.current.SecondaryColor))
	if s.current.BackgroundOpacity < 0 || s.current.BackgroundOpacity > 100 {
		s.current.BackgroundOpacity = 30
	}
}

func validate(settings Settings) error {
	switch settings.Theme {
	case "system", "light", "dark":
	default:
		return errors.New("theme must be system, light, or dark")
	}
	if !hexColorPattern.MatchString(settings.PrimaryColor) {
		return errors.New("primaryColor must be a six-digit hex color")
	}
	if !hexColorPattern.MatchString(settings.SecondaryColor) {
		return errors.New("secondaryColor must be a six-digit hex color")
	}
	if settings.BackgroundOpacity < 0 || settings.BackgroundOpacity > 100 {
		return errors.New("backgroundOpacity must be between 0 and 100")
	}
	return nil
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.current, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.settingsPath + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.settingsPath)
}
