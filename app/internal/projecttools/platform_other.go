//go:build !windows

package projecttools

import "os/exec"

func configureCommand(_ *exec.Cmd) {}
