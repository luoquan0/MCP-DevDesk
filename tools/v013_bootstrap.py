from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, content):
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8", newline="\n")


def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise RuntimeError(f"expected marker missing in {path}: {old[:100]!r}")
    text = text.replace(old, new, 1)
    write(path, text)


TASK_TOOLS = r'''package mcpcore

import (
    "bufio"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "strings"
    "time"
)

const (
    maxTaskGoalBytes = 16 * 1024
    maxTaskDiffBytes = 512 * 1024
    maxValidationOutputBytes = 256 * 1024
)

type agentTaskRecord struct {
    ID            string              `json:"id"`
    Goal          string              `json:"goal"`
    Status        string              `json:"status"`
    Phase         string              `json:"phase"`
    NextStep      string              `json:"nextStep,omitempty"`
    Workspace     string              `json:"workspace"`
    Worktree      string              `json:"worktree,omitempty"`
    BaseCommit    string              `json:"baseCommit,omitempty"`
    BaseBranch    string              `json:"baseBranch,omitempty"`
    ChangedFiles  []string            `json:"changedFiles,omitempty"`
    Commands      []taskCommandRecord `json:"commands,omitempty"`
    Validation    []validationResult  `json:"validation,omitempty"`
    Error         string              `json:"error,omitempty"`
    CreatedAt     time.Time           `json:"createdAt"`
    UpdatedAt     time.Time           `json:"updatedAt"`
    LastHeartbeat time.Time           `json:"lastHeartbeat"`
    AppliedAt     *time.Time          `json:"appliedAt,omitempty"`
    DiscardedAt   *time.Time          `json:"discardedAt,omitempty"`
}

type taskCommandRecord struct {
    Command    string    `json:"command"`
    StartedAt  time.Time `json:"startedAt"`
    FinishedAt time.Time `json:"finishedAt"`
    ExitCode   int       `json:"exitCode"`
}

type validationResult struct {
    Kind       string `json:"kind"`
    Command    string `json:"command"`
    ExitCode   int    `json:"exitCode"`
    DurationMS int64  `json:"durationMs"`
    Output     string `json:"output,omitempty"`
    Truncated  bool   `json:"truncated,omitempty"`
}

type agentTaskArgs struct {
    Action   string `json:"action"`
    ID       string `json:"id,omitempty"`
    Goal     string `json:"goal,omitempty"`
    Phase    string `json:"phase,omitempty"`
    NextStep string `json:"nextStep,omitempty"`
    Error    string `json:"error,omitempty"`
}

type validateProjectArgs struct {
    TaskID         string `json:"taskId,omitempty"`
    Run            bool   `json:"run,omitempty"`
    TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

type agentWorkflowArgs struct {
    Goal string `json:"goal"`
}

func taskTools() []Tool {
    return []Tool{
        {
            Name: "agent_task", Title: "Agent Task", Description: "Create and manage durable AI coding tasks backed by isolated Git worktrees. Tasks survive MCP/DevDesk restarts and support diff review, safe apply, discard, pause/resume and heartbeat updates.",
            InputSchema: map[string]any{"type":"object","properties":map[string]any{
                "action":map[string]any{"type":"string","enum":[]string{"create","get","list","heartbeat","update","diff","apply","discard","pause","resume","cancel","complete","fail"}},
                "id":map[string]any{"type":"string"}, "goal":map[string]any{"type":"string","maxLength":maxTaskGoalBytes},
                "phase":map[string]any{"type":"string","maxLength":200}, "nextStep":map[string]any{"type":"string","maxLength":1000}, "error":map[string]any{"type":"string","maxLength":4000},
            },"required":[]string{"action"},"additionalProperties":false},
        },
        {
            Name: "validate_project", Title: "Validate Project", Description: "Detect project-native test/lint/build commands and optionally execute them in the main workspace or an Agent Task worktree with bounded output and timeout.",
            InputSchema: map[string]any{"type":"object","properties":map[string]any{
                "taskId":map[string]any{"type":"string"}, "run":map[string]any{"type":"boolean","default":false}, "timeoutSeconds":map[string]any{"type":"integer","minimum":10,"maximum":600,"default":180},
            },"additionalProperties":false},
        },
        {
            Name: "agent_workflow", Title: "Start Agent Workflow", Description: "Start a durable coding workflow: create an isolated task worktree, return the execution contract, then use ordinary MCP tools inside the returned worktree before validation and review/apply.",
            InputSchema: map[string]any{"type":"object","properties":map[string]any{"goal":map[string]any{"type":"string","minLength":1,"maxLength":maxTaskGoalBytes}},"required":[]string{"goal"},"additionalProperties":false},
        },
    }
}

func (s *Server) executeTaskTool(name string, arguments map[string]any) (map[string]any, error) {
    switch name {
    case "agent_task":
        var args agentTaskArgs
        if err := decodeToolArguments(arguments, &args); err != nil { return nil, err }
        return s.agentTask(args)
    case "validate_project":
        var args validateProjectArgs
        if err := decodeToolArguments(arguments, &args); err != nil { return nil, err }
        return s.validateProject(args)
    case "agent_workflow":
        var args agentWorkflowArgs
        if err := decodeToolArguments(arguments, &args); err != nil { return nil, err }
        if strings.TrimSpace(args.Goal) == "" { return nil, errors.New("goal is required") }
        created, err := s.agentTask(agentTaskArgs{Action:"create", Goal:args.Goal})
        if err != nil { return nil, err }
        task, _ := created["task"].(agentTaskRecord)
        return map[string]any{
            "task": task,
            "workflow": []string{
                "Work only inside task.worktree for task-scoped code changes.",
                "Update agent_task heartbeat/phase during long work.",
                "Call validate_project with taskId and run=true after edits.",
                "Call agent_task action=diff for review evidence.",
                "Only call action=apply after the requested change is validated; apply refuses overlapping main-workspace edits.",
                "Call action=discard to abandon the isolated worktree.",
            },
        }, nil
    default:
        return nil, fmt.Errorf("unknown task tool: %s", name)
    }
}

func (s *Server) agentTask(args agentTaskArgs) (map[string]any, error) {
    action := strings.ToLower(strings.TrimSpace(args.Action))
    switch action {
    case "create":
        if s.permissionMode == "safe" || s.toolProfile != "full" { return nil, errors.New("creating Agent Tasks requires a writable full tool profile in trusted or dangerous mode") }
        return s.createAgentTask(args.Goal)
    case "list":
        tasks, err := s.listAgentTasks()
        return map[string]any{"tasks":tasks,"count":len(tasks)}, err
    }
    if strings.TrimSpace(args.ID) == "" { return nil, errors.New("id is required for this action") }
    task, path, err := s.loadAgentTask(args.ID)
    if err != nil { return nil, err }
    switch action {
    case "get":
        _ = s.refreshTaskChanges(&task)
    case "heartbeat", "update":
        if args.Phase != "" { task.Phase = args.Phase }
        if args.NextStep != "" { task.NextStep = args.NextStep }
        task.LastHeartbeat = time.Now().UTC(); task.UpdatedAt = task.LastHeartbeat
    case "pause":
        task.Status = "paused"; task.Phase = "paused"; task.UpdatedAt = time.Now().UTC()
    case "resume":
        task.Status = "running"; task.Phase = "working"; task.LastHeartbeat = time.Now().UTC(); task.UpdatedAt = task.LastHeartbeat
    case "cancel":
        task.Status = "cancelled"; task.Phase = "cancelled"; task.UpdatedAt = time.Now().UTC()
    case "complete":
        task.Status = "ready_for_review"; task.Phase = "review"; task.NextStep = "Review diff, then apply or discard."; task.UpdatedAt = time.Now().UTC(); _ = s.refreshTaskChanges(&task)
    case "fail":
        task.Status = "failed"; task.Phase = "failed"; task.Error = args.Error; task.UpdatedAt = time.Now().UTC()
    case "diff":
        _ = s.refreshTaskChanges(&task)
        diff, truncated, err := taskDiff(task)
        if err != nil { return nil, err }
        _ = saveTask(path, task)
        return map[string]any{"task":task,"diff":diff,"truncated":truncated}, nil
    case "apply":
        if s.permissionMode == "safe" || s.toolProfile != "full" { return nil, errors.New("applying Agent Task changes requires a writable full tool profile") }
        return s.applyAgentTask(task, path)
    case "discard":
        if s.permissionMode == "safe" || s.toolProfile != "full" { return nil, errors.New("discarding an Agent Task worktree requires a writable full tool profile") }
        return s.discardAgentTask(task, path)
    default:
        return nil, fmt.Errorf("unsupported agent_task action %q", args.Action)
    }
    if err := saveTask(path, task); err != nil { return nil, err }
    return map[string]any{"task":task}, nil
}

func (s *Server) createAgentTask(goal string) (map[string]any, error) {
    goal = strings.TrimSpace(goal)
    if goal == "" { return nil, errors.New("goal is required") }
    if len(goal) > maxTaskGoalBytes { return nil, fmt.Errorf("goal exceeds %d bytes", maxTaskGoalBytes) }
    workspace, err := s.workspaceRoot(); if err != nil { return nil, err }
    base, err := gitOutput(workspace, 20*time.Second, "rev-parse", "HEAD"); if err != nil { return nil, errors.New("Agent Tasks require a Git repository with at least one commit") }
    branch, _ := gitOutput(workspace, 10*time.Second, "branch", "--show-current")
    id, err := newTaskID(); if err != nil { return nil, err }
    if err := ensureDevDeskExcluded(workspace); err != nil { return nil, err }
    worktree := filepath.Join(workspace, ".devdesk", "worktrees", id)
    if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil { return nil, err }
    if _, err := gitOutput(workspace, 60*time.Second, "worktree", "add", "--detach", worktree, strings.TrimSpace(base)); err != nil { return nil, fmt.Errorf("create task worktree: %w", err) }
    now := time.Now().UTC()
    task := agentTaskRecord{ID:id, Goal:goal, Status:"running", Phase:"created", NextStep:"Inspect the task worktree and implement the requested change.", Workspace:workspace, Worktree:worktree, BaseCommit:strings.TrimSpace(base), BaseBranch:strings.TrimSpace(branch), CreatedAt:now, UpdatedAt:now, LastHeartbeat:now}
    statePath, err := s.taskStatePath(id); if err != nil { return nil, err }
    if err := saveTask(statePath, task); err != nil { _, _ = gitOutput(workspace, 30*time.Second, "worktree", "remove", "--force", worktree); return nil, err }
    return map[string]any{"task":task}, nil
}

func (s *Server) taskStateDir() (string, error) {
    workspace, err := s.workspaceRoot(); if err != nil { return "", err }
    common, err := gitOutput(workspace, 10*time.Second, "rev-parse", "--git-common-dir"); if err != nil { return "", err }
    dir := strings.TrimSpace(common)
    if !filepath.IsAbs(dir) { dir = filepath.Join(workspace, dir) }
    dir, err = filepath.Abs(dir); if err != nil { return "", err }
    return filepath.Join(dir, "devdesk", "tasks"), nil
}

func (s *Server) taskStatePath(id string) (string, error) {
    if !validTaskID(id) { return "", errors.New("invalid task id") }
    dir, err := s.taskStateDir(); if err != nil { return "", err }
    return filepath.Join(dir, id+".json"), nil
}

func (s *Server) loadAgentTask(id string) (agentTaskRecord, string, error) {
    path, err := s.taskStatePath(id); if err != nil { return agentTaskRecord{}, "", err }
    raw, err := os.ReadFile(path); if err != nil { return agentTaskRecord{}, path, fmt.Errorf("read task: %w", err) }
    var task agentTaskRecord
    if err := json.Unmarshal(raw, &task); err != nil { return agentTaskRecord{}, path, fmt.Errorf("decode task: %w", err) }
    return task, path, nil
}

func (s *Server) listAgentTasks() ([]agentTaskRecord, error) {
    dir, err := s.taskStateDir(); if err != nil { return nil, err }
    entries, err := os.ReadDir(dir); if errors.Is(err, os.ErrNotExist) { return []agentTaskRecord{}, nil }; if err != nil { return nil, err }
    tasks := make([]agentTaskRecord,0,len(entries))
    for _, entry := range entries { if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") { continue }; raw, e := os.ReadFile(filepath.Join(dir,entry.Name())); if e != nil { continue }; var task agentTaskRecord; if json.Unmarshal(raw,&task)==nil { tasks=append(tasks,task) } }
    sort.Slice(tasks, func(i,j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
    return tasks,nil
}

func saveTask(path string, task agentTaskRecord) error {
    if err := os.MkdirAll(filepath.Dir(path),0o700); err != nil { return err }
    raw, err := json.MarshalIndent(task,"","  "); if err != nil { return err }
    tmp := path+".tmp"
    if err := os.WriteFile(tmp,raw,0o600); err != nil { return err }
    return os.Rename(tmp,path)
}

func (s *Server) refreshTaskChanges(task *agentTaskRecord) error {
    if task.Worktree == "" { return nil }
    names, err := taskChangedFiles(*task); if err != nil { return err }
    task.ChangedFiles = names; task.UpdatedAt = time.Now().UTC(); return nil
}

func taskChangedFiles(task agentTaskRecord) ([]string,error) {
    tracked, err := gitOutput(task.Worktree,20*time.Second,"diff","--name-only",task.BaseCommit,"--"); if err != nil { return nil,err }
    values := map[string]struct{}{}
    scanner := bufio.NewScanner(strings.NewReader(tracked)); for scanner.Scan(){ p:=strings.TrimSpace(scanner.Text()); if p!="" { values[p]=struct{}{} } }
    untracked, _ := gitOutput(task.Worktree,20*time.Second,"ls-files","--others","--exclude-standard")
    scanner = bufio.NewScanner(strings.NewReader(untracked)); for scanner.Scan(){ p:=strings.TrimSpace(scanner.Text()); if p!="" { values[p]=struct{}{} } }
    out:=make([]string,0,len(values)); for p:=range values { out=append(out,p) }; sort.Strings(out); return out,nil
}

func taskDiff(task agentTaskRecord) (string,bool,error) {
    diff, err := gitOutputLimit(task.Worktree,30*time.Second,maxTaskDiffBytes,"diff","--binary",task.BaseCommit,"--"); if err != nil { return "",false,err }
    untracked,_:=gitOutput(task.Worktree,20*time.Second,"ls-files","--others","--exclude-standard")
    if strings.TrimSpace(untracked)!="" { diff += "\n\n# Untracked files\n"+untracked }
    truncated:=len(diff)>=maxTaskDiffBytes
    return diff,truncated,nil
}

func (s *Server) applyAgentTask(task agentTaskRecord, statePath string) (map[string]any,error) {
    if task.Status=="applied" || task.Status=="discarded" { return nil,fmt.Errorf("task is already %s",task.Status) }
    changed,err:=taskChangedFiles(task); if err!=nil{return nil,err}; if len(changed)==0{return nil,errors.New("task has no changes to apply")}
    for _,p:=range changed {
        trackedAtBase := gitSuccess(task.Workspace,"cat-file","-e",task.BaseCommit+":"+filepath.ToSlash(p))
        mainPath:=filepath.Join(task.Workspace,filepath.FromSlash(p))
        if trackedAtBase {
            if !gitSuccess(task.Workspace,"diff","--quiet",task.BaseCommit,"--",p) { return nil,fmt.Errorf("refusing apply: main workspace changed %q since task base",p) }
        } else if _,statErr:=os.Lstat(mainPath); statErr==nil { return nil,fmt.Errorf("refusing apply: new task path %q already exists in main workspace",p) } else if !errors.Is(statErr,os.ErrNotExist){return nil,statErr}
    }
    patch,err:=gitOutputLimit(task.Worktree,30*time.Second,8*1024*1024,"diff","--binary",task.BaseCommit,"--"); if err!=nil{return nil,err}
    if strings.TrimSpace(patch)!="" {
        cmd:=exec.Command("git","apply","--whitespace=nowarn","-"); cmd.Dir=task.Workspace; cmd.Stdin=strings.NewReader(patch); cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0")
        out,runErr:=cmd.CombinedOutput(); if runErr!=nil{return nil,fmt.Errorf("apply task patch: %v: %s",runErr,strings.TrimSpace(string(out)))}
    }
    untracked,_:=gitOutput(task.Worktree,20*time.Second,"ls-files","--others","--exclude-standard")
    scanner:=bufio.NewScanner(strings.NewReader(untracked)); for scanner.Scan(){ p:=strings.TrimSpace(scanner.Text()); if p==""{continue}; src:=filepath.Join(task.Worktree,filepath.FromSlash(p)); dst:=filepath.Join(task.Workspace,filepath.FromSlash(p)); info,e:=os.Stat(src); if e!=nil||!info.Mode().IsRegular(){continue}; raw,e:=os.ReadFile(src); if e!=nil{return nil,e}; if e=os.MkdirAll(filepath.Dir(dst),0o755);e!=nil{return nil,e}; if e=os.WriteFile(dst,raw,info.Mode().Perm());e!=nil{return nil,e} }
    now:=time.Now().UTC(); task.Status="applied"; task.Phase="done"; task.UpdatedAt=now; task.AppliedAt=&now; task.ChangedFiles=changed; if err:=saveTask(statePath,task);err!=nil{return nil,err}
    return map[string]any{"task":task,"appliedFiles":changed,"indexPreserved":true},nil
}

func (s *Server) discardAgentTask(task agentTaskRecord, statePath string) (map[string]any,error) {
    if task.Status=="applied" { return nil,errors.New("cannot discard a task after it was applied") }
    if task.Worktree!="" { if _,err:=gitOutput(task.Workspace,30*time.Second,"worktree","remove","--force",task.Worktree);err!=nil && !errors.Is(err,os.ErrNotExist){ return nil,fmt.Errorf("remove task worktree: %w",err) } }
    now:=time.Now().UTC(); task.Status="discarded";task.Phase="done";task.UpdatedAt=now;task.DiscardedAt=&now; if err:=saveTask(statePath,task);err!=nil{return nil,err}; return map[string]any{"task":task},nil
}

func ensureDevDeskExcluded(workspace string) error {
    common,err:=gitOutput(workspace,10*time.Second,"rev-parse","--git-common-dir"); if err!=nil{return err}; d:=strings.TrimSpace(common); if !filepath.IsAbs(d){d=filepath.Join(workspace,d)}; exclude:=filepath.Join(d,"info","exclude")
    raw,_:=os.ReadFile(exclude); if strings.Contains(string(raw),".devdesk/"){return nil}; if err:=os.MkdirAll(filepath.Dir(exclude),0o755);err!=nil{return err}; f,err:=os.OpenFile(exclude,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o644);if err!=nil{return err};defer f.Close();_,err=f.WriteString("\n# MCP DevDesk managed task worktrees\n.devdesk/\n");return err
}

func newTaskID()(string,error){ b:=make([]byte,8);if _,err:=rand.Read(b);err!=nil{return "",err};return "task-"+hex.EncodeToString(b),nil }
func validTaskID(id string)bool{ if !strings.HasPrefix(id,"task-")||len(id)!=21{return false};_,err:=hex.DecodeString(strings.TrimPrefix(id,"task-"));return err==nil }

func gitSuccess(cwd string,args ...string)bool{ cmd:=exec.Command("git",args...);cmd.Dir=cwd;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");return cmd.Run()==nil }
func gitOutput(cwd string,timeout time.Duration,args ...string)(string,error){return gitOutputLimit(cwd,timeout,1024*1024,args...)}
func gitOutputLimit(cwd string,timeout time.Duration,limit int,args ...string)(string,error){ ctx,cancel:=context.WithTimeout(context.Background(),timeout);defer cancel();cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=cwd;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");out,err:=cmd.CombinedOutput();if ctx.Err()!=nil{return "",fmt.Errorf("git command timed out: %s",strings.Join(args," "))};if len(out)>limit{out=out[:limit]};if err!=nil{return "",fmt.Errorf("git %s: %v: %s",strings.Join(args," "),err,strings.TrimSpace(string(out)))};return string(out),nil }

func (s *Server) validateProject(args validateProjectArgs)(map[string]any,error){
    root,err:=s.workspaceRoot();if err!=nil{return nil,err};var task agentTaskRecord;var taskPath string
    if args.TaskID!=""{task,taskPath,err=s.loadAgentTask(args.TaskID);if err!=nil{return nil,err};root=task.Worktree}
    commands:=detectValidationCommands(root)
    result:=map[string]any{"root":root,"commands":commands,"executed":false}
    if !args.Run{return result,nil}
    if s.permissionMode=="safe"||s.toolProfile!="full"{return nil,errors.New("running validation requires trusted/dangerous mode with full tool profile")}
    timeout:=args.TimeoutSeconds;if timeout<=0{timeout=180};if timeout>600{timeout=600}
    validations:=make([]validationResult,0,len(commands))
    for _,spec:=range commands{vr:=runValidation(root,spec[0],spec[1],time.Duration(timeout)*time.Second);validations=append(validations,vr);if vr.ExitCode!=0{break}}
    result["executed"]=true;result["results"]=validations
    ok:=true;for _,v:=range validations{if v.ExitCode!=0{ok=false;break}};result["ok"]=ok
    if args.TaskID!=""{task.Validation=validations;task.UpdatedAt=time.Now().UTC();task.LastHeartbeat=task.UpdatedAt;if ok{task.Phase="validated";task.NextStep="Review task diff and apply or discard."}else{task.Phase="validation_failed";task.Status="failed";task.Error="project validation failed"};_ = s.refreshTaskChanges(&task);_ = saveTask(taskPath,task);result["task"]=task}
    return result,nil
}

func detectValidationCommands(root string)[][]string{
    out:=[][]string{}
    if raw,err:=os.ReadFile(filepath.Join(root,"package.json"));err==nil{var pkg struct{Scripts map[string]string `json:"scripts"`};if json.Unmarshal(raw,&pkg)==nil{for _,pair:=range [][2]string{{"test","npm test"},{"lint","npm run lint"},{"build","npm run build"}}{if v:=strings.TrimSpace(pkg.Scripts[pair[0]]);v!=""&&!strings.Contains(v,"no test specified"){out=append(out,[]string{pair[0],pair[1]})}}}}
    if fileExists(filepath.Join(root,"go.mod")){out=append(out,[]string{"test","go test ./..."},[]string{"vet","go vet ./..."})}
    if fileExists(filepath.Join(root,"Cargo.toml")){out=append(out,[]string{"test","cargo test --workspace"},[]string{"check","cargo check --workspace"})}
    if matches,_:=filepath.Glob(filepath.Join(root,"*.sln"));len(matches)>0{out=append(out,[]string{"test","dotnet test"},[]string{"build","dotnet build --no-restore"})}
    if fileExists(filepath.Join(root,"pyproject.toml"))||fileExists(filepath.Join(root,"pytest.ini")){out=append(out,[]string{"test","python -m pytest"})}
    return out
}

func runValidation(root,kind,command string,timeout time.Duration)validationResult{started:=time.Now();ctx,cancel:=context.WithTimeout(context.Background(),timeout);defer cancel();var cmd *exec.Cmd;if filepath.Separator=='\\'{cmd=exec.CommandContext(ctx,"cmd.exe","/d","/s","/c",command)}else{cmd=exec.CommandContext(ctx,"sh","-lc",command)};cmd.Dir=root;cmd.Env=append(os.Environ(),"CI=1","GIT_TERMINAL_PROMPT=0");out,err:=cmd.CombinedOutput();exit:=0;if err!=nil{exit=1;if ee,ok:=err.(*exec.ExitError);ok{exit=ee.ExitCode()}};truncated:=len(out)>maxValidationOutputBytes;if truncated{out=out[:maxValidationOutputBytes]};return validationResult{Kind:kind,Command:command,ExitCode:exit,DurationMS:time.Since(started).Milliseconds(),Output:string(out),Truncated:truncated}}
func fileExists(path string)bool{info,err:=os.Stat(path);return err==nil&&info.Mode().IsRegular()}
'''

