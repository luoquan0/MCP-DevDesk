package model

import "time"

const CurrentConfigVersion = 1

type ScreenRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ScreenWindowInfo struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	ProcessID   uint32     `json:"processId"`
	ProcessName string     `json:"processName,omitempty"`
	Bounds      ScreenRect `json:"bounds"`
	Active      bool       `json:"active"`
	Minimized   bool       `json:"minimized"`
}

type Config struct {
	Version                      int      `json:"version"`
	Workspace                    string   `json:"workspace"`
	AllowedRoots                 []string `json:"allowedRoots"`
	MCPHost                      string   `json:"mcpHost"`
	MCPPort                      int      `json:"mcpPort"`
	AdminHost                    string   `json:"adminHost"`
	AdminPort                    int      `json:"adminPort"`
	WebControlEnabled            bool     `json:"webControlEnabled"`
	WebControlPort               int      `json:"webControlPort"`
	WebControlLANEnabled         bool     `json:"webControlLanEnabled"`
	WebControlAuthEnabled        bool     `json:"webControlAuthEnabled"`
	PermissionMode               string   `json:"permissionMode"`
	FileScope                    string   `json:"fileScope"`
	ToolProfile                  string   `json:"toolProfile"`
	AllowNetwork                 bool     `json:"allowNetwork"`
	ScreenCaptureEnabled         bool     `json:"screenCaptureEnabled"`
	ScreenCaptureMode            string   `json:"screenCaptureMode"`
	ScreenCaptureWindowID        string   `json:"screenCaptureWindowId"`
	ScreenCaptureWindowProcessID uint32   `json:"screenCaptureWindowProcessId"`
	ScreenCaptureWindowTitle     string   `json:"screenCaptureWindowTitle"`
	ScreenCaptureWindowProcess   string   `json:"screenCaptureWindowProcess"`
	Domain                       string   `json:"domain"`
	TunnelName                   string   `json:"tunnelName"`
	TunnelID                     string   `json:"tunnelId"`
	ProxyAddress                 string   `json:"proxyAddress"`
	ProxyUsername                string   `json:"proxyUsername"`
	ProxyPassword                string   `json:"proxyPassword,omitempty"`
	AutoStart                    bool     `json:"autoStart"`
	Watchdog                     bool     `json:"watchdog"`
	CoreMode                     string   `json:"coreMode"`
	CoreExecutable               string   `json:"coreExecutable"`
	GoCoreExecutable             string   `json:"goCoreExecutable"`
	CloudflaredExecutable        string   `json:"cloudflaredExecutable"`
	OpenBrowserOnStart           bool     `json:"openBrowserOnStart"`
	HideChildProcessWindows      bool     `json:"hideChildProcessWindows"`
	LoggingEnabled               bool     `json:"loggingEnabled"`
}

type PublicConfig struct {
	Version                      int                `json:"version"`
	Workspace                    string             `json:"workspace"`
	AllowedRoots                 []string           `json:"allowedRoots"`
	MCPHost                      string             `json:"mcpHost"`
	MCPPort                      int                `json:"mcpPort"`
	AdminHost                    string             `json:"adminHost"`
	AdminPort                    int                `json:"adminPort"`
	WebControlEnabled            bool               `json:"webControlEnabled"`
	WebControlPort               int                `json:"webControlPort"`
	WebControlLANEnabled         bool               `json:"webControlLanEnabled"`
	WebControlAuthEnabled        bool               `json:"webControlAuthEnabled"`
	PermissionMode               string             `json:"permissionMode"`
	FileScope                    string             `json:"fileScope"`
	ToolProfile                  string             `json:"toolProfile"`
	AllowNetwork                 bool               `json:"allowNetwork"`
	ScreenCaptureEnabled         bool               `json:"screenCaptureEnabled"`
	ScreenCaptureMode            string             `json:"screenCaptureMode"`
	ScreenCaptureWindowID        string             `json:"screenCaptureWindowId"`
	ScreenCaptureWindowProcessID uint32             `json:"screenCaptureWindowProcessId"`
	ScreenCaptureWindowTitle     string             `json:"screenCaptureWindowTitle"`
	ScreenCaptureWindowProcess   string             `json:"screenCaptureWindowProcess"`
	ScreenWindows                []ScreenWindowInfo `json:"screenWindows,omitempty"`
	Domain                       string             `json:"domain"`
	TunnelName                   string             `json:"tunnelName"`
	TunnelID                     string             `json:"tunnelId"`
	ProxyAddress                 string             `json:"proxyAddress"`
	ProxyUsername                string             `json:"proxyUsername"`
	HasProxyPassword             bool               `json:"hasProxyPassword"`
	AutoStart                    bool               `json:"autoStart"`
	Watchdog                     bool               `json:"watchdog"`
	CoreMode                     string             `json:"coreMode"`
	CoreExecutable               string             `json:"coreExecutable"`
	GoCoreExecutable             string             `json:"goCoreExecutable"`
	CloudflaredExecutable        string             `json:"cloudflaredExecutable"`
	OpenBrowserOnStart           bool               `json:"openBrowserOnStart"`
	HideChildProcessWindows      bool               `json:"hideChildProcessWindows"`
	LoggingEnabled               bool               `json:"loggingEnabled"`
}

