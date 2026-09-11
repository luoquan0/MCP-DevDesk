//go:build !windows

package agentstate

import "os/exec"

func configureBackgroundCommand(_ *exec.Cmd) {}