CODE_NAV = r'''package mcpcore

import (
    "bufio"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
)

type codeSymbolArgs struct { Path string `json:"path,omitempty"`; Query string `json:"query,omitempty"`; Limit int `json:"limit,omitempty"` }
type codeLookupArgs struct { Symbol string `json:"symbol"`; Path string `json:"path,omitempty"`; Limit int `json:"limit,omitempty"` }
type codeNavMatch struct { Path string `json:"path"`; Line int `json:"line"`; Kind string `json:"kind,omitempty"`; Name string `json:"name,omitempty"`; Preview string `json:"preview"` }

func codeNavigationTools() []Tool { return []Tool{
    {Name:"code_symbols",Title:"Document Symbols",Description:"List symbols from a source file using the v0.13 code-navigation engine. Reports detected installed language servers and uses a bounded lexical fallback when an LSP server is not attached.",InputSchema:codeSymbolSchema(true)},
    {Name:"workspace_symbols",Title:"Workspace Symbols",Description:"Search symbol declarations across the workspace with bounded traversal; reports available LSP server binaries and lexical fallback status.",InputSchema:codeSymbolSchema(false)},
    {Name:"find_definition",Title:"Find Definition",Description:"Find likely source definitions for a symbol across supported source languages.",InputSchema:codeLookupSchema()},
    {Name:"find_references",Title:"Find References",Description:"Find bounded whole-word references to a symbol across source files.",InputSchema:codeLookupSchema()},
} }
func codeSymbolSchema(requirePath bool)map[string]any{m:=map[string]any{"type":"object","properties":map[string]any{"path":map[string]any{"type":"string","default":"."},"query":map[string]any{"type":"string"},"limit":map[string]any{"type":"integer","minimum":1,"maximum":500,"default":100}},"additionalProperties":false};if requirePath{m["required"]=[]string{"path"}};return m}
func codeLookupSchema()map[string]any{return map[string]any{"type":"object","properties":map[string]any{"symbol":map[string]any{"type":"string","minLength":1,"maxLength":300},"path":map[string]any{"type":"string","default":"."},"limit":map[string]any{"type":"integer","minimum":1,"maximum":500,"default":100}},"required":[]string{"symbol"},"additionalProperties":false}}

func (s *Server) executeCodeNavigationTool(name string,args map[string]any)(map[string]any,error){status:=detectedLanguageServers();switch name{
case "code_symbols":var a codeSymbolArgs;if err:=decodeToolArguments(args,&a);err!=nil{return nil,err};matches,err:=s.scanSymbols(a.Path,a.Query,a.Limit,true);return map[string]any{"matches":matches,"count":len(matches),"engine":"lexical-fallback","availableLanguageServers":status},err
case "workspace_symbols":var a codeSymbolArgs;if err:=decodeToolArguments(args,&a);err!=nil{return nil,err};matches,err:=s.scanSymbols(a.Path,a.Query,a.Limit,false);return map[string]any{"matches":matches,"count":len(matches),"engine":"lexical-fallback","availableLanguageServers":status},err
case "find_definition":var a codeLookupArgs;if err:=decodeToolArguments(args,&a);err!=nil{return nil,err};matches,err:=s.scanDefinitions(a);return map[string]any{"matches":matches,"count":len(matches),"engine":"lexical-fallback","availableLanguageServers":status},err
case "find_references":var a codeLookupArgs;if err:=decodeToolArguments(args,&a);err!=nil{return nil,err};matches,err:=s.scanReferences(a);return map[string]any{"matches":matches,"count":len(matches),"engine":"lexical-fallback","availableLanguageServers":status},err
default:return nil,fmt.Errorf("unknown code navigation tool: %s",name)}}

var symbolPatterns=[]struct{Kind string;Re *regexp.Regexp}{
{"function",regexp.MustCompile(`^\s*(?:func\s+(?:\([^)]*\)\s*)?|fn\s+|def\s+|function\s+|(?:public|private|protected|internal|static|async|export|default|final|virtual|override|sealed|abstract|\s)+\s*[A-Za-z_][\w<>,\[\]? ]*\s+)([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
{"type",regexp.MustCompile(`^\s*(?:type|class|interface|struct|enum|trait)\s+([A-Za-z_][A-Za-z0-9_]*)`)},
{"constant",regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)},
}
func sourceExt(path string)bool{switch strings.ToLower(filepath.Ext(path)){case ".go",".rs",".py",".js",".jsx",".ts",".tsx",".cs",".java",".kt",".kts",".c",".cc",".cpp",".h",".hpp",".swift",".rb",".php":return true};return false}
func skipCodeDir(name string)bool{switch strings.ToLower(name){case ".git","node_modules","vendor","dist","build","target",".devdesk",".venv","venv","__pycache__":return true};return false}
func limitValue(v int)int{if v<=0{return 100};if v>500{return 500};return v}
func (s *Server) codeRoot(value string)(string,string,error){if strings.TrimSpace(value)==""{value="."};_,abs,rel,err:=s.resolveWorkspacePath(value);return abs,rel,err}
func symbolsInFile(path,rel,query string,limit int)([]codeNavMatch,error){f,err:=os.Open(path);if err!=nil{return nil,err};defer f.Close();q:=strings.ToLower(strings.TrimSpace(query));out:=[]codeNavMatch{};sc:=bufio.NewScanner(f);buf:=make([]byte,64*1024);sc.Buffer(buf,1024*1024);line:=0;for sc.Scan(){line++;text:=sc.Text();for _,p:=range symbolPatterns{m:=p.Re.FindStringSubmatch(text);if len(m)<2{continue};name:=m[1];if q!=""&&!strings.Contains(strings.ToLower(name),q){continue};out=append(out,codeNavMatch{Path:filepath.ToSlash(rel),Line:line,Kind:p.Kind,Name:name,Preview:strings.TrimSpace(text)});break};if len(out)>=limit{break}};return out,sc.Err()}
func (s *Server) scanSymbols(path,query string,limit int,single bool)([]codeNavMatch,error){root,rel,err:=s.codeRoot(path);if err!=nil{return nil,err};limit=limitValue(limit);info,err:=os.Stat(root);if err!=nil{return nil,err};if info.Mode().IsRegular(){if !sourceExt(root){return nil,errors.New("path is not a supported source file")};return symbolsInFile(root,rel,query,limit)};out:=[]codeNavMatch{};err=filepath.WalkDir(root,func(p string,d os.DirEntry,e error)error{if e!=nil{return nil};if d.IsDir(){if p!=root&&skipCodeDir(d.Name()){return filepath.SkipDir};return nil};if !sourceExt(p){return nil};r,_:=filepath.Rel(s.workspace,p);found,_:=symbolsInFile(p,r,query,limit-len(out));out=append(out,found...);if len(out)>=limit{return filepath.SkipAll};if single{return filepath.SkipAll};return nil});return out,err}
func (s *Server) scanDefinitions(a codeLookupArgs)([]codeNavMatch,error){all,err:=s.scanSymbols(a.Path,"",500,false);if err!=nil{return nil,err};limit:=limitValue(a.Limit);out:=[]codeNavMatch{};for _,m:=range all{if m.Name==a.Symbol{out=append(out,m);if len(out)>=limit{break}}};return out,nil}
func (s *Server) scanReferences(a codeLookupArgs)([]codeNavMatch,error){root,_,err:=s.codeRoot(a.Path);if err!=nil{return nil,err};limit:=limitValue(a.Limit);word:=regexp.MustCompile(`\b`+regexp.QuoteMeta(a.Symbol)+`\b`);out:=[]codeNavMatch{};err=filepath.WalkDir(root,func(p string,d os.DirEntry,e error)error{if e!=nil{return nil};if d.IsDir(){if p!=root&&skipCodeDir(d.Name()){return filepath.SkipDir};return nil};if !sourceExt(p){return nil};f,e:=os.Open(p);if e!=nil{return nil};defer f.Close();sc:=bufio.NewScanner(f);sc.Buffer(make([]byte,64*1024),1024*1024);line:=0;for sc.Scan(){line++;if word.MatchString(sc.Text()){rel,_:=filepath.Rel(s.workspace,p);out=append(out,codeNavMatch{Path:filepath.ToSlash(rel),Line:line,Name:a.Symbol,Preview:strings.TrimSpace(sc.Text())});if len(out)>=limit{return filepath.SkipAll}}};return nil});return out,err}
func detectedLanguageServers()[]string{candidates:=[]string{"gopls","rust-analyzer","typescript-language-server","pyright-langserver","pylsp","csharp-ls","OmniSharp"};out:=[]string{};for _,name:=range candidates{if p,err:=exec.LookPath(name);err==nil{out=append(out,filepath.Base(p))}};sort.Strings(out);return out}
'''

