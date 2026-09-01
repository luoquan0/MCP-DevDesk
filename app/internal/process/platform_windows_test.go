//go:build windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestConfigureChildProcessHidesConsoleWindow(t *testing.T) {
	command := exec.Command("netstat.exe", "-ano")
	configureChildProcess(command, true)
	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not configured")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("child console window is not hidden")
	}
	if command.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("child process group flag is missing")
	}
}

func TestCloudflareLoginCommandDetection(t *testing.T) {
	if !isCloudflareLoginCommand([]string{`C:\Tools\cloudflared.exe`, "tunnel", "login"}) {
		t.Fatal("expected cloudflared tunnel login to be detected")
	}
	if !isCloudflareLoginCommand([]string{"cloudflared", "--no-autoupdate", "tunnel", "login"}) {
		t.Fatal("expected cloudflared login with global flags to be detected")
	}
	if isCloudflareLoginCommand([]string{`C:\Tools\cloudflared.exe`, "tunnel", "run", "mcp-devdesk"}) {
		t.Fatal("tunnel run must not be treated as a login")
	}
	if isCloudflareLoginCommand([]string{`C:\Tools\other.exe`, "tunnel", "login"}) {
		t.Fatal("non-cloudflared command must not be treated as a login")
	}
}

func TestClearCloudflareLoginCertificate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(path, []byte("expired certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearCloudflareLoginCertificate(path); err != nil {
		t.Fatalf("clear certificate: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("certificate still exists, stat err = %v", err)
	}
	if err := clearCloudflareLoginCertificate(path); err != nil {
		t.Fatalf("clearing an already missing certificate should be idempotent: %v", err)
	}
}
