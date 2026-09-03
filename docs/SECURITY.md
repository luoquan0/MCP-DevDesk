# 安全模型

## 1. 权限模式

### 安全模式

- 默认禁止联网命令。
- Go 核心完全拒绝文件写入、删除和命令执行。
- 文件读取仍受 `fileScope` 限制。
- 危险命令拒绝或申请确认。

### 信任模式

- 允许文件写入和直接可执行文件命令。
- 兼容旧调用时可显式使用 `cmd` 字符串，由平台 Shell 执行。
- 删除、覆盖目标和补丁删除必须显式 `confirm=true`。
- 是否联网继续由 `allowNetwork` 控制。

### 危险模式

- 关闭应用层命令门控。
- 允许联网、内联脚本、shell 展开和子进程。
- 仍受当前 Windows 用户、UAC、NTFS 和防病毒软件限制。
- 文件访问范围仍由独立的 `fileScope` 控制。

命令会话不是操作系统级强沙箱。即使文件工具限制在工作区，信任/危险模式下启动的外部程序仍拥有当前 Windows 用户的系统权限。需要强隔离时应使用 Windows Sandbox、虚拟机或受限账户。

### 双核心边界

MVP 阶段仍调用现有 `coding-tools-mcp.exe`：

- 直接文件工具始终以 `--workspace` 为根目录。
- `dangerous` 模式下的 Shell 命令可能访问工作区外的路径，因为命令门控已关闭。
- `roots` 和 `computer` 是新版 Go MCP 核心的目标模型；在兼容核心中不能对所有工具提供完全一致的多根目录隔离。

Go 核心会对直接文件工具实施同样的范围检查，并阻止通过 `..` 或符号链接越界。旧核心仍保留用于回退，其历史限制不变。两种核心下，“危险模式 + 仅工作区”都不能被视为操作系统级强沙箱。

### 屏幕视觉最小权限

- Screen Vision 仍要求用户显式开启，并且只在 Go MCP Core 的 `trusted` / `dangerous` 权限模式下可用。
- 用户必须在 `指定窗口`、`当前窗口`、`整个桌面` 三种互斥模式中选择一个；Go Core 只暴露当前模式需要的屏幕工具。
- `指定窗口` 由本机管理界面手动选择并以窗口 ID + 进程 ID 锁定。目标窗口关闭、句柄被其他进程复用或身份发生变化时，捕获会拒绝执行而不是自动切到别的窗口。
- 管理界面列出的窗口信息只包含可见顶层窗口的标题、进程、尺寸和窗口 ID，不包含像素内容；截图仍只在 MCP 工具被主动调用时生成且不写入历史文件。

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

### 手机 / 局域网网页控制

- 网页控制使用独立端口，不直接暴露内部 `17860` 监听器；登录后会复用桌面 UI 所使用的同一套管理路由，因此网页功能与桌面软件保持 1:1。
- 未开启“允许局域网访问”时只监听 `127.0.0.1`；开启后监听 IPv4 网卡，但请求源仍必须是 loopback 或 RFC1918 私有地址。
- 局域网请求的 Host 也必须是 localhost、loopback 或私有 IP；不接受公网 Host，降低 DNS rebinding 风险。
- 完整管理 API 只在网页认证通过后可访问；未登录状态仅开放静态登录页与认证接口。若用户主动关闭网页密码认证，则同一局域网中的设备可直接使用完整管理界面，因此不建议在共享网络中关闭认证。
- 可选密码认证的密码保存在 DPAPI 加密的 `secrets.json`；登录后使用 HttpOnly、SameSite=Strict Cookie，会话仅保存在内存并在进程重启、关闭网页控制、切换监听模式或修改密码后失效。
- 网页控制登录按客户端 IP 限制连续失败次数；短时间内多次错误会返回 `429 Too Many Requests` 并临时锁定该 IP，降低局域网暴力破解风险。
- 即使网页控制已经登录，OAuth owner password、client secret、Token signing secret 等原始凭据也只允许通过本机桌面管理接口读取、生成或修改；局域网页端不会暴露这些明文凭据。
- 修改请求要求同源 Origin，避免其他网页借用浏览器会话跨站触发项目修改或服务操作。
- 网页端“项目 → 浏览”和“MCP 实例 → 项目目录 → 浏览”共用独立目录枚举接口替代 Windows 原生文件夹弹窗；该接口只枚举目录，不返回文件内容，其能力范围等同于当前 Windows 用户可访问的目录。
- 桌面端和网页登录后都会订阅同一个只读状态事件流；事件只携带递增 revision，不包含项目内容、凭据或日志数据。任意一端成功修改共享状态后，另一端收到事件再通过原有受保护 API 拉取最新数据，实现实时双向同步。

