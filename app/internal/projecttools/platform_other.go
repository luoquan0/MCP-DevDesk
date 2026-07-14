//go:build !windows

package projecttools

import "os/exec"

func configureCommand(_ *exec.Cmd) {}
func terminateGitProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
