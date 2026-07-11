package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mcp-devdesk/internal/model"
)

type Values struct {
	OwnerPassword string   `json:"ownerPassword"`
	ClientID      string   `json:"clientId"`
	ClientSecret  string   `json:"clientSecret"`
	TokenSecret   string   `json:"tokenSecret"`
	RedirectURIs  []string `json:"redirectUris,omitempty"`
}

type secretEnvelope struct {
	Version    int    `json:"version"`
	Protection string `json:"protection"`
	Data       string `json:"data"`
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
		var envelope secretEnvelope
		if json.Unmarshal(raw, &envelope) == nil && envelope.Version == 2 && envelope.Data != "" {
			protected, decodeErr := base64.StdEncoding.DecodeString(envelope.Data)
			if decodeErr != nil {
				return Values{}, fmt.Errorf("decode stored secrets: %w", decodeErr)
			}
			plain, unprotectErr := unprotectData(protected)
			if unprotectErr != nil {
				return Values{}, fmt.Errorf("decrypt stored secrets: %w", unprotectErr)
			}
			var values Values
			if unmarshalErr := json.Unmarshal(plain, &values); unmarshalErr != nil {
				return Values{}, fmt.Errorf("parse decrypted secrets: %w", unmarshalErr)
			}
			if validateErr := validate(values); validateErr != nil {
				return Values{}, fmt.Errorf("validate decrypted secrets: %w", validateErr)
			}
			return values, nil
		}
		var values Values
		if err := json.Unmarshal(raw, &values); err == nil && validate(values) == nil {
			if saveErr := s.saveLocked(values); saveErr != nil {
				return Values{}, fmt.Errorf("migrate plaintext secrets: %w", saveErr)
			}
			return values, nil
		}
		return Values{}, errors.New("stored secrets file is invalid and was not overwritten")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Values{}, fmt.Errorf("read secrets: %w", err)
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
	plain, err := json.Marshal(values)
	if err != nil {
		return err
	}
	protected, err := protectData(plain)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}
	envelope := secretEnvelope{
		Version:    2,
		Protection: protectionName(),
		Data:       base64.StdEncoding.EncodeToString(protected),
	}
	raw, err := json.MarshalIndent(envelope, "", "  ")
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
		return model.SecretSummary{ClientID: values.ClientID, Configured: true, EncryptedAtRest: encryptionAvailable()}, nil
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
	if request.RedirectURIs != nil {
		values.RedirectURIs = append([]string(nil), (*request.RedirectURIs)...)
	}
	for index := range values.RedirectURIs {
		values.RedirectURIs[index] = strings.TrimSpace(values.RedirectURIs[index])
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
		OwnerPassword:   values.OwnerPassword,
		ClientID:        values.ClientID,
		ClientSecret:    values.ClientSecret,
		TokenSecret:     values.TokenSecret,
		Configured:      true,
		EncryptedAtRest: encryptionAvailable(),
		RedirectURIs:    append([]string(nil), values.RedirectURIs...),
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
	if len(values.RedirectURIs) > 20 {
		return errors.New("no more than 20 OAuth redirect URIs are allowed")
	}
	seen := make(map[string]struct{}, len(values.RedirectURIs))
	for index, redirectURI := range values.RedirectURIs {
		redirectURI = strings.TrimSpace(redirectURI)
		if redirectURI == "" {
			return fmt.Errorf("redirect URI %d is empty", index+1)
		}
		if _, exists := seen[redirectURI]; exists {
			return fmt.Errorf("redirect URI %d is duplicated", index+1)
		}
		if err := validateRedirectURI(redirectURI); err != nil {
			return fmt.Errorf("redirect URI %d: %w", index+1, err)
		}
		seen[redirectURI] = struct{}{}
		values.RedirectURIs[index] = redirectURI
	}
	return nil
}

func validateRedirectURI(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("must be an absolute URI")
	}
	if parsed.Fragment != "" || parsed.User != nil {
		return errors.New("must not contain a fragment or user info")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	if !strings.EqualFold(parsed.Scheme, "http") {
		return errors.New("must use HTTPS, except loopback HTTP callbacks")
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("HTTP callback host must be loopback")
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

// ProtectForCurrentUser protects arbitrary local application data using the
// same platform mechanism as the secret store. On Windows this is current-user
// DPAPI; non-Windows builds use the documented platform fallback.
func ProtectForCurrentUser(value []byte) ([]byte, error) {
	return protectData(value)
}

// UnprotectForCurrentUser reverses ProtectForCurrentUser.
func UnprotectForCurrentUser(value []byte) ([]byte, error) {
	return unprotectData(value)
}

func ProtectionName() string { return protectionName() }

func EncryptionAvailable() bool { return encryptionAvailable() }
