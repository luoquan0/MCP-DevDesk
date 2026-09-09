# Screen Vision（实验性）

Screen Vision 是 MCP DevDesk 的 Windows 屏幕视觉实验功能。它让已授权的 MCP 客户端在需要时读取当前电脑上的窗口或桌面截图，用于分析 UI、错误提示、网页状态和开发工具画面。

## 设计边界

- 当前仅支持 Windows，并且仅由 Go MCP Core 提供。
- 默认关闭；只有用户在“设置 → 权限与安全 → 屏幕视觉（测试）”中显式开启后才会暴露视觉工具。
- Screen Vision 是整台 MCP DevDesk 的统一隐私策略，不按项目/MCP 实例分别放宽。主实例和附加 Go MCP 实例都会读取主配置中的同一套总开关、模式和指定窗口锁定信息。
- 设置发生变化时，当前正在运行的 Go MCP 实例会重新加载同一套 Screen Vision 策略，避免 ChatGPT 实际连接到另一个实例时仍保留旧的“当前窗口/整个桌面”权限。
- Go Core 无法读取统一 Screen Vision 配置时会关闭视觉工具（fail closed），不会回退成“当前窗口”模式。
- 需要 `trusted` 或 `dangerous` 权限模式；`safe` 模式不会暴露屏幕视觉工具。
- 没有后台录屏线程。客户端没有调用视觉工具时不会持续抓取屏幕。
- 每次截图仅在内存中生成、按需缩放并编码成 PNG，通过 MCP 响应返回；MCP DevDesk 不把截图历史保存到磁盘。
- 本实验版本不提供鼠标点击、键盘输入或自动控制电脑能力。
- Cloudflare/OAuth 远程连接一旦获得 MCP 授权，也可能调用这些视觉工具，因此只应在完全可信的客户端连接上启用。

## 三种模式的固定语义

三种模式互斥，并且语义不能互相回退：

- `指定窗口`：用户在 DevDesk 中手动选择一个目标窗口，例如 VMware、123 云盘或独立插件窗口。之后 AI 始终只能读取这个 HWND + PID 对应的窗口；即使浏览器一直在最前面、目标位于浏览器背后或已经最小化，也应读取指定窗口自身，而不是读取当前前台浏览器。最小化/隐藏目标会先尝试完全不改变窗口状态的后台捕获；只有这些路径全部失败时，才允许在 DWM cloak 保护下把同一个 HWND 无焦点恢复并移动到整个虚拟桌面以外的区域完成一次截图，随后恢复原状态。目标关闭或身份变化后不会自动改抓别的窗口，必须重新选择。
- `当前窗口`：每次 MCP 调用时读取当时的 Windows 前台窗口，也就是用户此刻肉眼正在看的内容。前台从 Edge 切到 DevDesk，AI 读取目标也随之变化。
- `整个桌面`：属于整机窗口浏览模式，而不只是“一张全屏截图”。AI 可以读取 Windows 虚拟桌面总览，也可以自行列出当前可读取的顶层应用窗口，并按需选择其中任意一个进行查看。因此浏览器在前台时，AI 仍可主动选择背后的 VMware、编辑器或其他应用单独读取。

模式、目标和总开关会立即写入主配置；正在运行的 Go MCP 实例会重新加载新策略。关闭后视觉工具从所有 Go MCP 实例的工具列表移除。

## MCP 工具

| 工具 | 用途 |
| --- | --- |
| `screen_list_windows` | 列出当前可读取的顶层应用窗口，包括已最小化及仍保留可恢复主窗体的托盘应用，可按标题或进程名筛选。 |
| `screen_get_active_window` | 读取当前前台窗口的标题、进程、尺寸等元数据，不截图。 |
| `screen_capture_window` | 按窗口 ID、精确标题或唯一标题片段截取指定窗口。 |
| `screen_capture_active_window` | 截取当前前台窗口。 |
| `screen_capture_desktop` | 截取 Windows 虚拟桌面（包括多显示器范围）。 |

`指定窗口`模式只允许 `screen_list_windows` 和已锁定目标的 `screen_capture_window`；`当前窗口`模式只允许读取/截图当前前台窗口；`整个桌面`模式允许以上全部五个工具，从而同时覆盖桌面总览与 AI 自主逐窗口读取。

