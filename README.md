# MCP DevDesk

MCP DevDesk 是一个面向 Windows 的可视化本地开发 MCP 管理器。项目目标是把现有便携版的 Cloudflare 固定域名、自动守护和权限控制，与 DevSpace 风格的多项目、Git Worktree、Skills 和现代管理界面结合起来。

当前仓库同时保留：

- 原始便携版：根目录中的 `coding-tools-mcp.exe`、`cloudflared.exe` 和 PowerShell 脚本。
- 新版管理器：`app/`，使用 Go 标准库实现，可离线编译为 Windows EXE。
- DevSpace 参考源码包：`devspace-1.0.4.tar.gz`，仅作为 MIT 许可证下的设计和实现参考。

## 当前里程碑

当前 `0.3.0-dev` 已完成：

- 可视化仪表盘
- 本地配置管理
- MCP 与 cloudflared 服务启停
- Cloudflare 浏览器登录授权
- 固定域名 Tunnel 创建与 DNS 路由
- 安全、信任、危险三档权限
- 实时状态与日志查看
- Windows 单文件编译
- Windows GUI 子系统启动，不显示 CMD 窗口
- Edge App 模式独立桌面窗口
- 系统托盘与托盘服务控制菜单
- 单实例运行
- Windows 登录时后台启动
- 关闭界面后继续在托盘运行
- MCP 端口在线切换
- Cloudflare Tunnel 自动跟随 MCP 端口
- 本机 cloudflared 进程监控
- 重复 Tunnel 检测与按 PID 关闭

详细文档见：

- [开发说明](docs/DEVELOPMENT.md)
- [总体架构](docs/ARCHITECTURE.md)
- [安全模型](docs/SECURITY.md)
- [Cloudflare 流程](docs/CLOUDFLARE.md)
- [Windows 桌面模式](docs/DESKTOP.md)
- [开发路线图](docs/ROADMAP.md)

## 本地运行

```powershell
cd app
go run ./cmd/mcp-devdesk
```

程序会优先以 Edge App 模式打开无地址栏独立窗口。也可直接访问：

```text
http://127.0.0.1:17860
```

## 编译

```powershell
.\build.ps1
```

生成文件：

```text
dist\MCP-DevDesk-amd64.exe
```

可选命令行控制：

```powershell
dist\devdeskctl-amd64.exe status
dist\devdeskctl-amd64.exe desktop
dist\devdeskctl-amd64.exe tunnels
dist\devdeskctl-amd64.exe sync-tunnel
dist\devdeskctl-amd64.exe open
dist\devdeskctl-amd64.exe startup-on
dist\devdeskctl-amd64.exe startup-off
dist\devdeskctl-amd64.exe start
dist\devdeskctl-amd64.exe stop
```

管理界面只监听本机地址。Cloudflare Tunnel 仅暴露 MCP/OAuth 服务，不暴露管理后台。

