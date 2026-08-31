# MCP DevDesk 发布与在线更新规范

本文档定义 MCP DevDesk 的版本号、GitHub Actions、GitHub Releases、Portable 更新包和客户端在线更新规则。发布流程以 `.github/workflows/release.yml` 为实现真源；若工作流发生变化，本文档必须同步更新。

## 1. 发布原则

- GitHub `main` 是正式源码真源。
- 普通代码、文档、CI 修改只提交源码，不自动发布新版本。
- 只有用户明确要求“发布新版本 / 发版 / Release”时，才允许触发正式 Release。
- 用户没有指定版本级别时，默认使用 patch 递增。
- 任何必需测试或构建失败时，不得创建正式 Release。
- 正式发布包必须来自 GitHub Actions 的 Windows runner，避免把某台本地电脑的偶然环境当作正式构建环境。

## 2. 版本号真源

程序版本定义在：

```text
app/internal/buildinfo/version.go
```

版本采用三段式语义版本：

```text
MAJOR.MINOR.PATCH
```

默认发布策略：

- `patch`：Bug 修复、小功能、UI 调整，例如 `0.12.9 -> 0.12.10`
- `minor`：明显新增能力但保持主要兼容，例如 `0.12.9 -> 0.13.0`
- `major`：存在明确不兼容变化，例如 `0.12.9 -> 1.0.0`

不要为了日常发版手工修改版本号。正常发布由 GitHub Actions 自动计算、写回并提交。

## 3. Release 工作流触发方式

工作流文件：

```text
.github/workflows/release.yml
```

支持三种入口。

### 3.1 推荐：明确发版提交

当用户明确要求发布时，可以向 `main` 提交包含：

```text
[release]
```

的提交信息，例如：

```text
chore: publish next patch release [release]
```

未指定 bump 类型时，工作流默认按 patch 处理。普通提交禁止带 `[release]`。

### 3.2 GitHub Actions 手动触发

工作流支持 `workflow_dispatch`，可以选择：

- `patch`
- `minor`
- `major`

适合在 GitHub 网页上手动点击 “Run workflow” 发版。

### 3.3 显式 Tag 发布

推送 `v*` Tag 也会触发工作流，例如 `v0.12.10`。Tag 模式主要用于明确指定版本或恢复场景。Tag 中的版本必须与该提交的 `app/internal/buildinfo/version.go` 一致，否则工作流应失败，避免 Tag 和程序实际版本不一致。

## 4. 自动版本递增逻辑

对于 `main` 上的 `[release]` 或 `workflow_dispatch`：

1. 工作流读取触发提交中的当前版本。
2. 根据 `patch / minor / major` 计算下一版本。
3. 再次同步 `origin/main` 和 Tags，避免并发发版或旧提交覆盖新版本。
4. 如果目标 Tag 已存在，则复用已经存在的发布提交，避免重复创建版本。
5. 如果目标版本号提交已经由先前发版任务创建，则复用该提交。
6. 否则自动修改 `app/internal/buildinfo/version.go` 和 README 中的当前版本展示。
7. 自动提交 `chore: bump version to vX.Y.Z [skip release]`。
8. 版本提交推送回 `main`。
9. 后续构建、Tag 和 Release 均以该版本提交为准。

`[skip release]` 是自动化保留标记，不应手工滥用。

如果发版开始后 `main` 已经移动到另一个版本，工作流应中止并要求从最新 `main` 重新开始，避免发布旧代码。

## 5. GitHub Actions 发布顺序

正式工作流按以下顺序执行：

1. Checkout 完整 Git 历史和 Tags。
2. 计算或校验 Release 版本。
3. 安装 Go 环境。
4. 安装 Node.js 环境。
5. `npm ci` 安装前端依赖。
6. 执行公开发布敏感信息检查。
7. 执行完整 Windows 构建和测试：

```powershell
.\build.ps1 -Arch amd64 -RunTests
```

8. 为所有发布资产生成 SHA256。
9. 将 `vX.Y.Z` Tag 指向实际构建提交。
10. 创建 GitHub Release；若该 Release 已存在，则使用新构建资产覆盖对应文件并保持其为最新版本。

任何前置步骤失败，都不得继续产生不完整的正式 Release。

## 6. 正式发布资产

每个 Windows amd64 正式 Release 至少应包含：

```text
MCP-DevDesk-amd64.exe
MCP-DevDesk-amd64.exe.sha256
MCP-DevDesk-Portable-amd64.zip
MCP-DevDesk-Portable-amd64.zip.sha256
devdesk-updater-amd64.exe
devdesk-updater-amd64.exe.sha256
devdeskctl-amd64.exe
devdeskctl-amd64.exe.sha256
mcp-core-amd64.exe
mcp-core-amd64.exe.sha256
```

在线更新最关键的两个资产是 `MCP-DevDesk-Portable-amd64.zip` 和对应 `.sha256`。不要在没有同步修改客户端更新逻辑的情况下重命名它们。

## 7. 客户端在线更新流程

MCP DevDesk 的更新管理器通过 GitHub Releases 检查新版本：

1. 读取当前程序版本。
2. 请求配置仓库的 GitHub Releases。
3. 根据稳定版 / 预发布通道选择可用版本。
4. 使用语义版本比较判断是否存在更新。
5. 下载对应 `.sha256` 并解析预期校验值。
6. 下载 `MCP-DevDesk-Portable-amd64.zip`。
7. 对 ZIP 计算 SHA256 并与 Release 校验值比较。
8. 启动独立 `devdesk-updater.exe`。
9. 主程序正常退出。
10. 更新器等待并停止需要退出的 MCP / Tunnel / 独立实例进程。
11. 替换程序文件并保留用户数据。
12. 重新启动新版本。
13. 如果替换失败，恢复备份并重新启动旧版本。

