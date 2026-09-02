package statefiles

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeLabel = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func RecoveryDir(path string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(path)), "recovery")
}

func Quarantine(path, label string) (string, error) {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	target, err := recoveryTarget(path, label)
	if err != nil {
		return "", err
	}
	if err := os.Rename(path, target); err == nil {
		return target, nil
	}
	if err := copyFile(path, target); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return target, nil
}

func Backup(path, label string) (string, error) {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	target, err := recoveryTarget(path, label)
	if err != nil {
		return "", err
	}
	if err := copyFile(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func recoveryTarget(path, label string) (string, error) {
	dir := RecoveryDir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	label = strings.Trim(unsafeLabel.ReplaceAllString(strings.TrimSpace(label), "-"), "-._")
	if label == "" {
		label = "invalid"
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	return filepath.Join(dir, fmt.Sprintf("%s.%s.%s.bak", filepath.Base(path), stamp, label)), nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	return nil
}