SCREEN_PROBE = r'''

type screenProbeArgs struct { Window string `json:"window,omitempty"` }

func screenCompatibilityTool() Tool { return Tool{
    Name:"screen_capture_probe", Title:"Screen Vision Compatibility Probe", Description:"Report the safe non-intrusive capture policy and target state without moving, restoring, foregrounding or capturing the target window.",
    InputSchema:map[string]any{"type":"object","properties":map[string]any{"window":map[string]any{"type":"string"}},"additionalProperties":false},
} }

func (s *Server) screenCaptureProbe(arguments map[string]any)(map[string]any,error){
    var args screenProbeArgs;if err:=decodeToolArguments(arguments,&args);err!=nil{return nil,err}
    result:=map[string]any{"previewPolicy":"non-intrusive-fail-closed","safeMethods":[]string{"PrintWindow(PW_RENDERFULLCONTENT)","PrintWindow","WindowDC"},"stateChangingFallbacks":false,"windowsGraphicsCapture":"planned-native-backend"}
    if strings.TrimSpace(args.Window)==""{return result,nil}
    windows,err:=platformListScreenWindowsForVision();if err!=nil{return nil,err};window,err:=resolveScreenWindow(windows,args.Window);if err!=nil{return nil,err};result["window"]=window;result["minimized"]=window.Minimized;if window.Minimized{result["expected"]="fail-closed unless a non-intrusive backend becomes available"}else{result["expected"]="PrintWindow/WindowDC only; no Z-order mutation"};return result,nil
}
'''

