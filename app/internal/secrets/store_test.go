package secrets

import (
	"encoding/hex"
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
