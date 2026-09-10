package mcpcore

import (
	"errors"
	"fmt"
	"strings"
)

type jobListArgs struct {
	Limit       int    `json:"limit,omitempty"`
	TaskID      string `json:"taskId,omitempty"`
	TaskIDSnake string `json:"task_id,omitempty"`
}
type jobGetArgs struct {
	JobID      string `json:"jobId"`
	JobIDSnake string `json:"job_id,omitempty"`
}

func jobTools() []Tool {
	return []Tool{
		{Name: "job_list", Title: "List Persistent Jobs", Description: "List persisted command/check Job IDs. Completed and interrupted job records survive MCP reconnects and core restarts.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "taskId": map[string]any{"type": "string"}, "task_id": map[string]any{"type": "string"}}, "additionalProperties": false}},
		{Name: "job_get", Title: "Get Persistent Job", Description: "Return one persisted command/check job by Job ID, including its bounded retained output.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"jobId": map[string]any{"type": "string", "minLength": 1}, "job_id": map[string]any{"type": "string", "minLength": 1}}, "anyOf": []any{map[string]any{"required": []string{"jobId"}}, map[string]any{"required": []string{"job_id"}}}, "additionalProperties": false}},
	}
}

func (s *Server) executeJobTool(name string, arguments map[string]any) (map[string]any, error) {
	if s.jobs == nil {
		return nil, errors.New("persistent job history is unavailable")
	}
	switch name {
	case "job_list":
		var args jobListArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		jobs, err := s.jobs.List(args.Limit, firstNonEmpty(args.TaskID, args.TaskIDSnake))
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": jobs, "count": len(jobs), "persistent": true}, nil
	case "job_get":
		var args jobGetArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		job, err := s.jobs.Get(firstNonEmpty(args.JobID, args.JobIDSnake))
		if err != nil {
			return nil, err
		}
		return map[string]any{"job": job, "jobId": job.ID, "persistent": true}, nil
	default:
		return nil, fmt.Errorf("unknown job tool: %s", strings.TrimSpace(name))
	}
}
