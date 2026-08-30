package secrets

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mcp-devdesk/internal/model"
)

func TestStoreGeneratesUpdatesAndPersistsSecrets(t *testing.T) {
	dataDir := t.TempDir()
	store := NewStore(dataDir)

	initial, err := store.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.OwnerPassword) != 48 || len(initial.ClientSecret) != 64 || len(initial.TokenSecret) != 64 {
		t.Fatalf("unexpected generated secret lengths: %#v", initial)
	}

	ownerPassword := "custom-owner-password"
	clientID := "custom.client-id"
	clientSecret := "custom-client-secret-value"
	tokenSecret := strings.Repeat("ab", 32)
	updated, err := store.Update(model.SecretUpdateRequest{
		OwnerPassword: &ownerPassword,
		ClientID:      &clientID,
		ClientSecret:  &clientSecret,
		TokenSecret:   &tokenSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.OwnerPassword != ownerPassword || updated.ClientID != clientID || updated.ClientSecret != clientSecret || updated.TokenSecret != tokenSecret {
		t.Fatalf("unexpected update result: %#v", updated)
	}

	reloaded, err := NewStore(dataDir).GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.OwnerPassword != ownerPassword || reloaded.ClientID != clientID || reloaded.ClientSecret != clientSecret || reloaded.TokenSecret != tokenSecret {
		t.Fatalf("secrets were not persisted: %#v", reloaded)
	}
	stored, err := os.ReadFile(filepath.Join(dataDir, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encryptionAvailable() && (strings.Contains(string(stored), ownerPassword) || strings.Contains(string(stored), clientSecret) || strings.Contains(string(stored), tokenSecret)) {
		t.Fatal("encrypted secrets file contains plaintext credential values")
	}
	var envelope secretEnvelope
	if err := json.Unmarshal(stored, &envelope); err != nil || envelope.Version != 2 || envelope.Data == "" {
		t.Fatalf("unexpected secret envelope: %#v, %v", envelope, err)
	}
}

func TestWebControlPasswordPersistsInsideProtectedSecretStore(t *testing.T) {
	dataDir := t.TempDir()
	store := NewStore(dataDir)
	password := "phone-control-123"
	if err := store.SetWebControlPassword(password); err != nil {
		t.Fatal(err)
	}
	configured, err := store.WebControlPasswordConfigured()
	if err != nil || !configured {
		t.Fatalf("web control password configured=%v err=%v", configured, err)
	}
	stored, err := os.ReadFile(filepath.Join(dataDir, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), password) {
		t.Fatal("web control password was stored in plaintext")
	}
	reloaded, err := NewStore(dataDir).WebControlPassword()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != password {
		t.Fatalf("reloaded web control password = %q", reloaded)
	}
	if err := store.SetWebControlPassword("short"); err == nil {
		t.Fatal("short web control password was accepted")
	}
}

func TestPlaintextSecretsAreMigrated(t *testing.T) {
	dataDir := t.TempDir()
	values := Values{
		OwnerPassword: "plaintext-owner-password",
		ClientID:      "plaintext-client",
		ClientSecret:  "plaintext-client-secret",
		TokenSecret:   strings.Repeat("cd", 32),
	}
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "secrets.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStore(dataDir).GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, values) {
		t.Fatalf("migrated values changed: %#v", loaded)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope secretEnvelope
	if err := json.Unmarshal(migrated, &envelope); err != nil || envelope.Version != 2 {
		t.Fatalf("plaintext file was not migrated: %s, %v", string(migrated), err)
	}
}

func TestRedirectURIValidation(t *testing.T) {
	store := NewStore(t.TempDir())
	initial, err := store.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{"https://example.com/oauth/callback", "http://127.0.0.1:43210/callback"}
	updated, err := store.Update(model.SecretUpdateRequest{RedirectURIs: &valid})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.RedirectURIs, valid) {
		t.Fatalf("redirect URIs were not saved: %#v", updated.RedirectURIs)
	}
	invalid := []string{"http://example.com/callback"}
	if _, err := store.Update(model.SecretUpdateRequest{RedirectURIs: &invalid}); err == nil {
		t.Fatal("non-loopback HTTP redirect URI was accepted")
	}
	reloaded, err := store.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ClientID != initial.ClientID || !reflect.DeepEqual(reloaded.RedirectURIs, valid) {
		t.Fatalf("invalid update changed stored secrets: %#v", reloaded)
	}
}

func TestGenerateSecretDoesNotPersistUntilSaved(t *testing.T) {
	store := NewStore(t.TempDir())
	initial, err := store.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}

	generated, err := store.Generate("tokenSecret")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.TokenSecret) != 64 {
		t.Fatalf("expected 64-character token secret, got %d", len(generated.TokenSecret))
	}
	if _, err := hex.DecodeString(generated.TokenSecret); err != nil {
		t.Fatalf("generated token secret is not hexadecimal: %v", err)
	}
	after, err := store.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if after.TokenSecret != initial.TokenSecret {
		t.Fatal("generation should not persist before save")
	}
}

func TestUpdateRejectsInvalidTokenSecret(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.GetOrCreate(); err != nil {
		t.Fatal(err)
	}
	invalid := "not-hex"
	if _, err := store.Update(model.SecretUpdateRequest{TokenSecret: &invalid}); err == nil {
		t.Fatal("expected invalid token secret to be rejected")
	}
}
