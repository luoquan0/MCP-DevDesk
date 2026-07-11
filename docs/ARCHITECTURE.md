# 总体架构

## 1. 逻辑结构

```text
┌──────────────────────────────────────────────┐
│ Windows 桌面壳                               │
│ Win32 原生窗口 / WebView2 / 托盘 / 单实例    │
└──────────────────────┬───────────────────────┘
                       │ 打开本机管理地址
┌──────────────────────▼───────────────────────┐
│ MCP DevDesk 管理界面                         │
│ 内嵌 HTML / CSS / JavaScript                 │
└──────────────────────┬───────────────────────┘
                       │ 本机管理 API
┌──────────────────────▼───────────────────────┐
│ Go 管理后端                                  │
│                                              │
│ Config      Process       Tunnel             │
│ Status      Logs          Watchdog            │
│ Desktop     Security      Diagnostics         │
│ Port Switch Tunnel Inventory / Duplicate Guard│
└──────────────┬───────────────────┬───────────┘
               │                   │
┌──────────────▼─────────┐  ┌──────▼───────────┐
│ coding-tools-mcp.exe   │  │ cloudflared.exe  │
│ 127.0.0.1:8765         │  │ 固定域名 Tunnel  │
└────────────────────────┘  └──────────────────┘
```

## 2. 端口规划

| 服务 | 默认地址 | 是否可公网暴露 |
|---|---|---|
| 管理后台 | `127.0.0.1:17860` | 否 |
| MCP 服务 | `127.0.0.1:8765` | 通过 Tunnel 暴露 |
| OAuth | 与 MCP 同端口 | 通过 Tunnel 暴露 |

## 3. 进程模型

主程序负责启动两个子进程：

1. `coding-tools-mcp.exe`
2. `cloudflared.exe`

主程序保存 PID、启动时间、退出状态和日志位置。Watchdog 每隔固定时间检查进程及本地端口；异常退出时根据配置自动重启。

主程序本身以 Windows GUI 子系统运行，不显示控制台窗口。它通过命名 Mutex 保证单实例，通过 Win32 `Shell_NotifyIcon` 提供系统托盘，并在自身 Win32 主窗口中嵌入 WebView2 展示管理界面。

第二次启动 EXE 时，新进程只调用现有实例的 `POST /api/ui/open`，由原实例恢复并前置窗口，随后新进程退出。整个过程不会调用默认浏览器或 `msedge.exe --app`。

管理器还会通过 Windows 进程信息枚举所有 `cloudflared.exe`，解析命令行中的 Tunnel UUID、名称和 `--url` 本地目标。启动前会检查同一 Tunnel 是否已经连接，防止 Watchdog 或重复点击产生新的重复进程。

修改 MCP 端口时采用“先启动新 MCP、再切换 Tunnel”的顺序。新端口未就绪前不会关闭旧 Tunnel；切换失败时会尝试恢复旧端口和旧连接。

## 4. 兼容启动参数

现有核心支持：

```text
--workspace
--host
--port
--tool-profile
--oauth-mode
--allow-network
--permission-mode
--shell-env-inherit
--dangerously-skip-all-permissions
```

映射规则：

| UI 模式 | 核心参数 |
|---|---|
| 安全 | `--permission-mode safe` |
| 信任 | `--permission-mode trusted --allow-network` |
| 危险 | `--permission-mode dangerous --allow-network` |

## 5. 后续 Go MCP 核心

第二阶段会实现：

- MCP Streamable HTTP
- OAuth 2.1 + PKCE
- 多项目根目录
- 文件工具
- 命令会话
- Git 和 Worktree
- Skills / AGENTS.md
- 权限申请与审计

完成后可删除对 Python PyInstaller 核心的依赖。

