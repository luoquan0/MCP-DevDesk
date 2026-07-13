package projecttools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxOutput = 512 * 1024

type Details struct {
	Path          string     `json:"path"`
	Git           bool       `json:"git"`
	Branch        string     `json:"branch,omitempty"`
	CurrentCommit string     `json:"currentCommit,omitempty"`
	CurrentShort  string     `json:"currentShort,omitempty"`
	ChangedFiles  int        `json:"changedFiles"`
	Ahead         int        `json:"ahead"`
	Behind        int        `json:"behind"`
	HasAgents     bool       `json:"hasAgents"`
	AgentsPath    string     `json:"agentsPath,omitempty"`
	Skills        []string   `json:"skills"`
	Worktrees     []Worktree `json:"worktrees"`
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

type Commit struct {
	Hash        string   `json:"hash"`
	ShortHash   string   `json:"shortHash"`
	Author      string   `json:"author"`
	AuthorEmail string   `json:"authorEmail,omitempty"`
	Timestamp   string   `json:"timestamp"`
	Subject     string   `json:"subject"`
	Decorations []string `json:"decorations"`
	Current     bool     `json:"current"`
}

type History struct {
	Branch        string   `json:"branch,omitempty"`
	CurrentCommit string   `json:"currentCommit,omitempty"`
	CurrentShort  string   `json:"currentShort,omitempty"`
	Commits       []Commit `json:"commits"`
	Truncated     bool     `json:"truncated"`
}

type RollbackResult struct {
	PreviousCommit string `json:"previousCommit"`
	CurrentCommit  string `json:"currentCommit"`
	BackupBranch   string `json:"backupBranch,omitempty"`
}

var commitHashPattern = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)

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
	if head, err := git(root, "rev-parse", "--verify", "HEAD"); err == nil {
		d.CurrentCommit = strings.TrimSpace(head)
		if short, shortErr := git(root, "rev-parse", "--short=12", "HEAD"); shortErr == nil {
			d.CurrentShort = strings.TrimSpace(short)
		}
	}
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
	if _, err := git(path, "rev-parse", "--show-toplevel"); err != nil {
		return Diff{}, nil
	}
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

func GetHistory(path string, limit int) (History, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	result := History{Commits: []Commit{}}
	root, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return result, errors.New("当前项目不是 Git 仓库")
	}
	root = strings.TrimSpace(root)
	if branch, branchErr := git(root, "branch", "--show-current"); branchErr == nil {
		result.Branch = strings.TrimSpace(branch)
	}
	head, err := git(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return result, nil
	}
	result.CurrentCommit = strings.TrimSpace(head)
	if short, shortErr := git(root, "rev-parse", "--short=12", "HEAD"); shortErr == nil {
		result.CurrentShort = strings.TrimSpace(short)
	}

	format := "%H%x1f%h%x1f%an%x1f%ae%x1f%aI%x1f%s%x1f%D%x1e"
	out, err := gitBytes(root, "log", "-n", strconv.Itoa(limit+1), "--decorate=short", "--date=iso-strict", "--format="+format, "HEAD")
	if err != nil {
		return History{}, err
	}
	for _, raw := range bytes.Split(out, []byte{0x1e}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		fields := bytes.Split(raw, []byte{0x1f})
		if len(fields) < 7 {
			continue
		}
		commit := Commit{
			Hash:        strings.TrimSpace(string(fields[0])),
			ShortHash:   strings.TrimSpace(string(fields[1])),
			Author:      strings.TrimSpace(string(fields[2])),
			AuthorEmail: strings.TrimSpace(string(fields[3])),
			Timestamp:   strings.TrimSpace(string(fields[4])),
			Subject:     strings.TrimSpace(string(fields[5])),
			Decorations: parseDecorations(string(fields[6])),
		}
		commit.Current = commit.Hash == result.CurrentCommit
		result.Commits = append(result.Commits, commit)
	}
	if len(result.Commits) > limit {
		result.Truncated = true
		result.Commits = result.Commits[:limit]
	}
	return result, nil
}

func Rollback(path, commit string) (RollbackResult, error) {
	commit = strings.TrimSpace(commit)
	if !commitHashPattern.MatchString(commit) {
		return RollbackResult{}, errors.New("Git 提交 ID 格式无效")
	}
	root, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return RollbackResult{}, errors.New("当前项目不是 Git 仓库")
	}
	root = strings.TrimSpace(root)
	branch, err := git(root, "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) == "" {
		return RollbackResult{}, errors.New("HEAD 处于分离状态，无法执行回档")
	}
	status, err := git(root, "status", "--porcelain=v1")
	if err != nil {
		return RollbackResult{}, err
	}
	if strings.TrimSpace(status) != "" {
		return RollbackResult{}, errors.New("工作区存在未提交修改，请先提交或暂存后再回档")
	}
	previous, err := git(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return RollbackResult{}, errors.New("当前仓库还没有提交记录")
	}
	previous = strings.TrimSpace(previous)
	resolved, err := git(root, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return RollbackResult{}, errors.New("找不到指定的 Git 提交 ID")
	}
	resolved = strings.TrimSpace(resolved)
	if previous == resolved {
		return RollbackResult{PreviousCommit: previous, CurrentCommit: resolved}, nil
	}
	if err := gitIsAncestor(root, resolved, previous); err != nil {
		return RollbackResult{}, err
	}

	backup := uniqueBackupBranch(root, previous)
	if _, err := git(root, "branch", backup, previous); err != nil {
		return RollbackResult{}, fmt.Errorf("创建回档备份分支失败: %w", err)
	}
	if _, err := git(root, "reset", "--hard", resolved); err != nil {
		return RollbackResult{}, fmt.Errorf("回档失败，原版本已保存在备份分支 %s: %w", backup, err)
	}
	return RollbackResult{PreviousCommit: previous, CurrentCommit: resolved, BackupBranch: backup}, nil
}

func parseDecorations(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "HEAD -> ")
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func gitIsAncestor(root, target, head string) error {
	cmd := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", target, head)
	configureCommand(cmd)
	if err := cmd.Run(); err != nil {
		return errors.New("所选提交不在当前分支历史中")
	}
	return nil
}

func uniqueBackupBranch(root, previous string) string {
	short := previous
	if len(short) > 12 {
		short = short[:12]
	}
	base := "mcp-devdesk-backup-" + time.Now().Format("20060102-150405") + "-" + short
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate += "-" + strconv.Itoa(suffix)
		}
		cmd := exec.Command("git", "-C", root, "show-ref", "--verify", "--quiet", "refs/heads/"+candidate)
		configureCommand(cmd)
		if cmd.Run() != nil {
			return candidate
		}
	}
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
	configureCommand(cmd)
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
