package projecttools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const maxOutput = 512 * 1024

type Details struct {
	Path         string     `json:"path"`
	Git          bool       `json:"git"`
	Branch       string     `json:"branch,omitempty"`
	ChangedFiles int        `json:"changedFiles"`
	Ahead        int        `json:"ahead"`
	Behind       int        `json:"behind"`
	HasAgents    bool       `json:"hasAgents"`
	AgentsPath   string     `json:"agentsPath,omitempty"`
	Skills       []string   `json:"skills"`
	Worktrees    []Worktree `json:"worktrees"`
}

type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch,omitempty"`
	Bare     bool   `json:"bare"`
	Detached bool   `json:"detached"`
}

type Diff struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func Inspect(path string) (Details, error) {
	d := Details{Path: path, Skills: []string{}, Worktrees: []Worktree{}}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return d, errors.New("project directory is unavailable")
	}
	if agents := filepath.Join(path, "AGENTS.md"); fileExists(agents) {
		d.HasAgents, d.AgentsPath = true, agents
	}
	d.Skills = findSkills(path)
	root, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return d, nil
	}
	d.Git = true
	root = strings.TrimSpace(root)
	if branch, err := git(root, "branch", "--show-current"); err == nil {
		d.Branch = strings.TrimSpace(branch)
	}
	if status, err := git(root, "status", "--porcelain=v1"); err == nil {
		d.ChangedFiles = countNonEmptyLines(status)
	}
	if counts, err := git(root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		fields := strings.Fields(counts)
		if len(fields) == 2 {
			d.Behind, _ = strconv.Atoi(fields[0])
			d.Ahead, _ = strconv.Atoi(fields[1])
		}
	}
	d.Worktrees, _ = ListWorktrees(root)
	return d, nil
}

func GetDiff(path string) (Diff, error) {
	out, err := gitBytes(path, "diff", "--no-ext-diff", "--no-color")
	if err != nil {
		return Diff{}, err
	}
	truncated := len(out) > maxOutput
	if truncated {
		out = out[:maxOutput]
	}
	return Diff{Text: string(out), Truncated: truncated}, nil
}

func ListWorktrees(path string) ([]Worktree, error) {
	out, err := git(path, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var result []Worktree
	var current *Worktree
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			result = append(result, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
			current = &result[len(result)-1]
		case current != nil && strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case current != nil && strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case current != nil && line == "bare":
			current.Bare = true
		case current != nil && line == "detached":
			current.Detached = true
		}
	}
	return result, scanner.Err()
}

func CreateWorktree(projectPath, targetPath, branch, base string) error {
	if strings.TrimSpace(targetPath) == "" || strings.TrimSpace(branch) == "" {
		return errors.New("target path and branch are required")
	}
	if base == "" {
		base = "HEAD"
	}
	_, err := git(projectPath, "worktree", "add", "-b", branch, targetPath, base)
	return err
}

func RemoveWorktree(projectPath, targetPath string) error {
	if strings.TrimSpace(targetPath) == "" {
		return errors.New("worktree path is required")
	}
	_, err := git(projectPath, "worktree", "remove", targetPath)
	return err
}

func findSkills(root string) []string {
	var found []string
	for _, base := range []string{filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".codex", "skills"), filepath.Join(root, "skills")} {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && fileExists(filepath.Join(base, entry.Name(), "SKILL.md")) {
				found = append(found, entry.Name())
			}
		}
	}
	return found
}

func git(path string, args ...string) (string, error) {
	out, err := gitBytes(path, args...)
	return string(out), err
}
func gitBytes(path string, args ...string) ([]byte, error) {
	all := append([]string{"-C", path}, args...)
	cmd := exec.Command("git", all...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
func fileExists(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }
func countNonEmptyLines(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
