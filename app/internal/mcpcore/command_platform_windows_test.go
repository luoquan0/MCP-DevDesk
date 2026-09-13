//go:build windows

package mcpcore

import (
	"os/exec"
	"testing"
)

func TestTerminateCommandIsIdempotentAfterWait(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "ping 127.0.0.1 -n 31 >nul")
	configureCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	if err := terminateCommand(cmd); err != nil {
		t.Fatalf("first terminate: %v", err)
	}
	_ = cmd.Wait()
	if err := terminateCommand(cmd); err != nil {
		t.Fatalf("repeated terminate after Wait must be idempotent: %v", err)
	}
}
