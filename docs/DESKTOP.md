# Windows 桌面模式

## 1. 当前实现

MCP DevDesk `0.4.0-dev` 使用 Go、Win32 和内嵌 WebView2 实现桌面运行，不依赖 Electron、Node.js Runtime，也不会启动外部 Edge 浏览器窗口。

程序启动后包含三个部分：

1. 后台 Go 管理器与本地 HTTP API。
2. Windows 系统托盘图标。
3. 由 `MCP-DevDesk.exe` 自身创建的 Windows 原生窗口。

正式构建使用：

```text
-H=windowsgui
```

因此双击 EXE 不会弹出 CMD 窗口。

## 2. Windows 原生窗口

程序通过 `go-webview2` 在自己的 Win32 窗口内创建 WebView2 控件，并加载本机管理地址：

```text
MCP-DevDesk-amd64.exe
└─ Win32 主窗口：MCP DevDesk
   └─ WebView2 控件：http://127.0.0.1:17860
```

窗口进程、标题栏、任务栏图标和生命周期都属于 MCP DevDesk，不属于 `msedge.exe`。WebView2 只作为内嵌渲染控件使用，因此没有浏览器地址栏、标签页或 Edge App 进程。

如果系统缺少 WebView2 Runtime，程序会返回明确错误，不会静默退回浏览器模式。

## 3. 系统托盘

托盘菜单包含：

- 打开 MCP DevDesk
- 启动全部服务
- 停止全部服务
- 重新启动服务
- 退出 MCP DevDesk

双击托盘图标也会打开原生窗口。关闭窗口只关闭界面，后台管理器和服务继续运行；只有选择托盘“退出”才结束管理器。

## 4. 单实例

程序通过 Windows 命名 Mutex：

```text
Local\MCPDevDesk.Manager
```

防止重复启动后台实例。第二次双击 EXE 时不会创建新管理器，而是通过本机管理 API 激活现有原生窗口，不会启动浏览器。

## 5. 开机启动

启用“Windows 登录时后台运行”后，程序写入当前用户注册表：

```text
HKCU\Software\Microsoft\Windows\CurrentVersion\Run
```

值名：

```text
MCPDevDesk
```

启动参数：

```text
"<当前 EXE 路径>" --background
```

`--background` 模式会启动托盘和本地管理服务，但不会主动弹出界面。该功能不需要管理员权限。

## 6. 桌面 API

```text
GET  /api/system/desktop
PUT  /api/system/startup
POST /api/ui/open
```

命令行控制：

```powershell
devdeskctl.exe desktop
devdeskctl.exe open
devdeskctl.exe startup-on
devdeskctl.exe startup-off
```

## 7. 后续工作

- 自定义应用和托盘图标
- Windows 通知气泡
- NSIS 安装器
- 自动更新与代码签名

## 8. CMD 黑框处理

管理器的状态轮询会调用 Windows 系统工具读取端口和进程信息。所有以下子进程都使用 `HideWindow` 启动：

- `netstat.exe`
- `wmic.exe`
- `tasklist.exe`
- `taskkill.exe`
- `reg.exe`
- PowerShell 的进程查询回退

因此后台每 3 秒刷新状态时不会再出现短暂 CMD 黑框。

