# 开发路线图

## M0：仓库和文档

- [x] 初始化 Git
- [x] 提交原始便携版基线
- [x] 创建 `legacy-portable-v0.1.7` 标签
- [x] 创建 `develop` 分支
- [x] 编写开发、架构、安全和 Cloudflare 文档

## M1：可视化管理 MVP

- [x] Go 项目骨架
- [x] 配置读取和保存
- [x] 仪表盘状态 API
- [x] MCP 服务启停
- [x] Tunnel 服务启停
- [x] Cloudflare 登录
- [x] Tunnel 和 DNS 自动配置
- [x] 日志查看
- [x] 漂亮的响应式管理界面
- [x] Windows EXE 构建脚本
- [x] 单元测试

## M2：桌面程序

- [x] Windows GUI 子系统，无 CMD 窗口
- [x] Edge App 模式独立桌面窗口
- [x] 系统托盘
- [x] 关闭窗口后继续在托盘运行
- [x] 开机启动
- [x] Edge 安装路径检测
- [x] 单实例保护
- [x] 桌面状态与控制 API
- [ ] 自定义 EXE 和托盘图标
- [ ] 可选 Wails/WebView2 内嵌壳
- [ ] NSIS 安装包

## M2.1：端口与 Tunnel 运行管理

- [x] MCP 端口在线修改
- [x] Cloudflare 自动跟随 MCP 端口
- [x] 新端口占用检测
- [x] cloudflared 全局进程枚举
- [x] Tunnel UUID、名称和本地目标解析
- [x] 重复 Tunnel 进程检测
- [x] 按 PID 关闭单个 Tunnel
- [x] 一键清理旧连接并同步端口
- [x] Watchdog 防止重复启动

## M3：多项目开发能力

- [ ] 允许根目录
- [ ] 项目列表
- [ ] 动态打开工作区
- [ ] Git Worktree
- [ ] Skills 和 AGENTS.md
- [ ] 文件 Diff 卡片

## M4：Go MCP 核心

- [ ] MCP Streamable HTTP
- [ ] OAuth 2.1 + PKCE
- [ ] 文件工具
- [ ] 命令会话
- [ ] Git 工具
- [ ] 权限申请
- [ ] 审计日志

## M5：发布

- [x] ZIP 便携版
- [ ] 安装版
- [ ] 自动更新
- [ ] 签名
- [ ] 中文与英文界面