func (c Config) Public() PublicConfig {
	return PublicConfig{
		Version:                      c.Version,
		Workspace:                    c.Workspace,
		AllowedRoots:                 append([]string(nil), c.AllowedRoots...),
		MCPHost:                      c.MCPHost,
		MCPPort:                      c.MCPPort,
		AdminHost:                    c.AdminHost,
		AdminPort:                    c.AdminPort,
		WebControlEnabled:            c.WebControlEnabled,
		WebControlPort:               c.WebControlPort,
		WebControlLANEnabled:         c.WebControlLANEnabled,
		WebControlAuthEnabled:        c.WebControlAuthEnabled,
		PermissionMode:               c.PermissionMode,
		FileScope:                    c.FileScope,
		ToolProfile:                  c.ToolProfile,
		AllowNetwork:                 c.AllowNetwork,
		ScreenCaptureEnabled:         c.ScreenCaptureEnabled,
		ScreenCaptureMode:            c.ScreenCaptureMode,
		ScreenCaptureWindowID:        c.ScreenCaptureWindowID,
		ScreenCaptureWindowProcessID: c.ScreenCaptureWindowProcessID,
		ScreenCaptureWindowTitle:     c.ScreenCaptureWindowTitle,
		ScreenCaptureWindowProcess:   c.ScreenCaptureWindowProcess,
		Domain:                       c.Domain,
		TunnelName:                   c.TunnelName,
		TunnelID:                     c.TunnelID,
		ProxyAddress:                 c.ProxyAddress,
		ProxyUsername:                c.ProxyUsername,
		HasProxyPassword:             c.ProxyPassword != "",
		AutoStart:                    c.AutoStart,
		Watchdog:                     c.Watchdog,
		CoreMode:                     c.CoreMode,
		CoreExecutable:               c.CoreExecutable,
		GoCoreExecutable:             c.GoCoreExecutable,
		CloudflaredExecutable:        c.CloudflaredExecutable,
		OpenBrowserOnStart:           c.OpenBrowserOnStart,
		HideChildProcessWindows:      c.HideChildProcessWindows,
		LoggingEnabled:               c.LoggingEnabled,
	}
}

type ConfigUpdate struct {
	Workspace                    *string   `json:"workspace"`
	AllowedRoots                 *[]string `json:"allowedRoots"`
	MCPPort                      *int      `json:"mcpPort"`
	AdminPort                    *int      `json:"adminPort"`
	WebControlEnabled            *bool     `json:"webControlEnabled"`
	WebControlPort               *int      `json:"webControlPort"`
	WebControlLANEnabled         *bool     `json:"webControlLanEnabled"`
	WebControlAuthEnabled        *bool     `json:"webControlAuthEnabled"`
	PermissionMode               *string   `json:"permissionMode"`
	FileScope                    *string   `json:"fileScope"`
	ToolProfile                  *string   `json:"toolProfile"`
	AllowNetwork                 *bool     `json:"allowNetwork"`
	ScreenCaptureEnabled         *bool     `json:"screenCaptureEnabled"`
	ScreenCaptureMode            *string   `json:"screenCaptureMode"`
	ScreenCaptureWindowID        *string   `json:"screenCaptureWindowId"`
	ScreenCaptureWindowProcessID *uint32   `json:"screenCaptureWindowProcessId"`
	ScreenCaptureWindowTitle     *string   `json:"screenCaptureWindowTitle"`
	ScreenCaptureWindowProcess   *string   `json:"screenCaptureWindowProcess"`
	Domain                       *string   `json:"domain"`
	TunnelName                   *string   `json:"tunnelName"`
	ProxyAddress                 *string   `json:"proxyAddress"`
	ProxyUsername                *string   `json:"proxyUsername"`
	ProxyPassword                *string   `json:"proxyPassword"`
	AutoStart                    *bool     `json:"autoStart"`
	Watchdog                     *bool     `json:"watchdog"`
	CoreMode                     *string   `json:"coreMode"`
	ConfirmCoreSwitch            bool      `json:"confirmCoreSwitch"`
	OpenBrowserOnStart           *bool     `json:"openBrowserOnStart"`
	HideChildProcessWindows      *bool     `json:"hideChildProcessWindows"`
	LoggingEnabled               *bool     `json:"loggingEnabled"`
}

