# Windows 桌面模式

## 1. 当前实现

MCP DevDesk `0.2.0-dev` 使用纯 Go 和 Windows 系统 API 实现桌面运行，不依赖 Wails、Electron、Node.js Runtime 或额外 DLL。

程序启动后包含三个部分：

1. 后台 Go 管理器与本地 HTTP API。
2. Windows 系统托盘图标。
3. Microsoft Edge App 模式独立窗口。

正式构建使用：

```text
-H=windowsgui
```

因此双击 EXE 不会弹出 CMD 窗口。

## 2. 独立窗口

检测到 Microsoft Edge 后执行等价命令：

```powershell
msedge.exe --app=http://127.0.0.1:17860 --start-maximized --no-first-run
```

该窗口没有普通浏览器地址栏和标签栏，但继续复用现有 HTML、CSS、JavaScript 管理界面。

如果没有找到 Edge，程序会调用 Windows 默认浏览器打开管理地址。

## 3. 系统托盘

托盘菜单包含：

- 打开 MCP DevDesk
- 启动全部服务
- 停止全部服务
- 重新启动服务
- 退出 MCP DevDesk

双击托盘图标也会打开独立窗口。关闭 Edge App 窗口只关闭界面，后台管理器和服务继续运行；只有选择托盘“退出”才结束管理器。

## 4. 单实例

程序通过 Windows 命名 Mutex：

```text
Local\MCPDevDesk.Manager
```

防止重复启动后台实例。第二次双击 EXE 时不会创建新管理器，只会打开现有管理地址。

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
- 可选 Wails/WebView2 内嵌窗口
- 自动更新与代码签名

