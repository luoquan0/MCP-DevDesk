package agentstate

import (
	"crypto/rand"
	"encoding/base64"
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
	taskStoreVersion = 2
	maxTaskJobIDs    = 64
)

type Task struct {
	ID                    string   `json:"id"`
	Title                 string   `json:"title"`
	Summary               string   `json:"summary,omitempty"`
	Status                string   `json:"status"`
	BaseWorkspace         string   `json:"baseWorkspace"`
	WorktreePath          string   `json:"worktreePath"`
	Branch                string   `json:"branch"`
	BaseCommit            string   `json:"baseCommit"`
	ResultCommit          string   `json:"resultCommit,omitempty"`
	BaseDirtyFilesAtStart []string `json:"baseDirtyFilesAtStart,omitempty"`
	CurrentStep           string   `json:"currentStep,omitempty"`
	NextStep              string   `json:"nextStep,omitempty"`
	ChangedFiles          []string `json:"changedFiles,omitempty"`
	JobIDs                []string `json:"jobIds,omitempty"`
	CheckJobIDs           []string `json:"checkJobIds,omitempty"`
	LastJobID             string   `json:"lastJobId,omitempty"`
	LastValidationStatus  string   `json:"lastValidationStatus,omitempty"`
	LastValidationAt      string   `json:"lastValidationAt,omitempty"`
	LastHeartbeatAt       string   `json:"lastHeartbeatAt,omitempty"`
	FailureReason         string   `json:"failureReason,omitempty"`
	CreatedAt             string   `json:"createdAt"`
	UpdatedAt             string   `json:"updatedAt"`
	FinishedAt            string   `json:"finishedAt,omitempty"`
	AcceptedAt            string   `json:"acceptedAt,omitempty"`
	RejectedAt            string   `json:"rejectedAt,omitempty"`
}

type taskFile struct {
	Version      int    `json:"version"`
	ActiveTaskID string `json:"activeTaskId,omitempty"`
	Tasks        []Task `json:"tasks"`
}

type TaskStore struct {
	path         string
	worktreeRoot string
}

func NewTaskStore(dataDir string) *TaskStore {
	root := filepath.Join(dataDir, "agent-runtime")
	return &TaskStore{path: filepath.Join(root, "tasks.json"), worktreeRoot: filepath.Join(root, "worktrees")}
}

func (s *TaskStore) Path() string { return s.path }

