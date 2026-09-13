# MCP DevDesk v0.13.0-beta.6

0.13 Preview 把 MCP DevDesk 从 MCP 管理器推进为可恢复、可审核的 Windows AI Coding Workspace。

## 本次可测试内容

- Agent Runtime：Task v2 持久化当前步骤、下一步、心跳、改动文件、Job/验证结果和失败原因；Task ID / Job ID、隔离 Worktree 与后台命令在重启后可恢复。
- Worktree 安全：主工作区即使已有未提交修改也可启动任务；这些修改不会复制进 AI Worktree。验收时只要 HEAD 未前进且本地修改不与任务改动文件重叠即可应用，重叠时 fail closed。AI 只能 `task_finish`，Accept / Reject 仍只在本机 DevDesk UI。
- 高层验证：新增 `validate_project`，自动识别 build.ps1、go.mod、package.json、Cargo.toml、.sln、pyproject/pytest/requirements，并把 test/check/build 作为持久 Job 关联回 Task。
- 代码导航：正式暴露 `document_symbols`、`workspace_symbols`、`find_definition`、`find_references`；`list_symbols` 继续作为 Beta 2 兼容调用别名，但不再单独占用 tools/list 槽位。Preview 会探测已安装 language-server，但当前结果明确标记 `lexical-fallback`，不把未接入的 LSP 会话伪装成完整 LSP。
- OpenAI Secure Tunnel：主实例可在 Cloudflare / OpenAI / Local 之间选择；OpenAI API Key 使用现有 Windows 加密 secrets，sidecar token 仅允许 loopback。
- Screen Vision：改为每 MCP 实例读取自己的 `config.json`，新增无截图的 `screen_capture_probe` 兼容性探针。后台窗口仅尝试 PrintWindow / WindowDC 等不改 Z-order 的 HWND 自有路径；最小化窗口直接 fail closed，不再临时恢复。Windows Graphics Capture 原生后端暂不在未完成实机矩阵前启用。
- 文档状态同步：稳定版仍为 0.12.34，0.13.0-beta.6 是独立 prerelease。

## 已知 Preview 边界

- Windows Graphics Capture 原生后端仍需要 VMware / Chromium / WebView2 / 多显示器实机验证；本版本不会宣称未验证能力已完成。
- Authenticode 需要实际代码签名证书；本测试版可能触发 Windows SmartScreen。
- OpenAI Secure Tunnel Preview 使用用户提供的官方 `tunnel-client.exe`，当前不捆绑第三方二进制。
- macOS / Linux 不属于 0.13 范围。

## Beta 6：命令终止幂等与 Connector Probe 兼容入口

- 修复 Windows `kill_session` 的双重终止竞态：手动终止只触发一次 CommandContext 取消，由既有 `cmd.Cancel` 负责终止进程树，并可等待会话收敛后返回 `completed=true`。
- Windows `terminateCommand` 对已结束进程幂等；`taskkill` 非零退出时不再把本地化 stdout/stderr 拼进 MCP 错误，fallback `Process.Kill` 的 `os.ErrProcessDone` 被视为成功，从而消除乱码与虚假的 exit 128 终止错误。
- `check_exec_environment` 现在直接携带 `screenCaptureProbe` 无像素策略 payload。即使第三方 Connector 继续缓存旧 tools/list、看不到独立 `screen_capture_probe` Schema，也能通过稳定旧工具读取 fail-closed 策略、后端方法、是否广告 probe 以及兼容入口标识。
- MCP Core catalog identity 升级到 `mcp-devdesk-go-core-v013-catalog3`；最终 built-core smoke 除三种 Screen Vision 模式目录契约外，还会启动真实长命令并通过 JSON-RPC `kill_session` 验证干净终止。

## Beta 5：后台无黑框、UIA UTF-8 与 Connector 目录代际修复

- Windows 后台子进程统一补齐 `CREATE_NO_WINDOW`：覆盖 MCP Core/Cloudflare/OpenAI tunnel 进程、端口/进程探测、Task Git、Updater、自动重启和桌面启动检查；继续保留显式可见模式，不影响需要用户交互的流程。
- UI Automation PowerShell 5.1 stdout 固定为 UTF-8 no-BOM，并新增真实 Unicode JSON 回归，避免中文窗口标题和“最小化/关闭”等控件名出现 mojibake。
- MCP Core serverInfo 做一次性 catalog identity 升级为 `mcp-devdesk-go-core-v013-catalog2`，帮助会缓存旧 tools/list 的 Connector 建立新的工具目录代际。
- `check_exec_environment` 增加目录诊断兜底：公开 catalog generation、实际广告工具数、`screen_capture_probe` 是否被服务端广告、旧 `list_symbols` 是否仍在 tools/list；即使第三方 Connector 自身缓存异常也可以判定问题所在层。
- 正式 built-core smoke 继续要求 active 52 / window 52 / desktop 55，并额外断言新 server identity、probe 存在、`list_symbols` 不再广告以及 fallback 目录状态一致。

