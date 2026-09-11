//go:build !windows

package application

import "os/exec"

func configureDetachedProcess(_ *exec.Cmd) {}
