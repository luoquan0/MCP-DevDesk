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

- Go
- Wails v2
- React + TypeScript
- WebView2

当前网页界面和 API 会设计为可被 Wails 复用。安装 Wails 后，前端可直接迁入桌面窗口，不需要重写业务后端。

## 3. 目录结构

```text
app/
├── cmd/mcp-devdesk/       程序入口
├── internal/
│   ├── config/            配置读取、校验和持久化
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
POST /api/cloudflare/login
GET  /api/cloudflare/login/status
POST /api/cloudflare/configure
GET  /api/logs
```

所有修改接口仅接受本机请求，并验证 `Origin` 和 `Host`。

## 7. 本地构建

```powershell
cd app
go test ./...
go build -trimpath -ldflags "-s -w" -o ..\dist\MCP-DevDesk.exe ./cmd/mcp-devdesk
```

或者运行仓库根目录的：

```powershell
.\build.ps1
```

## 8. Wails 集成计划

网络依赖可用后执行：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

随后建立 Wails 壳，将现有管理 API 服务封装为 Go service binding。现有 `app/web` 页面也可以先作为 Wails 内嵌资源运行。