截图工具默认把返回宽度限制在 1920 像素，客户端也可以在 320–4096 之间指定 `maxWidth`。如果 PNG 仍超过 MCP 图像大小限制，工具会要求客户端降低 `maxWidth`，避免无限增大内存和网络开销。

## Windows 捕获方式

### 指定窗口 / 整个桌面的逐窗口读取

如果目标本身已经在前台，可以直接使用目标窗口自身的捕获路径；如果目标位于其他窗口后面，则不能简单截取它在桌面上的矩形区域，因为那样会得到覆盖在它上面的 Edge/Chrome 等前台应用。

后台窗口现在采用“**无状态后台捕获优先，屏幕外恢复兜底**”的固定顺序。普通后台窗口不会再通过提升 Z-order、置顶或在当前可见桌面临时显露目标来截图：

1. 对锁定的 HWND 先执行 `DwmFlush`，随后按顺序尝试 `PrintWindow(PW_RENDERFULLCONTENT)`、经典 `PrintWindow` 和目标自己的 `GetWindowDC + BitBlt`。这些路径都不移动、不显示、不激活目标窗口。
2. 每次 `PrintWindow` 前都会清空目标位图，并检测近黑、近白和几乎纯色的可疑空白结果。Chromium、WebView2、WPF、VMware 等 GPU/合成器窗口即使 `PrintWindow` 返回成功，只要像素被判断为空白/无效，就不会把该结果当作真实画面。
3. 上述路径无效时自动切换到 `Windows.Graphics.Capture`（WGC）。WGC 使用 `IGraphicsCaptureItemInterop.CreateForWindow` + `Direct3D11CaptureFramePool.CreateFreeThreaded` 获取目标合成器表面，再通过 D3D11 staging texture 映射到 CPU 内存。它不会调用 `ShowWindow`、`SetForegroundWindow` 或改变目标 Z-order。
4. WGC 会尽力关闭光标捕获和系统捕获边框；Windows/系统策略如果不允许无边框捕获，不会因此扩大 Screen Vision 权限范围。
5. 只有当前真实前台窗口允许在所有 HWND/WGC 路径失败后使用桌面 `BitBlt`；后台目标绝不会拿前台应用覆盖后的桌面像素冒充自身内容。
6. 普通后台窗口的这些无状态路径全部失败时直接报错，不再执行旧的“临时置顶/可见 reveal”逻辑。

因此，Edge/Chrome 可以一直保持在用户面前；读取其后方的普通后台 VMware、编辑器或插件窗口时，MCP DevDesk 不会主动把目标抬到前台、改变 Z-order 或让目标窗口在当前桌面跳动。

### 最小化 / 隐藏到托盘的窗口

最小化和仍保留可恢复主 HWND 的托盘窗口也先走完全相同的无状态路径：`PrintWindow` / window DC → WGC。只要能直接拿到有效像素，就保持原最小化/隐藏状态完成截图，不执行任何恢复动作。

只有无状态后台捕获全部失败时，才进入一次性的 DWM-cloaked 屏幕外恢复兜底。该路径的核心约束是：**任何可能经过正常屏幕坐标的恢复动作发生前，目标 HWND 必须已经被 DWM cloak；只有确认目标已经完全位于虚拟桌面之外后才允许解除 cloak。**

