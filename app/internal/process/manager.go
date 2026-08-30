package process

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	devlogging "mcp-devdesk/internal/logging"
	"mcp-devdesk/internal/model"
	"mcp-devdesk/internal/secrets"
)

type managedProcess struct {
	mu       sync.RWMutex
	cmd      *exec.Cmd
	stopping bool
	status   model.ProcessStatus
}

type Manager struct {
	rootDir        string
	dataDir        string
	secrets        *secrets.Store
	loggingEnabled devlogging.EnabledFunc
	instructions   func(string) string
	mcp            managedProcess
	tunnel         managedProcess
	login          managedProcess
}

func NewManager(rootDir, dataDir string, secretStore *secrets.Store, loggingEnabled devlogging.EnabledFunc, instructions ...func(string) string) *Manager {
	m := &Manager{rootDir: rootDir, dataDir: dataDir, secrets: secretStore, loggingEnabled: loggingEnabled}
	if len(instructions) > 0 {
		m.instructions = instructions[0]
	}
	m.mcp.status.Name = "mcp"
	m.tunnel.status.Name = "tunnel"
	m.login.status.Name = "cloudflare-login"
	return m
}

func (m *Manager) StartMCP(cfg model.Config) error {
	executable := selectedMCPExecutable(cfg)
	if _, err := os.Stat(executable); err != nil {
		return fmt.Errorf("MCP executable unavailable: %w", err)
	}
	if info, err := os.Stat(cfg.Workspace); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return fmt.Errorf("workspace unavailable: %w", err)
	}

	values, err := m.secrets.GetOrCreate()
	if err != nil {
		return err
	}

	baseURL := "http://" + cfg.MCPHost + ":" + strconv.Itoa(cfg.MCPPort)
	if cfg.Domain != "" {
		baseURL = "https://" + cfg.Domain
	}

	instructionsFile, err := m.syncInstructionsFile(cfg)
	if err != nil {
		return err
	}
	args := mcpArguments(cfg, m.dataDir, baseURL, instructionsFile)

	env := mcpEnvironment(cfg, values, baseURL)

	stdout := filepath.Join(m.dataDir, "logs", "mcp-stdout.log")
	stderr := filepath.Join(m.dataDir, "logs", "mcp-stderr.log")
	return m.start(&m.mcp, executable, args, m.rootDir, env, stdout, stderr, cfg.HideChildProcessWindows)
}

// SyncInstructions writes the effective MCP DevDesk instructions for cfg without
// restarting the core. A running Go core watches this file and invalidates its
// current MCP sessions when the contents change so the client reconnects and
// receives the new initialize instructions.
func (m *Manager) SyncInstructions(cfg model.Config) error {
	_, err := m.syncInstructionsFile(cfg)
	return err
}

func (m *Manager) syncInstructionsFile(cfg model.Config) (string, error) {
	if cfg.CoreMode != "go" || m.instructions == nil {
		return "", nil
	}
	path := filepath.Join(m.dataDir, "project-instructions.md")
	content := strings.TrimSpace(m.instructions(cfg.Workspace))
	if content == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("remove project instructions: %w", err)
		}
		// Always return the watched path for Go cores, even when the file does
		// not exist yet. This lets a running core notice the first prompt saved
		// after startup instead of requiring a manual restart.
		return path, nil
	}
	desired := content + "\n"
	if current, err := os.ReadFile(path); err == nil && string(current) == desired {
		return path, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read project instructions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create project instructions directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(desired), 0o600); err != nil {
		return "", fmt.Errorf("write project instructions: %w", err)
	}
	return path, nil
}

func selectedMCPExecutable(cfg model.Config) string {
	if cfg.CoreMode == "go" {
		return cfg.GoCoreExecutable
	}
	return cfg.CoreExecutable
}

