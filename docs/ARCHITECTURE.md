# 总体架构

## 1. 逻辑结构

```text
┌──────────────────────────────────────────────┐
│ MCP DevDesk 管理界面                         │
│ 本地网页 / 后续 Wails 桌面窗口               │
└──────────────────────┬───────────────────────┘
                       │ 本机管理 API
┌──────────────────────▼───────────────────────┐
│ Go 管理后端                                  │
│                                              │
│ Config      Process       Tunnel             │
│ Status      Logs          Watchdog            │
│ Security    Diagnostics   Update              │
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

