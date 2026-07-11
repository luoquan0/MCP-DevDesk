# 开发路线图

当前稳定开发版本：`0.7.2`。M4 Go MCP 核心已完成，旧核心仍作为可切换的兼容回退。

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
- [x] MCP DevDesk 自身的 Win32 原生窗口
- [x] WebView2 内嵌管理界面
- [x] 移除 Edge App 外部浏览器窗口
- [x] 消除状态轮询 CMD 闪烁
- [x] 系统托盘
- [x] 关闭窗口后继续在托盘运行
- [x] 开机启动
- [x] Edge 安装路径检测
- [x] 单实例保护
- [x] 桌面状态与控制 API
- [x] 自定义 EXE、窗口和托盘图标
- [x] WebView2 内嵌壳
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

## 阶段 5：界面与品牌完善

- [x] Vue 3 + TypeScript 管理界面
- [x] Apple 风格桌面布局与明暗主题
- [x] Windows 桌面尺寸适配，默认 1200 × 800 居中启动
- [x] 软件不再默认最大化
- [x] 使用 `logo/` 品牌源图生成界面、窗口、托盘和 EXE 图标
- [x] 前端产物自动嵌入 Go 程序
- [x] 完整前端生产构建
- [x] Go 单元测试
- [x] Windows amd64 GUI 与 CLI 构建
- [x] ZIP 便携版打包流程

## M3：多项目开发能力

- [x] 允许添加本地项目目录
- [x] 项目列表与本地持久化
- [x] 动态打开工作区与 MCP 热切换
- [x] 切换失败自动回滚旧项目
- [x] 切换目录时保持 Cloudflare Tunnel 地址
- [x] Git 仓库、分支与变更状态识别
- [x] Git Worktree 创建、列表与移除
- [x] Skills 和 AGENTS.md 检测
- [x] 文件 Diff 卡片

## M4：Go MCP 核心

- [x] 独立 Go MCP 核心命令与 Windows 构建产物
- [x] Streamable HTTP JSON-RPC、初始化与会话生命周期
- [x] SSE 响应、独立 GET 流、事件 ID 与断线恢复
- [x] OAuth 2.1 授权码流程、PKCE S256、资源绑定与刷新令牌轮换
- [x] 受保护资源元数据、授权服务器元数据与动态客户端注册
- [x] OAuth 凭据自定义、随机生成、显示、复制、静态回调地址与保存后重启
- [x] Windows DPAPI 密钥加密与旧明文数据自动迁移
- [x] 动态 OAuth 客户端加密持久化
- [x] 刷新令牌 DPAPI 加密持久化与核心重启续签
- [x] ChatGPT/DevSpace 根资源和 `/mcp` 资源兼容
- [x] 未知会话 404 恢复、协议版本协商与 202 通知响应
- [x] `workspace`、多授权根目录与整台电脑三档文件范围
- [x] 文件读取、递归列表、搜索、原子写入、补丁、移动与删除工具
- [x] 旧版多文件 `*** Begin Patch` 信封与失败回滚
- [x] 长时间命令会话、输出续读、标准输入与进程树终止
- [x] Git 状态、Diff、日志、Show、Blame 与 Worktree 工具
- [x] 权限模式、工具配置档位与禁止静默提权
- [x] JSONL 审计日志与敏感参数脱敏
- [x] 图片保存、查看与 MCP image content 返回
- [x] 旧核心 22 个工具名称及常用参数兼容
- [x] 管理界面中旧核心/Go 核心安全切换与自动重启
- [x] 服务页工作目录安全热切换与失败自动回滚
- [x] 编译产物和便携包内核心端到端自动验收
- [x] 旧核心保留为一键回退模式

## M5：发布

- [x] ZIP 便携版
- [ ] 安装版
- [ ] 自动更新
- [ ] 签名
- [ ] 中文与英文界面

