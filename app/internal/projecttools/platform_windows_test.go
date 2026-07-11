//go:build windows

package projecttools

import (
	"os/exec"
	"testing"
)

func TestConfigureCommandHidesWindow(t *testing.T) {
	command := exec.Command("git.exe", "--version")
	configureCommand(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("expected background Git commands to hide their console window")
	}
}
