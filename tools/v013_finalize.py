from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def write(path: str, text: str) -> None:
    (ROOT / path).write_text(text, encoding="utf-8", newline="\n")


def replace_once(path: str, old: str, new: str) -> None:
    text = read(path)
    if old not in text:
        raise RuntimeError(f"missing marker in {path}: {old[:180]!r}")
    write(path, text.replace(old, new, 1))


# Link every persistent command/check Job back into its parent Task. This is what
# lets the task center recover command history, validation status and heartbeat.
replace_once(
    "app/internal/mcpcore/command_tools.go",
    '''\tjob.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)\n\treturn m.server.jobs.Upsert(job)\n}''',
    '''\tjob.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)\n\tif err := m.server.jobs.Upsert(job); err != nil {\n\t\treturn err\n\t}\n\tif m.server.tasks != nil {\n\t\treturn m.server.tasks.RecordJob(job)\n\t}\n\treturn nil\n}''',
)

# Add task_update so an agent can explicitly checkpoint its current/next step and
# failure reason. Task heartbeats also advance automatically when Jobs are persisted.
task_tools = read("app/internal/mcpcore/task_tools.go")
task_tools = task_tools.replace(
    '''type taskDiffArgs struct {\n\tTaskID      string `json:"taskId"`\n\tTaskIDSnake string `json:"task_id,omitempty"`\n\tMaxBytes    int    `json:"maxBytes,omitempty"`\n}\n''',
    '''type taskDiffArgs struct {\n\tTaskID      string `json:"taskId"`\n\tTaskIDSnake string `json:"task_id,omitempty"`\n\tMaxBytes    int    `json:"maxBytes,omitempty"`\n}\n\ntype taskUpdateArgs struct {\n\tTaskID        string `json:"taskId,omitempty"`\n\tTaskIDSnake   string `json:"task_id,omitempty"`\n\tCurrentStep   string `json:"currentStep,omitempty"`\n\tNextStep      string `json:"nextStep,omitempty"`\n\tFailureReason string `json:"failureReason,omitempty"`\n\tClearFailure  bool   `json:"clearFailure,omitempty"`\n}\n''',
    1,
)
task_tools = task_tools.replace(
    'Description: "Create a persistent Task ID and a clean isolated Git worktree. After this succeeds, normal file, Git, and command tools automatically operate inside the task worktree until the task is accepted or rejected locally.",',
    'Description: "Create a persistent Task ID and an isolated Git worktree from the current HEAD. The base workspace may already contain uncommitted edits; they stay untouched, and acceptance later fails closed only if local changes overlap files changed by the task.",',
    1,
)
task_tools = task_tools.replace(
    '''\t\t{\n\t\t\tName: "task_resume", Title: "Resume AI Task",''',
    '''\t\t{\n\t\t\tName: "task_update", Title: "Update AI Task Progress",\n\t\t\tDescription: "Persist the task current step, next step, heartbeat, and optional failure reason so work can be recovered after reconnect or restart.",\n\t\t\tInputSchema: map[string]any{"type": "object", "properties": map[string]any{\n\t\t\t\t"taskId":        map[string]any{"type": "string"},\n\t\t\t\t"task_id":       map[string]any{"type": "string"},\n\t\t\t\t"currentStep":   map[string]any{"type": "string", "maxLength": 512},\n\t\t\t\t"nextStep":      map[string]any{"type": "string", "maxLength": 512},\n\t\t\t\t"failureReason": map[string]any{"type": "string", "maxLength": 4000},\n\t\t\t\t"clearFailure":  map[string]any{"type": "boolean", "default": false},\n\t\t\t}, "additionalProperties": false},\n\t\t},\n\t\t{\n\t\t\tName: "task_resume", Title: "Resume AI Task",''',
    1,
)
task_tools = task_tools.replace(
    '''\tcase "task_get":\n\t\tvar args taskIDArgs\n\t\tif err := decodeToolArguments(arguments, &args); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tid := firstNonEmpty(args.TaskID, args.TaskIDSnake)\n\t\ttask, err := s.tasks.Get(id)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tactive, ok, _ := s.tasks.Active()\n\t\treturn taskResult(task, ok && active.ID == task.ID), nil\n\tcase "task_resume":''',
    '''\tcase "task_get":\n\t\tvar args taskIDArgs\n\t\tif err := decodeToolArguments(arguments, &args); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tid := firstNonEmpty(args.TaskID, args.TaskIDSnake)\n\t\ttask, err := s.tasks.Get(id)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tif refreshed, refreshErr := s.tasks.RefreshChangedFiles(task.ID); refreshErr == nil {\n\t\t\ttask = refreshed\n\t\t}\n\t\tactive, ok, _ := s.tasks.Active()\n\t\treturn taskResult(task, ok && active.ID == task.ID), nil\n\tcase "task_update":\n\t\tif err := s.requireWritePermission(false, true); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tvar args taskUpdateArgs\n\t\tif err := decodeToolArguments(arguments, &args); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\ttask, err := s.tasks.UpdateProgress(firstNonEmpty(args.TaskID, args.TaskIDSnake), args.CurrentStep, args.NextStep, args.FailureReason, args.ClearFailure)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tactive, ok, _ := s.tasks.Active()\n\t\treturn taskResult(task, ok && active.ID == task.ID), nil\n\tcase "task_resume":''',
    1,
)
task_tools = task_tools.replace(
    '''\t\ttask, err := s.tasks.Get(firstNonEmpty(args.TaskID, args.TaskIDSnake))\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tif _, err := os.Stat(task.WorktreePath); err != nil {''',
    '''\t\ttask, err := s.tasks.Get(firstNonEmpty(args.TaskID, args.TaskIDSnake))\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tif refreshed, refreshErr := s.tasks.RefreshChangedFiles(task.ID); refreshErr == nil {\n\t\t\ttask = refreshed\n\t\t}\n\t\tif _, err := os.Stat(task.WorktreePath); err != nil {''',
    1,
)
write("app/internal/mcpcore/task_tools.go", task_tools)

