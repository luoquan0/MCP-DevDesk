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
cloudflared.exe tunnel run --credentials-file <path> --protocol http2 --url http://127.0.0.1:<当前 MCP 端口> mcp-devdesk
```

## 3. 登录状态检测

Windows 默认检查：

```text
%USERPROFILE%\.cloudflared\cert.pem
```

登录进程退出且文件存在时判定授权成功。

用户点击“重新授权”时，Windows 版会在启动 `cloudflared tunnel login` 前清理旧的 `cert.pem`。`cloudflared` 默认不会覆盖已经存在的 Origin Certificate；如果旧证书仍在，二次登录会立即退出，表现为点击“重新授权”后浏览器没有打开。清理后授权流程会重新生成 `cert.pem`。如果用户中途取消授权，界面会回到未授权状态，可以再次点击授权；已有 Tunnel JSON 凭据、Tunnel UUID 和 DNS 配置不会因此被删除。

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
- 凭据缺失或 Origin Certificate 过期：允许重新授权并重建 `cert.pem`；已有 Tunnel 凭据、Tunnel UUID 和 DNS 配置不会被删除。
- Tunnel 掉线：Watchdog 自动重启并记录日志。

## 6. MCP 端口联动

MCP 端口不再固定为 `8765`。用户在“项目与服务”页面修改端口时，管理器会：

1. 检查新端口是否被其他进程占用。
2. 使用新端口启动 MCP，并等待端口真正就绪。
3. 关闭与当前 Tunnel UUID 或名称匹配的旧 cloudflared 连接。
4. 使用新的 `--url http://127.0.0.1:<端口>` 启动一个 Tunnel 进程。
5. 成功后保存新端口；失败时尝试恢复旧 MCP 与 Tunnel。

固定域名和 DNS 记录不需要重新创建，因为 Cloudflare DNS 始终指向同一个 Tunnel UUID，只有 Tunnel 在本机连接的目标端口发生变化。

## 7. 隧道进程监控

管理界面会枚举本机所有 `cloudflared.exe` 进程并显示：

- PID 和父进程 PID
- 可执行文件路径
- Tunnel 名称与 UUID
- 实际本地转发 URL 和端口
- 是否由当前 MCP DevDesk 管理
- 是否匹配当前配置
- 是否存在同 UUID 或同名称的重复进程

用户可以按 PID 关闭单个进程，也可以点击“同步到当前 MCP 端口”，清理当前 Tunnel 的旧连接并只启动一个正确连接。其他 Tunnel UUID 不会被自动关闭。

为了避免泄露凭据，进程命令行中的 `--token` 会在 API 返回前替换为 `***`。