write("app/internal/mcpcore/task_tools.go", TASK_TOOLS)
write("app/internal/mcpcore/code_navigation.go", CODE_NAV)

replace_once("app/internal/mcpcore/server.go", "\ttools = append(tools, permissionTools()...)\n", "\ttools = append(tools, permissionTools()...)\n\ttools = append(tools, taskTools()...)\n\ttools = append(tools, codeNavigationTools()...)\n")
replace_once("app/internal/mcpcore/file_tools.go", "\tcase \"permission_status\", \"request_permissions\":\n\t\treturn s.executePermissionTool(name, arguments)\n", "\tcase \"permission_status\", \"request_permissions\":\n\t\treturn s.executePermissionTool(name, arguments)\n\tcase \"agent_task\", \"validate_project\", \"agent_workflow\":\n\t\treturn s.executeTaskTool(name, arguments)\n\tcase \"code_symbols\", \"workspace_symbols\", \"find_definition\", \"find_references\":\n\t\treturn s.executeCodeNavigationTool(name, arguments)\n")

# Add the compatibility probe to Screen Vision without taking pixels.
replace_once("app/internal/mcpcore/screen_tools.go", "\treturn []Tool{\n", "\treturn []Tool{\n\t\tscreenCompatibilityTool(),\n")
replace_once("app/internal/mcpcore/screen_tools.go", "\tswitch name {\n\tcase \"screen_list_windows\":", "\tswitch name {\n\tcase \"screen_capture_probe\":\n\t\treturn s.screenCaptureProbe(arguments)\n\tcase \"screen_list_windows\":")
text = read("app/internal/mcpcore/screen_tools.go")
if "func screenCompatibilityTool()" not in text:
    write("app/internal/mcpcore/screen_tools.go", text + SCREEN_PROBE)