# Dispatch and permission profile accounting for the two new high-level tools.
file_tools = read("app/internal/mcpcore/file_tools.go")
file_tools = file_tools.replace(
    'case "task_start", "task_list", "task_get", "task_resume", "task_diff", "task_finish":',
    'case "task_start", "task_list", "task_get", "task_update", "task_resume", "task_diff", "task_finish":',
    1,
)
file_tools = file_tools.replace(
    'case "checks_run":\n\t\treturn s.executeCheckTool(name, arguments)',
    'case "checks_run", "validate_project":\n\t\treturn s.executeCheckTool(name, arguments)',
    1,
)
file_tools = file_tools.replace(
    '"task_start", "task_resume", "task_finish", "checks_run":',
    '"task_start", "task_update", "task_resume", "task_finish", "checks_run", "validate_project":',
    1,
)
write("app/internal/mcpcore/file_tools.go", file_tools)

# High-level validate_project: build a trusted, manifest-derived validation plan and
# run it as one persistent Job. No user-supplied shell fragment is concatenated.
check_tools = read("app/internal/mcpcore/check_tools.go")
check_tools = check_tools.replace(
    '''type checksRunArgs struct {\n\tType       string `json:"type"`\n\tPath       string `json:"path,omitempty"`\n\tWaitMillis int    `json:"waitMillis,omitempty"`\n}\n''',
    '''type checksRunArgs struct {\n\tType       string `json:"type"`\n\tPath       string `json:"path,omitempty"`\n\tWaitMillis int    `json:"waitMillis,omitempty"`\n}\n\ntype validateProjectArgs struct {\n\tPath       string `json:"path,omitempty"`\n\tWaitMillis int    `json:"waitMillis,omitempty"`\n}\n''',
    1,
)
check_tools = check_tools.replace(
    '''func checkTools() []Tool {\n\treturn []Tool{{\n\t\tName: "checks_run", Title: "Run Structured Project Check",\n\t\tDescription: "Auto-detect the project toolchain and run a structured test, check, or format verification without requiring the MCP client to assemble shell commands. The returned Job ID is persisted.",\n\t\tInputSchema: map[string]any{"type": "object", "properties": map[string]any{\n\t\t\t"type":       map[string]any{"type": "string", "enum": []string{"test", "check", "format"}},\n\t\t\t"path":       map[string]any{"type": "string", "default": "."},\n\t\t\t"waitMillis": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "default": 30000},\n\t\t}, "required": []string{"type"}, "additionalProperties": false},\n\t}}\n}\n''',
    '''func checkTools() []Tool {\n\treturn []Tool{\n\t\t{\n\t\t\tName: "checks_run", Title: "Run Structured Project Check",\n\t\t\tDescription: "Auto-detect the project toolchain and run a structured test, check, or format verification without requiring the MCP client to assemble shell commands. The returned Job ID is persisted.",\n\t\t\tInputSchema: map[string]any{"type": "object", "properties": map[string]any{\n\t\t\t\t"type":       map[string]any{"type": "string", "enum": []string{"test", "check", "format"}},\n\t\t\t\t"path":       map[string]any{"type": "string", "default": "."},\n\t\t\t\t"waitMillis": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "default": 30000},\n\t\t\t}, "required": []string{"type"}, "additionalProperties": false},\n\t\t},\n\t\t{\n\t\t\tName: "validate_project", Title: "Validate Project",\n\t\t\tDescription: "Auto-detect a safe project validation workflow from build.ps1, go.mod, package.json, Cargo.toml, .sln, or Python metadata, then run test/check/build phases as one persistent Job linked to the active Agent Task.",\n\t\t\tInputSchema: map[string]any{"type": "object", "properties": map[string]any{\n\t\t\t\t"path":       map[string]any{"type": "string", "default": "."},\n\t\t\t\t"waitMillis": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "default": 30000},\n\t\t\t}, "additionalProperties": false},\n\t\t},\n\t}\n}\n''',
    1,
)
check_tools = check_tools.replace(
    '''func (s *Server) executeCheckTool(name string, arguments map[string]any) (map[string]any, error) {\n\tif name != "checks_run" {\n\t\treturn nil, fmt.Errorf("unknown check tool: %s", name)\n\t}\n''',
    '''func (s *Server) executeCheckTool(name string, arguments map[string]any) (map[string]any, error) {\n\tif name == "validate_project" {\n\t\treturn s.executeValidateProject(arguments)\n\t}\n\tif name != "checks_run" {\n\t\treturn nil, fmt.Errorf("unknown check tool: %s", name)\n\t}\n''',
    1,
)
validation_code = r'''

func (s *Server) executeValidateProject(arguments map[string]any) (map[string]any, error) {
	if s.permissionMode == "safe" {
		return nil, errors.New("project validation requires trusted or dangerous permission mode")
	}
	if err := s.requireTaskEditable(); err != nil {
		return nil, err
	}
	var args validateProjectArgs
	if err := decodeToolArguments(arguments, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	_, cwd, display, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, errors.New("validation path must be an existing directory")
	}
	plan, err := detectValidationPlan(cwd)
	if err != nil {
		return nil, err
	}
	command, commandArgs := validationCommand(plan)
	wait := args.WaitMillis
	if wait == 0 {
		wait = 30000
	}
	result, err := s.commands.start(execCommandArgs{Command: command, Args: commandArgs, CWD: display, TimeoutSeconds: 1800, WaitMillis: wait, Kind: "validate_project"})
	if err != nil {
		return nil, err
	}
	steps := make([]map[string]any, 0, len(plan))
	for _, step := range plan {
		steps = append(steps, map[string]any{"runtime": step.Runtime, "command": step.Command, "args": step.Args, "label": step.Label})
	}
	result["validationPlan"] = steps
	result["validationPhases"] = len(steps)
	result["structured"] = true
	return result, nil
}

func detectValidationPlan(cwd string) ([]detectedCheck, error) {
	exists := func(name string) bool {
		info, err := os.Stat(filepath.Join(cwd, name))
		return err == nil && !info.IsDir()
	}
	if exists("build.ps1") && runtime.GOOS == "windows" {
		return []detectedCheck{{Runtime: "powershell", Command: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "build.ps1", "-Arch", "amd64", "-RunTests"}, Label: "full Windows build and regression tests"}}, nil
	}
	if exists("go.mod") {
		vendorArgs := []string{}
		if info, err := os.Stat(filepath.Join(cwd, "vendor")); err == nil && info.IsDir() {
			vendorArgs = []string{"-mod=vendor"}
		}
		withVendor := func(prefix string, suffix ...string) []string {
			args := []string{prefix}
			args = append(args, vendorArgs...)
			return append(args, suffix...)
		}
		return []detectedCheck{
			{Runtime: "go", Command: "go", Args: withVendor("test", "./..."), Label: "Go tests"},
			{Runtime: "go", Command: "go", Args: withVendor("vet", "./..."), Label: "Go vet"},
			{Runtime: "go", Command: "go", Args: withVendor("build", "./..."), Label: "Go build"},
		}, nil
	}
	if exists("package.json") {
		raw, err := os.ReadFile(filepath.Join(cwd, "package.json"))
		if err != nil {
			return nil, err
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return nil, errors.New("package.json is invalid")
		}
		steps := make([]detectedCheck, 0, 3)
		add := func(name, label string) {
			if strings.TrimSpace(pkg.Scripts[name]) != "" {
				steps = append(steps, detectedCheck{Runtime: "node", Command: "npm", Args: []string{"run", name}, Label: label})
			}
		}
		add("test", "npm test")
		for _, name := range []string{"check", "typecheck", "lint"} {
			if strings.TrimSpace(pkg.Scripts[name]) != "" {
				add(name, "npm run "+name)
				break
			}
		}
		add("build", "npm run build")
		if len(steps) == 0 {
			return nil, errors.New("package.json has no safe test/check/lint/build scripts")
		}
		return steps, nil
	}
	if exists("Cargo.toml") {
		return []detectedCheck{
			{Runtime: "rust", Command: "cargo", Args: []string{"test", "--all-targets"}, Label: "cargo test"},
			{Runtime: "rust", Command: "cargo", Args: []string{"check", "--all-targets"}, Label: "cargo check"},
			{Runtime: "rust", Command: "cargo", Args: []string{"build", "--all-targets"}, Label: "cargo build"},
		}, nil
	}
	entries, _ := os.ReadDir(cwd)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".sln") {
			continue
		}
		return []detectedCheck{
			{Runtime: "dotnet", Command: "dotnet", Args: []string{"build", entry.Name()}, Label: "dotnet build"},
			{Runtime: "dotnet", Command: "dotnet", Args: []string{"test", entry.Name(), "--no-build"}, Label: "dotnet test"},
		}, nil
	}
	if exists("pyproject.toml") || exists("pytest.ini") || exists("requirements.txt") {
		python := "python"
		if runtime.GOOS != "windows" {
			python = "python3"
		}
		return []detectedCheck{
			{Runtime: "python", Command: python, Args: []string{"-m", "pytest"}, Label: "pytest"},
			{Runtime: "python", Command: python, Args: []string{"-m", "compileall", "-q", "."}, Label: "Python compile check"},
		}, nil
	}
	return nil, fmt.Errorf("could not auto-detect a validation workflow in %s", cwd)
}

func validationCommand(plan []detectedCheck) (string, []string) {
	if len(plan) == 1 {
		return plan[0].Command, append([]string(nil), plan[0].Args...)
	}
	if runtime.GOOS == "windows" {
		parts := []string{"$ErrorActionPreference = 'Stop'"}
		for _, step := range plan {
			command := powershellQuote(step.Command)
			args := make([]string, 0, len(step.Args))
			for _, arg := range step.Args {
				args = append(args, powershellQuote(arg))
			}
			line := "& " + command
			if len(args) > 0 {
				line += " " + strings.Join(args, " ")
			}
			parts = append(parts, line+"; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }")
		}
		return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", strings.Join(parts, "; ")}
	}
	parts := make([]string, 0, len(plan))
	for _, step := range plan {
		values := []string{shellQuote(step.Command)}
		for _, arg := range step.Args {
			values = append(values, shellQuote(arg))
		}
		parts = append(parts, strings.Join(values, " "))
	}
	return "sh", []string{"-c", strings.Join(parts, " && ")}
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
'''
marker = "\nfunc detectStructuredCheck(cwd, checkType string) (detectedCheck, error) {"
if marker not in check_tools:
    raise RuntimeError("missing detectStructuredCheck marker")
