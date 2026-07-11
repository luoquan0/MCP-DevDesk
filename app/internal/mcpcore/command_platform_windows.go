//go:build windows

package mcpcore

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func shellCommand(commandLine string) (string, []string) {
	return "cmd.exe", []string{"/d", "/s", "/c", commandLine}
}

func terminateCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	taskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	taskkill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := taskkill.CombinedOutput(); err != nil {
		if killErr := cmd.Process.Kill(); killErr != nil {
			return fmt.Errorf("terminate process tree: %v (%s); fallback kill: %w", err, string(output), killErr)
		}
	}
	return nil
}