replace_once("app/internal/mcpcore/file_tools.go", "\tcase \"screen_list_windows\", \"screen_get_active_window\", \"screen_capture_window\", \"screen_capture_active_window\", \"screen_capture_desktop\":", "\tcase \"screen_capture_probe\", \"screen_list_windows\", \"screen_get_active_window\", \"screen_capture_window\", \"screen_capture_active_window\", \"screen_capture_desktop\":")

# Preview safety: remove the state-changing temporary Z-order reveal fallback.
path = "app/internal/mcpcore/screen_capture_windows.go"
text = read(path)
text = text.replace("\tvar backgroundRevealErr error\n", "")
pattern = re.compile(r'''\n\t\t// VMware and other compositor-heavy windows commonly report successful.*?\n\t\tif captured == 0 && screenBackgroundRevealRequired\(hwnd, foreground\) \{.*?\n\t\t\}\n''', re.S)
text, count = pattern.subn("\n\t\t// v0.13 preview deliberately refuses Z-order/window-state mutation fallbacks.\n\t\t// A native Windows Graphics Capture backend can be added after capability probing;\n\t\t// until then background targets fail closed when HWND-owned methods cannot capture them.\n", text, count=1)
if count != 1:
    raise RuntimeError("failed to remove background reveal fallback")
