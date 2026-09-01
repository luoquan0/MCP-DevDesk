//go:build windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func configureChildProcess(cmd *exec.Cmd, hidden bool) {
	// cloudflared refuses `tunnel login` when the default origin certificate
	// already exists. Treat an explicit login action as credential rotation so
	// the browser authorization flow can be started again after the old cert
	// expires or becomes unusable.
	if isCloudflareLoginCommand(cmd.Args) {
		_ = clearCloudflareLoginCertificate(CertificatePath())
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    hidden,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func isCloudflareLoginCommand(args []string) bool {
	if len(args) < 3 {
		return false
	}
	name := strings.ToLower(filepath.Base(args[0]))
	if name != "cloudflared.exe" && name != "cloudflared" {
		return false
	}
	for index := 1; index+1 < len(args); index++ {
		if strings.EqualFold(args[index], "tunnel") && strings.EqualFold(args[index+1], "login") {
			return true
		}
	}
	return false
}

func clearCloudflareLoginCertificate(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