1. 先快照原 `WINDOWPLACEMENT`、原前台 HWND、原 Z-order 相邻窗口、topmost 状态以及原隐藏/最小化状态；无法可靠取得 `WINDOWPLACEMENT` 时直接失败，不冒险恢复。
2. 根据整个 Windows virtual desktop 的边界计算一个与所有可见显示器完全不相交的屏幕外矩形。
3. 对**原始且同一个 HWND**先设置 `DWMWA_CLOAK`。如果系统不支持或设置失败，兜底直接失败，不继续执行任何会改变窗口状态的恢复操作。这里不能用 `SetWindowPlacement` 预先把 `rcNormalPosition` 写到屏幕外，因为 Windows 会自动把完全离屏的 placement 调整回可见显示器范围。
4. 保持 cloak 的情况下调用 `ShowWindowAsync(SW_SHOWNOACTIVATE)`，随后循环使用 `SetWindowPos(..., SWP_NOACTIVATE | SWP_NOZORDER | SWP_NOOWNERZORDER | SWP_NOSENDCHANGING)` 把同一个 HWND 固定到屏幕外。期间持续检查原前台焦点，并验证窗口矩形始终不与虚拟桌面相交。
5. 只有目标在屏幕外连续多次保持稳定、且仍然是最初快照的同一 HWND，才解除 `DWMWA_CLOAK` 让 GPU/Chromium/WPF 等应用在屏幕外恢复渲染。如果应用在唤醒过程中销毁或替换该 HWND，兜底会 fail closed，不追踪新 HWND，因为新窗口没有对应的完整 placement/Z-order 状态快照，无法保证无闪烁地恢复。
6. 目标只在屏幕外解除 cloak 后，仍使用 `PrintWindow` / window DC / WGC 对目标自身截图；绝不读取当前桌面上被其他应用覆盖后的矩形像素。
7. 截图结束后执行逆向清理：先重新对目标设置 `DWMWA_CLOAK`，在 cloak 保护下恢复原隐藏/最小化状态与原 `WINDOWPLACEMENT`，再恢复原 topmost/Z-order 和原前台焦点，最后只有在这些原始状态恢复后才解除 cloak。

这套兜底的目标是让“应用必须恢复渲染才能截图”的状态变化永远被 DWM cloak 或屏幕外位置隔离。MCP DevDesk 自身不会主动激活目标、提升 Z-order 或把正常窗口显示到当前桌面。第三方应用如果在收到恢复消息时自行创建/激活另一个窗口，属于应用自身行为；由于这种行为无法由目标 HWND 的 placement 快照完整回滚，当前实现会尽量 fail closed，并要求在测试版中把任何可见闪动、任务栏状态变化、焦点变化或新窗口跳出都视为缺陷反馈。

### 当前窗口

当前窗口模式始终跟随 `GetForegroundWindow`。它不尝试穿透前台窗口，也不会读取某个之前选中的后台应用。

### 整个桌面

桌面总览使用虚拟桌面 `BitBlt`，代表用户当前肉眼看到的多显示器合成画面。与此同时，“整个桌面”模式还允许 AI 使用 `screen_list_windows` + `screen_capture_window` 自己查看其他已打开的可读取应用，因此“整个桌面”不是只能看到最前面的窗口。

窗口枚举会保留仍有顶层窗体的已最小化应用，并额外尝试识别“隐藏到系统托盘但仍保留主 HWND”的应用；隐藏候选会排除有 Owner 的辅助窗体、`WS_EX_TOOLWINDOW` 和尺寸过小的内部窗口，不会把纯后台服务或无顶层窗体进程伪装成可截图目标。对于 Windows 最小化后常见的 158×26 一类图标矩形，DevDesk 会优先读取 `GetWindowPlacement` 的正常窗口尺寸。读取最小化/托盘目标时优先完全不恢复窗口；只有 PrintWindow/window DC/WGC 都不可用时才执行上述 DWM-cloaked 屏幕外恢复，并且只操作最初的 HWND。整个过程失败时直接报错，绝不改抓当前 Edge/Chrome。

Windows 自身的保护边界仍然生效。UAC 安全桌面、DRM/受保护内容、被系统禁止的 WGC 目标、已经销毁主 HWND 的托盘应用或彻底停止 GPU 渲染的进程仍可能返回黑屏、旧画面或无法捕获。这些情况不会通过降低 MCP 权限边界或把窗口强行显示到当前桌面来绕过。

## 测试步骤

