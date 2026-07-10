//go:build !windows

package process

import "os/exec"

func configureChildProcess(_ *exec.Cmd, _ bool) {}
