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
	rootDir string
	dataDir string
	secrets *secrets.Store
	mcp     managedProcess
	tunnel  managedProcess
	login   managedProcess
}

func NewManager(rootDir, dataDir string, secretStore *secrets.Store) *Manager {
	m := &Manager{rootDir: rootDir, dataDir: dataDir, secrets: secretStore}
	m.mcp.status.Name = "mcp"
	m.tunnel.status.Name = "tunnel"
	m.login.status.Name = "cloudflare-login"
	return m
}

func (m *Manager) StartMCP(cfg model.Config) error {
	if _, err := os.Stat(cfg.CoreExecutable); err != nil {
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

	args := []string{
		"--workspace", cfg.Workspace,
		"--host", cfg.MCPHost,
		"--port", strconv.Itoa(cfg.MCPPort),
		"--tool-profile", cfg.ToolProfile,
		"--oauth-mode",
		"--permission-mode", cfg.PermissionMode,
	}
	if cfg.PermissionMode == "safe" {
		args = append(args, "--shell-env-inherit", "core")
	} else {
		args = append(args, "--shell-env-inherit", "all")
	}
	if cfg.AllowNetwork {
		args = append(args, "--allow-network")
	}

	env := mcpEnvironment(cfg, values, baseURL)

	stdout := filepath.Join(m.dataDir, "logs", "mcp-stdout.log")
	stderr := filepath.Join(m.dataDir, "logs", "mcp-stderr.log")
	return m.start(&m.mcp, cfg.CoreExecutable, args, m.rootDir, env, stdout, stderr, cfg.HideChildProcessWindows)
}

func mcpEnvironment(cfg model.Config, values secrets.Values, baseURL string) []string {
	env := append(os.Environ(),
		"CODING_TOOLS_MCP_SERVER_URL="+baseURL,
		"CODING_TOOLS_MCP_OAUTH_PASSWORD="+values.OwnerPassword,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_ID="+values.ClientID,
		"CODING_TOOLS_MCP_OAUTH_CLIENT_SECRET="+values.ClientSecret,
		"CODING_TOOLS_MCP_OAUTH_TOKEN_SECRET="+values.TokenSecret,
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

func (m *Manager) Status() (model.ProcessStatus, model.ProcessStatus, model.ProcessStatus) {
	return statusOf(&m.mcp), statusOf(&m.tunnel), statusOf(&m.login)
}

func (m *Manager) start(target *managedProcess, executable string, args []string, workDir string, env []string, stdoutPath, stderrPath string, hidden bool) error {
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.cmd != nil && target.status.Running {
		return fmt.Errorf("%s is already running", target.status.Name)
	}

	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o700); err != nil {
		return err
	}
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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
