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
┌──────────────▼─────────────────────┐  ┌──────▼───────────┐
│ 可切换 MCP 核心                    │  │ cloudflared.exe  │
│ legacy: coding-tools-mcp.exe       │  │ 固定域名 Tunnel  │
│ go:     mcp-core.exe               │  │                 │
│ 127.0.0.1:8765                     │  │                 │
└────────────────────────────────────┘  └──────────────────┘
```

## 2. 端口规划

| 服务 | 默认地址 | 是否可公网暴露 |
|---|---|---|
| 管理后台 | `127.0.0.1:17860` | 否 |
| MCP 服务 | `127.0.0.1:8765` | 通过 Tunnel 暴露 |
| OAuth | 与 MCP 同端口 | 通过 Tunnel 暴露 |

## 3. 进程模型

主程序负责启动两个子进程：

1. 当前选择的 `coding-tools-mcp.exe` 或 `mcp-core.exe`
2. `cloudflared.exe`

主程序保存 PID、启动时间、退出状态和日志位置。Watchdog 每隔固定时间检查进程及本地端口；异常退出时根据配置自动重启。

主程序本身以 Windows GUI 子系统运行，不显示控制台窗口。它通过命名 Mutex 保证单实例，通过 Win32 `Shell_NotifyIcon` 提供系统托盘，并在自身 Win32 主窗口中嵌入 WebView2 展示管理界面。

第二次启动 EXE 时，新进程只调用现有实例的 `POST /api/ui/open`，由原实例恢复并前置窗口，随后新进程退出。整个过程不会调用默认浏览器或 `msedge.exe --app`。

管理器还会通过 Windows 进程信息枚举所有 `cloudflared.exe`，解析命令行中的 Tunnel UUID、名称和 `--url` 本地目标。启动前会检查同一 Tunnel 是否已经连接，防止 Watchdog 或重复点击产生新的重复进程。

修改 MCP 端口时采用“先启动新 MCP、再切换 Tunnel”的顺序。新端口未就绪前不会关闭旧 Tunnel；切换失败时会尝试恢复旧端口和旧连接。

切换核心时配置只改变 `coreMode`，端口、固定域名、工作区和 OAuth 凭据保持不变。保存后管理器停止当前核心并启动目标核心；旧核心文件不会删除，因此可以立即回退。

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

## 5. Go MCP 核心

`0.7.0` 的 Go 核心已经实现：

- MCP Streamable HTTP、SSE 与事件恢复
- OAuth 2.1、PKCE、动态客户端注册与资源受众绑定
- 文件范围：工作区、多个授权根目录、整台电脑
- 文件读取、写入、搜索、补丁、移动和删除
- 长时间命令会话、输出续读、标准输入和进程树终止
- Git 状态、Diff、日志、Show、Blame 和 Worktree
- 权限状态、工具档位和审计日志
- 图片保存及 MCP image content 输出
- 旧核心工具名称与常用参数兼容

当前仍保留旧核心作为兼容回退，不会强制删除。后续版本会在更多真实客户端验证通过后再考虑把 Go 核心设为默认。