text = text.replace("\t\t\t} else if backgroundRevealErr != nil {\n\t\t\t\treturn screenCaptureFrame{}, fmt.Errorf(\"capture selected background window: %w\", backgroundRevealErr)\n\t\t\t} else {", "\t\t\t} else {")
write(path, text)

# Preview version. Stable update channel will not see prereleases unless explicitly enabled.
replace_once("app/internal/buildinfo/version.go", 'const Version = "0.12.34"', 'const Version = "0.13.0-preview.1"')

# Keep docs honest about what is implemented in this preview.
roadmap = read("docs/ROADMAP.md")
roadmap = re.sub(r"当前稳定开发版本：`[^`]+`。", "当前预览开发版本：`0.13.0-preview.1`（稳定版仍为 `0.12.34`）。", roadmap, count=1)
roadmap = roadmap.replace("- [ ] 自动更新", "- [x] 自动更新")
roadmap += """

## M6：0.13 AI Coding Workspace Preview

- [x] 持久 Agent Task：Task ID、阶段、下一步、心跳、错误、验证结果和重启恢复
- [x] 每任务独立 Git Worktree，位于项目 `.devdesk/worktrees/` 并通过 `.git/info/exclude` 隔离
- [x] Diff review、Apply / Discard；Apply 对任务改动路径执行主工作区基线冲突保护，不静默覆盖人工修改
- [x] `agent_workflow` 高层工作流入口
- [x] `validate_project` 自动识别 Node/Go/Rust/.NET/Python 验证命令并支持受限执行
- [x] 代码导航基础工具：symbols / definition / references；检测可用 language-server，并明确标记 preview 使用 lexical fallback
- [x] Screen Vision preview 禁用改变 Z-order 的后台 reveal fallback，捕获失败时 fail closed
- [x] `screen_capture_probe` 非侵入式兼容性探测
- [ ] Windows Graphics Capture 原生后端（需要 Windows 实机兼容性矩阵后再启用，preview 不伪装实现）
- [ ] Authenticode 正式签名（发布流水线可预留签名步骤，但仍需要用户提供代码签名证书）
- [ ] macOS / Linux（本阶段明确不做）
"""
write("docs/ROADMAP.md", roadmap)