check_tools = check_tools.replace(marker, validation_code + marker, 1)
write("app/internal/mcpcore/check_tools.go", check_tools)

# Treat validate_project exactly like a structured check for task-level status.
tasks = read("app/internal/agentstate/tasks.go")
tasks = tasks.replace(
    'if strings.HasPrefix(strings.ToLower(strings.TrimSpace(job.Kind)), "check:") {',
    'kind := strings.ToLower(strings.TrimSpace(job.Kind))\n\t\tif strings.HasPrefix(kind, "check:") || kind == "validate_project" {',
    1,
)
write("app/internal/agentstate/tasks.go", tasks)

# Desktop task list refreshes the active task's live changed-file list.
replace_once(
    "app/internal/application/agent_tasks.go",
    '''func (a *App) AgentTasks() (AgentTaskList, error) {\n\ttasks, activeID, err := a.agentTasks.List()''',
    '''func (a *App) AgentTasks() (AgentTaskList, error) {\n\tif active, ok, _ := a.agentTasks.Active(); ok {\n\t\t_, _ = a.agentTasks.RefreshChangedFiles(active.ID)\n\t}\n\ttasks, activeID, err := a.agentTasks.List()''',
)

# TypeScript contract for the richer Task v2 record.
api = read("frontend/src/types/api.ts")
api = api.replace(
    '''  resultCommit?: string;\n  createdAt: string;''',
    '''  resultCommit?: string;\n  baseDirtyFilesAtStart?: string[];\n  currentStep?: string;\n  nextStep?: string;\n  changedFiles?: string[];\n  jobIds?: string[];\n  checkJobIds?: string[];\n  lastJobId?: string;\n  lastValidationStatus?: string;\n  lastValidationAt?: string;\n  lastHeartbeatAt?: string;\n  failureReason?: string;\n  createdAt: string;''',
    1,
)
write("frontend/src/types/api.ts", api)

