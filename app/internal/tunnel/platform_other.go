//go:build !windows

package tunnel

import "os/exec"

func configureCommand(_ *exec.Cmd) {}
