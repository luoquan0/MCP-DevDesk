# Cloudflare 固定域名配置

## 1. 用户体验

目标流程：

```text
点击“登录 Cloudflare”
→ 浏览器打开授权页面
→ 用户登录并选择域名
→ 程序检测 cert.pem
→ 用户填写完整子域名
→ 程序创建 Tunnel
→ 程序创建 DNS 路由
→ 启动 MCP 和 Tunnel
→ 显示最终 MCP 地址
```

## 2. CLI 流程

```powershell
cloudflared.exe tunnel login
cloudflared.exe tunnel list
cloudflared.exe tunnel create mcp-devdesk
cloudflared.exe tunnel route dns mcp-devdesk mcp.example.com
cloudflared.exe tunnel run --credentials-file <path> --protocol http2 --url http://127.0.0.1:8765 mcp-devdesk
```

## 3. 登录状态检测

Windows 默认检查：

```text
%USERPROFILE%\.cloudflared\cert.pem
```

登录进程退出且文件存在时判定授权成功。

## 4. 自动配置后的最终展示

```text
MCP URL:       https://mcp.example.com/mcp
Authorize URL: https://mcp.example.com/oauth/authorize
Tunnel Name:   mcp-devdesk
Tunnel UUID:   xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

## 5. 错误处理

- 同名 Tunnel：允许复用或创建新名称。
- DNS 已存在：要求用户确认，不静默覆盖。
- 网络不通：展示需要手工添加的 CNAME。
- 凭据缺失：提示重新授权或选择凭据文件。
- Tunnel 掉线：Watchdog 自动重启并记录日志。

