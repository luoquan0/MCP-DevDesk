package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"mcp-devdesk/internal/model"
)

type Values struct {
	OwnerPassword string `json:"ownerPassword"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	TokenSecret   string `json:"tokenSecret"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "secrets.json")}
}

func (s *Store) GetOrCreate() (Values, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if raw, err := os.ReadFile(s.path); err == nil {
		var values Values
		if err := json.Unmarshal(raw, &values); err == nil && valid(values) {
			return values, nil
		}
	}

	values := Values{
		OwnerPassword: randomHex(24),
		ClientID:      "mcp-devdesk",
		ClientSecret:  randomHex(32),
		TokenSecret:   randomHex(32),
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return Values{}, fmt.Errorf("create secrets directory: %w", err)
	}
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return Values{}, err
	}
	if err := os.WriteFile(s.path, append(raw, '\n'), 0o600); err != nil {
		return Values{}, fmt.Errorf("save secrets: %w", err)
	}
	return values, nil
}

func (s *Store) Summary(reveal bool) (model.SecretSummary, error) {
	values, err := s.GetOrCreate()
	if err != nil {
		return model.SecretSummary{}, err
	}
	if !reveal {
		return model.SecretSummary{ClientID: values.ClientID}, nil
	}
	return model.SecretSummary{
		OwnerPassword: values.OwnerPassword,
		ClientID:      values.ClientID,
		ClientSecret:  values.ClientSecret,
	}, nil
}

func valid(values Values) bool {
	return values.OwnerPassword != "" && values.ClientID != "" && values.ClientSecret != "" && len(values.TokenSecret) == 64
}

func randomHex(bytesCount int) string {
	buffer := make([]byte, bytesCount)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("secure random generation failed: %v", err))
	}
	return hex.EncodeToString(buffer)
}