# Task Center shows recoverable progress, validation, changed files and conflict hints.
page = read("frontend/src/pages/AgentTasksPage.vue")
page = page.replace(
    'message: `将把 ${task.branch} 的修改以 fast-forward 方式应用到原项目。只有原项目仍停留在任务开始时的提交且工作区干净时才会成功。`,',
    'message: `将把 ${task.branch} 的修改以 fast-forward 方式应用到原项目。原项目 HEAD 必须仍是任务开始提交；未提交修改可以保留，但如果与任务改动文件重叠会拒绝应用。`,',
    1,
)
page = page.replace(
    '''        <p v-if="task.summary" class="task-summary">{{ task.summary }}</p>\n        <div class="task-meta">''',
    '''        <p v-if="task.summary" class="task-summary">{{ task.summary }}</p>\n        <div v-if="task.currentStep || task.nextStep" class="task-progress">\n          <div v-if="task.currentStep"><b>当前步骤</b><span>{{ task.currentStep }}</span></div>\n          <div v-if="task.nextStep"><b>下一步</b><span>{{ task.nextStep }}</span></div>\n        </div>\n        <div v-if="task.failureReason" class="task-failure"><AppIcon name="warning" :size="16" /><span>{{ task.failureReason }}</span></div>\n        <div class="task-meta">''',
    1,
)
page = page.replace(
    '''          <span><b>Base</b><code>{{ task.baseCommit.slice(0, 12) }}</code></span>\n          <span><b>更新</b>{{ new Date(task.updatedAt).toLocaleString('zh-CN') }}</span>\n        </div>''',
    '''          <span><b>Base</b><code>{{ task.baseCommit.slice(0, 12) }}</code></span>\n          <span><b>改动文件</b>{{ task.changedFiles?.length || 0 }} 个<span v-if="task.changedFiles?.length" class="task-files">{{ task.changedFiles.slice(0, 8).join(' · ') }}<template v-if="task.changedFiles.length > 8"> · …</template></span></span>\n          <span><b>Jobs</b>{{ task.jobIds?.length || 0 }} 个<span v-if="task.lastJobId"><code>{{ task.lastJobId }}</code></span></span>\n          <span><b>验证</b>{{ task.lastValidationStatus || '尚未运行 validate_project' }}</span>\n          <span v-if="task.baseDirtyFilesAtStart?.length"><b>启动时本地修改</b>{{ task.baseDirtyFilesAtStart.length }} 个文件（已隔离保留）</span>\n          <span><b>心跳</b>{{ new Date(task.lastHeartbeatAt || task.updatedAt).toLocaleString('zh-CN') }}</span>\n        </div>''',
    1,
)
page = page.replace(
    '''.task-summary { margin: 0; color: var(--text-secondary); white-space: pre-wrap; }\n.task-meta { display: grid; gap: 7px; color: var(--text-tertiary); font-size: 12px; }''',
    '''.task-summary { margin: 0; color: var(--text-secondary); white-space: pre-wrap; }\n.task-progress { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }\n.task-progress > div { display: grid; gap: 4px; padding: 10px 12px; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--surface-subtle); }\n.task-progress b { font-size: 11px; color: var(--text-tertiary); }\n.task-progress span { color: var(--text-secondary); font-size: 13px; }\n.task-failure { display: flex; gap: 8px; align-items: flex-start; padding: 10px 12px; border: 1px solid var(--border-subtle); border-radius: 12px; color: var(--danger); }\n.task-meta { display: grid; gap: 7px; color: var(--text-tertiary); font-size: 12px; }''',
    1,
)
page = page.replace(
    '''.task-meta b { color: var(--text-secondary); font-weight: 600; }''',
    '''.task-meta b { color: var(--text-secondary); font-weight: 600; }\n.task-files { overflow-wrap: anywhere; }''',
    1,
)
page = page.replace(
    '''@media (max-width: 700px) { .task-actions { flex-direction: column-reverse; align-items: stretch; } .task-meta span { grid-template-columns: 1fr; } }''',
    '''@media (max-width: 700px) { .task-actions { flex-direction: column-reverse; align-items: stretch; } .task-progress { grid-template-columns: 1fr; } .task-meta span { grid-template-columns: 1fr; } }''',
    1,
)
write("frontend/src/pages/AgentTasksPage.vue", page)