func mcpArguments(cfg model.Config, dataDir, baseURL, instructionsFile string) []string {
	args := []string{
		"--workspace", cfg.Workspace,
		"--host", cfg.MCPHost,
		"--port", strconv.Itoa(cfg.MCPPort),
		"--tool-profile", cfg.ToolProfile,
		"--oauth-mode",
		"--permission-mode", cfg.PermissionMode,
	}
	if cfg.CoreMode == "go" {
		args = append(args,
			"--data-dir", dataDir,
			"--server-url", baseURL,
			"--audit-path", filepath.Join(dataDir, "logs", "mcp-audit.jsonl"),
			"--logging-config", filepath.Join(dataDir, "config.json"),
			"--file-scope", cfg.FileScope,
		)
		if strings.TrimSpace(instructionsFile) != "" {
			args = append(args, "--instructions-file", instructionsFile)
		}
		for _, root := range cfg.AllowedRoots {
			if strings.TrimSpace(root) != "" {
				args = append(args, "--allowed-root", root)
			}
		}
	} else if cfg.PermissionMode == "safe" {
		args = append(args, "--shell-env-inherit", "core")
	} else {
		args = append(args, "--shell-env-inherit", "all")
	}
	if cfg.AllowNetwork {
		args = append(args, "--allow-network")
	}
	return args
}

func mcpEnvironment(cfg model.Config, values secrets.Values, baseURL string) []string {
	env := append(os.Environ(),
		"CODING_TOOLS_MCP_SERVER_URL="+baseURL,
		"CODING_TOOLS_MCP_OAUTH_PASSWORD="+values.OwnerPassword,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_ID="+values.ClientID,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET="+values.ClientSecret,
		"CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET="+values.TokenSecret,
		"CODING_TOOLS_MCP_OAUTH_REDIRECT_URIS="+strings.Join(values.RedirectURIs, "\n"),
		"CODING_TOOLS_MCP_TOOL_PROFILE="+cfg.ToolProfile,
	)
	return appendProxy(env, cfg)
}

func (m *Manager) StartTunnel(cfg model.Config) error {
	if _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {
		return fmt.Errorf("cloudflared executable unavailable: %w", err)
	}
	if cfg.TunnelID == "" {
		return errors.New("Cloudflare tunnel has not been configured")
	}
	if cfg.TunnelName == "" || cfg.Domain == "" {
		return errors.New("tunnel name and domain are required")
	}

	credentials := CredentialsPath(cfg.TunnelID)
	if _, err := os.Stat(credentials); err != nil {
		return fmt.Errorf("tunnel credentials unavailable at %s: %w", credentials, err)
	}

	localURL := "http://" + cfg.MCPHost + ":" + strconv.Itoa(cfg.MCPPort)
	args := []string{
		"tunnel", "run",
		"--credentials-file", credentials,
		"--protocol", "http2",
		"--url", localURL,
		cfg.TunnelName,
	}

	stdout := filepath.Join(m.dataDir, "logs", "tunnel-stdout.log")
	stderr := filepath.Join(m.dataDir, "logs", "tunnel-stderr.log")
	return m.start(&m.tunnel, cfg.CloudflaredExecutable, args, m.rootDir, appendProxy(os.Environ(), cfg), stdout, stderr, cfg.HideChildProcessWindows)
}

func (m *Manager) StartCloudflareLogin(cfg model.Config) error {
	if _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {
		return fmt.Errorf("cloudflared executable unavailable: %w", err)
	}
	stdout := filepath.Join(m.dataDir, "logs", "cloudflare-login.log")
	stderr := filepath.Join(m.dataDir, "logs", "cloudflare-login-error.log")
	return m.start(&m.login, cfg.CloudflaredExecutable, []string{"tunnel", "login"}, m.rootDir, appendProxy(os.Environ(), cfg), stdout, stderr, true)
}

func (m *Manager) StopAll() error {
	var errs []string
	if err := m.stop(&m.login); err != nil {
		errs = append(errs, err.Error())
	}
	if err := m.stop(&m.tunnel); err != nil {
		errs = append(errs, err.Error())
	}
	if err := m.stop(&m.mcp); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) StopMCP() error    { return m.stop(&m.mcp) }
func (m *Manager) StopTunnel() error { return m.stop(&m.tunnel) }
func (m *Manager) StopLogin() error  { return m.stop(&m.login) }

func (m *Manager) Status() (model.ProcessStatus, model.ProcessStatus, model.ProcessStatus) {
	return statusOf(&m.mcp), statusOf(&m.tunnel), statusOf(&m.login)
}