func (s *TaskStore) Start(workspace, title, summary string) (Task, error) {
	workspace, err := cleanExistingDir(workspace)
	if err != nil {
		return Task{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "AI task"
	}
	if len(title) > 160 {
		return Task{}, errors.New("task title cannot exceed 160 characters")
	}
	summary = strings.TrimSpace(summary)
	if len(summary) > 4000 {
		return Task{}, errors.New("task summary cannot exceed 4000 characters")
	}
	if inside, _, err := runGit(workspace, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(inside) != "true" {
		return Task{}, errors.New("task isolation requires a Git working tree")
	}
	baseDirtyFiles, err := gitDirtyPaths(workspace)
	if err != nil {
		return Task{}, fmt.Errorf("read Git status: %w", err)
	}
	baseCommit, _, err := runGit(workspace, "rev-parse", "HEAD")
	if err != nil {
		return Task{}, fmt.Errorf("resolve task base commit: %w", err)
	}
	baseCommit = strings.TrimSpace(baseCommit)

	id, err := newID("tsk_", 12)
	if err != nil {
		return Task{}, err
	}
	branch := "mcp-task/" + strings.TrimPrefix(id, "tsk_")
	worktree := filepath.Join(s.worktreeRoot, id)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := Task{
		ID:                    id,
		Title:                 title,
		Summary:               summary,
		Status:                "editing",
		BaseWorkspace:         workspace,
		WorktreePath:          worktree,
		Branch:                branch,
		BaseCommit:            baseCommit,
		BaseDirtyFilesAtStart: baseDirtyFiles,
		CurrentStep:           "已创建隔离 Worktree",
		NextStep:              "分析任务并修改代码",
		LastHeartbeatAt:       now,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	err = s.withLock(func(state *taskFile) error {
		if state.ActiveTaskID != "" {
			if active := findTask(state.Tasks, state.ActiveTaskID); active != nil && (active.Status == "editing" || active.Status == "review") {
				return fmt.Errorf("task %s is already active; finish, accept, reject, or resume it before starting another task", active.ID)
			}
			state.ActiveTaskID = ""
		}
		if err := os.MkdirAll(s.worktreeRoot, 0o700); err != nil {
			return fmt.Errorf("create task worktree directory: %w", err)
		}
		if output, stderr, gitErr := runGit(workspace, "worktree", "add", "-b", branch, worktree, baseCommit); gitErr != nil {
			return fmt.Errorf("create isolated Git worktree: %w: %s", gitErr, compact(output+"\n"+stderr))
		}
		state.Tasks = append(state.Tasks, task)
		state.ActiveTaskID = id
		if saveErr := s.save(state); saveErr != nil {
			_, _, _ = runGit(workspace, "worktree", "remove", "--force", worktree)
			_, _, _ = runGit(workspace, "branch", "-D", branch)
			return saveErr
		}
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *TaskStore) List() ([]Task, string, error) {
	state, err := s.load()
	if err != nil {
		return nil, "", err
	}
	tasks := append([]Task(nil), state.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].UpdatedAt > tasks[j].UpdatedAt })
	return tasks, state.ActiveTaskID, nil
}

func (s *TaskStore) Get(id string) (Task, error) {
	state, err := s.load()
	if err != nil {
		return Task{}, err
	}
	task := findTask(state.Tasks, strings.TrimSpace(id))
	if task == nil {
		return Task{}, errors.New("task not found")
	}
	return *task, nil
}

func (s *TaskStore) Active() (Task, bool, error) {
	state, err := s.load()
	if err != nil {
		return Task{}, false, err
	}
	if state.ActiveTaskID == "" {
		return Task{}, false, nil
	}
	task := findTask(state.Tasks, state.ActiveTaskID)
	if task == nil || (task.Status != "editing" && task.Status != "review") {
		return Task{}, false, nil
	}
	if info, statErr := os.Stat(task.WorktreePath); statErr != nil || !info.IsDir() {
		return Task{}, false, nil
	}
	return *task, true, nil
}

func (s *TaskStore) ActiveWorkspace(baseWorkspace string) string {
	task, ok, err := s.Active()
	if err != nil || !ok || !samePath(baseWorkspace, task.BaseWorkspace) {
		return baseWorkspace
	}
	return task.WorktreePath
}

func (s *TaskStore) Resume(id string) (Task, error) {
	id = strings.TrimSpace(id)
	var result Task
	err := s.withLock(func(state *taskFile) error {
		task := findTask(state.Tasks, id)
		if task == nil {
			return errors.New("task not found")
		}
		if task.Status != "editing" && task.Status != "review" {
			return fmt.Errorf("task %s is %s and cannot be resumed", task.ID, task.Status)
		}
		if info, err := os.Stat(task.WorktreePath); err != nil || !info.IsDir() {
			return errors.New("task worktree is unavailable")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "editing"
		task.CurrentStep = "继续修改任务"
		if strings.TrimSpace(task.NextStep) == "" {
			task.NextStep = "完成修改并运行项目验证"
		}
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		state.ActiveTaskID = task.ID
		result = *task
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) UpdateProgress(id, currentStep, nextStep, failureReason string, clearFailure bool) (Task, error) {
	id = strings.TrimSpace(id)
	currentStep = strings.TrimSpace(currentStep)
	nextStep = strings.TrimSpace(nextStep)
	failureReason = strings.TrimSpace(failureReason)
	if len(currentStep) > 512 {
		return Task{}, errors.New("current step cannot exceed 512 characters")
	}
	if len(nextStep) > 512 {
		return Task{}, errors.New("next step cannot exceed 512 characters")
	}
	if len(failureReason) > 4000 {
		return Task{}, errors.New("failure reason cannot exceed 4000 characters")
	}
	var result Task
	err := s.withLock(func(state *taskFile) error {
		if id == "" {
			id = state.ActiveTaskID
		}
		task := findTask(state.Tasks, id)
		if task == nil {
			return errors.New("task not found")
		}
		if task.Status != "editing" && task.Status != "review" {
			return fmt.Errorf("task %s is %s and cannot be updated", task.ID, task.Status)
		}
		if currentStep != "" {
			task.CurrentStep = currentStep
		}
		if nextStep != "" {
			task.NextStep = nextStep
		}
		if clearFailure {
			task.FailureReason = ""
		} else if failureReason != "" {
			task.FailureReason = failureReason
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		result = *task
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) RefreshChangedFiles(id string) (Task, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if active, ok, err := s.Active(); err != nil {
			return Task{}, err
		} else if ok {
			id = active.ID
		}
	}
	task, err := s.Get(id)
	if err != nil {
		return Task{}, err
	}
	if task.Status != "editing" && task.Status != "review" {
		return task, nil
	}
	changed, err := gitDirtyPaths(task.WorktreePath)
	if err != nil {
		return Task{}, err
	}
	var result Task
	err = s.withLock(func(state *taskFile) error {
		current := findTask(state.Tasks, id)
		if current == nil {
			return errors.New("task not found")
		}
		current.ChangedFiles = changed
		result = *current
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) RecordJob(job Job) error {
	if strings.TrimSpace(job.TaskID) == "" || strings.TrimSpace(job.ID) == "" {
		return nil
	}
	return s.withLock(func(state *taskFile) error {
		task := findTask(state.Tasks, strings.TrimSpace(job.TaskID))
		if task == nil {
			return nil
		}
		task.JobIDs = appendBoundedUniqueID(task.JobIDs, job.ID, maxTaskJobIDs)
		task.LastJobID = job.ID
		kind := strings.ToLower(strings.TrimSpace(job.Kind))
		if strings.HasPrefix(kind, "check:") || kind == "validate_project" {
			task.CheckJobIDs = appendBoundedUniqueID(task.CheckJobIDs, job.ID, maxTaskJobIDs)
			if job.Running {
				task.LastValidationStatus = "running"
			} else if job.ExitCode != nil && *job.ExitCode == 0 && strings.TrimSpace(job.LastError) == "" {
				task.LastValidationStatus = "passed"
				task.FailureReason = ""
			} else {
				task.LastValidationStatus = "failed"
				if strings.TrimSpace(job.LastError) != "" {
					task.FailureReason = compact(job.LastError)
				} else {
					task.FailureReason = "project validation failed; inspect job " + job.ID
				}
			}
			task.LastValidationAt = job.UpdatedAt
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if strings.TrimSpace(job.UpdatedAt) != "" {
			now = job.UpdatedAt
		}
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		return s.save(state)
	})
}

func (s *TaskStore) Finish(id, summary string) (Task, error) {
	id = strings.TrimSpace(id)
	summary = strings.TrimSpace(summary)
	if len(summary) > 8000 {
		return Task{}, errors.New("task finish summary cannot exceed 8000 characters")
	}
	var result Task
	err := s.withLock(func(state *taskFile) error {
		if id == "" {
			id = state.ActiveTaskID
		}
		task := findTask(state.Tasks, id)
		if task == nil {
			return errors.New("task not found")
		}
		if task.Status != "editing" && task.Status != "review" {
			return fmt.Errorf("task %s is %s and cannot be finished", task.ID, task.Status)
		}
		changed, err := gitDirtyPaths(task.WorktreePath)
		if err != nil {
			return fmt.Errorf("read task changes: %w", err)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "review"
		if summary != "" {
			task.Summary = summary
		}
		task.ChangedFiles = changed
		task.CurrentStep = "等待人工验收"
		task.NextStep = "在 MCP DevDesk 中审查 Diff 并选择接受或放弃"
		task.FinishedAt = now
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		state.ActiveTaskID = task.ID
		result = *task
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) Accept(id string) (Task, error) {
	id = strings.TrimSpace(id)
	var result Task
	err := s.withLock(func(state *taskFile) error {
		task := findTask(state.Tasks, id)
		if task == nil {
			return errors.New("task not found")
		}
		if task.Status != "review" {
			return errors.New("task must be finished and awaiting review before it can be accepted")
		}
		fail := func(message string) error {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			task.FailureReason = compact(message)
			task.CurrentStep = "验收被安全边界阻止"
			task.NextStep = "处理主工作区冲突后再次验收，或放弃此任务"
			task.LastHeartbeatAt = now
			task.UpdatedAt = now
			if saveErr := s.save(state); saveErr != nil {
				return fmt.Errorf("%s; persist task failure: %w", message, saveErr)
			}
			return errors.New(message)
		}
		if _, err := cleanExistingDir(task.BaseWorkspace); err != nil {
			return fail(err.Error())
		}
		if _, err := cleanExistingDir(task.WorktreePath); err != nil {
			return fail(err.Error())
		}
		baseHead, _, err := runGit(task.BaseWorkspace, "rev-parse", "HEAD")
		if err != nil {
			return fail("resolve base HEAD: " + err.Error())
		}
		if !strings.EqualFold(strings.TrimSpace(baseHead), task.BaseCommit) {
			return fail("base branch advanced while the task was isolated; rebase or create a new task before accepting")
		}
		if output, stderr, err := runGit(task.WorktreePath, "add", "-A"); err != nil {
			return fail(fmt.Sprintf("stage task changes: %v: %s", err, compact(output+"\n"+stderr)))
		}
		_, _, diffErr := runGit(task.WorktreePath, "diff", "--cached", "--quiet")
		if diffErr != nil {
			message := "MCP DevDesk task " + task.ID
			if task.Title != "" {
				message += ": " + task.Title
			}
			if output, stderr, err := runGit(task.WorktreePath, "commit", "-m", message); err != nil {
				return fail(fmt.Sprintf("commit accepted task changes: %v: %s", err, compact(output+"\n"+stderr)))
			}
		}
		taskHead, _, err := runGit(task.WorktreePath, "rev-parse", "HEAD")
		if err != nil {
			return fail("resolve task HEAD: " + err.Error())
		}
		taskHead = strings.TrimSpace(taskHead)
		changed, err := gitDiffPaths(task.WorktreePath, task.BaseCommit, taskHead)
		if err != nil {
			return fail("resolve task changed files: " + err.Error())
		}
		task.ChangedFiles = changed
		baseDirty, err := gitDirtyPaths(task.BaseWorkspace)
		if err != nil {
			return fail("read base Git status: " + err.Error())
		}
		conflicts := conflictingTaskPaths(baseDirty, changed)
		if len(conflicts) > 0 {
			return fail("base working tree has local changes that overlap this task: " + strings.Join(conflicts, ", "))
		}
		if output, stderr, err := runGit(task.BaseWorkspace, "merge", "--ff-only", taskHead); err != nil {
			return fail(fmt.Sprintf("apply accepted task to base branch: %v: %s", err, compact(output+"\n"+stderr)))
		}
		if output, stderr, err := runGit(task.BaseWorkspace, "worktree", "remove", task.WorktreePath); err != nil {
			return fail(fmt.Sprintf("remove accepted task worktree: %v: %s", err, compact(output+"\n"+stderr)))
		}
		_, _, _ = runGit(task.BaseWorkspace, "branch", "-D", task.Branch)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "accepted"
		task.ResultCommit = taskHead
		task.CurrentStep = "修改已安全应用"
		task.NextStep = ""
		task.FailureReason = ""
		task.AcceptedAt = now
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		if state.ActiveTaskID == task.ID {
			state.ActiveTaskID = ""
		}
		result = *task
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) Reject(id string) (Task, error) {
	id = strings.TrimSpace(id)
	var result Task
	err := s.withLock(func(state *taskFile) error {
		task := findTask(state.Tasks, id)
		if task == nil {
			return errors.New("task not found")
		}
		if task.Status == "accepted" || task.Status == "rejected" {
			return fmt.Errorf("task is already %s", task.Status)
		}
		if _, err := os.Stat(task.WorktreePath); err == nil {
			if output, stderr, gitErr := runGit(task.BaseWorkspace, "worktree", "remove", "--force", task.WorktreePath); gitErr != nil {
				return fmt.Errorf("remove rejected task worktree: %w: %s", gitErr, compact(output+"\n"+stderr))
			}
		}
		_, _, _ = runGit(task.BaseWorkspace, "branch", "-D", task.Branch)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "rejected"
		task.CurrentStep = "任务已放弃"
		task.NextStep = ""
		task.RejectedAt = now
		task.LastHeartbeatAt = now
		task.UpdatedAt = now
		if state.ActiveTaskID == task.ID {
			state.ActiveTaskID = ""
		}
		result = *task
		return s.save(state)
	})
	return result, err
}

func (s *TaskStore) load() (taskFile, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return taskFile{Version: taskStoreVersion, Tasks: []Task{}}, nil
	}
	if err != nil {
		return taskFile{}, fmt.Errorf("read agent task state: %w", err)
	}
	var state taskFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return taskFile{}, fmt.Errorf("parse agent task state: %w", err)
	}
	if state.Version != 1 && state.Version != taskStoreVersion {
		return taskFile{}, fmt.Errorf("unsupported agent task state version %d", state.Version)
	}
	if state.Tasks == nil {
		state.Tasks = []Task{}
	}
	if state.Version == 1 {
		state.Version = taskStoreVersion
		for i := range state.Tasks {
			migrateTaskV1(&state.Tasks[i])
		}
	}
	return state, nil
}

func migrateTaskV1(task *Task) {
	if task == nil {
		return
	}
	if strings.TrimSpace(task.LastHeartbeatAt) == "" {
		task.LastHeartbeatAt = task.UpdatedAt
	}
	if strings.TrimSpace(task.CurrentStep) == "" {
		switch task.Status {
		case "review":
			task.CurrentStep = "等待人工验收"
			task.NextStep = "在 MCP DevDesk 中审查 Diff 并选择接受或放弃"
		case "accepted":
			task.CurrentStep = "修改已安全应用"
		case "rejected":
			task.CurrentStep = "任务已放弃"
		default:
			task.CurrentStep = "继续任务"
			task.NextStep = "完成修改并运行项目验证"
		}
	}
}

func (s *TaskStore) save(state *taskFile) error {
	state.Version = taskStoreVersion
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create agent task state directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".tasks-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() { _ = temp.Close(); _ = os.Remove(tempPath) }
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (s *TaskStore) withLock(fn func(*taskFile) error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	var lock *os.File
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			lock = file
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("lock agent task state: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("agent task state is busy; retry shortly")
		}
		time.Sleep(40 * time.Millisecond)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	state, err := s.load()
	if err != nil {
		return err
	}
	return fn(&state)
}

func findTask(tasks []Task, id string) *Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}

func appendBoundedUniqueID(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	result := make([]string, 0, len(values)+1)
	for _, current := range values {
		if current != value {
			result = append(result, current)
		}
	}
	result = append(result, value)
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result
}

func gitDirtyPaths(workspace string) ([]string, error) {
	status, stderr, err := runGit(workspace, "-c", "core.quotepath=false", "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, compact(stderr))
	}
	return porcelainPaths(status), nil
}

func gitDiffPaths(workspace, baseCommit, headCommit string) ([]string, error) {
	output, stderr, err := runGit(workspace, "-c", "core.quotepath=false", "diff", "--name-only", baseCommit+".."+headCommit)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, compact(stderr))
	}
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		path := normalizeTaskPath(line)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func porcelainPaths(output string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if index := strings.LastIndex(path, " -> "); index >= 0 {
			path = path[index+4:]
		}
		path = normalizeTaskPath(path)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func normalizeTaskPath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"")
	value = filepath.ToSlash(filepath.Clean(value))
	value = strings.TrimPrefix(value, "./")
	if value == "." || value == "" {
		return ""
	}
	return value
}

func conflictingTaskPaths(baseDirty, taskChanged []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, dirty := range baseDirty {
		for _, changed := range taskChanged {
			if !taskPathOverlap(dirty, changed) {
				continue
			}
			key := strings.ToLower(normalizeTaskPath(dirty))
			if _, exists := seen[key]; exists {
				break
			}
			seen[key] = struct{}{}
			result = append(result, normalizeTaskPath(dirty))
			break
		}
	}
	sort.Strings(result)
	return result
}

func taskPathOverlap(left, right string) bool {
	left = strings.ToLower(normalizeTaskPath(left))
	right = strings.ToLower(normalizeTaskPath(right))
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func cleanExistingDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("workspace is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", errors.New("workspace directory is unavailable")
	}
	return absolute, nil
}

func samePath(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func newID(prefix string, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func runGit(cwd string, args ...string) (string, string, error) {
	command := exec.Command("git", args...)
	configureBackgroundCommand(command)
	command.Dir = cwd
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return stdout.String(), stderr.String(), fmt.Errorf("git %s exited with code %d", strings.Join(args, " "), exit.ExitCode())
	}
	return stdout.String(), stderr.String(), err
}

func compact(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r", ""))
	if len(value) > 2000 {
		return value[:2000] + "..."
	}
	return value
}
