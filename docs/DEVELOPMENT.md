# MCP DevDesk 开发文档

## 1. 产品目标

MCP DevDesk 是一个 Windows 优先的本地开发 MCP 管理器，目标是让普通用户无需手写命令即可完成：

1. 选择本地项目目录。
2. 选择安全、信任或危险权限模式。
3. 登录 Cloudflare。
4. 使用自己的固定子域名创建 Tunnel 和 DNS 路由。
5. 启动本地 MCP 服务并连接 ChatGPT。
6. 在桌面界面查看状态、日志、凭证和权限申请。

项目第一阶段先兼容并管理现有 `coding-tools-mcp.exe`。第二阶段逐步将 MCP 核心改写为 Go，从而减少运行时依赖并获得完整源码控制。

## 2. 技术栈

### 当前 MVP

- Go 标准库
- Vue 3 + TypeScript 前端
- Windows 子进程管理
- `cloudflared.exe`
- 现有 `coding-tools-mcp.exe`
- JSON 配置文件

业务后端仍主要使用 Go 标准库。桌面窗口额外使用已提交到 `app/vendor/` 的 WebView2 Go 绑定，因此构建过程不需要联网下载 Go 模块。

### 桌面封装阶段

- Go 标准库 Win32 API
- Windows GUI 子系统
- Win32 原生窗口与内嵌 WebView2
- vendored `github.com/jchv/go-webview2`
- 系统托盘和单实例 Mutex
- HKCU 开机启动

当前桌面版直接在 `MCP-DevDesk.exe` 自身创建的 Win32 窗口内嵌 WebView2，不调用 `msedge.exe --app`。依赖已经 vendoring，构建过程统一使用 `-mod=vendor`。

## 3. 目录结构

```text
app/
├── cmd/mcp-devdesk/       程序入口
├── cmd/mcp-core/          Go MCP Core
├── cmd/devdesk-updater/   独立在线更新器
├── internal/
│   ├── application/       管理器业务编排
│   ├── config/            配置读取、校验和持久化
│   ├── desktop/           Win32 窗口、WebView2、托盘、单实例和开机启动
│   ├── instances/         多 MCP 实例
│   ├── mcpcore/           MCP Core 管理
│   ├── model/             公共数据结构
│   ├── process/           MCP 与 Tunnel 进程管理
│   ├── projects/          项目管理
│   ├── secrets/           Windows 凭证保护
│   ├── tunnel/            Cloudflare 登录和配置
│   ├── updater/           在线更新检查和下载
│   └── web/               HTTP API、SSE 与静态管理界面
└── vendor/                WebView2 Go 绑定和 Windows Loader

frontend/                  Vue 3 + TypeScript 前端
.github/workflows/          GitHub Actions
build.ps1                  完整构建入口
package-portable.ps1       Portable 打包入口
tools/                     smoke test、品牌资源和发布检查脚本
```

## 4. 开发原则

### 4.1 管理后台网络边界

内部桌面管理服务默认监听 `127.0.0.1`。可选“网页控制”使用独立监听器和独立认证机制；不开启 LAN 时仍只监听 loopback，开启 LAN 后也必须遵守 `docs/SECURITY.md` 定义的私网和认证限制。

Cloudflare Tunnel 只能用于明确设计为公网暴露的 MCP / OAuth 服务，不应直接暴露内部桌面管理端口。

### 4.2 旧版核心与新版管理器解耦

管理器保留旧版核心作为兼容回退，同时提供 Go MCP Core。切换核心时要保持管理界面、项目配置和客户端连接行为尽量兼容。

### 4.3 权限和文件范围分离

权限模式决定命令门控；文件范围决定可访问路径。危险模式不自动等于访问整台电脑。

### 4.4 GitHub `main` 是源代码真源

当前仓库不再使用旧的 `master / develop` 作为正式协作约定。

现行规则：

- `main`：唯一权威源代码和发布基线。
- `feature/*`：可选的较大功能开发分支。
- 本地长期开发分支不是发布真源，重新开始工作前应先与 `origin/main` 同步。
- 禁止用落后的本地历史覆盖远端 `main`。
- 普通修改可以直接提交到 `main`，但必须保持提交范围清晰、测试充分。

详细修改规范见仓库根目录 `AGENTS.md`。

提交格式：

```text
feat: 新功能
fix: 修复
docs: 文档
refactor: 重构
test: 测试
ci: CI / GitHub Actions
chore: 构建或仓库维护
```

`[release]` 只在用户明确要求发版时使用；普通提交不得带该标记。发版流程见 `docs/RELEASE.md`。

## 5. 配置文件

Portable 版本的用户运行数据默认位于：

```text
data/devdesk/
```

该目录属于用户运行数据，不应提交到 GitHub，也不得被在线更新包覆盖。

主要配置包含工作目录、MCP 端口、管理端口、权限模式、核心选择、Cloudflare、实例和桌面设置。配置结构以 `app/internal/config/` 的当前实现为准；修改字段时必须同时考虑旧配置兼容和迁移。

## 6. API 约定

管理 API 使用 JSON。当前接口已覆盖状态、项目、多实例、服务、Cloudflare、日志、安全、设置、更新和桌面控制等能力。

所有修改接口必须保持输入验证、Host / Origin / 身份认证边界。桌面内部 API 与可选网页控制入口虽然复用业务逻辑，但网络暴露范围不同，不能因为前端复用而降低安全检查。

具体接口以 `app/internal/web/` 的路由和测试为准。

## 7. 本地构建与测试

Go 测试：

```powershell
cd app
go test -mod=vendor ./...
```

前端生产构建：

```powershell
cd frontend
npm run build
```

依赖环境不确定时先执行：

```powershell
npm ci
```

仓库根目录完整构建：

```powershell
.\build.ps1 -Arch amd64 -RunTests
```

Portable 打包：

```powershell
.\package-portable.ps1 -Arch amd64
```

发布前以 GitHub Actions Windows runner 的完整测试结果为最终准入条件。

## 8. Windows 桌面模式

当前已经提供纯 Go 桌面壳：

- 双击 EXE 后不显示控制台窗口。
- `MCP-DevDesk.exe` 自己创建 Win32 主窗口并内嵌 Microsoft Edge WebView2。
- 关闭主界面默认继续在系统托盘后台运行。
- 托盘菜单可以打开界面、启停服务和退出。
- 命名 Mutex 防止重复后台实例。
- 第二次启动优先唤醒已经运行的实例。
- `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 保存当前用户开机启动。
- WebView2、SSE、HTTP Server、MCP、Tunnel 和后台任务的退出顺序必须可控，避免长连接拖住程序退出。

Windows 桌面行为的详细约束见 `docs/DESKTOP.md`。涉及 WebView2、托盘、进程生命周期或更新器替换运行中 EXE 的修改，即使自动测试通过，也应进行 Windows 实机验证。

## 9. UI 开发

前端使用 Vue 3 + TypeScript，并由桌面端和局域网页控制复用同一套页面。

UI 修改必须遵守根目录 `DESIGN.md`，不得为桌面端和移动网页复制两套业务页面。移动适配优先通过响应式布局完成。

## 10. 发布与在线更新

GitHub Releases 是正式在线更新源。版本号自动递增、Release Tag、Windows 构建、Portable ZIP、SHA256 和失败恢复流程统一记录在：

```text
docs/RELEASE.md
```

没有用户明确发版指令时，只提交源码，不创建 Release。
