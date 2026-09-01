from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected block not found in {path}: {old[:120]!r}")
    text = text.replace(old, new, 1)
    p.write_text(text, encoding="utf-8")


# 1) Cloudflare DNS route: support older cloudflared, verify public DNS, and expose repair API.
path = "app/internal/tunnel/cloudflare.go"
p = Path(path)
text = p.read_text(encoding="utf-8")
text = text.replace('"fmt"\n\t"os"', '"fmt"\n\t"net"\n\t"os"', 1)
old_route = '''\trouteOutput, routeErr := c.run(commandCtx, cfg, dnsRouteArguments(tunnelID, request.Domain)...)
\tif routeErr != nil {
\t\treturn model.ConfigureTunnelResult{}, fmt.Errorf("配置 DNS 路由失败: %w; %s", routeErr, compactOutput(routeOutput))
\t}
'''
new_route = '''\trouteOutput, routeErr := c.ensureDNSRoute(commandCtx, cfg, tunnelID, request.Domain)
\tif routeErr != nil {
\t\treturn model.ConfigureTunnelResult{}, routeErr
\t}
'''
if old_route not in text:
    raise SystemExit("cloudflare route block not found")
text = text.replace(old_route, new_route, 1)
text = text.replace('Message:         "Tunnel 和 DNS 已配置完成，域名已指向当前 Tunnel",', 'Message:         "Tunnel 和 DNS 已配置完成，公网 DNS 验证通过",', 1)
marker = '''func dnsRouteArguments(tunnelID, domain string) []string {
\treturn []string{"tunnel", "route", "dns", "--overwrite-dns", tunnelID, domain}
}
'''
if marker not in text:
    raise SystemExit("dnsRouteArguments marker not found")
addition = marker + r'''
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
'''
text = text.replace(marker, addition, 1)
p.write_text(text, encoding="utf-8")

# 2) Tunnel unit tests for overwrite compatibility.
path = "app/internal/tunnel/cloudflare_test.go"
p = Path(path)
text = p.read_text(encoding="utf-8")
append = r'''

func TestLegacyDNSRouteArguments(t *testing.T) {
	const tunnelID = "11111111-2222-3333-4444-555555555555"
	const domain = "mcp2.example.com"
	got := legacyDNSRouteArguments(tunnelID, domain)
	want := []string{"tunnel", "route", "dns", tunnelID, domain}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy dns route arguments mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestOverwriteDNSFlagUnsupported(t *testing.T) {
	err := errors.New("exit status 1")
	if !overwriteDNSFlagUnsupported("Incorrect Usage: flag provided but not defined: -overwrite-dns", err) {
		t.Fatal("expected old cloudflared overwrite flag failure to use legacy fallback")
	}
	if overwriteDNSFlagUnsupported("failed to create route: permission denied", err) {
		t.Fatal("unrelated route errors must not silently fall back")
	}
}
'''
if 'func TestLegacyDNSRouteArguments' not in text:
    text = text.rstrip() + append + "\n"
text = text.replace('import (\n\t"reflect"', 'import (\n\t"errors"\n\t"reflect"', 1)
p.write_text(text, encoding="utf-8")

# 3) Application method that repairs DNS for primary or managed instances.
path = "app/internal/application/instances.go"
p = Path(path)
text = p.read_text(encoding="utf-8")
start = text.find("func (a *App) ConfigureInstanceTunnel(")
if start < 0:
    raise SystemExit("ConfigureInstanceTunnel not found")
next_func = text.find("\nfunc (a *App) ", start + 10)
if next_func < 0:
    raise SystemExit("next application method after ConfigureInstanceTunnel not found")
repair_method = r'''

func (a *App) RepairInstanceTunnelDNS(ctx context.Context, id string) (model.ConfigureTunnelResult, error) {
	if id == model.PrimaryInstanceID {
		return a.tunnel.RepairDNS(ctx, a.config.Get())
	}
	_, runtime, err := a.instanceRecordAndRuntime(id)
	if err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	result, err := a.tunnel.RepairDNS(ctx, runtime.config.Get())
	if err != nil {
		return model.ConfigureTunnelResult{}, err
	}
	_, _ = a.instances.Touch(id)
	return result, nil
}
'''
if "RepairInstanceTunnelDNS" not in text:
    text = text[:next_func] + repair_method + text[next_func:]
