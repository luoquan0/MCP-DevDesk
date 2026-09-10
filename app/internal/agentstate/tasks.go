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

const taskStoreVersion = 1

type Task struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Summary       string `json:"summary,omitempty"`
	Status        string `json:"status"`
	BaseWorkspace string `json:"baseWorkspace"`
	WorktreePath  string `json:"worktreePath"`
	Branch        string `json:"branch"`
	BaseCommit    string `json:"baseCommit"`
	ResultCommit  string `json:"resultCommit,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	FinishedAt    string `json:"finishedAt,omitempty"`
	AcceptedAt    string `json:"acceptedAt,omitempty"`
	RejectedAt    string `json:"rejectedAt,omitempty"`
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
	if status, _, err := runGit(workspace, "status", "--porcelain"); err != nil {
		return Task{}, fmt.Errorf("read Git status: %w", err)
	} else if strings.TrimSpace(status) != "" {
		return Task{}, errors.New("task isolation requires a clean base working tree; commit or stash existing changes first")
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
	task := Task{ID: id, Title: title, Summary: summary, Status: "editing", BaseWorkspace: workspace, WorktreePath: worktree, Branch: branch, BaseCommit: baseCommit, CreatedAt: now, UpdatedAt: now}

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
		task.Status = "editing"
		task.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		state.ActiveTaskID = task.ID
		result = *task
		return s.save(state)
	})
	return result, err
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
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "review"
		if summary != "" {
			task.Summary = summary
		}
		task.FinishedAt = now
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
		if _, err := cleanExistingDir(task.BaseWorkspace); err != nil {
			return err
		}
		if _, err := cleanExistingDir(task.WorktreePath); err != nil {
			return err
		}
		baseStatus, _, err := runGit(task.BaseWorkspace, "status", "--porcelain")
		if err != nil {
			return fmt.Errorf("read base Git status: %w", err)
		}
		if strings.TrimSpace(baseStatus) != "" {
			return errors.New("base working tree changed while the task was isolated; commit or stash those changes before accepting")
		}
		baseHead, _, err := runGit(task.BaseWorkspace, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("resolve base HEAD: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(baseHead), task.BaseCommit) {
			return errors.New("base branch advanced while the task was isolated; rebase or create a new task before accepting")
		}
		if output, stderr, err := runGit(task.WorktreePath, "add", "-A"); err != nil {
			return fmt.Errorf("stage task changes: %w: %s", err, compact(output+"\n"+stderr))
		}
		_, _, diffErr := runGit(task.WorktreePath, "diff", "--cached", "--quiet")
		if diffErr != nil {
			message := "MCP DevDesk task " + task.ID
			if task.Title != "" {
				message += ": " + task.Title
			}
			if output, stderr, err := runGit(task.WorktreePath, "commit", "-m", message); err != nil {
				return fmt.Errorf("commit accepted task changes: %w: %s", err, compact(output+"\n"+stderr))
			}
		}
		taskHead, _, err := runGit(task.WorktreePath, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("resolve task HEAD: %w", err)
		}
		taskHead = strings.TrimSpace(taskHead)
		if output, stderr, err := runGit(task.BaseWorkspace, "merge", "--ff-only", taskHead); err != nil {
			return fmt.Errorf("apply accepted task to base branch: %w: %s", err, compact(output+"\n"+stderr))
		}
		if output, stderr, err := runGit(task.BaseWorkspace, "worktree", "remove", task.WorktreePath); err != nil {
			return fmt.Errorf("remove accepted task worktree: %w: %s", err, compact(output+"\n"+stderr))
		}
		_, _, _ = runGit(task.BaseWorkspace, "branch", "-D", task.Branch)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		task.Status = "accepted"
		task.ResultCommit = taskHead
		task.AcceptedAt = now
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
		task.RejectedAt = now
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
	if state.Version != taskStoreVersion {
		return taskFile{}, fmt.Errorf("unsupported agent task state version %d", state.Version)
	}
	if state.Tasks == nil {
		state.Tasks = []Task{}
	}
	return state, nil
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
