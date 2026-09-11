# MCP DevDesk v0.13.0-beta.1

0.13 Preview 把 MCP DevDesk 从 MCP 管理器推进为可恢复、可审核的 Windows AI Coding Workspace。

## 本次可测试内容

- Agent Runtime：Task v2 持久化当前步骤、下一步、心跳、改动文件、Job/验证结果和失败原因；Task ID / Job ID、隔离 Worktree 与后台命令在重启后可恢复。
- Worktree 安全：主工作区即使已有未提交修改也可启动任务；这些修改不会复制进 AI Worktree。验收时只要 HEAD 未前进且本地修改不与任务改动文件重叠即可应用，重叠时 fail closed。AI 只能 `task_finish`，Accept / Reject 仍只在本机 DevDesk UI。
- 高层验证：新增 `validate_project`，自动识别 build.ps1、go.mod、package.json、Cargo.toml、.sln、pyproject/pytest/requirements，并把 test/check/build 作为持久 Job 关联回 Task。
- 代码导航：新增 `list_symbols`、`document_symbols`、`workspace_symbols`、`find_definition`、`find_references`。Preview 会探测已安装 language-server，但当前结果明确标记 `lexical-fallback`，不把未接入的 LSP 会话伪装成完整 LSP。
- OpenAI Secure Tunnel：主实例可在 Cloudflare / OpenAI / Local 之间选择；OpenAI API Key 使用现有 Windows 加密 secrets，sidecar token 仅允许 loopback。
- Screen Vision：改为每 MCP 实例读取自己的 `config.json`，新增无截图的 `screen_capture_probe` 兼容性探针。后台窗口仅尝试 PrintWindow / WindowDC 等不改 Z-order 的 HWND 自有路径；最小化窗口直接 fail closed，不再临时恢复。Windows Graphics Capture 原生后端暂不在未完成实机矩阵前启用。
- 文档状态同步：稳定版仍为 0.12.34，0.13.0-beta.1 是独立 prerelease。

## 已知 Preview 边界

- Windows Graphics Capture 原生后端仍需要 VMware / Chromium / WebView2 / 多显示器实机验证；本版本不会宣称未验证能力已完成。
- Authenticode 需要实际代码签名证书；本测试版可能触发 Windows SmartScreen。
- OpenAI Secure Tunnel Preview 使用用户提供的官方 `tunnel-client.exe`，当前不捆绑第三方二进制。
- macOS / Linux 不属于 0.13 范围。
