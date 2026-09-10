package mcpcore

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"mcp-devdesk/internal/agentstate"
)

type taskStartArgs struct {
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

type taskIDArgs struct {
	TaskID      string `json:"taskId"`
	TaskIDSnake string `json:"task_id,omitempty"`
}
type taskFinishArgs struct {
	TaskID      string `json:"taskId,omitempty"`
	TaskIDSnake string `json:"task_id,omitempty"`
	Summary     string `json:"summary,omitempty"`
}
type taskDiffArgs struct {
	TaskID      string `json:"taskId"`
	TaskIDSnake string `json:"task_id,omitempty"`
	MaxBytes    int    `json:"maxBytes,omitempty"`
}

func taskTools() []Tool {
	return []Tool{
		{
			Name: "task_start", Title: "Start Isolated AI Task",
			Description: "Create a persistent Task ID and a clean isolated Git worktree. After this succeeds, normal file, Git, and command tools automatically operate inside the task worktree until the task is accepted or rejected locally.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"title":   map[string]any{"type": "string", "minLength": 1, "maxLength": 160},
				"summary": map[string]any{"type": "string", "maxLength": 4000},
			}, "required": []string{"title"}, "additionalProperties": false},
		},
		{
			Name: "task_list", Title: "List Persistent AI Tasks",
			Description: "List persistent task records and the active Task ID. Task state survives MCP client disconnects and core restarts.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			Name: "task_get", Title: "Get AI Task",
			Description: "Return one persistent task record by Task ID.",
			InputSchema: taskIDSchema(),
		},
		{
			Name: "task_resume", Title: "Resume AI Task",
			Description: "Resume an existing isolated task and make its worktree the active workspace. A task awaiting human review is moved back to editing.",
			InputSchema: taskIDSchema(),
		},
		{
			Name: "task_diff", Title: "Review AI Task Diff",
			Description: "Return a bounded Git diff from an isolated task worktree without accepting it.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"taskId":   map[string]any{"type": "string", "minLength": 1},
				"task_id":  map[string]any{"type": "string", "minLength": 1},
				"maxBytes": map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576, "default": 262144},
			}, "anyOf": []any{map[string]any{"required": []string{"taskId"}}, map[string]any{"required": []string{"task_id"}}}, "additionalProperties": false},
		},
		{
			Name: "task_finish", Title: "Finish AI Task for Human Review",
			Description: "Mark the active isolated task ready for human review. This does not merge, accept, or delete anything. Acceptance/rejection is intentionally available only from the local MCP DevDesk management UI.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"taskId":  map[string]any{"type": "string"},
				"task_id": map[string]any{"type": "string"},
				"summary": map[string]any{"type": "string", "maxLength": 8000},
			}, "additionalProperties": false},
		},
	}
}

func taskIDSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"taskId":  map[string]any{"type": "string", "minLength": 1},
		"task_id": map[string]any{"type": "string", "minLength": 1},
	}, "anyOf": []any{map[string]any{"required": []string{"taskId"}}, map[string]any{"required": []string{"task_id"}}}, "additionalProperties": false}
}

func (s *Server) executeTaskTool(name string, arguments map[string]any) (map[string]any, error) {
	if s.tasks == nil {
		return nil, errors.New("agent task persistence is unavailable")
	}
	switch name {
	case "task_start":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args taskStartArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		task, err := s.tasks.Start(s.workspace, args.Title, args.Summary)
		if err != nil {
			return nil, err
		}
		s.setDefaultCWDDirect(task.WorktreePath)
		return taskResult(task, true), nil
	case "task_list":
		tasks, active, err := s.tasks.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": tasks, "count": len(tasks), "activeTaskId": active, "persistent": true}, nil
	case "task_get":
		var args taskIDArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		id := firstNonEmpty(args.TaskID, args.TaskIDSnake)
		task, err := s.tasks.Get(id)
		if err != nil {
			return nil, err
		}
		active, ok, _ := s.tasks.Active()
		return taskResult(task, ok && active.ID == task.ID), nil
	case "task_resume":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args taskIDArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		task, err := s.tasks.Resume(firstNonEmpty(args.TaskID, args.TaskIDSnake))
		if err != nil {
			return nil, err
		}
		s.setDefaultCWDDirect(task.WorktreePath)
		return taskResult(task, true), nil
	case "task_diff":
		var args taskDiffArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		task, err := s.tasks.Get(firstNonEmpty(args.TaskID, args.TaskIDSnake))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(task.WorktreePath); err != nil {
			return nil, errors.New("task worktree is unavailable")
		}
		limit := args.MaxBytes
		if limit <= 0 {
			limit = 262144
		}
		if limit > maxGitOutputBytes {
			limit = maxGitOutputBytes
		}
		tracked, trackedTruncated, err := runGit(task.WorktreePath, limit, "diff", "--no-ext-diff", "--no-color", "HEAD")
		if err != nil {
			return nil, err
		}
		untracked, _, _ := runGit(task.WorktreePath, 64*1024, "ls-files", "--others", "--exclude-standard")
		return map[string]any{"task": task, "diff": tracked, "untracked": strings.Fields(strings.TrimSpace(untracked)), "returnedBytes": len(tracked), "outputLimitBytes": limit, "truncated": trackedTruncated}, nil
	case "task_finish":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args taskFinishArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		task, err := s.tasks.Finish(firstNonEmpty(args.TaskID, args.TaskIDSnake), args.Summary)
		if err != nil {
			return nil, err
		}
		return map[string]any{"task": task, "humanReviewRequired": true, "acceptanceAvailableIn": "MCP DevDesk local UI", "message": "Task is ready for human review. The MCP client cannot self-accept or self-reject it."}, nil
	default:
		return nil, fmt.Errorf("unknown task tool: %s", name)
	}
}

func taskResult(task agentstate.Task, active bool) map[string]any {
	return map[string]any{"task": task, "active": active, "taskId": task.ID, "workspace": task.WorktreePath, "persistent": true, "isolated": true}
}

func (s *Server) setDefaultCWDDirect(path string) {
	s.cwdMu.Lock()
	s.defaultCWD = path
	s.cwdMu.Unlock()
}

func (s *Server) activeTaskID() string {
	if s.tasks == nil {
		return ""
	}
	task, ok, err := s.tasks.Active()
	if err != nil || !ok {
		return ""
	}
	return task.ID
}

func (s *Server) requireTaskEditable() error {
	if s.tasks == nil {
		return nil
	}
	task, ok, err := s.tasks.Active()
	if err != nil || !ok {
		return nil
	}
	if task.Status == "review" {
		return fmt.Errorf("task %s is awaiting human review; call task_resume only after the user requests more changes", task.ID)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
