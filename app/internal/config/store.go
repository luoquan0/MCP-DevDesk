package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mcp-devdesk/internal/model"
	secretstore "mcp-devdesk/internal/secrets"
)

var domainPattern = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

const protectedProxyPasswordPrefix = "dpapi:v1:"

type Store struct {
	mu      sync.RWMutex
	path    string
	rootDir string
	dataDir string
	current model.Config
}

func NewStore(rootDir, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	s := &Store{
		path:    filepath.Join(dataDir, "config.json"),
		rootDir: rootDir,
		dataDir: dataDir,
	}

	cfg := s.defaults()
	if raw, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if err := decodeProxyPassword(&cfg); err != nil {
			return nil, fmt.Errorf("decrypt proxy password: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config: %w", err)
	}

	s.normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	s.current = cfg
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) defaults() model.Config {
	workspace := s.rootDir
	core := filepath.Join(s.rootDir, "coding-tools-mcp.exe")
	goCore := defaultGoCoreExecutable(s.rootDir)
	cloudflared := filepath.Join(s.rootDir, "cloudflared.exe")

	return model.Config{
		Version:                 model.CurrentConfigVersion,
		Workspace:               workspace,
		AllowedRoots:            []string{workspace},
		MCPHost:                 "127.0.0.1",
		MCPPort:                 8765,
		AdminHost:               "127.0.0.1",
		AdminPort:               17860,
		PermissionMode:          "trusted",
		FileScope:               "workspace",
		ToolProfile:             "full",
		AllowNetwork:            true,
		TunnelName:              "mcp-devdesk",
		AutoStart:               false,
		Watchdog:                true,
		CoreMode:                "legacy",
		CoreExecutable:          core,
		GoCoreExecutable:        goCore,
		CloudflaredExecutable:   cloudflared,
		OpenBrowserOnStart:      true,
		HideChildProcessWindows: true,
		LoggingEnabled:          true,
	}
}

func defaultGoCoreExecutable(rootDir string) string {
	candidates := []string{
		filepath.Join(rootDir, "mcp-core.exe"),
		filepath.Join(rootDir, "dist", "mcp-core.exe"),
		filepath.Join(rootDir, "dist", "mcp-core-amd64.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func (s *Store) normalize(cfg *model.Config) {
	if cfg.Version == 0 {
		cfg.Version = model.CurrentConfigVersion
	}
	if cfg.Workspace == "" {
		cfg.Workspace = s.rootDir
	}
	if len(cfg.AllowedRoots) == 0 {
		cfg.AllowedRoots = []string{cfg.Workspace}
	}
	if cfg.MCPHost == "" {
		cfg.MCPHost = "127.0.0.1"
	}
	if cfg.MCPPort == 0 {
		cfg.MCPPort = 8765
	}
	if cfg.AdminHost == "" {
		cfg.AdminHost = "127.0.0.1"
	}
	if cfg.AdminPort == 0 {
		cfg.AdminPort = 17860
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = "trusted"
	}
	if cfg.FileScope == "" {
		cfg.FileScope = "workspace"
	}
	if cfg.ToolProfile == "" {
		cfg.ToolProfile = "full"
	}
	if cfg.TunnelName == "" {
		cfg.TunnelName = "mcp-devdesk"
	}
	if cfg.CoreMode == "" {
		cfg.CoreMode = "legacy"
	}
	if cfg.CoreExecutable == "" {
		cfg.CoreExecutable = filepath.Join(s.rootDir, "coding-tools-mcp.exe")
	}
	if cfg.GoCoreExecutable == "" {
		cfg.GoCoreExecutable = defaultGoCoreExecutable(s.rootDir)
	}
	if cfg.CloudflaredExecutable == "" {
		cfg.CloudflaredExecutable = filepath.Join(s.rootDir, "cloudflared.exe")
	}

	cfg.Workspace = cleanPath(cfg.Workspace)
	for i := range cfg.AllowedRoots {
		cfg.AllowedRoots[i] = cleanPath(cfg.AllowedRoots[i])
	}
	cfg.CoreExecutable = cleanPath(cfg.CoreExecutable)
	cfg.GoCoreExecutable = cleanPath(cfg.GoCoreExecutable)
	cfg.CloudflaredExecutable = cleanPath(cfg.CloudflaredExecutable)
	cfg.Domain = strings.ToLower(strings.TrimSpace(cfg.Domain))
	cfg.TunnelName = strings.TrimSpace(cfg.TunnelName)
	cfg.ProxyAddress = strings.TrimSpace(cfg.ProxyAddress)
	cfg.ProxyUsername = strings.TrimSpace(cfg.ProxyUsername)

	// Dangerous and trusted modes always support networking. Safe mode keeps
	// the explicit toggle so users can selectively allow package downloads.
	if cfg.PermissionMode == "trusted" || cfg.PermissionMode == "dangerous" {
		cfg.AllowNetwork = true
	}
}

func cleanPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	abs, err := filepath.Abs(value)
	if err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(value)
}

func Validate(cfg model.Config) error {
	if cfg.Version != model.CurrentConfigVersion {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.AdminHost != "127.0.0.1" && cfg.AdminHost != "localhost" && cfg.AdminHost != "::1" {
		return errors.New("admin host must be a loopback address")
	}
	if ip := net.ParseIP(cfg.MCPHost); ip == nil || !ip.IsLoopback() {
		return errors.New("MCP host must be a loopback IP")
	}
	if cfg.AdminPort < 1024 || cfg.AdminPort > 65535 {
		return errors.New("admin port must be between 1024 and 65535")
	}
	if cfg.MCPPort < 1024 || cfg.MCPPort > 65535 {
		return errors.New("MCP port must be between 1024 and 65535")
	}
	if cfg.AdminPort == cfg.MCPPort {
		return errors.New("admin and MCP ports must differ")
	}
	if cfg.Workspace == "" {
		return errors.New("workspace is required")
	}
	switch cfg.PermissionMode {
	case "safe", "trusted", "dangerous":
	default:
		return errors.New("permissionMode must be safe, trusted, or dangerous")
	}
	switch cfg.FileScope {
	case "workspace", "roots", "computer":
	default:
		return errors.New("fileScope must be workspace, roots, or computer")
	}
	switch cfg.ToolProfile {
	case "full", "read-only", "compat-readonly-all":
	default:
		return errors.New("unsupported tool profile")
	}
	switch cfg.CoreMode {
	case "legacy", "go":
	default:
		return errors.New("coreMode must be legacy or go")
	}
	if cfg.Domain != "" && !ValidDomain(cfg.Domain) {
		return errors.New("invalid domain")
	}
	if strings.ContainsAny(cfg.TunnelName, "\r\n\t") {
		return errors.New("invalid tunnel name")
	}
	return nil
}

func ValidDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return domainPattern.MatchString(domain)
}

func (s *Store) Get() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.current)
}