p.write_text(text, encoding="utf-8")

# 4) Web API endpoint.
path = "app/internal/web/server.go"
p = Path(path)
text = p.read_text(encoding="utf-8")
route = '\tmux.HandleFunc("POST /api/instances/{id}/cloudflare/configure", s.handleConfigureInstanceTunnel)\n'
if route not in text:
    raise SystemExit("instance tunnel route not found")
if "cloudflare/repair-dns" not in text:
    text = text.replace(route, route + '\tmux.HandleFunc("POST /api/instances/{id}/cloudflare/repair-dns", s.handleRepairInstanceTunnelDNS)\n', 1)
handler_marker = "func (s *Server) handleInstanceLogs"
idx = text.find(handler_marker)
if idx < 0:
    raise SystemExit("handleInstanceLogs marker not found")
handler = r'''func (s *Server) handleRepairInstanceTunnelDNS(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	result, err := s.app.RepairInstanceTunnelDNS(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

'''
if "func (s *Server) handleRepairInstanceTunnelDNS" not in text:
    text = text[:idx] + handler + text[idx:]
p.write_text(text, encoding="utf-8")

# 5) Frontend store action.
path = "frontend/src/stores/app.ts"
p = Path(path)
text = p.read_text(encoding="utf-8")
needle = '''    async loadInstanceLog(id: string, name: string, limit = 100) {
'''
if needle not in text:
    raise SystemExit("loadInstanceLog marker not found")
store_method = '''    async repairInstanceTunnelDNS(id: string) {
      const ui = useUiStore();
      const result = await this.runAction(`repair-instance-dns-${id}`, () => api<ConfigureTunnelResult>(`/api/instances/${encodeURIComponent(id)}/cloudflare/repair-dns`, {
        method: "POST",
      }));
      await this.loadInstances();
      ui.toast("DNS 修复完成", result.message || result.domain, "success");
      return result;
    },
'''
if "async repairInstanceTunnelDNS" not in text:
    text = text.replace(needle, store_method + needle, 1)
p.write_text(text, encoding="utf-8")

# 6) Multi-instance UI repair button and error surfacing.
path = "frontend/src/pages/InstancesPage.vue"
p = Path(path)
text = p.read_text(encoding="utf-8")
function_marker = '''async function copyValue(value: string, label: string) {
'''
if function_marker not in text:
    raise SystemExit("copyValue marker not found")
repair_function = '''async function repairTunnelDNS(instance: MCPInstance) {
  try {
    await app.repairInstanceTunnelDNS(instance.id);
  } catch (error) {
    ui.toast("修复 DNS 失败", errorMessage(error), "danger");
  }
}

'''
if "async function repairTunnelDNS" not in text:
    text = text.replace(function_marker, repair_function + function_marker, 1)
button = '''          <AppButton tone="secondary" icon="cloud" @click="startTunnelEdit(instance)">配置 Tunnel</AppButton>
'''
if button not in text:
    raise SystemExit("configure tunnel button not found")
repair_button = '''          <AppButton tone="secondary" icon="cloud" @click="startTunnelEdit(instance)">配置 Tunnel</AppButton>
          <AppButton v-if="instance.domain && instance.tunnelId" tone="secondary" icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instance.id}`" @click="repairTunnelDNS(instance)">修复 DNS</AppButton>
'''
if "repair-instance-dns-" not in text:
    text = text.replace(button, repair_button, 1)
# Make the configure form promise explicit so users know success means public DNS exists.
text = text.replace('配置独立域名</h3>', '配置独立域名</h3><small>保存后会自动创建并验证公网 DNS；失败会显示 cloudflared 原始错误。</small>', 1)
p.write_text(text, encoding="utf-8")

print("DNS repair patch applied")
