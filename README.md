# MCP DevDesk

MCP DevDesk 是一个面向 Windows 的可视化本地开发 MCP 管理器。项目目标是把现有便携版的 Cloudflare 固定域名、自动守护和权限控制，与 DevSpace 风格的多项目、Git Worktree、Skills 和现代管理界面结合起来。

当前仓库同时保留：

- 原始便携版：根目录中的 `coding-tools-mcp.exe`、`cloudflared.exe` 和 PowerShell 脚本。
- 新版管理器：`app/`，使用 Go 标准库实现，可离线编译为 Windows EXE。
- DevSpace 参考源码包：`devspace-1.0.4.tar.gz`，仅作为 MIT 许可证下的设计和实现参考。

## 当前里程碑

当前版本 `0.8.4` 已完成：

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
- MCP、OAuth、命令会话和工具并发均增加硬上限，防止长期运行或异常请求持续推高内存与线程占用
- Git 输出在读取阶段即限制大小并增加超时和进程树终止，避免大型 Diff 或异常仓库拖垮管理器
- 日志写入改为有界内存窗口，避免每条日志重复扫描整个文件；多实例状态改为批量端口查询
- Windows 代理密码使用当前用户 DPAPI 加密保存；配置写盘失败时自动恢复旧的内存配置
- 界面隐藏或后台运行时暂停高频状态请求，并拆分主状态和实例状态刷新周期
- 修复反复关闭和重开主界面时旧 WebView2 窗口对象与 COM 引用未释放的问题
- WebView2 初始化异常不再直接终止整个管理器；创建超过 20 秒或窗口线程无响应时自动使用系统浏览器打开管理界面
- 第二次启动软件时优先通过 Windows 托盘消息唤醒已有实例，不再完全依赖本地管理端口可用
- 增加 50 次窗口循环和多轮高频状态请求的长运行加速测试脚本
- 项目页新增全局项目提示词和单项目提示词，可统一限定 AI 的执行方式、完成标准和沟通规则
- 全局提示词对所有项目生效，单项目提示词在其后叠加；两级提示词均独立持久化且不会写入用户项目目录
- Go MCP 核心启动时由管理器生成独立指令文件，并注入 MCP `initialize.instructions`；可与工作区 `AGENTS.md` / `CLAUDE.md` 同时生效
- 内置“任务完成后再回复”模板，用于要求 AI 在没有真实阻塞时连续完成读取、修改、测试、构建和验证后再回复用户
- 新增 `read_files` 批量读取工具，一次可受限读取最多 8 个文件，单文件失败不影响其他结果
- 新增 `project_snapshot` 项目概览工具，一次返回顶层结构、Git 状态、构建文件、主要语言和项目规则
- Go 核心启动时自动识别工作区根目录的 `AGENTS.md` 和 `CLAUDE.md`，总注入内容限制为 32 KB
- 文件、搜索和 Git 工具统一返回截断、字节数、结果上限及下一页参数；超大文本结果自动压缩为摘要，完整数据保留在结构化结果中
- 运行中的实例禁止直接切换核心；公网实例要求二次确认，并支持一键复制为使用另一核心的新实例
- 设置页新增脱敏诊断报告导出，汇总资源、实例、端口、桌面和最近管理日志，不导出 OAuth 密钥与密码

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

`0.8.4` 提供可直接使用的独立 Go MCP 核心。新建实例建议使用 Go 核心；Python 核心继续作为兼容回退。已经添加到 ChatGPT 的实例不建议在同一域名下频繁切换核心，因为切换后可能需要重新授权：

```powershell
cd app
go run ./cmd/mcp-core --workspace .. --port 18765 --permission-mode trusted
```

预览地址：

```text
http://127.0.0.1:18765/mcp
```

Go 核心当前提供 32 个工具，覆盖文件读写、批量读取、项目概览、递归文件列表、文本搜索、多文件补丁、命令会话、Git、权限状态、审计和图片传输。旧核心已有的 22 个工具名称继续保留，并兼容常用的下划线参数和命令字符串格式。

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