# Cross-platform tests for dirty-base isolation and high-level validation planning.
tasks_test = read("app/internal/agentstate/tasks_test.go")
pattern = re.compile(r'''func TestTaskStartRequiresCleanBase\(t \*testing\.T\) \{.*?\n\}\n\nfunc initTestRepository''', re.S)
replacement = r'''func TestTaskAllowsDirtyBaseAndRejectsOnlyOverlappingChanges(t *testing.T) {
	repo := initTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewTaskStore(filepath.Join(t.TempDir(), "state"))
	task, err := store.Start(repo, "dirty base", "")
	if err != nil {
		t.Fatalf("dirty base should be allowed: %v", err)
	}
	if len(task.BaseDirtyFilesAtStart) != 1 || task.BaseDirtyFilesAtStart[0] != "local.txt" {
		t.Fatalf("base dirty snapshot = %#v", task.BaseDirtyFilesAtStart)
	}
	if err := os.WriteFile(filepath.Join(task.WorktreePath, "task.txt"), []byte("task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(task.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(task.ID); err != nil {
		t.Fatalf("unrelated dirty file should survive accept: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "local.txt")); err != nil || strings.ReplaceAll(string(raw), "\r\n", "\n") != "local\n" {
		t.Fatalf("local dirty file changed: %q err=%v", raw, err)
	}

	repo2 := initTestRepository(t)
	store2 := NewTaskStore(filepath.Join(t.TempDir(), "state2"))
	task2, err := store2.Start(repo2, "overlap", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task2.WorktreePath, "hello.txt"), []byte("task change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Finish(task2.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo2, "hello.txt"), []byte("human change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.Accept(task2.ID); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap protection, got %v", err)
	}
	blocked, err := store2.Get(task2.ID)
	if err != nil || blocked.FailureReason == "" {
		t.Fatalf("failure reason was not persisted: %#v err=%v", blocked, err)
	}
}

func initTestRepository'''
tasks_test, count = pattern.subn(replacement, tasks_test, count=1)
if count != 1:
    raise RuntimeError("failed to replace clean-base task test")
