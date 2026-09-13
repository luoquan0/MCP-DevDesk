//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"os"
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
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}
	taskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	taskkill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := taskkill.Run(); err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil
		}
		if killErr := cmd.Process.Kill(); killErr != nil {
			if errors.Is(killErr, os.ErrProcessDone) || errors.Is(killErr, syscall.EINVAL) {
				return nil
			}
			return fmt.Errorf("terminate process tree: taskkill failed: %v; fallback kill: %w", err, killErr)
		}
	}
	return nil
}
