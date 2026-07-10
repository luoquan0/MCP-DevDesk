//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

func configureChildProcess(cmd *exec.Cmd, hidden bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    hidden,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
