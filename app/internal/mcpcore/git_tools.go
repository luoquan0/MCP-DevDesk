package mcpcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxGitOutputBytes = 1024 * 1024

type gitStatusArgs struct {
	Path string `json:"path,omitempty"`
}

type gitDiffArgs struct {
	Path          string   `json:"path,omitempty"`
	Paths         []string `json:"paths,omitempty"`
	Staged        bool     `json:"staged,omitempty"`
	Unstaged      bool     `json:"unstaged,omitempty"`
	ContextLines  int      `json:"contextLines,omitempty"`
	ContextSnake  int      `json:"context_lines,omitempty"`
	MaxBytes      int      `json:"maxBytes,omitempty"`
	MaxBytesSnake int      `json:"max_bytes,omitempty"`
}

type gitLogArgs struct {
	Path          string `json:"path,omitempty"`
	MaxCount      int    `json:"maxCount,omitempty"`
	MaxCountSnake int    `json:"max_count,omitempty"`
	Skip          int    `json:"skip,omitempty"`
	Ref           string `json:"ref,omitempty"`
}

type gitShowArgs struct {
	Path          string `json:"path,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Rev           string `json:"rev,omitempty"`
	IncludeDiff   *bool  `json:"includeDiff,omitempty"`
	IncludeSnake  *bool  `json:"include_diff,omitempty"`
	MaxBytes      int    `json:"maxBytes,omitempty"`
	MaxBytesSnake int    `json:"max_bytes,omitempty"`
}

type gitWorktreesArgs struct {
	Path string `json:"path,omitempty"`
}

func gitTools() []Tool {
	return []Tool{
		{
			Name:        "git_status",
			Title:       "Git Status",
			Description: "Return branch and porcelain status for a Git repository inside the workspace.",
			InputSchema: pathOnlySchema(),
		},
		{
			Name:        "git_diff",
			Title:       "Git Diff",
			Description: "Return a bounded working tree or staged Git diff.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":          map[string]any{"type": "string", "default": "."},
					"paths":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 100},
					"staged":        map[string]any{"type": "boolean", "default": false},
					"unstaged":      map[string]any{"type": "boolean", "default": true},
					"contextLines":  map[string]any{"type": "integer", "minimum": 0, "maximum": 20, "default": 3},
					"context_lines": map[string]any{"type": "integer", "minimum": 0, "maximum": 20, "description": "Legacy alias for contextLines."},
					"maxBytes":      map[string]any{"type": "integer", "minimum": 1, "maximum": maxGitOutputBytes, "default": 262144},
					"max_bytes":     map[string]any{"type": "integer", "minimum": 1, "maximum": maxGitOutputBytes, "description": "Legacy alias for maxBytes."},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "git_log",
			Title:       "Git Log",
			Description: "Return recent Git commits as structured records.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "default": "."},
					"maxCount":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20},
					"max_count": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "Legacy alias for maxCount."},
					"skip":      map[string]any{"type": "integer", "minimum": 0, "maximum": 10000, "default": 0},
					"ref":       map[string]any{"type": "string", "default": "HEAD"},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "git_show",
			Title:       "Git Show",
			Description: "Return bounded commit metadata and patch text for a revision.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":         map[string]any{"type": "string", "default": "."},
					"revision":     map[string]any{"type": "string", "default": "HEAD"},
					"rev":          map[string]any{"type": "string", "description": "Legacy alias for revision."},
					"includeDiff":  map[string]any{"type": "boolean", "default": true},
					"include_diff": map[string]any{"type": "boolean", "description": "Legacy alias for includeDiff."},
					"maxBytes":     map[string]any{"type": "integer", "minimum": 1, "maximum": maxGitOutputBytes, "default": 262144},
					"max_bytes":    map[string]any{"type": "integer", "minimum": 1, "maximum": maxGitOutputBytes, "description": "Legacy alias for maxBytes."},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "git_worktrees",
			Title:       "Git Worktrees",
			Description: "List Git worktrees for a repository inside the workspace.",
			InputSchema: pathOnlySchema(),
		},
	}
}

func pathOnlySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"path": map[string]any{"type": "string", "default": "."}},
		"additionalProperties": false,
	}
}

func (s *Server) executeGitTool(name string, arguments map[string]any) (map[string]any, error) {
	switch name {
	case "git_status":
		var args gitStatusArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		cwd, relative, err := s.gitCWD(args.Path)
		if err != nil {
			return nil, err
		}
		output, _, err := runGit(cwd, maxGitOutputBytes, "status", "--porcelain=v2", "--branch")
		if err != nil {
			return nil, err
		}
		clean := true
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				clean = false
				break
			}
		}
		return map[string]any{"path": relative, "porcelain": output, "clean": clean}, nil
	case "git_diff":
		var args gitDiffArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		cwd, relative, err := s.gitCWD(args.Path)
		if err != nil {
			return nil, err
		}
		if args.MaxBytes <= 0 {
			args.MaxBytes = args.MaxBytesSnake
		}
		if args.ContextLines <= 0 {
			args.ContextLines = args.ContextSnake
		}
		if args.ContextLines < 0 || args.ContextLines > 20 {
			return nil, errors.New("contextLines must be between 0 and 20")
		}
		if args.ContextLines == 0 {
			args.ContextLines = 3
		}
		if !args.Staged && !args.Unstaged {
			args.Unstaged = true
		}
		paths, err := sanitizeGitPaths(args.Paths)
		if err != nil {
			return nil, err
		}
		limit := boundedGitLimit(args.MaxBytes)
		parts := make([]string, 0, 2)
		truncated := false
		run := func(staged bool) error {
			gitArgs := []string{"diff", "--no-ext-diff", "--no-color", "--unified=" + strconv.Itoa(args.ContextLines)}
			if staged {
				gitArgs = append(gitArgs, "--cached")
			}
			if len(paths) > 0 {
				gitArgs = append(gitArgs, "--")
				gitArgs = append(gitArgs, paths...)
			}
			output, wasTruncated, runErr := runGit(cwd, limit, gitArgs...)
			if runErr != nil {
				return runErr
			}
			if staged && args.Unstaged {
				output = "## staged\n" + output
			} else if !staged && args.Staged {
				output = "## unstaged\n" + output
			}
			parts = append(parts, output)
			truncated = truncated || wasTruncated
			return nil
		}
		if args.Staged {
			if err := run(true); err != nil {
				return nil, err
			}
		}
		if args.Unstaged {
			if err := run(false); err != nil {
				return nil, err
			}
		}
		return map[string]any{"path": relative, "staged": args.Staged, "unstaged": args.Unstaged, "paths": paths, "diff": strings.Join(parts, "\n"), "truncated": truncated}, nil
	case "git_log":
		var args gitLogArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		cwd, relative, err := s.gitCWD(args.Path)
		if err != nil {
			return nil, err
		}
		if args.MaxCount <= 0 {
			args.MaxCount = args.MaxCountSnake
		}
		if args.MaxCount <= 0 {
			args.MaxCount = 20
		}
		if args.MaxCount > 100 {
			args.MaxCount = 100
		}
		if args.Skip < 0 || args.Skip > 10000 {
			return nil, errors.New("skip must be between 0 and 10000")
		}
		ref := strings.TrimSpace(args.Ref)
		if ref == "" {
			ref = "HEAD"
		}
		if strings.HasPrefix(ref, "-") {
			return nil, errors.New("ref must not begin with a dash")
		}
		format := "%H%x1f%h%x1f%an%x1f%ae%x1f%aI%x1f%s%x1e"
		logArgs := []string{"log", "-n", strconv.Itoa(args.MaxCount), "--skip", strconv.Itoa(args.Skip), "--format=" + format, ref}
		output, _, err := runGit(cwd, maxGitOutputBytes, logArgs...)
		if err != nil {
			return nil, err
		}
		commits := make([]map[string]any, 0)
		for _, record := range strings.Split(output, "\x1e") {
			record = strings.TrimSpace(record)
			if record == "" {
				continue
			}
			fields := strings.Split(record, "\x1f")
			if len(fields) != 6 {
				continue
			}
			commits = append(commits, map[string]any{
				"hash": fields[0], "shortHash": fields[1], "author": fields[2],
				"authorEmail": fields[3], "authorDate": fields[4], "subject": fields[5],
			})
		}
		return map[string]any{"path": relative, "ref": ref, "skip": args.Skip, "commits": commits, "count": len(commits)}, nil
	case "git_show":
		var args gitShowArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		cwd, relative, err := s.gitCWD(args.Path)
		if err != nil {
			return nil, err
		}
		revision := strings.TrimSpace(args.Revision)
		if revision == "" {
			revision = strings.TrimSpace(args.Rev)
		}
		if revision == "" {
			revision = "HEAD"
		}
		if strings.HasPrefix(revision, "-") {
			return nil, errors.New("revision must not begin with a dash")
		}
		if args.MaxBytes <= 0 {
			args.MaxBytes = args.MaxBytesSnake
		}
		includeDiff := true
		if args.IncludeDiff != nil {
			includeDiff = *args.IncludeDiff
		} else if args.IncludeSnake != nil {
			includeDiff = *args.IncludeSnake
		}
		showArgs := []string{"show", "--no-ext-diff", "--no-color", "--format=fuller"}
		if !includeDiff {
			showArgs = append(showArgs, "--no-patch")
		}
		showArgs = append(showArgs, revision)
		output, truncated, err := runGit(cwd, boundedGitLimit(args.MaxBytes), showArgs...)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": relative, "revision": revision, "includeDiff": includeDiff, "content": output, "truncated": truncated}, nil
	case "git_worktrees":
		var args gitWorktreesArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		cwd, relative, err := s.gitCWD(args.Path)
		if err != nil {
			return nil, err
		}
		output, _, err := runGit(cwd, maxGitOutputBytes, "worktree", "list", "--porcelain")
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": relative, "porcelain": output}, nil
	default:
		return nil, fmt.Errorf("unknown Git tool: %s", name)
	}
}

func (s *Server) gitCWD(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		value = "."
	}
	_, cwd, relative, err := s.resolveWorkspacePath(value)
	if err != nil {
		return "", "", err
	}
	if _, _, err := runGit(cwd, 4096, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", "", errors.New("path is not inside a Git working tree")
	}
	return cwd, relative, nil
}

func runGit(cwd string, maxBytes int, args ...string) (string, bool, error) {
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	configureCommand(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", false, errors.New("Git command timed out")
	}
	text := output.String()
	if err != nil {
		return "", false, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(text))
	}
	truncated := false
	if len(text) > maxBytes {
		text = text[:maxBytes]
		truncated = true
	}
	return text, truncated, nil
}

func boundedGitLimit(value int) int {
	if value <= 0 {
		return 256 * 1024
	}
	if value > maxGitOutputBytes {
		return maxGitOutputBytes
	}
	return value
}

func sanitizeGitPaths(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if filepath.IsAbs(value) || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "-") {
			return nil, fmt.Errorf("invalid Git path filter: %s", value)
		}
		result = append(result, value)
	}
	return result, nil
}

func contextWithTimeout(duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), duration)
}
