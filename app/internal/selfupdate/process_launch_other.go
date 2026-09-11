//go:build !windows

package selfupdate

import "os/exec"

func configureRestartCommand(_ *exec.Cmd) {}