write("docs/V013_PREVIEW.md", """# MCP DevDesk 0.13.0 Preview\n\n本预览版把 MCP DevDesk 从工具集合推进到可恢复的 AI Coding Workspace。\n\n## 重点\n\n- Agent Task：持久任务状态、心跳、阶段、错误与验证结果。\n- Task Worktree：任务自动创建隔离 Git Worktree；主工作区重叠修改时 Apply 拒绝覆盖。\n- Agent Workflow：一条高层入口建立任务闭环。\n- Project Validation：自动识别 Node、Go、Rust、.NET 与 Python 常用验证命令。\n- Code Navigation：新增 symbols/definition/references；本 preview 会探测 language-server，但结果引擎明确标记为 lexical fallback，避免把未接入的 LSP 伪装成已接入。\n- Screen Vision 安全预览：禁用临时改变窗口 Z-order 的后台 reveal fallback；新增只读兼容性探针。Windows Graphics Capture 原生后端待实机矩阵验证后再打开。\n\n## 任务闭环\n\n1. `agent_workflow(goal=...)` 创建任务与 `.devdesk/worktrees/<task-id>`。\n2. Agent 只在任务 worktree 中修改。\n3. 长任务使用 `agent_task heartbeat/update` 保存阶段。\n4. `validate_project(taskId=..., run=true)` 验证。\n5. `agent_task diff` 审查。\n6. `agent_task apply` 安全应用；若主工作区相同路径已变化则拒绝。\n7. 不需要的任务使用 `agent_task discard`。\n\n## Preview 边界\n\n这是预览版，不替代 0.12.34 stable。Screen Vision 不再为了后台截图临时提升 Z-order；因此某些 VMware/GPU 窗口在 native Windows Graphics Capture 后端完成前会明确失败，而不是扰动桌面状态。\n""")

print("v0.13 bootstrap source changes generated")