func (s *Store) Update(update model.ConfigUpdate) (model.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := cloneConfig(s.current)
	applyUpdate(&cfg, update)
	s.normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return model.Config{}, err
	}
	previous := s.current
	s.current = cfg
	if err := s.saveLocked(); err != nil {
		s.current = previous
		return model.Config{}, err
	}
	return cloneConfig(cfg), nil
}

func (s *Store) Replace(cfg model.Config) (model.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return model.Config{}, err
	}
	previous := s.current
	s.current = cloneConfig(cfg)
	if err := s.saveLocked(); err != nil {
		s.current = previous
		return model.Config{}, err
	}
	return cloneConfig(cfg), nil
}

func (s *Store) saveLocked() error {
	persisted := cloneConfig(s.current)
	if err := encodeProxyPassword(&persisted); err != nil {
		return fmt.Errorf("encrypt proxy password: %w", err)
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func encodeProxyPassword(cfg *model.Config) error {
	if cfg.ProxyPassword == "" || !secretstore.EncryptionAvailable() {
		return nil
	}
	protected, err := secretstore.ProtectForCurrentUser([]byte(cfg.ProxyPassword))
	if err != nil {
		return err
	}
	cfg.ProxyPassword = protectedProxyPasswordPrefix + base64.StdEncoding.EncodeToString(protected)
	return nil
}

func decodeProxyPassword(cfg *model.Config) error {
	if !strings.HasPrefix(cfg.ProxyPassword, protectedProxyPasswordPrefix) {
		return nil
	}
	protected, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(cfg.ProxyPassword, protectedProxyPasswordPrefix))
	if err != nil {
		return err
	}
	plain, err := secretstore.UnprotectForCurrentUser(protected)
	if err != nil {
		return err
	}
	cfg.ProxyPassword = string(plain)
	return nil
}

func applyUpdate(cfg *model.Config, update model.ConfigUpdate) {
	if update.Workspace != nil {
		cfg.Workspace = *update.Workspace
	}
	if update.AllowedRoots != nil {
		cfg.AllowedRoots = append([]string(nil), (*update.AllowedRoots)...)
	}
	if update.MCPPort != nil {
		cfg.MCPPort = *update.MCPPort
	}
	if update.AdminPort != nil {
		cfg.AdminPort = *update.AdminPort
	}
	if update.PermissionMode != nil {
		cfg.PermissionMode = *update.PermissionMode
	}
	if update.FileScope != nil {
		cfg.FileScope = *update.FileScope
	}
	if update.ToolProfile != nil {
		cfg.ToolProfile = *update.ToolProfile
	}
	if update.AllowNetwork != nil {
		cfg.AllowNetwork = *update.AllowNetwork
	}
	if update.Domain != nil {
		cfg.Domain = *update.Domain
	}
	if update.TunnelName != nil {
		cfg.TunnelName = *update.TunnelName
	}
	if update.ProxyAddress != nil {
		cfg.ProxyAddress = *update.ProxyAddress
	}
	if update.ProxyUsername != nil {
		cfg.ProxyUsername = *update.ProxyUsername
	}
	if update.ProxyPassword != nil {
		cfg.ProxyPassword = *update.ProxyPassword
	}
	if update.AutoStart != nil {
		cfg.AutoStart = *update.AutoStart
	}
	if update.Watchdog != nil {
		cfg.Watchdog = *update.Watchdog
	}
	if update.CoreMode != nil {
		cfg.CoreMode = *update.CoreMode
	}
	if update.OpenBrowserOnStart != nil {
		cfg.OpenBrowserOnStart = *update.OpenBrowserOnStart
	}
	if update.HideChildProcessWindows != nil {
		cfg.HideChildProcessWindows = *update.HideChildProcessWindows
	}
	if update.LoggingEnabled != nil {
		cfg.LoggingEnabled = *update.LoggingEnabled
	}
}

func cloneConfig(cfg model.Config) model.Config {
	copy := cfg
	copy.AllowedRoots = append([]string(nil), cfg.AllowedRoots...)
	return copy
}