type ProcessStatus struct {
	Name       string     `json:"name"`
	Running    bool       `json:"running"`
	Managed    bool       `json:"managed"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	StoppedAt  *time.Time `json:"stoppedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
	StdoutPath string     `json:"stdoutPath,omitempty"`
	StderrPath string     `json:"stderrPath,omitempty"`
}

type TunnelProcess struct {
	PID             int    `json:"pid"`
	ParentPID       int    `json:"parentPid,omitempty"`
	ProcessPath     string `json:"processPath,omitempty"`
	CommandLine     string `json:"commandLine,omitempty"`
	TunnelName      string `json:"tunnelName,omitempty"`
	TunnelID        string `json:"tunnelId,omitempty"`
	CredentialsPath string `json:"credentialsPath,omitempty"`
	LocalURL        string `json:"localUrl,omitempty"`
	LocalHost       string `json:"localHost,omitempty"`
	LocalPort       int    `json:"localPort,omitempty"`
	Managed         bool   `json:"managed"`
	MatchesConfig   bool   `json:"matchesConfig"`
	Duplicate       bool   `json:"duplicate"`
}

type TunnelInventory struct {
	Count            int             `json:"count"`
	MatchingCount    int             `json:"matchingCount"`
	DuplicateCount   int             `json:"duplicateCount"`
	ExpectedLocalURL string          `json:"expectedLocalUrl"`
	Processes        []TunnelProcess `json:"processes"`
}

