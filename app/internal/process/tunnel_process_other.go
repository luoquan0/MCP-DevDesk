//go:build !windows

package process

import (
	"errors"

	"mcp-devdesk/internal/model"
)

func ListCloudflaredProcesses() ([]model.TunnelProcess, error) {
	return []model.TunnelProcess{}, nil
}

func StopCloudflaredProcess(_ int) error {
	return errors.New("cloudflared process control is only supported on Windows")
}
