package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

	// Always bind the hostname to the exact Tunnel UUID and ask cloudflared to
	// replace an existing A/AAAA/CNAME record. Previously an "already exists"
	// error was treated as success, which could leave the hostname pointing at
	// an older, offline Tunnel and surface Cloudflare Error 1033 even while the
	// newly configured Tunnel itself was healthy.
	routeOutput, routeErr := c.ensureDNSRoute(commandCtx, cfg, tunnelID, request.Domain)
	if routeErr != nil {
		return model.ConfigureTunnelResult{}, routeErr
	}

	return model.ConfigureTunnelResult{
		TunnelID:        tunnelID,
		TunnelName:      request.TunnelName,
		Domain:          request.Domain,
		CredentialsPath: credentials,
		RemoteMCPURL:    "https://" + request.Domain + "/mcp",
		AuthorizeURL:    "https://" + request.Domain + "/oauth/authorize",
		Message:         "Tunnel 和 DNS 已配置完成，公网 DNS 验证通过",
	}, nil
}

func dnsRouteArguments(tunnelID, domain string) []string {
	return []string{"tunnel", "route", "dns", "--overwrite-dns", tunnelID, domain}
}

func legacyDNSRouteArguments(tunnelID, domain string) []string {
	return []string{"tunnel", "route", "dns", tunnelID, domain}
}

func overwriteDNSFlagUnsupported(output string, err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "overwrite-dns") {
		return false
	}
	return strings.Contains(lower, "unknown flag") ||
		strings.Contains(lower, "flag provided but not defined") ||
		strings.Contains(lower, "unknown shorthand flag")
}

func (c *Client) routeDNS(ctx context.Context, cfg model.Config, tunnelID, domain string) (string, error) {
	output, err := c.run(ctx, cfg, dnsRouteArguments(tunnelID, domain)...)
	if err == nil || !overwriteDNSFlagUnsupported(output, err) {
		return output, err
	}

	legacyOutput, legacyErr := c.run(ctx, cfg, legacyDNSRouteArguments(tunnelID, domain)...)
	combined := strings.TrimSpace(output)
	if strings.TrimSpace(legacyOutput) != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += legacyOutput
	}
	return combined, legacyErr
}

func (c *Client) ensureDNSRoute(ctx context.Context, cfg model.Config, tunnelID, domain string) (string, error) {
	output, err := c.routeDNS(ctx, cfg, tunnelID, domain)
	if err != nil {
		return output, fmt.Errorf("配置 DNS 路由失败: %w; %s", err, compactOutput(output))
	}

	verified, verifyErr := waitForDNS(ctx, domain, 8*time.Second)
	if verified {
		return output, nil
	}

	// A Tunnel can be created successfully while the DNS route is missing. Retry
	// the exact UUID binding once, then require public DNS visibility before the
	// UI reports the instance as fully configured.
	retryOutput, retryErr := c.routeDNS(ctx, cfg, tunnelID, domain)
	if strings.TrimSpace(retryOutput) != "" {
		if strings.TrimSpace(output) != "" {
			output += "\n"
		}
		output += retryOutput
	}
	if retryErr != nil {
		return output, fmt.Errorf("DNS 首次配置后公网仍不可解析，自动重试失败: %w; %s", retryErr, compactOutput(output))
	}
	verified, verifyErr = waitForDNS(ctx, domain, 8*time.Second)
	if !verified {
		return output, fmt.Errorf("Cloudflare 已接受 DNS 路由，但公网仍无法解析 %s: %v; cloudflared: %s", domain, verifyErr, compactOutput(output))
	}
	return output, nil
}

func (c *Client) RepairDNS(ctx context.Context, cfg model.Config) (model.ConfigureTunnelResult, error) {
	domain := strings.ToLower(strings.TrimSpace(cfg.Domain))
	tunnelID := strings.ToLower(strings.TrimSpace(cfg.TunnelID))
	if !appconfig.ValidDomain(domain) {
		return model.ConfigureTunnelResult{}, errors.New("当前实例没有有效的 Cloudflare 域名，请先配置 Tunnel")
	}
	if !uuidPattern.MatchString(tunnelID) {
		return model.ConfigureTunnelResult{}, errors.New("当前实例没有有效的 Tunnel UUID，请先配置 Tunnel")
	}
	if _, err := os.Stat(cfg.CloudflaredExecutable); err != nil {
		return model.ConfigureTunnelResult{}, fmt.Errorf("cloudflared.exe 不存在: %w", err)
	}
	if _, err := os.Stat(processmanager.CertificatePath()); err != nil {
		return model.ConfigureTunnelResult{}, errors.New("Cloudflare 尚未授权，请先点击登录 Cloudflare")
	}

	commandCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if _, err := c.ensureDNSRoute(commandCtx, cfg, tunnelID, domain); err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	return model.ConfigureTunnelResult{
		TunnelID:        tunnelID,
		TunnelName:      cfg.TunnelName,
		Domain:          domain,
		CredentialsPath: processmanager.CredentialsPath(tunnelID),
		RemoteMCPURL:    "https://" + domain + "/mcp",
		AuthorizeURL:    "https://" + domain + "/oauth/authorize",
		Message:         "DNS 路由已修复，并通过公网解析验证",
	}, nil
}

func waitForDNS(ctx context.Context, domain string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lookupCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		lastErr = resolvePublicDNS(lookupCtx, domain)
		cancel()
		if lastErr == nil {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if time.Now().After(deadline) {
			return false, lastErr
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func resolvePublicDNS(ctx context.Context, domain string) error {
	addresses, defaultErr := net.DefaultResolver.LookupHost(ctx, domain)
	if defaultErr == nil && len(addresses) > 0 {
		return nil
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "udp", "1.1.1.1:53")
		},
	}
	addresses, cloudflareErr := resolver.LookupHost(ctx, domain)
	if cloudflareErr == nil && len(addresses) > 0 {
		return nil
	}
	return fmt.Errorf("系统 DNS: %v; 1.1.1.1: %v", defaultErr, cloudflareErr)
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

func compactOutput(output string) string {
	output = strings.TrimSpace(strings.ReplaceAll(output, "\r", ""))
	if len(output) > 800 {
		return output[:800] + "…"
	}
	return output
}
