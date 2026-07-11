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
- 内嵌 HTML、CSS、JavaScript
- Windows 子进程管理
- `cloudflared.exe`
- 现有 `coding-tools-mcp.exe`
- JSON 配置文件

该方案不依赖联网安装第三方 Go 包，当前环境可直接编译。

### 桌面封装阶段

- Go 标准库 Win32 API
- Windows GUI 子系统
- Edge App 模式
- 系统托盘和单实例 Mutex
- HKCU 开机启动

当前桌面版不依赖联网下载第三方模块。Microsoft Edge 存在时，使用 `--app=http://127.0.0.1:17860` 打开无地址栏独立窗口；未检测到 Edge 时退回默认浏览器。Wails/WebView2 仍作为以后可选的真正内嵌壳，不影响当前业务后端。

## 3. 目录结构

```text
app/
├── cmd/mcp-devdesk/       程序入口
├── internal/
│   ├── config/            配置读取、校验和持久化
│   ├── desktop/           Win32 托盘、单实例、Edge App 和开机启动
│   ├── model/             公共数据结构
│   ├── process/           MCP 与 Tunnel 进程管理
│   ├── tunnel/            Cloudflare 登录和配置
│   └── web/               HTTP API 与静态管理界面
└── web/                   内嵌前端资源
```

## 4. 开发原则

### 4.1 管理后台只监听本机

管理服务固定监听 `127.0.0.1`，不得绑定 `0.0.0.0`。公网 Tunnel 只能指向 MCP 服务端口。

### 4.2 旧版核心与新版管理器解耦

首版通过命令行参数启动旧版核心。以后替换为 Go MCP 核心时，管理界面和配置格式保持兼容。

### 4.3 权限和文件范围分离

权限模式决定命令门控；文件范围决定可访问路径。危险模式不自动等于访问整台电脑。

### 4.4 所有重要修改必须提交 Git

分支约定：

- `master`：原始便携版基线。
- `develop`：新版日常开发。
- `feature/*`：较大的独立功能。

提交格式：

```text
feat: 新功能
fix: 修复
docs: 文档
refactor: 重构
test: 测试
chore: 构建或仓库维护
```

## 5. 配置文件

便携开发版默认位置：

```text
data/devdesk/config.json
```

正式桌面版将改到：

```text
%LOCALAPPDATA%\MCPDevDesk\config.json
```

主要字段：

```json
{
  "workspace": "D:\\Projects",
  "mcpPort": 8765,
  "adminPort": 17860,
  "permissionMode": "trusted",
  "fileScope": "workspace",
  "allowNetwork": true,
  "domain": "mcp.example.com",
  "tunnelName": "mcp-devdesk",
  "tunnelId": "",
  "autoStart": false,
  "watchdog": true
}
```

## 6. API 约定

管理 API 使用 JSON：

```text
GET  /api/status
GET  /api/config
PUT  /api/config
POST /api/services/start
POST /api/services/stop
POST /api/services/restart
POST /api/services/takeover
POST /api/cloudflare/login
GET  /api/cloudflare/login/status
POST /api/cloudflare/configure
GET  /api/logs
GET  /api/system/desktop
PUT  /api/system/startup
POST /api/ui/open
POST /api/services/change-port
GET  /api/tunnels/processes
DELETE /api/tunnels/processes/{pid}
POST /api/tunnels/sync-port
```

所有修改接口仅接受本机请求，并验证 `Origin` 和 `Host`。

## 7. 本地构建

```powershell
cd app
go test ./...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ..\dist\MCP-DevDesk.exe ./cmd/mcp-devdesk
```

或者运行仓库根目录的：

```powershell
.\build.ps1
```

## 8. 桌面模式与 Wails 计划

当前已经提供纯 Go 桌面壳：

- 双击 EXE 后不显示控制台窗口。
- Microsoft Edge 使用 App 模式展示现有前端。
- 关闭 App 窗口不会退出后台管理器。
- 托盘菜单可以打开界面、启停服务和退出。
- 命名 Mutex 防止重复后台实例。
- `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 保存当前用户开机启动。

网络依赖可用后仍可评估 Wails：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

随后可建立 Wails 壳，将现有管理 API 服务封装为 Go service binding。当前页面和后端无需重写。

