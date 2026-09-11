# Agent Runtime：隔离任务、持久化 Job 与结构化验证

`0.13.0-beta.1` 开始，Go MCP Core 增加一层可恢复的 Agent Runtime。目标不是增加任意权限，而是把 AI 的写入工作放进可审核、可恢复、可拒绝的任务边界。

## 任务生命周期

写任务可以通过 `task_start` 创建。DevDesk 要求基础 Git 工作区处于干净状态，然后自动在 `data/devdesk/agent-runtime/worktrees/<task-id>` 创建独立 Git Worktree 和 `mcp-task/*` 分支。任务处于 `editing` 时，文件写入、命令和结构化检查都解析到该 Worktree，而基础工作区保持不变。

典型流程：

```text
task_start -> edit / exec / checks_run -> task_finish -> 本机 DevDesk 审核 -> 接受或拒绝
```

`task_finish` 只把任务切换到 `review`。MCP 客户端没有 `task_accept` / `task_reject` 工具，不能自行批准自己的修改。接受和拒绝只通过本机管理 API / 桌面 UI 暴露；局域网页控制入口显式阻止这两个动作。

接受时，DevDesk 再次要求基础工作区干净且 HEAD 仍与任务创建时一致，然后提交 Worktree 中的修改并使用 `git merge --ff-only` 合并回基础分支。条件不满足时 fail closed，不自动覆盖基础工作区。拒绝时移除隔离 Worktree 和任务分支，不改基础工作区。

## 持久化 Task ID / Job ID

任务状态保存在：

```text
data/devdesk/agent-runtime/tasks.json
```

命令和结构化检查的 Job 元数据保存在：

```text
data/devdesk/agent-runtime/jobs.json
```

状态文件原子替换写入，并限制保留数量和输出体积。MCP Core 重启后，未完成的进程不能跨进程继续执行，因此会被标记为 interrupted，但 Task ID、Job ID、工作目录、命令、有限输出和结束状态仍可通过 `task_list` / `task_get` / `job_list` / `job_get` 重新读取。

## 结构化验证

`checks_run` 只接受 `test`、`check`、`format` 三种类型。它从项目的标准构建文件自动选择安全的验证入口，例如 Go、npm、Cargo、Python；在 MCP DevDesk 仓库的 Windows 根目录会优先调用完整 `build.ps1 -Arch amd64 -RunTests`。

结构化检查仍遵守权限模式和当前任务状态，不会绕过 `safe`、网络或文件范围限制。每次检查都返回持久化 Job ID。

## 安全边界

- Task/Job 状态属于用户运行数据，不能提交到仓库，也不会被在线更新覆盖。
- AI 不能自行接受或拒绝任务。
- 基础分支发生变化时，接受任务失败，而不是尝试自动覆盖或强制合并。
- 任务隔离依赖 Git；非 Git 工作区不能启动隔离任务。
- 现有直接文件/命令工具保持兼容。需要强审核边界的工作应先进入 `task_start`。
