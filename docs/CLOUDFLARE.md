# Cloudflare 固定域名配置

## 1. 用户体验

目标流程：

```text
点击“登录 Cloudflare”
→ 浏览器打开授权页面
→ 用户登录并选择域名
→ 程序检测 cert.pem
→ 用户填写完整子域名
→ 程序创建 Tunnel，并让 cloudflared 直接把 JSON 写入 data/devdesk/cloudflare/
→ 程序按 Tunnel UUID 固定凭据文件名
→ 程序创建 DNS 路由
→ 启动 MCP 和 Tunnel
→ 显示最终 MCP 地址
```

## 2. CLI 流程

```powershell
cloudflared.exe tunnel login
cloudflared.exe tunnel list
cloudflared.exe tunnel create --credentials-file <MCP-DevDesk目录>\data\devdesk\cloudflare\.tunnel-create-<随机值>.json mcp-devdesk
cloudflared.exe tunnel route dns mcp-devdesk mcp.example.com
cloudflared.exe tunnel run --credentials-file <MCP-DevDesk目录>\data\devdesk\cloudflare\<Tunnel-UUID>.json --protocol http2 --url http://127.0.0.1:<当前 MCP 端口> mcp-devdesk
```

MCP DevDesk 不再依赖 `%USERPROFILE%\.cloudflared\<Tunnel-UUID>.json` 运行 Tunnel。当前 cloudflared 支持 `--credentials-file` 时，新 Tunnel 的 JSON 从创建时就直接写入软件便携数据目录，随后重命名为稳定的 UUID 文件名：

```text
<MCP-DevDesk目录>\data\devdesk\cloudflare\<Tunnel-UUID>.json
```

为了兼容非常旧、不认识 `--credentials-file` 的 cloudflared，程序会检测该参数错误并仅在这种情况下回退到旧创建方式，然后立即把生成的 JSON 自动迁移到便携目录。

旧版本已经存在于 `%USERPROFILE%\.cloudflared\` 的 Tunnel JSON 会在首次使用时自动复制到便携目录。之后 Tunnel 进程使用便携目录中的文件，因此整个 MCP DevDesk 文件夹可以一起迁移到另一台电脑。

## 3. 登录状态检测

Windows 默认检查：

```text
%USERPROFILE%\.cloudflared\cert.pem
```

登录进程退出且文件存在时判定授权成功。

`cert.pem` 是账号级 Origin Certificate，仍由 cloudflared 保存在当前 Windows 用户目录，不进入便携包。仅运行已经配置好的 Tunnel 依赖单 Tunnel JSON，不依赖 `cert.pem`；创建、删除 Tunnel 或修改 DNS 等账号级管理操作仍需要当前电脑完成 Cloudflare 授权。

用户点击“重新授权”时，Windows 版会在启动 `cloudflared tunnel login` 前清理旧的 `cert.pem`。`cloudflared` 默认不会覆盖已经存在的 Origin Certificate；如果旧证书仍在，二次登录会立即退出，表现为点击“重新授权”后浏览器没有打开。清理后授权流程会重新生成 `cert.pem`。如果用户中途取消授权，界面会回到未授权状态，可以再次点击授权；已有 Tunnel JSON 凭据、Tunnel UUID 和 DNS 配置不会因此被删除。

## 4. 自动配置后的最终展示

```text
MCP URL:       https://mcp.example.com/mcp
Authorize URL: https://mcp.example.com/oauth/authorize
Tunnel Name:   mcp-devdesk
Tunnel UUID:   xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
Credentials:   data/devdesk/cloudflare/<Tunnel-UUID>.json
```

## 5. 错误处理

- 同名 Tunnel：允许复用或创建新名称。
- DNS 已存在：要求用户确认，不静默覆盖。
- 网络不通：展示需要手工添加的 CNAME。
- 便携凭据缺失：尝试从当前用户 `%USERPROFILE%\.cloudflared\<Tunnel-UUID>.json` 迁移；两处都不存在时提示重新配置。
- 新 Tunnel 已创建但 UUID 输出解析异常：程序还会从刚生成的 JSON 中读取 Tunnel UUID；仍无法识别时保留临时凭据文件并报告其路径，避免凭据丢失。
- Origin Certificate 过期：允许重新授权并重建 `cert.pem`；已有便携 Tunnel 凭据、Tunnel UUID 和 DNS 配置不会被删除。
- Tunnel 掉线：Watchdog 自动重启并记录日志。

## 6. MCP 端口联动

MCP 端口不再固定为 `8765`。用户在“项目与服务”页面修改端口时，管理器会：

1. 检查新端口是否被其他进程占用。
2. 使用新端口启动 MCP，并等待端口真正就绪。
3. 关闭与当前 Tunnel UUID 或名称匹配的旧 cloudflared 连接。
4. 使用便携 JSON 凭据和新的 `--url http://127.0.0.1:<端口>` 启动一个 Tunnel 进程。
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
