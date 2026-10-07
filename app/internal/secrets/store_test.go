package secrets

import (
	"encoding/base64"
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
	if err := json.Unmarshal(stored, &envelope); err != nil || envelope.Version != portableSecretEnvelopeVersion || envelope.Protection != portableSecretProtection || envelope.Data == "" {
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
	if err := json.Unmarshal(migrated, &envelope); err != nil || envelope.Version != portableSecretEnvelopeVersion {
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


func TestPortableSecretsSurviveDirectoryCopy(t *testing.T) {
	sourceDir := t.TempDir()
	sourceStore := NewStore(sourceDir)
	source, err := sourceStore.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	password := "portable-web-password"
	if err := sourceStore.SetWebControlPassword(password); err != nil {
		t.Fatal(err)
	}
	source, err = sourceStore.GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()
	for _, name := range []string{"secrets.json", portableMasterKeyName} {
		raw, err := os.ReadFile(filepath.Join(sourceDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	copied, err := NewStore(targetDir).GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copied, source) {
		t.Fatalf("portable copy changed secrets: got %#v want %#v", copied, source)
	}
	if copied.WebControlPassword != password {
		t.Fatalf("portable web password = %q", copied.WebControlPassword)
	}
}

func TestLegacyVersion2SecretsMigrateToPortableEnvelope(t *testing.T) {
	dataDir := t.TempDir()
	values := Values{
		OwnerPassword: "legacy-owner-password",
		ClientID: "legacy-client",
		ClientSecret: "legacy-client-secret-value",
		TokenSecret: strings.Repeat("ef", 32),
		WebControlPassword: "legacy-web-password",
	}
	plain, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protectData(plain)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(secretEnvelope{
		Version: 2,
		Protection: protectionName(),
		Data: base64.StdEncoding.EncodeToString(protected),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "secrets.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewStore(dataDir).GetOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, values) {
		t.Fatalf("legacy migration changed values: got %#v want %#v", loaded, values)
	}
	migratedRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope secretEnvelope
	if err := json.Unmarshal(migratedRaw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != portableSecretEnvelopeVersion || envelope.Protection != portableSecretProtection {
		t.Fatalf("legacy envelope was not migrated: %#v", envelope)
	}
	if _, err := os.Stat(filepath.Join(dataDir, portableMasterKeyName)); err != nil {
		t.Fatalf("portable master key missing after migration: %v", err)
	}
}
