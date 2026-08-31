# MCP DevDesk 维护与修改规范

本文件适用于整个仓库。任何 AI Agent、自动化工具或开发者在修改 MCP DevDesk 前，都应先阅读并遵守本文件。若某个子目录以后增加更具体的 `AGENTS.md`，则该子目录内以更具体的规则为准。

## 1. 唯一源代码真源

- GitHub 仓库 `luoquan0/MCP-DevDesk` 的 `main` 分支是唯一权威源代码真源。
- 本地开发目录只是工作副本，不得用落后的本地历史覆盖 GitHub `main`。
- 开始本地修改前，应先获取远端最新状态并基于 `origin/main` 开始工作；较大功能可以使用 `feature/*` 分支。
- 禁止对 `main` 做无必要的 force push。只有发布流程可以按既定规则维护 Release Tag。
- 当本地状态与 GitHub 不一致时，优先保留 GitHub `main`，先同步、比较，再继续修改。

## 2. 修改前必须确认的文档

根据改动范围先阅读对应文档：

- 通用开发：`docs/DEVELOPMENT.md`
- 架构：`docs/ARCHITECTURE.md`
- UI / 视觉：`DESIGN.md`
- Windows 窗口、WebView2、托盘、单实例：`docs/DESKTOP.md`
- 安全、凭证、权限、网络边界：`docs/SECURITY.md`
- Cloudflare / Tunnel：`docs/CLOUDFLARE.md`
- 发布和在线更新：`docs/RELEASE.md`
- 路线规划：`docs/ROADMAP.md`

如果代码行为已经与文档不同，应在同一次修改中同步修正文档，不允许长期保留已知错误说明。

## 3. 代码修改原则

- 只修改完成当前任务所必需的范围，避免顺带重构无关模块。
- 优先修复根因，不通过单纯缩短超时、吞掉错误或增加重试掩盖问题。
- 保持现有配置、API 和数据格式兼容；确需破坏兼容时必须明确说明迁移方案。
- UI 修改必须遵守 `DESIGN.md`，保持桌面端与局域网页复用同一套 Vue 页面和交互逻辑。
- Windows 桌面相关修改要同时考虑 WebView2 生命周期、托盘、单实例、后台进程和退出顺序。
- MCP / OAuth / Cloudflare / 文件系统权限相关改动必须优先考虑最小权限、输入校验和安全边界。
- 不随意替换仓库里的二进制依赖；如果确实需要升级 `cloudflared.exe`、旧核心或其他二进制，要记录来源、版本和验证方式。
- 不提交 `dist/`、临时目录、测试数据、运行日志或本地个人配置，除非仓库规则明确要求跟踪对应文件。

## 4. 测试与构建要求

根据修改范围执行对应验证：

| 修改范围 | 最低验证要求 |
| --- | --- |
| Go 后端 / MCP Core | `cd app` 后执行 `go test -mod=vendor ./...` |
| Vue / TypeScript / 样式 | `cd frontend` 后执行 `npm run build`；依赖环境不可信时先 `npm ci` |
| 构建脚本 / 发布 / 跨模块修改 | 仓库根目录执行 `.\build.ps1 -Arch amd64 -RunTests` |
| Portable 打包逻辑 | 执行 `.\package-portable.ps1 -Arch amd64`，并确认包内 smoke test 通过 |
| Windows 托盘 / WebView2 / 退出 / 进程管理 | 自动测试通过后，仍应标记为需要 Windows 实机验证 |

发布前必须以 GitHub Actions 的 Windows 构建结果为最终准入条件。任何必需测试失败时，不得发布正式 Release。

## 5. 安全与敏感数据

严禁把以下内容提交到 GitHub：

- 密码、OAuth Token、Cloudflare 凭证、Cookie、私钥、访问密钥。
- `data/devdesk` 中的用户运行数据、DPAPI 加密数据或个人项目配置。
- 本机绝对路径、个人账号信息或仅适用于某台机器的秘密配置。
- 测试过程中临时生成的真实凭证。

发布前保留并执行仓库现有的公开发布安全检查。不得为了让 CI 通过而关闭、绕过或弱化安全检查。

## 6. 文档同步规则

出现下列变化时，必须同步更新对应文档：

- 架构或模块职责变化 → `docs/ARCHITECTURE.md`
- WebView2、托盘、窗口生命周期、单实例变化 → `docs/DESKTOP.md`
- 权限、认证、凭证、监听地址、网络安全变化 → `docs/SECURITY.md`
- Cloudflare / Tunnel 流程变化 → `docs/CLOUDFLARE.md`
- UI 视觉规范变化 → `DESIGN.md`
- 开发命令、目录结构、分支协作方式变化 → `docs/DEVELOPMENT.md`
- 版本号、GitHub Actions、Release、在线更新机制变化 → `docs/RELEASE.md`
- 面向用户的重要版本说明或文档入口变化 → `README.md`

## 7. Git 提交规范

优先使用以下前缀：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档
- `refactor:` 结构调整但不改变外部行为
- `test:` 测试
- `ci:` CI / GitHub Actions
- `chore:` 仓库或构建维护

特殊标记：

- `[release]` 仅用于用户已经明确要求“发布新版本”时触发正式发版。
- `[skip release]` 保留给发布自动化生成的版本号提交。
- 普通代码或文档提交禁止随意带 `[release]`。

## 8. 发布规则

- 没有用户明确的发版指令，不得创建新版本、Tag 或 Release。
- 用户只说“修改、修复、提交、推送”时，默认只更新源码，不发布。
- 用户明确说“发布新版本”且没有指定版本级别时，默认使用 patch 版本递增，例如 `0.12.9 -> 0.12.10`。
- minor / major 版本只有在用户明确要求或存在明确兼容性理由时才使用。
- 正常发版不需要手工修改 `app/internal/buildinfo/version.go`；由 `.github/workflows/release.yml` 自动计算并提交版本号。
- 发布流程和恢复方式详见 `docs/RELEASE.md`。

正式 Release 至少必须包含：

- `MCP-DevDesk-amd64.exe`
- `MCP-DevDesk-Portable-amd64.zip`
- `devdesk-updater-amd64.exe`
- `devdeskctl-amd64.exe`
- `mcp-core-amd64.exe`
- 上述发布资产对应的 `.sha256`

## 9. 在线更新兼容约束

- 不得随意重命名 `MCP-DevDesk-Portable-amd64.zip` 或对应 `.sha256`；若必须改名，要同步修改更新器和发布工作流。
- 更新流程不得覆盖用户的 `data/devdesk`、项目目录或个人运行数据。
- 更新器失败时必须保持可回滚到旧版本的能力。
- 当前更新源使用 GitHub Releases。若未来把仓库改为私有，必须先为更新器设计可靠的认证下载机制，不能假设匿名 GitHub API 仍可用。

## 10. 完成任务前检查

在回复“已完成”前确认：

1. 已基于最新 GitHub `main` 工作，或明确说明为何不能同步。
2. 修改范围与用户需求一致，没有无关大改。
3. 对应测试 / 构建已执行并记录结果。
4. 需要同步的文档已经同步。
5. `git status` / GitHub 变更中没有敏感信息、临时文件或意外产物。
6. 未经用户明确要求，没有触发 Release。
7. 若涉及 Windows 专属行为但无法实机验证，必须明确说明仍需实机验证。
