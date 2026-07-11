//go:build windows

package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureChildProcessHidesConsoleWindow(t *testing.T) {
	command := exec.Command("netstat.exe", "-ano")
	configureChildProcess(command, true)
	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not configured")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("child console window is not hidden")
	}
	if command.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("child process group flag is missing")
	}
}