write("app/internal/agentstate/tasks_test.go", tasks_test)

check_test = read("app/internal/mcpcore/check_tools_test.go")
if "TestDetectValidationPlan" not in check_test:
    check_test += r'''

func TestDetectValidationPlan(t *testing.T) {
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.test/validate\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(goDir, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := detectValidationPlan(goDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].Label != "Go tests" || plan[1].Label != "Go vet" || plan[2].Label != "Go build" {
		t.Fatalf("Go validation plan = %#v", plan)
	}

	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(`{"scripts":{"test":"vitest","lint":"eslint .","build":"vite build"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = detectValidationPlan(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].Args[1] != "test" || plan[1].Args[1] != "lint" || plan[2].Args[1] != "build" {
		t.Fatalf("Node validation plan = %#v", plan)
	}
}
'''
write("app/internal/mcpcore/check_tools_test.go", check_test)

# Default full tool profile gains task_update + validate_project.
server_test = read("app/internal/mcpcore/server_test.go")
server_test = server_test.replace("len(listResult.Result.Tools) != 47", "len(listResult.Result.Tools) != 49", 1)
server_test = server_test.replace("len(seen) != 47", "len(seen) != 49")
write("app/internal/mcpcore/server_test.go", server_test)

preview = read("docs/V013_PREVIEW.md")
preview = preview.replace(
    "- Agent Runtime：持久 Task ID / Job ID、隔离 Worktree、重启后状态恢复、后台命令与结构化检查。",
    "- Agent Runtime：Task v2 持久化当前步骤、下一步、心跳、改动文件、Job/验证结果和失败原因；Task ID / Job ID、隔离 Worktree 与后台命令在重启后可恢复。",
    1,
)
preview = preview.replace(
    "- 人工审核：AI 只能 `task_finish`，不能自行接受自己的修改；Accept / Reject 由本机 DevDesk UI 控制。",
    "- Worktree 安全：主工作区即使已有未提交修改也可启动任务；这些修改不会复制进 AI Worktree。验收时只要 HEAD 未前进且本地修改不与任务改动文件重叠即可应用，重叠时 fail closed。AI 只能 `task_finish`，Accept / Reject 仍只在本机 DevDesk UI。\n- 高层验证：新增 `validate_project`，自动识别 build.ps1、go.mod、package.json、Cargo.toml、.sln、pyproject/pytest/requirements，并把 test/check/build 作为持久 Job 关联回 Task。",
    1,
)
write("docs/V013_PREVIEW.md", preview)

roadmap = read("docs/ROADMAP.md")
roadmap = roadmap.replace("- [ ] NSIS 安装包", "- [x] NSIS 安装包", 1)
roadmap = roadmap.replace("- [ ] 安装版", "- [x] 安装版", 1)
roadmap = roadmap.replace(
    "- [x] 每任务独立 Git Worktree，人工 Accept / Reject，基础工作区变化时 fail closed",
    "- [x] 每任务独立 Git Worktree；主工作区允许未提交修改，Accept 仅在 HEAD 前进或文件级改动重叠时 fail closed",
    1,
)
roadmap = roadmap.replace(
    "- [x] 结构化 checks_run 自动测试 / 检查 / 格式化",
    "- [x] Task v2 持久步骤 / 下一步 / 心跳 / 改动文件 / Job / 验证状态 / 失败原因\n- [x] 结构化 checks_run + validate_project 自动测试 / 检查 / 构建 / 格式化",
    1,
)
write("docs/ROADMAP.md", roadmap)

print("v0.13 final task workflow migration applied")
