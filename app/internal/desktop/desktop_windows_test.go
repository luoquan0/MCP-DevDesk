//go:build windows

package desktop

import (
	"strings"
	"testing"
)

func TestValidDriveLetter(t *testing.T) {
	for _, value := range []string{"C:", "d:"} {
		if !validDriveLetter(value) {
			t.Fatalf("expected valid drive: %q", value)
		}
	}
	for _, value := range []string{"", "%SystemDrive%", "C", "CC:"} {
		if validDriveLetter(value) {
			t.Fatalf("expected invalid drive: %q", value)
		}
	}
}

func TestNormalizedWindowsEnvironmentResolvesPlaceholders(t *testing.T) {
	environment := normalizedWindowsEnvironment()
	for _, entry := range environment {
		if strings.Contains(strings.ToLower(entry), "%systemdrive%") {
			t.Fatalf("unresolved SystemDrive placeholder: %s", entry)
		}
	}
}
