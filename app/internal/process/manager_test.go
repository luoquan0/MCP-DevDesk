package process

import "testing"

func TestUserHomeDirIsAvailable(t *testing.T) {
	if home := UserHomeDir(); home == "" {
		t.Fatal("expected a Windows user home directory")
	}
}
