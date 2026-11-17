//go:build !linux

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Retain the existing Windows stop implementation unchanged.
func stopManagedCommand(cmd *exec.Cmd) error {
	kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	configureChildProcess(kill, true)
	if output, err := kill.CombinedOutput(); err != nil {
		text := strings.ToLower(string(output))
		if !strings.Contains(text, "not found") && !strings.Contains(text, "no running instance") {
			return fmt.Errorf("stop PID %d: %w: %s", cmd.Process.Pid, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}