1. 使用包含 Screen Vision 的测试版，并确保需要连接的实例使用 **Go MCP Core**。
2. 打开“设置 → 权限与安全”，选择“信任模式”或“危险模式”。
3. 开启“允许 AI 按需读取窗口画面”，确认隐私提示并保存；若存在正在运行的 Go MCP 实例，DevDesk 会让它们重新加载统一 Screen Vision 策略。
4. 测试“指定窗口”：先锁定 VMware/123 云盘等目标，再分别测试“被 Edge/Chrome 覆盖”“最小化到任务栏”“隐藏到托盘”三种状态。客户端调用 `screen_capture_window` 时必须返回锁定目标自身内容或明确错误，绝不能返回前台浏览器画面；整个截图期间浏览器应保持原焦点，目标不得在当前可见桌面出现、闪烁、置顶或跳动，完成后最小化/隐藏状态和原窗口位置应保持不变。
5. 对 Chromium/WebView2/WPF/VMware 等 GPU 应用重点验证：当 `PrintWindow` 返回黑/白/纯色空白时，`captureMethod` 应自动转到 `windows-graphics-capture`（或 DWM-cloaked 屏幕外恢复后的 WGC），而不是先把应用显示到当前桌面。
6. 对必须恢复渲染的最小化/托盘目标，重点确认恢复期间原目标 HWND 被 cloak、真正解除 cloak 时已经完全位于所有显示器之外；如果应用替换 HWND，应明确失败而不是让新 HWND 在桌面上跳出。
7. 若有多个 MCP 实例，刻意让 ChatGPT 连接一个附加实例，再重复指定窗口测试，确认其权限与主设置完全一致。
8. 测试“整个桌面”：让客户端先 `screen_list_windows`，随后自行选择 VMware、浏览器、DevDesk 等不同窗口分别 `screen_capture_window`；同时 `screen_capture_desktop` 仍应返回用户当前肉眼看到的整块虚拟桌面。
9. 测试“当前窗口”：在 Edge 与其他程序之间来回切换，`screen_capture_active_window` 应始终跟随用户当前前台内容。
10. 测试完成后关闭 Screen Vision；视觉工具应从所有 Go MCP 实例的工具列表消失。

建议重点反馈：指定后台窗口能否在 Edge/Chrome 覆盖时仍正确读取；最小化/托盘目标是否优先无状态捕获；进入兜底时是否始终被 DWM cloak 或位于虚拟桌面外；是否发生任何闪动、抢焦点、窗口跳动、任务栏状态变化或 Z-order 变化；Chromium/WebView2/WPF/VMware 是否正确自动走 WGC；是否还出现 158×26 一类小辅助窗体；多实例连接下权限是否一致；DPI/多显示器下屏幕外矩形与截图尺寸是否正确；截图延迟、开启/关闭后的空闲资源占用，以及所使用的 Windows 版本和目标应用。

## 默认 AI 桌面视觉读取策略

当 Screen Vision 已启用且当前权限/模式允许相应工具时，MCP Core 会在初始化 instructions 和工具描述中向模型声明以下默认策略：

- 用户询问“某个软件界面现在显示什么”“后台软件内容是什么”时，应把它视为 GUI 内容问题，优先直接抓取窗口画面，而不是只检查进程、端口、服务或命令行状态。
- `desktop` 模式下，已命名/后台应用优先 `screen_list_windows` 定位目标，再用 `screen_capture_window` 读取；“当前窗口显示什么”优先 `screen_capture_active_window`。
- `window` 模式下，优先直接读取 DevDesk 已锁定的目标窗口；目标被浏览器遮挡、最小化或隐藏到托盘时，不要求用户先切前台。
- `active` 模式下，只针对 Windows 当前前台窗口使用 `screen_capture_active_window`，不越权枚举或读取其他后台窗口。
- 最小化/托盘目标先尝试完全无状态的 PrintWindow/window DC/WGC；只有这些路径实际失败后才允许在 `DWMWA_CLOAK` 保护下执行 `SW_SHOWNOACTIVATE` + `SWP_NOACTIVATE/SWP_NOZORDER` 的一次性屏幕外恢复，并在结束后恢复原 placement/Z-order/隐藏或最小化状态和原前台焦点。
- 只有实际 Screen Vision 捕获已经失败或明确报告目标不可读取后，模型才应请求用户手动把软件切到前台。
- 进程、端口、服务等元数据可以作为视觉结果的补充，但不能替代“界面现在显示什么”的像素证据。
- `screen_capture_desktop` 用于桌面总览；如果用户明确问某个后台软件的界面，应优先单独捕获该窗口，而不是仅依赖桌面总览。

这是一项工具选择默认策略，不会扩大 Screen Vision 的权限边界。实际可调用工具仍由 `active` / `window` / `desktop` 模式、用户显式开关和 permission mode 共同限制。