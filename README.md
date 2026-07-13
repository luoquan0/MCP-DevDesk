# MCP DevDesk

MCP DevDesk 是一个面向 Windows 的可视化本地开发 MCP 管理器。项目目标是把现有便携版的 Cloudflare 固定域名、自动守护和权限控制，与 DevSpace 风格的多项目、Git Worktree、Skills 和现代管理界面结合起来。

当前仓库同时保留：

- 原始便携版：根目录中的 `coding-tools-mcp.exe`、`cloudflared.exe` 和 PowerShell 脚本。
- 新版管理器：`app/`，使用 Go 标准库实现，可离线编译为 Windows EXE。
- DevSpace 参考源码包：`devspace-1.0.4.tar.gz`，仅作为 MIT 许可证下的设计和实现参考。

## 当前里程碑

当前版本 `0.8.0` 已完成：

- 可视化仪表盘
- 本地配置管理
- MCP 与 cloudflared 服务启停
- Cloudflare 浏览器登录授权
- 固定域名 Tunnel 创建与 DNS 路由
- 安全、信任、危险三档权限
- 实时状态与日志查看
- Windows 单文件编译
- Windows GUI 子系统启动，不显示 CMD 窗口
- MCP DevDesk 自身创建的 Windows 原生窗口
- 内嵌 Microsoft Edge WebView2 渲染，不再启动 Edge 浏览器窗口
- 状态轮询子进程全部隐藏，不再闪烁 CMD 黑框
- 系统托盘与托盘服务控制菜单
- 单实例运行
- Windows 登录时后台启动
- 关闭界面后继续在托盘运行
- MCP 端口在线切换
- Cloudflare Tunnel 自动跟随 MCP 端口
- 本机 cloudflared 进程监控
- 重复 Tunnel 检测与按 PID 关闭
- Apple 风格 Vue 3 + TypeScript 桌面界面
- 默认以 1200 × 800 居中窗口启动，不再默认最大化
- 从 `logo/` 品牌源图自动生成界面、窗口、托盘和 EXE 图标
- 前端、Go 测试、Windows GUI/CLI 和便携版的一体化构建流程
- 可在旧核心和新版 Go 核心之间切换，旧核心持续保留为兼容回退
- Go MCP Streamable HTTP、SSE 断线恢复与会话管理
- OAuth 2.1 授权码流程、PKCE S256、动态客户端注册和资源绑定
- 文件、命令会话、Git、权限、审计和图片工具
- Windows DPAPI 密钥加密、随机生成、自定义、显示和复制
- 多授权根目录及工具配置档位的实际执行限制
- ChatGPT/DevSpace 兼容的 OAuth 根资源与 `/mcp` 资源发现
- 服务工作目录和本地项目目录支持 Windows 原生文件夹浏览选择，无需复制粘贴路径
- 项目页支持取消添加和随时修改项目目录；当前活动项目修改后会安全切换 MCP 工作目录
- 服务页工作目录改为只读展示，目录管理统一收口到项目页
- 未知 MCP 会话返回 404，客户端可在核心重启后自动重新初始化
- 刷新令牌 DPAPI 加密持久化，核心重启后仍可续签
- 项目页当前工作目录安全热切换与失败自动回滚
- 编译产物和便携包内核心的端到端自动验收
- `save_chatgpt_image` 已与旧 Python 核心统一：只接受 `source_image` 文件参数并流式下载原始字节
- `write_image` 独立保留 Base64/Data URL 小图写入，避免大图误走工具消息传输
- 图片下载限制 HTTPS/443、拒绝私网解析、限制重定向和大小，并在校验 MIME、扩展名与解码结果后原子落盘
- 项目详情展示当前分支 Git 提交历史、完整提交 ID、作者、时间和分支/标签，并使用固定高度滚动区域避免大量记录撑开界面
- Git 历史支持复制提交 ID 和安全回档；回档前要求工作区干净并自动创建备份分支保存原 HEAD
- 管理器、MCP、Tunnel、Watchdog 与审计日志统一最多保留最新 100 条，并增加 2 MB 单文件保护
- 设置页可随时关闭或开启日志记录；关闭后运行中的服务立即停止写入新日志，无需重启
- 一个桌面管理器可同时管理多个独立 MCP 实例；每个实例绑定一个项目目录和独立端口
- 多实例支持独立启动、停止、重启、自动启动、Watchdog、权限、核心模式与日志开关
- 每个附加实例使用独立配置目录和日志目录，删除实例不会删除项目文件
- 每个实例可配置独立 Cloudflare Tunnel 名称与域名，并自动检查端口、域名和 Tunnel 名称冲突
- MCP 实例页面支持自动分配空闲端口、浏览选择项目目录、复制本地/公网地址及查看实例日志

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
go run -mod=vendor ./cmd/mcp-devdesk
```

程序会直接创建属于 `MCP-DevDesk.exe` 的 Windows 窗口，并在窗口内嵌 WebView2 渲染管理界面。不会再执行 `msedge.exe --app=...`。

本机管理地址仍然保留，主要供内嵌窗口、CLI 和故障诊断使用：

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

## Go MCP 核心

`0.8.0` 提供可直接使用的独立 Go MCP 核心。桌面管理器默认继续使用旧核心，用户可在“服务 → MCP 核心”中切换到 Go 核心，并在需要时一键切回：

```powershell
cd app
go run ./cmd/mcp-core --workspace .. --port 18765 --permission-mode trusted
```

预览地址：

```text
http://127.0.0.1:18765/mcp
```

Go 核心当前提供 30 个工具，覆盖文件读写、递归文件列表、文本搜索、多文件补丁、命令会话、Git、权限状态、审计和图片传输。旧核心已有的 22 个工具名称继续保留，并兼容常用的下划线参数和命令字符串格式。

图片工具职责已拆分：ChatGPT 生成图或附件必须通过 `save_chatgpt_image.source_image` 文件引用传输；已经持有完整 Base64 或 Data URL 的传统客户端使用 `write_image`。可选环境变量 `MCP_DEV_DESK_IMAGE_DOWNLOAD_HOSTS` 可以设置逗号分隔的下载域名白名单，例如 `*.openai.com,*.oaiusercontent.com`；留空时允许所有可解析到公网地址的 HTTPS/443 主机。

设置页集中管理各核心和 MCP 实例使用的 OAuth 配置。附加实例拥有独立运行数据目录，并沿用管理器中的静态客户端配置。Windows 下 `secrets.json` 使用当前用户 DPAPI 加密，旧明文文件会在首次读取时自动迁移。

Go 核心的 OAuth 模式还支持：

- 受保护资源与授权服务器元数据发现
- 动态客户端注册
- 授权码 + PKCE S256
- 精确回调地址校验
- `resource` 受众绑定
- 短期访问令牌和刷新令牌轮换
- 根资源与 `/mcp` 资源的旧连接兼容
- 刷新令牌加密持久化和重启续签

命令工具不会隐式继承 OAuth Token、密码和其他常见敏感环境变量。安全模式完全拒绝命令和写入；信任模式允许工作区开发操作，但删除、覆盖和补丁删除仍要求明确确认。