func (m *Manager) start(target *managedProcess, executable string, args []string, workDir string, env []string, stdoutPath, stderrPath string, hidden bool) error {
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.cmd != nil && target.status.Running {
		return fmt.Errorf("%s is already running", target.status.Name)
	}

	stdout, err := devlogging.NewFileWriter(stdoutPath, m.loggingEnabled)
	if err != nil {
		return err
	}
	stderr, err := devlogging.NewFileWriter(stderrPath, m.loggingEnabled)
	if err != nil {
		_ = stdout.Close()
		return err
	}

	writeLaunchHeader(stdout, executable, args)
	cmd := exec.Command(executable, args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	configureChildProcess(cmd, hidden)

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}

	now := time.Now()
	target.cmd = cmd
	target.stopping = false
	target.status.Running = true
	target.status.Managed = true
	target.status.PID = cmd.Process.Pid
	target.status.StartedAt = &now
	target.status.StoppedAt = nil
	target.status.ExitCode = nil
	target.status.LastError = ""
	target.status.StdoutPath = stdoutPath
	target.status.StderrPath = stderrPath

	go m.wait(target, cmd, stdout, stderr)
	return nil
}

func (m *Manager) wait(target *managedProcess, cmd *exec.Cmd, stdout, stderr io.Closer) {
	err := cmd.Wait()
	_ = stdout.Close()
	_ = stderr.Close()

	target.mu.Lock()
	defer target.mu.Unlock()
	if target.cmd != cmd {
		return
	}
	manualStop := target.stopping
	target.cmd = nil
	target.stopping = false
	now := time.Now()
	target.status.Running = false
	target.status.StoppedAt = &now
	if manualStop {
		code := 0
		target.status.ExitCode = &code
		target.status.LastError = ""
	} else if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		target.status.ExitCode = &code
	}
	if err != nil && !manualStop {
		target.status.LastError = err.Error()
	}
}

func (m *Manager) stop(target *managedProcess) error {
	target.mu.Lock()
	cmd := target.cmd
	running := target.status.Running
	if running {
		target.stopping = true
	}
	target.mu.Unlock()
	if cmd == nil || !running || cmd.Process == nil {
		return nil
	}

	// taskkill /T terminates package-manager and shell descendants as well.
	kill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	configureChildProcess(kill, true)
	if output, err := kill.CombinedOutput(); err != nil {
		// A process may exit naturally between the status check and taskkill.
		text := strings.ToLower(string(output))
		if !strings.Contains(text, "not found") && !strings.Contains(text, "no running instance") {
			target.mu.Lock()
			if target.cmd == cmd {
				target.stopping = false
			}
			target.mu.Unlock()
			return fmt.Errorf("stop PID %d: %w: %s", cmd.Process.Pid, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func statusOf(target *managedProcess) model.ProcessStatus {
	target.mu.RLock()
	defer target.mu.RUnlock()
	copy := target.status
	return copy
}

func CredentialsPath(tunnelID string) string {
	home := UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cloudflared", tunnelID+".json")
}

func CertificatePath() string {
	home := UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cloudflared", "cert.pem")
}

// UserHomeDir is deliberately more defensive than os.UserHomeDir. Desktop
// launchers, Windows services and sandboxed management clients sometimes
// remove USERPROFILE even though the interactive Windows account is known.
func UserHomeDir() string {
	// Prefer native Windows identity sources. Sandboxed launchers often set
	// HOME to an isolated directory while Cloudflare still stores credentials
	// under the interactive Windows profile.
	candidates := []string{os.Getenv("USERPROFILE")}
	if current, err := user.Current(); err == nil {
		candidates = append(candidates, current.HomeDir)
	}
	if drive, path := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); drive != "" && path != "" {
		candidates = append(candidates, drive+path)
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		candidates = append(candidates, filepath.Dir(filepath.Dir(localAppData)))
	}
	if roamingAppData := os.Getenv("APPDATA"); roamingAppData != "" {
		candidates = append(candidates, filepath.Dir(filepath.Dir(roamingAppData)))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, home)
	}
	candidates = append(candidates, os.Getenv("HOME"))

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if absolute, err := filepath.Abs(candidate); err == nil {
			candidate = absolute
		}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

func appendProxy(env []string, cfg model.Config) []string {
	if cfg.ProxyAddress == "" {
		return env
	}
	proxy := cfg.ProxyAddress
	if cfg.ProxyUsername != "" {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(proxy, "http://"), "https://")
		proxy = "http://" + cfg.ProxyUsername + ":" + cfg.ProxyPassword + "@" + trimmed
	}
	return append(env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy)
}

func writeLaunchHeader(writer io.Writer, executable string, args []string) {
	_, _ = fmt.Fprintf(writer, "\n[%s] starting %s %s\n", time.Now().Format(time.RFC3339), executable, strings.Join(args, " "))
}