## 4. 密钥存储

Windows 正式版使用当前用户 DPAPI 加密 `secrets.json`：

- OAuth owner password
- OAuth client secret
- Cloudflare API Token
- 其他长期凭证

旧版明文 `secrets.json` 会在首次成功读取后自动迁移为加密信封。动态注册的 OAuth 客户端在 Windows 下同样使用当前用户 DPAPI 加密存储，因此轮换 Token 签名密钥不会导致客户端数据无法解密。

Cloudflare 凭据分两级处理：

- 单 Tunnel 的 `<Tunnel-UUID>.json` 只具备运行对应 Tunnel 的能力，保存在便携数据目录 `data/devdesk/cloudflare/`，随用户主动复制整个 MCP DevDesk 目录迁移。旧版用户目录中的同名 JSON 会自动复制到该目录。
- `%USERPROFILE%\.cloudflared\cert.pem` 是 Cloudflare 账号级 Origin Certificate，继续留在 Windows 用户配置目录，不进入便携包、不提交 GitHub，也不随在线更新分发。需要创建、删除 Tunnel 或修改 DNS 时，目标电脑应重新完成 Cloudflare 授权。

`data/devdesk` 本身属于敏感运行数据，不得提交到源码仓库或作为公共 Release 内容固化。便携复制意味着拿到该目录的人可能获得其中 Tunnel 的运行能力，因此用户应像保护其他本地应用凭据一样保护整个便携目录。

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

Go 核心还会过滤命令子进程继承的常见敏感环境变量，包括密码、Token、API Key、私钥和授权头。命令参数、Shell 字符串、文件内容、补丁和图片数据在审计日志中只保存长度与 SHA-256 摘要。

## 6. OAuth 与远程访问

- 授权码流程必须使用 PKCE S256。
- 内置静态客户端允许基于 PKCE 的 public-client Token 交换；客户端如果主动提交 Client Secret，则仍必须与本机保存值一致。
- ChatGPT 为不同自定义 MCP 应用生成不同 `/connector/oauth/<id>` 回调路径；当用户已经登记过一个 ChatGPT 回调时，仅在同一 `https://chatgpt.com/connector/oauth/` 家族内允许新的生成式回调，其他已配置回调仍保持精确匹配。
- 授权服务器声明并接受 `offline_access`，继续为远程 MCP 会话签发可轮换 Refresh Token。
- OAuth Token 绑定到精确的 MCP `resource` 受众。
- 非 ChatGPT 回调以及不属于已登记 ChatGPT 回调家族的地址仍必须精确匹配；所有回调只允许 HTTPS 或本机回环 HTTP。
- 刷新令牌使用一次后立即轮换，旧令牌不能重复使用。
- MCP 未授权响应返回受保护资源元数据地址。
- 浏览器 Origin 只允许本机或已配置的公开服务源，降低 DNS rebinding 风险。
- 管理后台继续只监听回环地址，不经过 Cloudflare Tunnel。

## 7. Tunnel 进程控制

- 关闭进程接口仍受本机管理 API、Host 和 Origin 检查保护。
- 根据 PID 关闭前会重新枚举进程，并确认该 PID 当前确实属于 `cloudflared.exe`。
- “同步端口”只结束与当前配置具有相同 Tunnel UUID；缺少 UUID 时才退回同名称匹配。
- 其他 Cloudflare Tunnel 不会被批量终止。
- 进程命令行中的 `--token` 值在返回管理 API 前会被替换为 `***`。
- 修改端口采用新 MCP 先就绪、旧 Tunnel 后关闭的顺序，降低公网连接中断时间。