## Beta 4：生产运行时工具目录与 UIA 集合修复

- Beta 3 实机复测确认：Go Core 在 `New()` 后会先建立完整工具目录，但生产入口随后执行 `ConfigureScreenVision` 按 active / window / desktop 模式做最小权限裁剪；此前 CI 只覆盖前者，因此错误地把“55 个”当成所有运行模式的固定数量。
- `screen_capture_probe` 是不读取像素的策略/兼容性诊断工具，Beta 4 将它从模式裁剪中豁免：只要 Screen Vision 已启用且权限为 trusted/dangerous，active、指定窗口、desktop 三种模式都保留 probe。
- full profile + UI Automation 下的生产目录契约现在按模式验证：active 52 个、已锁定 window 52 个、desktop 55 个；指定窗口但尚未选择目标时为 51 个。截图能力本身仍严格按模式最小授权，不为凑数量放宽。
- 新增真正的产品二进制验收：Windows 发布流程在生成 `dist/mcp-core-amd64.exe` 后，会启动该 EXE 并分别完成 MCP initialize / tools/list / server_info 的 active、window、desktop 三模式契约检查，避免再次出现“库测试通过但最终运行时目录不同”的盲区。
- 修复 Windows PowerShell 5.1 UI Automation 最终 JSON 组装：`System.Collections.Generic.List[object]` 先显式 `ToArray()`，再写入节点 payload，避免真实 WebView2/DevDesk 窗口在最终集合转换处抛 `ArgumentException`。
- 新增不依赖交互桌面的 Windows PowerShell 5.1 Generic.List → JSON 回归测试，与生产 UIA 的最终数据形状一致。

## Beta 3：55 工具目录兼容与 UI Automation 修复

- Windows CI 诊断确认 Go Core 在 Beta 2 源码下会生成 56 个工具，但当前 ChatGPT Connector 实测只导入 55 个，且遗漏 `screen_capture_probe`。
- 为减少重复目录槽位，Beta 3 不再单独广告完全重复的 `list_symbols`；标准 `document_symbols` 保留，旧客户端直接调用 `list_symbols` 仍兼容。后续 Beta 4 实机复测确认，生产运行时还会按 Screen Vision 模式继续做最小权限目录裁剪，因此 55 只对应 desktop 模式而不是所有模式。
- Beta 3 新增了构造阶段目录契约测试；Beta 4 将其补强为生产二进制、模式感知的端到端目录契约测试。
- 修复 `ui_automation_tree` 在部分 Windows UIA Provider 将 `FrameworkId` 返回为 `System.Int32` 时触发 `InvalidCastIConvertible` 的问题；UIA 标量和边界值现在先安全规范化，再生成 JSON。
- PowerShell UIA 子进程改为 stdout/stderr 分离：结构化 JSON 只从 stdout 解码，首次模块加载产生的 CLIXML/progress 诊断不会再污染成功结果。
- 新增 Windows 非交互回归测试，直接用 Int32/Double 验证同一套 PowerShell 安全转换函数。
- Beta 3 修复已通过定向 MCP 测试、全量 Go 测试、版本/文档一致性检查后写回预览分支；正式发布仍需通过完整 Windows 构建、NSIS 与发布流水线。

## Beta 2：工具目录自动刷新

- 修复从 0.12.x 原地升级到 0.13 后 ChatGPT Connector 继续缓存旧 38 个工具的问题。
- MCP initialize 现在声明 `tools.listChanged=true`，并在客户端 `notifications/initialized` 后发送标准 `notifications/tools/list_changed`。
- Streamable HTTP GET/SSE 现在会实时投递会话期间新增的服务端事件，而不只是连接建立瞬间的历史事件。
- 回归测试明确验证 0.13 新工具组存在：代码导航、Agent Task、持久 Job、`checks_run` / `validate_project` 与 `screen_capture_probe`。
- 修正 Screen Vision 工具描述，使 Schema 与 Beta 的非侵入式 / fail-closed 捕获策略一致。
- 升级验证建议：安装 Beta 2 后重启对应 MCP 实例并重新连接 Connector；客户端应自动重新执行 `tools/list`，无需删除并重新创建连接。
