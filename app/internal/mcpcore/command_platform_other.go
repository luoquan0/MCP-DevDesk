//go:build !windows

package mcpcore

import "os/exec"

func configureCommand(_ *exec.Cmd) {}

func shellCommand(commandLine string) (string, []string) {
	return "/bin/sh", []string{"-lc", commandLine}
}

func terminateCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
