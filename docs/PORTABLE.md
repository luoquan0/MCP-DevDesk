# MCP DevDesk 便携运行与目录迁移

MCP DevDesk 的正式便携版应把程序文件和 `data/devdesk` 放在同一个运行根目录中。该目录可以位于桌面、`C:\MCP-DevDesk`、其他磁盘或可移动盘，只要当前 Windows 用户对目录具有读写权限即可。

推荐结构：

```text
MCP-DevDesk/
├── MCP-DevDesk.exe
├── mcp-core.exe
├── coding-tools-mcp.exe
├── cloudflared.exe
├── devdesk-updater.exe
├── devdeskctl.exe
└── data/
    └── devdesk/
        └── cloudflare/
            └── <Tunnel-UUID>.json
```

## 程序文件路径

以下三个随软件一起移动的程序路径在 `config.json` 中使用相对于运行根目录的路径保存：

```json
{
  "coreExecutable": "coding-tools-mcp.exe",
  "goCoreExecutable": "mcp-core.exe",
  "cloudflaredExecutable": "cloudflared.exe"
}
```

程序加载配置时会把这些相对路径解析为当前 MCP DevDesk 运行根目录下的绝对路径，因此移动整个文件夹后不需要手工修改配置。

如果旧版本已经把这些内置程序保存成旧安装目录的绝对路径，而旧路径已经不存在，新版本会根据内置程序文件名自动重新绑定到当前运行目录，并在下一次保存时迁移成相对路径。

用户明确配置到运行目录之外的自定义程序路径仍保留为绝对路径，不会被自动改写。

## 项目和授权目录

项目目录、Workspace 和 Allowed Roots 不属于 MCP DevDesk 自身文件，因此仍保存为实际绝对路径。例如：

```text
C:\Projects\my-app
D:\work\backend
```

移动 MCP DevDesk 软件目录不会移动、重写或删除这些项目目录。

## 用户数据

`data/devdesk` 属于用户运行数据，在线更新和目录迁移都必须保留，包括但不限于：

- `config.json`
- `projects.json`
- `instances.json`
- `instances/`
- `secrets.json`
- OAuth 刷新令牌
- `cloudflare/<Tunnel-UUID>.json`
- 外观设置与背景
- 更新设置

Cloudflare Tunnel 的单隧道 JSON 凭据现在统一保存在 `data/devdesk/cloudflare/`，因此复制整个 MCP DevDesk 目录到另一台电脑时，该 Tunnel 的运行凭据会一起迁移。旧版本位于 `%USERPROFILE%\.cloudflared\<Tunnel-UUID>.json` 的文件会在首次使用时自动复制到便携目录。

`%USERPROFILE%\.cloudflared\cert.pem` 仍是 Cloudflare 账号级 Origin Certificate，不属于便携运行数据，也不应放进发布包。新电脑仅运行已经配置好的 Tunnel 不依赖它；若需要在新电脑创建、删除 Tunnel 或修改 DNS，则应在该电脑重新执行 Cloudflare 授权。

不要把真实运行中的 `data/devdesk` 提交到 GitHub，也不要把 Tunnel JSON 或 `cert.pem` 放进源码仓库或 Release 固定文件中。

## 推荐迁移方式

1. 彻底退出 MCP DevDesk，并确认管理器和由它管理的服务已经停止。
2. 将整个运行目录移动到新位置，例如 `C:\MCP-DevDesk`。
3. 从新位置启动 `MCP-DevDesk.exe`。
4. 确认项目、实例、端口、Tunnel 凭据和外观仍然正常。
5. 如果需要在新电脑管理 Cloudflare 账号或 DNS，再单独执行一次 Cloudflare 登录授权。
6. 确认无误后再删除旧空目录。

新版本不应要求因为单纯移动 MCP DevDesk 运行目录而手工修改三个内置 EXE 路径或重新创建 Tunnel。
