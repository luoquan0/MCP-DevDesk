//go:build linux

package mcpcore

import (
 "errors"
 "os/exec"
 "syscall"
)

func configureCommand(cmd *exec.Cmd) {
 cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid:true}
}

func shellCommand(line string) (string, []string) {
 return "/bin/sh", []string{"-c",line}
}

func terminateCommand(cmd *exec.Cmd) error {
 if cmd == nil || cmd.Process == nil { return nil }
 // Every command owns its own process group. Killing only the shell would
 // leave compilers/dev servers alive and their inherited output pipes open.
 if cmd.Process.Pid <= 1 { return errors.New("refusing invalid command process group") }
 err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
 if errors.Is(err,syscall.ESRCH) { return nil }
 return err
}
