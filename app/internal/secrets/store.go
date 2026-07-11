package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "secrets.json")}
}

func (s *Store) GetOrCreate() (Values, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getOrCreateLocked()
}

func (s *Store) getOrCreateLocked() (Values, error) {
	if raw, err := os.ReadFile(s.path); err == nil {
		var values Values
		if err := json.Unmarshal(raw, &values); err == nil && validate(values) == nil {
			return values, nil
		}
	}

	ownerPassword, err := randomHex(24)
	if err != nil {
		return Values{}, err
	}
	clientSecret, err := randomHex(32)
	if err != nil {
		return Values{}, err
	}
	tokenSecret, err := randomHex(32)
	if err != nil {
		return Values{}, err
	}
	values := Values{
		OwnerPassword: ownerPassword,
		ClientID:      "mcp-devdesk",
		ClientSecret:  clientSecret,
		TokenSecret:   tokenSecret,
	}
	if err := s.saveLocked(values); err != nil {
		return Values{}, err
	}
	return values, nil
}

func (s *Store) saveLocked(values Values) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create secrets directory: %w", err)
	}
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("save temporary secrets: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace secrets: %w", err)
	}
	return nil
}

func (s *Store) Summary(reveal bool) (model.SecretSummary, error) {
	values, err := s.GetOrCreate()
	if err != nil {
		return model.SecretSummary{}, err
	}
	if !reveal {
		return model.SecretSummary{ClientID: values.ClientID, Configured: true}, nil
	}
	return summary(values), nil
}

func (s *Store) Update(request model.SecretUpdateRequest) (model.SecretSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	values, err := s.getOrCreateLocked()
	if err != nil {
		return model.SecretSummary{}, err
	}
	if request.OwnerPassword != nil {
		values.OwnerPassword = *request.OwnerPassword
	}
	if request.ClientID != nil {
		values.ClientID = *request.ClientID
	}
	if request.ClientSecret != nil {
		values.ClientSecret = *request.ClientSecret
	}
	if request.TokenSecret != nil {
		values.TokenSecret = *request.TokenSecret
	}
	if err := validate(values); err != nil {
		return model.SecretSummary{}, err
	}
	if err := s.saveLocked(values); err != nil {
		return model.SecretSummary{}, err
	}
	return summary(values), nil
}

func (s *Store) Generate(field string) (model.SecretSummary, error) {
	field = strings.TrimSpace(field)
	result := model.SecretSummary{Configured: false}
	generate := func(bytesCount int) (string, error) { return randomHex(bytesCount) }

	switch field {
	case "ownerPassword":
		value, err := generate(24)
		result.OwnerPassword = value
		return result, err
	case "clientId":
		value, err := generate(8)
		result.ClientID = "mcp-devdesk-" + value
		return result, err
	case "clientSecret":
		value, err := generate(32)
		result.ClientSecret = value
		return result, err
	case "tokenSecret":
		value, err := generate(32)
		result.TokenSecret = value
		return result, err
	case "all", "":
		ownerPassword, err := generate(24)
		if err != nil {
			return model.SecretSummary{}, err
		}
		clientSecret, err := generate(32)
		if err != nil {
			return model.SecretSummary{}, err
		}
		tokenSecret, err := generate(32)
		if err != nil {
			return model.SecretSummary{}, err
		}
		result.OwnerPassword = ownerPassword
		result.ClientSecret = clientSecret
		result.TokenSecret = tokenSecret
		return result, nil
	default:
		return model.SecretSummary{}, errors.New("unknown secret field")
	}
}

func summary(values Values) model.SecretSummary {
	return model.SecretSummary{
		OwnerPassword: values.OwnerPassword,
		ClientID:      values.ClientID,
		ClientSecret:  values.ClientSecret,
		TokenSecret:   values.TokenSecret,
		Configured:    true,
	}
}

func validate(values Values) error {
	if length := len(values.OwnerPassword); length < 12 || length > 256 {
		return errors.New("owner password must be between 12 and 256 characters")
	}
	if hasControl(values.OwnerPassword) {
		return errors.New("owner password cannot contain control characters")
	}
	if length := len(values.ClientID); length < 3 || length > 128 || !clientIDPattern.MatchString(values.ClientID) {
		return errors.New("client ID must be 3 to 128 characters using letters, numbers, dot, underscore, colon, or dash")
	}
	if length := len(values.ClientSecret); length < 16 || length > 512 {
		return errors.New("client secret must be between 16 and 512 characters")
	}
	if hasControl(values.ClientSecret) {
		return errors.New("client secret cannot contain control characters")
	}
	if len(values.TokenSecret) != 64 {
		return errors.New("token secret must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(values.TokenSecret); err != nil {
		return errors.New("token secret must contain exactly 64 hexadecimal characters")
	}
	return nil
}

func hasControl(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}

func randomHex(bytesCount int) (string, error) {
	buffer := make([]byte, bytesCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("secure random generation failed: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