网络超时策略必须区分“小请求”和“大文件”：

- GitHub Release 元数据和 SHA256 请求使用短超时，当前单次最长 45 秒。
- Portable ZIP 不再使用 `http.Client` 的 45 秒全局硬超时；下载主体可以持续流式接收，最终由安装请求自己的整体 deadline 约束。
- ZIP 和 SHA256 遇到临时网络错误、HTTP 408、429 或 5xx 时最多自动尝试 3 次；永久性的 4xx 不盲目重试。
- ZIP 下载中断后保留受大小上限保护的 `.tmp` 部分文件；后续重试通过 HTTP `Range` 从已有字节继续下载。若服务端不支持 Range，则安全地从头重新下载；若旧断点收到 HTTP 416，则删除该断点后自动从 0 重试。
- 无论是否发生续传，最终 SHA256 校验始终是进入安装阶段前的完整性准入条件；校验失败必须删除临时包，不得安装。

下载状态属于运行时信息，不写入 `update-settings.json`。`GET /api/update/settings` 会在原有设置之外附带只读 `progress`，包括阶段、已下载字节、总字节、百分比、当前尝试次数和状态说明。前端仅在“立即更新”请求执行期间每 500 ms 轮询该状态，并显示一个轻量玻璃进度卡；有 Content-Length / Content-Range 时显示 `已下载 / 总量 / 百分比`，未知总量时显示不确定进度，发生自动重试时显示当前第几次尝试。下载结束后停止轮询，不增加常驻后台请求。

## 8. 更新时必须保留的数据

在线更新不得覆盖或删除：

- `data/devdesk`
- 用户项目目录
- DPAPI 加密凭证
- 项目配置
- 用户日志和运行状态数据，除非代码本身有明确的数据迁移逻辑

Portable ZIP 是程序文件来源，不应把用户运行目录当作发布内容打包覆盖。

## 9. GitHub 仓库公开性

当前更新逻辑使用 GitHub Releases 的公开 API 和匿名资产下载。

如果未来把仓库改为 private：

- 不能假设现有客户端仍可以匿名检查或下载 Release。
- 必须先设计 GitHub Token、代理下载服务或其他安全认证机制。
- 禁止把长期 GitHub Token 硬编码在客户端或仓库源码中。
- 在私有仓库更新链路完成并验证前，不应直接切换仓库可见性后继续依赖现有自动更新。

## 10. 发版前检查清单

触发正式 Release 前至少确认：

1. 用户已经明确要求发布。
2. GitHub `main` 包含所有准备发布的代码和文档。
3. 没有未提交的关键修复只存在于某台本地电脑。
4. 版本级别选择正确。
5. `tools/check-public-release.ps1` 没有被绕过。
6. Go / 前端 / Portable / smoke test 均应由工作流执行。
7. 重要 Windows 专属行为已经实机验证，或明确记录仍需实机验证。

## 11. 发版后验证清单

GitHub Actions 完成后检查：

1. 工作流结论为 `success`。
2. GitHub Releases 出现新的 `vX.Y.Z`。
3. Release Tag 指向预期版本提交。
4. 所有必需资产和 `.sha256` 都存在。
5. Release 不是 draft，也不是意外的 prerelease。
6. 旧版本 MCP DevDesk 的“检查更新”能识别新版本。
7. 下载和 SHA256 校验成功。
8. 更新器自身有修改时，必须进行 Windows 的“更新 -> 退出旧版 -> 替换 -> 启动新版”实际验证。

## 12. 失败与重试

- 构建或测试失败：先修复原因，不手工创建 Release 绕过 CI。
- GitHub Actions 短暂基础设施失败：可以重跑失败 Job / Workflow。
- 客户端下载 Release 资产时遇到临时断网、读取中断、HTTP 408、429 或 5xx：更新器会自动重试，ZIP 尽可能使用已下载部分续传；用户再次点击更新时也可以继续使用同一版本遗留的安全 `.tmp` 部分文件。
- 客户端收到永久性的 4xx、SHA256 不匹配或包超过大小上限：停止自动重试并显示错误；SHA256 不匹配或超大临时包必须清理，不能继续安装。
- Release 已创建但资产需要重新构建：现有工作流支持对同一 Tag 上传并覆盖资产；必须确认代码和 Tag 仍然对应同一版本。
- Tag、版本号或代码不一致：停止发布，先修正版本关系，再重新执行。
- 更新器修改导致更新失败：优先验证回滚路径，不把可能破坏用户安装的数据迁移直接推给所有用户。

## 13. GitHub 开发与本地开发的关系

日常修改可以直接以 GitHub 为工作中心：

```text
需求 -> 修改 GitHub 源码 -> 测试 / CI -> 明确发版 -> GitHub Actions 构建 -> Release -> 软件内更新
```

以下情况强烈建议使用本地 Windows 环境：

- WebView2 生命周期和窗口行为
- 托盘菜单和单实例
- Windows 进程树、退出、启动项
- DPAPI
- 本机文件选择器
- 真实 Cloudflare / MCP / OAuth 联调
- 更新器替换正在运行的程序

一旦重新使用本地开发目录，应先同步 GitHub `main`，不要基于长期落后的本地副本直接发布。