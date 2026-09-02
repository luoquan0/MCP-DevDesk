package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mcp-devdesk/internal/model"
	"mcp-devdesk/internal/statefiles"
)

// NewRecoveringStore is used for the primary portable application config.
// Corrupt or machine-bound fields are backed up instead of preventing the
// whole desktop app from starting. Additional MCP instance configs continue to
// use NewStore so a broken instance can be skipped without silently changing
// its workspace or port.
func NewRecoveringStore(rootDir, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	s := &Store{
		path:    filepath.Join(dataDir, "config.json"),
		rootDir: rootDir,
		dataDir: dataDir,
	}

	cfg := s.defaults()
	loaded := false
	if raw, err := os.ReadFile(s.path); err == nil {
		var persisted model.Config
		if err := json.Unmarshal(raw, &persisted); err != nil {
			if _, backupErr := statefiles.Quarantine(s.path, "parse-config"); backupErr != nil {
				return nil, fmt.Errorf("parse config: %w; quarantine failed: %v", err, backupErr)
			}
		} else {
			cfg = persisted
			loaded = true
			if err := decodeProxyPassword(&cfg); err != nil {
				if _, backupErr := statefiles.Backup(s.path, "proxy-password-decrypt"); backupErr != nil {
					return nil, fmt.Errorf("decrypt proxy password: %w; backup failed: %v", err, backupErr)
				}
				// DPAPI ciphertext is bound to the Windows user/machine. Keep every
				// other portable setting and only require the proxy password again.
				cfg.ProxyPassword = ""
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config: %w", err)
	}

	s.normalize(&cfg)
	if err := Validate(cfg); err != nil {
		if loaded {
			if _, backupErr := statefiles.Quarantine(s.path, "validate-config"); backupErr != nil {
				return nil, fmt.Errorf("validate config: %w; quarantine failed: %v", err, backupErr)
			}
		}
		cfg = s.defaults()
		s.normalize(&cfg)
		if validateErr := Validate(cfg); validateErr != nil {
			return nil, fmt.Errorf("validate recovered config: %w", validateErr)
		}
	}

	s.current = cfg
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}
