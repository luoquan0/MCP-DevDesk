# 安全模型

## 1. 权限模式

### 安全模式

- 默认禁止联网命令。
- 拒绝内联脚本和 shell 展开。
- 文件限制在工作区。
- 危险命令拒绝或申请确认。

### 信任模式

- 允许 npm、pip、Git 等联网开发命令。
- 允许内联脚本和常规 shell 展开。
- 文件默认仍限制在工作区。
- 系统级危险操作应请求确认。

### 危险模式

- 关闭应用层命令门控。
- 允许联网、内联脚本、shell 展开和子进程。
- 仍受当前 Windows 用户、UAC、NTFS 和防病毒软件限制。
- 文件访问范围仍由独立的 `fileScope` 控制。

### 兼容旧核心时的边界

MVP 阶段仍调用现有 `coding-tools-mcp.exe`：

- 直接文件工具始终以 `--workspace` 为根目录。
- `dangerous` 模式下的 Shell 命令可能访问工作区外的路径，因为命令门控已关闭。
- `roots` 和 `computer` 是新版 Go MCP 核心的目标模型；在兼容核心中不能对所有工具提供完全一致的多根目录隔离。

因此，在 Go MCP 核心完成前，“危险模式 + 仅工作区”不能被视为强沙箱。界面必须持续显示该风险。

## 2. 文件范围

支持三个级别：

- `workspace`：当前项目。
- `roots`：用户明确授权的多个根目录。
- `computer`：当前用户能访问的整台电脑。

选择 `computer` 时必须二次确认，并在界面持续显示红色警告。

## 3. 管理后台保护

- 只监听 `127.0.0.1`。
- 拒绝非本机 Host。
- 修改接口检查 Origin。
- Cloudflare ingress 不得转发管理端口。
- 不在 API 中返回完整敏感密钥，除非用户主动点击显示。

## 4. 密钥存储

MVP 暂时兼容旧版文件。正式版必须使用 Windows DPAPI 加密：

- OAuth owner password
- OAuth client secret
- Cloudflare API Token
- 其他长期凭证

Cloudflare Tunnel JSON 凭据继续由 `cloudflared` 放在用户配置目录，并限制文件 ACL。

## 5. 命令审计

正式版记录：

- 发起时间
- 工作区
- 命令摘要
- 权限模式
- 是否联网
- 退出码
- 执行时长

日志默认不记录敏感环境变量和完整 Token。