type PortOwner struct {
	Occupied    bool   `json:"occupied"`
	PID         int    `json:"pid,omitempty"`
	ParentPID   int    `json:"parentPid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
	ProcessPath string `json:"processPath,omitempty"`
	Managed     bool   `json:"managed"`
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
	OAuthClientID    string           `json:"oauthClientId"`
	OAuthClientType  string           `json:"oauthClientType"`
	OAuthTokenAuth   string           `json:"oauthTokenAuth"`
	CoreMode         string           `json:"coreMode"`
	MCP              ProcessStatus    `json:"mcp"`
	MCPPortOwner     PortOwner        `json:"mcpPortOwner"`
	Tunnel           ProcessStatus    `json:"tunnel"`
	TunnelInventory  TunnelInventory  `json:"tunnelInventory"`
	Cloudflare       CloudflareStatus `json:"cloudflare"`
	PermissionMode   string           `json:"permissionMode"`
	FileScope        string           `json:"fileScope"`
	AllowNetwork     bool             `json:"allowNetwork"`
	WatchdogEnabled  bool             `json:"watchdogEnabled"`
	ConfigurationOK  bool             `json:"configurationOk"`
	ConfigurationMsg string           `json:"configurationMessage,omitempty"`
}

type ChangeMCPPortRequest struct {
	Port int `json:"port"`
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
	OwnerPassword   string   `json:"ownerPassword,omitempty"`
	ClientID        string   `json:"clientId,omitempty"`
	ClientSecret    string   `json:"clientSecret,omitempty"`
	TokenSecret     string   `json:"tokenSecret,omitempty"`
	Configured      bool     `json:"configured"`
	EncryptedAtRest bool     `json:"encryptedAtRest"`
	RedirectURIs    []string `json:"redirectUris,omitempty"`
}

type SecretUpdateRequest struct {
	OwnerPassword *string   `json:"ownerPassword"`
	ClientID      *string   `json:"clientId"`
	ClientSecret  *string   `json:"clientSecret"`
	TokenSecret   *string   `json:"tokenSecret"`
	RedirectURIs  *[]string `json:"redirectUris"`
	Restart       bool      `json:"restart"`
}

type SecretGenerateRequest struct {
	Field string `json:"field"`
}

type SecretSaveResult struct {
	Secrets         SecretSummary `json:"secrets"`
	Restarted       bool          `json:"restarted"`
	RestartRequired bool          `json:"restartRequired"`
	RestartError    string        `json:"restartError,omitempty"`
}

type DesktopStatus struct {
	Available       bool   `json:"available"`
	AppMode         bool   `json:"appMode"`
	NativeWindow    bool   `json:"nativeWindow"`
	EdgePath        string `json:"edgePath,omitempty"`
	RenderEngine    string `json:"renderEngine,omitempty"`
	RuntimeVersion  string `json:"runtimeVersion,omitempty"`
	StartupEnabled  bool   `json:"startupEnabled"`
	TrayAvailable   bool   `json:"trayAvailable"`
	SingleInstance  bool   `json:"singleInstance"`
	DashboardURL    string `json:"dashboardUrl"`
	WindowModeLabel string `json:"windowModeLabel"`
}

const PrimaryInstanceID = "primary"

// MCPInstance is the management-plane view of one independently running MCP
// service. The primary instance is backed by the legacy top-level config so
// existing installations and APIs remain compatible. Additional instances
// keep their own config and logs under data/devdesk/instances/<id>.
type MCPInstance struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	ProjectID            string        `json:"projectId,omitempty"`
	Primary              bool          `json:"primary"`
	TunnelMode           string        `json:"tunnelMode"`
	Workspace            string        `json:"workspace"`
	MCPHost              string        `json:"mcpHost"`
	MCPPort              int           `json:"mcpPort"`
	LocalMCPURL          string        `json:"localMcpUrl"`
	RemoteMCPURL         string        `json:"remoteMcpUrl,omitempty"`
	AuthorizeURL         string        `json:"authorizeUrl,omitempty"`
	Domain               string        `json:"domain,omitempty"`
	TunnelName           string        `json:"tunnelName,omitempty"`
	TunnelID             string        `json:"tunnelId,omitempty"`
	CoreMode             string        `json:"coreMode"`
	PermissionMode       string        `json:"permissionMode"`
	FileScope            string        `json:"fileScope"`
	ToolProfile          string        `json:"toolProfile"`
	AllowNetwork         bool          `json:"allowNetwork"`
	AutoStart            bool          `json:"autoStart"`
	Watchdog             bool          `json:"watchdog"`
	LoggingEnabled       bool          `json:"loggingEnabled"`
	DataDirectory        string        `json:"dataDirectory"`
	MCP                  ProcessStatus `json:"mcp"`
	Tunnel               ProcessStatus `json:"tunnel"`
	MCPPortOwner         PortOwner     `json:"mcpPortOwner"`
	ConfigurationOK      bool          `json:"configurationOk"`
	ConfigurationMessage string        `json:"configurationMessage,omitempty"`
	CreatedAt            *time.Time    `json:"createdAt,omitempty"`
	UpdatedAt            *time.Time    `json:"updatedAt,omitempty"`
}

type MCPInstanceCreateRequest struct {
	Name           string `json:"name"`
	ProjectID      string `json:"projectId"`
	Workspace      string `json:"workspace"`
	MCPPort        int    `json:"mcpPort"`
	Domain         string `json:"domain"`
	TunnelName     string `json:"tunnelName"`
	CoreMode       string `json:"coreMode"`
	PermissionMode string `json:"permissionMode"`
	FileScope      string `json:"fileScope"`
	ToolProfile    string `json:"toolProfile"`
	AllowNetwork   *bool  `json:"allowNetwork"`
	AutoStart      *bool  `json:"autoStart"`
	Watchdog       *bool  `json:"watchdog"`
	LoggingEnabled *bool  `json:"loggingEnabled"`
}

type MCPInstanceUpdateRequest struct {
	Name              *string `json:"name"`
	ProjectID         *string `json:"projectId"`
	Workspace         *string `json:"workspace"`
	MCPPort           *int    `json:"mcpPort"`
	Domain            *string `json:"domain"`
	TunnelName        *string `json:"tunnelName"`
	CoreMode          *string `json:"coreMode"`
	PermissionMode    *string `json:"permissionMode"`
	FileScope         *string `json:"fileScope"`
	ToolProfile       *string `json:"toolProfile"`
	AllowNetwork      *bool   `json:"allowNetwork"`
	AutoStart         *bool   `json:"autoStart"`
	Watchdog          *bool   `json:"watchdog"`
	LoggingEnabled    *bool   `json:"loggingEnabled"`
	ConfirmCoreSwitch bool    `json:"confirmCoreSwitch"`
}

type MCPInstanceCloneRequest struct {
	Name     string `json:"name"`
	CoreMode string `json:"coreMode"`
}
