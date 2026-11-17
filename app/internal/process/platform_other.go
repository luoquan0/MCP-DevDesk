//go:build !windows && !linux

package process

import "os/exec"

func configureChildProcess(_ *exec.Cmd, _ bool) {}
