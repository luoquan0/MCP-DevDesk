package model

import "time"

const CurrentConfigVersion = 1

type Config struct {
	Version                 int      `json:"version"`
	Workspace               string   `json:"workspace"`
	AllowedRoots            []string `json:"allowedRoots"`
	MCPHost                 string   `json:"mcpHost"`
	MCPPort                 int      `json:"mcpPort"`
	AdminHost               string   `json:"adminHost"`
	AdminPort               int      `json:"adminPort"`
	PermissionMode          string   `json:"permissionMode"`
	FileScope               string   `json:"fileScope"`
	ToolProfile             string   `json:"toolProfile"`
	AllowNetwork            bool     `json:"allowNetwork"`
	Domain                  string   `json:"domain"`
	TunnelName              string   `json:"tunnelName"`
	TunnelID                string   `json:"tunnelId"`
	ProxyAddress            string   `json:"proxyAddress"`
	ProxyUsername           string   `json:"proxyUsername"`
	ProxyPassword           string   `json:"proxyPassword,omitempty"`
	AutoStart               bool     `json:"autoStart"`
	Watchdog                bool     `json:"watchdog"`
	CoreExecutable          string   `json:"coreExecutable"`
	CloudflaredExecutable   string   `json:"cloudflaredExecutable"`
	OpenBrowserOnStart      bool     `json:"openBrowserOnStart"`
	HideChildProcessWindows bool     `json:"hideChildProcessWindows"`
}

type PublicConfig struct {
	Version                 int      `json:"version"`
	Workspace               string   `json:"workspace"`
	AllowedRoots            []string `json:"allowedRoots"`
	MCPHost                 string   `json:"mcpHost"`
	MCPPort                 int      `json:"mcpPort"`
	AdminHost               string   `json:"adminHost"`
	AdminPort               int      `json:"adminPort"`
	PermissionMode          string   `json:"permissionMode"`
	FileScope               string   `json:"fileScope"`
	ToolProfile             string   `json:"toolProfile"`
	AllowNetwork            bool     `json:"allowNetwork"`
	Domain                  string   `json:"domain"`
	TunnelName              string   `json:"tunnelName"`
	TunnelID                string   `json:"tunnelId"`
	ProxyAddress            string   `json:"proxyAddress"`
	ProxyUsername           string   `json:"proxyUsername"`
	HasProxyPassword        bool     `json:"hasProxyPassword"`
	AutoStart               bool     `json:"autoStart"`
	Watchdog                bool     `json:"watchdog"`
	CoreExecutable          string   `json:"coreExecutable"`
	CloudflaredExecutable   string   `json:"cloudflaredExecutable"`
	OpenBrowserOnStart      bool     `json:"openBrowserOnStart"`
	HideChildProcessWindows bool     `json:"hideChildProcessWindows"`
}

func (c Config) Public() PublicConfig {
	return PublicConfig{
		Version:                 c.Version,
		Workspace:               c.Workspace,
		AllowedRoots:            append([]string(nil), c.AllowedRoots...),
		MCPHost:                 c.MCPHost,
		MCPPort:                 c.MCPPort,
		AdminHost:               c.AdminHost,
		AdminPort:               c.AdminPort,
		PermissionMode:          c.PermissionMode,
		FileScope:               c.FileScope,
		ToolProfile:             c.ToolProfile,
		AllowNetwork:            c.AllowNetwork,
		Domain:                  c.Domain,
		TunnelName:              c.TunnelName,
		TunnelID:                c.TunnelID,
		ProxyAddress:            c.ProxyAddress,
		ProxyUsername:           c.ProxyUsername,
		HasProxyPassword:        c.ProxyPassword != "",
		AutoStart:               c.AutoStart,
		Watchdog:                c.Watchdog,
		CoreExecutable:          c.CoreExecutable,
		CloudflaredExecutable:   c.CloudflaredExecutable,
		OpenBrowserOnStart:      c.OpenBrowserOnStart,
		HideChildProcessWindows: c.HideChildProcessWindows,
	}
}

type ConfigUpdate struct {
	Workspace               *string   `json:"workspace"`
	AllowedRoots            *[]string `json:"allowedRoots"`
	MCPPort                 *int      `json:"mcpPort"`
	AdminPort               *int      `json:"adminPort"`
	PermissionMode          *string   `json:"permissionMode"`
	FileScope               *string   `json:"fileScope"`
	ToolProfile             *string   `json:"toolProfile"`
	AllowNetwork            *bool     `json:"allowNetwork"`
	Domain                  *string   `json:"domain"`
	TunnelName              *string   `json:"tunnelName"`
	ProxyAddress            *string   `json:"proxyAddress"`
	ProxyUsername           *string   `json:"proxyUsername"`
	ProxyPassword           *string   `json:"proxyPassword"`
	AutoStart               *bool     `json:"autoStart"`
	Watchdog                *bool     `json:"watchdog"`
	OpenBrowserOnStart      *bool     `json:"openBrowserOnStart"`
	HideChildProcessWindows *bool     `json:"hideChildProcessWindows"`
}

type ProcessStatus struct {
	Name       string     `json:"name"`
	Running    bool       `json:"running"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	StoppedAt  *time.Time `json:"stoppedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
	StdoutPath string     `json:"stdoutPath,omitempty"`
	StderrPath string     `json:"stderrPath,omitempty"`
}

type CloudflareStatus struct {
	Installed       bool   `json:"installed"`
	Authenticated   bool   `json:"authenticated"`
	LoginInProgress bool   `json:"loginInProgress"`
	CertificatePath string `json:"certificatePath"`
	TunnelID        string `json:"tunnelId"`
	TunnelName      string `json:"tunnelName"`
	Domain          string `json:"domain"`
	CredentialsPath string `json:"credentialsPath"`
}

type ServiceStatus struct {
	Version          string           `json:"version"`
	RootDirectory    string           `json:"rootDirectory"`
	DataDirectory    string           `json:"dataDirectory"`
	AdminURL         string           `json:"adminUrl"`
	LocalMCPURL      string           `json:"localMcpUrl"`
	RemoteMCPURL     string           `json:"remoteMcpUrl,omitempty"`
	AuthorizeURL     string           `json:"authorizeUrl,omitempty"`
	MCP              ProcessStatus    `json:"mcp"`
	Tunnel           ProcessStatus    `json:"tunnel"`
	Cloudflare       CloudflareStatus `json:"cloudflare"`
	PermissionMode   string           `json:"permissionMode"`
	FileScope        string           `json:"fileScope"`
	AllowNetwork     bool             `json:"allowNetwork"`
	WatchdogEnabled  bool             `json:"watchdogEnabled"`
	ConfigurationOK  bool             `json:"configurationOk"`
	ConfigurationMsg string           `json:"configurationMessage,omitempty"`
}

type ConfigureTunnelRequest struct {
	Domain     string `json:"domain"`
	TunnelName string `json:"tunnelName"`
	Reuse      bool   `json:"reuse"`
}

type ConfigureTunnelResult struct {
	TunnelID        string `json:"tunnelId"`
	TunnelName      string `json:"tunnelName"`
	Domain          string `json:"domain"`
	CredentialsPath string `json:"credentialsPath"`
	RemoteMCPURL    string `json:"remoteMcpUrl"`
	AuthorizeURL    string `json:"authorizeUrl"`
	Message         string `json:"message"`
}

type LogResponse struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

type SecretSummary struct {
	OwnerPassword string `json:"ownerPassword,omitempty"`
	ClientID      string `json:"clientId,omitempty"`
	ClientSecret  string `json:"clientSecret,omitempty"`
}
