//go:build !windows

package secrets

func protectData(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

func unprotectData(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

func protectionName() string    { return "plain-platform-fallback" }
func encryptionAvailable() bool { return false }
