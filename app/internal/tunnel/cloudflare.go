package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	appconfig "mcp-devdesk/internal/config"
	"mcp-devdesk/internal/model"
	processmanager "mcp-devdesk/internal/process"
)

var uuidPattern = regexp.MustCompile(`(?i)[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)

type Client struct{}

func NewClient() *Client { return &Client{} }

func (c *Client) Status(cfg model.Config, login model.ProcessStatus) model.CloudflareStatus {
	_, executableErr := os.Stat(cfg.CloudflaredExecutable)
	certificate := processmanager.CertificatePath()
	_, certErr := os.Stat(certificate)
	credentials := ""
	if cfg.TunnelID != "" {
		credentials = processmanager.CredentialsPath(cfg.TunnelID)
	}
	return model.CloudflareStatus{
		Installed:       executableErr == nil,
		Authenticated:   certErr == nil,
		LoginInProgress: login.Running,
		CertificatePath: certificate,
		TunnelID:        cfg.TunnelID,
		TunnelName:      cfg.TunnelName,
		Domain:          cfg.Domain,
		CredentialsPath: credentials,
	}
}

func (c *Client) Configure(ctx context.Context, cfg model.Config, request model.ConfigureTunnelRequest) (model.ConfigureTunnelResult, error) {
	request.Domain = strings.ToLower(strings.TrimSpace(request.Domain))
	request.TunnelName = strings.TrimSpace(request.TunnelName)
	if !appconfig.ValidDomain(request.Domain) {
		return model.ConfigureTunnelResult{}, errors.New("请输入完整且有效的子域名，例如 mcp.example.com")
	}
	if request.TunnelName == "" {
		request.TunnelName = "mcp-devdesk"
	}
	if _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {
		return model.ConfigureTunnelResult{}, fmt.Errorf("cloudflared.exe 不存在: %w", err)
	}
	if _, err := os.Stat(processmanager.CertificatePath()); err != nil {
		return model.ConfigureTunnelResult{}, errors.New("Cloudflare 尚未授权，请先点击登录 Cloudflare")
	}

	commandCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	tunnelID, found, listOutput, err := c.findTunnel(commandCtx, cfg, request.TunnelName)
	if err != nil {
		return model.ConfigureTunnelResult{}, fmt.Errorf("查询 Tunnel 失败: %w; %s", err, compactOutput(listOutput))
	}
	if found && !request.Reuse {
		return model.ConfigureTunnelResult{}, fmt.Errorf("已存在同名 Tunnel %q，请勾选复用或更换名称", request.TunnelName)
	}

	if !found {
		output, createErr := c.run(commandCtx, cfg, "tunnel", "create", request.TunnelName)
		if createErr != nil {
			return model.ConfigureTunnelResult{}, fmt.Errorf("创建 Tunnel 失败: %w; %s", createErr, compactOutput(output))
		}
		match := uuidPattern.FindString(output)
		if match == "" {
			return model.ConfigureTunnelResult{}, fmt.Errorf("创建成功但未能解析 Tunnel UUID: %s", compactOutput(output))
		}
		tunnelID = strings.ToLower(match)
	}

	credentials := processmanager.CredentialsPath(tunnelID)
	if _, err := os.Stat(credentials); err != nil {
		return model.ConfigureTunnelResult{}, fmt.Errorf("Tunnel 凭据文件不存在: %s", credentials)
	}

	routeOutput, routeErr := c.run(commandCtx, cfg, "tunnel", "route", "dns", request.TunnelName, request.Domain)
	if routeErr != nil && !routeAlreadyConfigured(routeOutput) {
		return model.ConfigureTunnelResult{}, fmt.Errorf("配置 DNS 路由失败: %w; %s", routeErr, compactOutput(routeOutput))
	}

	return model.ConfigureTunnelResult{
		TunnelID:        tunnelID,
		TunnelName:      request.TunnelName,
		Domain:          request.Domain,
		CredentialsPath: credentials,
		RemoteMCPURL:    "https://" + request.Domain + "/mcp",
		AuthorizeURL:    "https://" + request.Domain + "/oauth/authorize",
		Message:         "Tunnel 和 DNS 已配置完成",
	}, nil
}

func (c *Client) findTunnel(ctx context.Context, cfg model.Config, name string) (string, bool, string, error) {
	output, err := c.run(ctx, cfg, "tunnel", "list", "--output", "json")
	if err == nil {
		var rows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(output), &rows) == nil {
			for _, row := range rows {
				if strings.EqualFold(row.Name, name) && uuidPattern.MatchString(row.ID) {
					return strings.ToLower(row.ID), true, output, nil
				}
			}
			return "", false, output, nil
		}
	}

	// Older cloudflared builds or localized output may not support JSON.
	textOutput, textErr := c.run(ctx, cfg, "tunnel", "list")
	if textErr != nil {
		if err != nil {
			return "", false, textOutput, textErr
		}
		return "", false, output, err
	}
	for _, line := range strings.Split(textOutput, "\n") {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(name)) {
			continue
		}
		if id := uuidPattern.FindString(line); id != "" {
			return strings.ToLower(id), true, textOutput, nil
		}
	}
	return "", false, textOutput, nil
}

func (c *Client) run(ctx context.Context, cfg model.Config, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, cfg.CloudflaredExecutable, args...)
	cmd.Env = proxyEnvironment(cfg)
	configureCommand(cmd)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func proxyEnvironment(cfg model.Config) []string {
	env := os.Environ()
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

func routeAlreadyConfigured(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "already configured") ||
		strings.Contains(lower, "already exists") ||
		strings.Contains(lower, "added cname") ||
		strings.Contains(lower, "will route")
}

func compactOutput(output string) string {
	output = strings.TrimSpace(strings.ReplaceAll(output, "\r", ""))
	if len(output) > 800 {
		return output[:800] + "…"
	}
	return output
}
