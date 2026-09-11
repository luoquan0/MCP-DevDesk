# OpenAI Secure Tunnel（Beta）

`0.13.0-beta.1` 为主 MCP 实例增加三种互斥连接方式：

```text
Cloudflare / OpenAI Secure Tunnel / Local
```

Cloudflare 保持 `0.12.34` 的固定域名流程；Local 只启动本机 MCP；OpenAI Secure Tunnel 使用 OpenAI 官方 `tunnel-client` 将 loopback MCP 私有连接到 ChatGPT，不要求把本机 MCP 端口暴露为公网入口。

## Beta 前置条件

本 Beta 不把第三方 `tunnel-client.exe` 固定进 MCP DevDesk 发布包。用户需要使用 OpenAI 官方发布的 Windows `tunnel-client`，并在“连接方式”页指定可执行文件路径。

需要：

- Tunnel ID，格式 `tunnel_` + 32 位小写十六进制字符；
- Tunnel Runtime API Key；长期 daemon 使用 Tunnels Read + Use 权限，不要使用 Admin key；
- Go MCP Core。

Runtime API Key 保存在 DevDesk 已有的 Windows 加密 secrets 中，不写入 argv。启动 `tunnel-client` 时只通过 `CONTROL_PLANE_API_KEY` 子进程环境传入。

## 本机 MCP 授权

OpenAI Tunnel 模式下，MCP Core 始终使用 loopback 地址，例如：

```text
http://127.0.0.1:8765/mcp
```

DevDesk 为本机 tunnel-client 自动生成一个随机 256-bit sidecar token，并把它保存在加密 secrets 中。启动 tunnel-client 时，DevDesk 把 token 写到私有运行文件，再使用官方支持的 `file:` header reference：

```text
--mcp.extra-headers "X-MCP-DevDesk-Tunnel-Token: file:<private-file>"
```

Go MCP Core 只有在请求来源地址确实是 loopback 且 header 使用常量时间比较匹配时，才允许这个本机 sidecar 凭证通过。远程客户端即使伪造同名 header，也不能获得该本机旁路。

因此在 ChatGPT 创建对应 Connector 时，选择 **Tunnel**，MCP 认证选择 **No authentication**；本机 MCP 凭证由 tunnel-client 在 Windows 上注入，不复制给 ChatGPT。

## 控制面代理

如果当前 Windows 主机访问 OpenAI control plane 需要 HTTP 代理，可以为 OpenAI Tunnel 单独配置 control-plane proxy。DevDesk 仅把该值传给：

```text
--control-plane.http-proxy <url>
```

它不改变项目命令的网络权限，也不会自动把浏览器或系统代理视为 MCP 命令权限。

## 生命周期

- 切换 Cloudflare / OpenAI / Local 前必须先停止主实例。
- OpenAI 模式由现有 Process Manager 和 Watchdog 管理 tunnel-client 生命周期。
- 修改 MCP 端口时，先停止当前 Tunnel/MCP，再以新 loopback 地址启动并保留失败回滚路径。
- Local 模式不启动任何 Tunnel 子进程。
- 附加多实例在本 Beta 中继续使用现有 Cloudflare 配置模型；三模式选择器先作用于主实例。

OpenAI `tunnel-client` 自身的 Tunnel 创建、权限和控制面行为以 OpenAI 官方文档与当前发布版本为准。
